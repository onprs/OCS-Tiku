package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/onprs/OCS-Tiku/internal/catalog"
)

var (
	answerPattern = regexp.MustCompile(`(?is)<answer>\s*(.*?)\s*</answer>`)
	tagPattern    = regexp.MustCompile(`<[^>]+>`)
	spacePattern  = regexp.MustCompile(`\s+`)
	optionLabel   = regexp.MustCompile(`^[A-Za-z][.、)）:：\s]+`)
)

// Solver 完成一次答题：图片处理、按题型引导模型调用、答案校验与异常重试。
type Solver struct {
	config     *Config
	models     *ModelClient
	images     *ImageProcessor
	catalog    *catalog.Store
	store      *Store
	retryDelay func(attempt int) time.Duration
}

func cleanTitle(text string) string {
	return strings.TrimSpace(spacePattern.ReplaceAllString(html.UnescapeString(tagPattern.ReplaceAllString(text, "")), " "))
}

// splitOptions 按选项前缀分组，保留同一选项的续行与图片地址。
func splitOptions(options string) []string {
	lines := strings.Split(options, "\n")
	hasLabel := false
	for _, line := range lines {
		if optionLabel.MatchString(strings.TrimSpace(line)) {
			hasLabel = true
			break
		}
	}
	groups := []string{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !hasLabel || optionLabel.MatchString(line) || len(groups) == 0 {
			groups = append(groups, line)
		} else {
			groups[len(groups)-1] += " " + line
		}
	}
	return groups
}

func optionName(index int) string {
	if index < 26 {
		return string(rune('A' + index))
	}
	return fmt.Sprintf("%d", index+1)
}

func labelOptions(options string) string {
	groups := splitOptions(options)
	result := make([]string, 0, len(groups))
	for i, group := range groups {
		result = append(result, optionName(i)+". "+strings.TrimSpace(optionLabel.ReplaceAllString(group, "")))
	}
	return strings.Join(result, "\n")
}

// truncateText 按字节上限截断文本，不切断多字节字符。
func truncateText(text string, maxBytes int) string {
	if len(text) <= maxBytes {
		return text
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

// shouldIncludeOptions 决定是否向模型展示带字母编号的选项列表。
// 填空题不需要选项；判断题不展示 A./B. 编号，防止诱导模型返回选项字母。
func shouldIncludeOptions(effectiveKind, options string) bool {
	if effectiveKind == "completion" || effectiveKind == "judgement" {
		return false
	}
	return strings.TrimSpace(options) != ""
}

func effectiveOptionCount(effectiveKind, options string) int {
	if !shouldIncludeOptions(effectiveKind, options) {
		return 0
	}
	return len(splitOptions(options))
}

func buildPrompt(title, options, kind string) string {
	effectiveKind := effectiveQuestionType(kind, options)
	optionCount := effectiveOptionCount(effectiveKind, options)
	parts := []string{"题型：" + questionLabel(effectiveKind), "题目：" + cleanTitle(title)}
	if shouldIncludeOptions(effectiveKind, options) {
		parts = append(parts, "选项：\n"+labelOptions(options))
	}
	parts = append(parts, formatHintForType(effectiveKind, optionCount))
	return strings.Join(parts, "\n")
}

// directContent 构造直传图片的消息内容：图片交给回答模型自行理解。
// 仅替换已确认下载成功的图片链接，题干与选项中的普通文本链接保持原样。
// 没有可用图片时返回纯文本，与文本请求保持一致的消息格式。
func directContent(title, options, kind string, images map[string][]byte) any {
	effectiveKind := effectiveQuestionType(kind, options)
	optionCount := effectiveOptionCount(effectiveKind, options)
	titleText := replaceImageURLs(cleanTitle(title), images)
	parts := []contentPart{{Type: "text", Text: "题型：" + questionLabel(effectiveKind) + "\n题目：" + titleText}}
	for _, raw := range extractImageReferences(title) {
		if data := images[raw]; data != nil {
			if strings.HasPrefix(raw, "[图片") {
				parts = append(parts, textPart(raw))
			}
			parts = append(parts, imagePart(data))
		}
	}
	if shouldIncludeOptions(effectiveKind, options) {
		parts = append(parts, textPart("选项：\n"+labelOptions(replaceImageURLs(options, images))))
		for optionIndex, group := range splitOptions(options) {
			for _, raw := range extractImageReferences(group) {
				if data := images[raw]; data != nil {
					label := fmt.Sprintf("[选项 %s 的图片]", optionName(optionIndex))
					if strings.HasPrefix(raw, "[图片") {
						label += " " + raw
					}
					parts = append(parts, textPart(label), imagePart(data))
				}
			}
		}
	} else if effectiveKind == "judgement" && strings.TrimSpace(options) != "" {
		// 极少数判断题若选项包含图片，直接附带图片而不赋予 A/B 字母编号。
		for _, group := range splitOptions(options) {
			for _, raw := range extractImageReferences(group) {
				if data := images[raw]; data != nil {
					if strings.HasPrefix(raw, "[图片") {
						parts = append(parts, textPart(raw))
					}
					parts = append(parts, imagePart(data))
				}
			}
		}
	}
	hint := formatHintForType(effectiveKind, optionCount)
	parts = append(parts, textPart(hint))
	for _, part := range parts {
		if part.Type == "image_url" {
			return parts
		}
	}
	text := "题型：" + questionLabel(effectiveKind) + "\n题目：" + titleText
	if shouldIncludeOptions(effectiveKind, options) {
		text += "\n选项：\n" + labelOptions(replaceImageURLs(options, images))
	}
	return text + "\n" + hint
}

// replaceImageURLs 仅把实际作为图片加载成功的 URL 替换为 [图]，保留普通文本链接与 [图片N]。
func replaceImageURLs(text string, images map[string][]byte) string {
	for _, raw := range extractImageReferences(text) {
		if !strings.HasPrefix(raw, "[图片") && images[raw] != nil {
			text = strings.ReplaceAll(text, raw, "[图]")
		}
	}
	return text
}

func (s *Solver) waitRetry(ctx context.Context, retryIndex int) error {
	delay := time.Duration(retryIndex) * 300 * time.Millisecond
	if delay > 2*time.Second {
		delay = 2 * time.Second
	}
	if s.retryDelay != nil {
		delay = s.retryDelay(retryIndex)
	}
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// solve 完成一次答题。图片路径由回答模型的视觉能力决定：
// 具备视觉能力时图片直接交给回答模型，否则交给图片转述模型转为文字。
// 根据题型构造专属提示词并严格校验答案，调用失败或答案不符合题型要求时在同一记录内自动重试。
func (s *Solver) solve(ctx context.Context, title, options, kind string, inlineImages map[string][]byte) (result Record, solveErr error) {
	cfg := s.config.snapshot()
	start := time.Now()
	answerService := cfg.Services[cfg.Answer.Answer.Service]
	captionService := cfg.Services[cfg.Answer.Caption.Service]
	capability := s.capability(answerService, cfg.Answer.Answer)
	direct := capability.Vision
	answerModelLabel := modelLabel(answerService, cfg.Answer.Answer)

	result = Record{
		Timestamp: start.Format("2006-01-02T15:04:05.000000"), Type: kind, Status: "complete",
		RawTitle: title, RawOptions: options, ProcessedTitle: title, ProcessedOptions: options,
		Images: []ImageInfo{}, Attempts: []AttemptRecord{}, Vision: capability.Vision,
		AnswerModel: answerModelLabel, CaptionModel: captionLabel(captionService, cfg.Answer.Caption),
	}
	defer func() {
		result.TotalTimeMS = time.Since(start).Milliseconds()
		if len(result.Attempts) > 1 {
			result.RetryCount = len(result.Attempts) - 1
		}
	}()

	var caption *captionTask
	if cfg.Answer.Caption.Model != "" {
		captionModel := cfg.Answer.Caption
		caption = &captionTask{run: func(callCtx context.Context, data []byte) (string, error) {
			describeCtx := s.modelContext(callCtx, "caption", captionService, captionModel)
			return s.models.describe(describeCtx, captionService, captionModel, internalCaptionPrompt, data)
		}}
	}
	processedTitle, processedOptions, infos, images, err := s.images.process(ctx, title, options, caption, direct, inlineImages)
	result.ProcessedTitle, result.ProcessedOptions, result.Images = processedTitle, processedOptions, infos
	if err != nil {
		result.Answer = "ERROR: " + err.Error()
		return result, err
	}

	effectiveOptions := processedOptions
	if direct {
		effectiveOptions = options
	}
	effectiveKind := effectiveQuestionType(kind, effectiveOptions)
	optionCount := effectiveOptionCount(effectiveKind, effectiveOptions)

	var userContent any
	if direct {
		result.FinalPrompt = buildPrompt(title, options, effectiveKind)
		if len(images) > 0 {
			result.FinalPrompt += fmt.Sprintf("\n[图片直传: 含 %d 张原图]", len(images))
		}
		userContent = directContent(title, options, effectiveKind, images)
	} else {
		result.FinalPrompt = buildPrompt(processedTitle, processedOptions, effectiveKind)
		userContent = result.FinalPrompt
	}

	var lastContent, lastReasoning string
	attemptNo := 0

	// 步骤 1：若开启 Agent 模式且模型支持工具调用，优先尝试 Agent 作答，失败或答案不合法时回退到普通作答。
	if cfg.Answer.Agent.Enabled {
		if !capability.ToolCall {
			result.AgentError = "回答模型未标注工具调用能力，已按普通方式作答"
		} else {
			attemptNo++
			attemptStart := time.Now()
			agentPrompt := systemPromptForType(effectiveKind, optionCount, true)
			agentCtx := s.modelContext(ctx, "agent", answerService, cfg.Answer.Answer)
			content, reasoning, agentErr := s.runAgent(agentCtx, cfg, answerService, agentPrompt, []chatMessage{{Role: "user", Content: userContent}})
			latency := time.Since(attemptStart).Milliseconds()
			if agentErr != nil {
				reason := agentErrorText(agentErr)
				result.Attempts = append(result.Attempts, AttemptRecord{
					Attempt: attemptNo, Stage: "agent", Model: answerModelLabel, Status: "error",
					Error: reason, LatencyMS: latency, Timestamp: attemptStart.Format("2006-01-02T15:04:05.000000"),
				})
				result.AgentError = "Agent 作答失败，已回退：" + reason
			} else {
				if strings.TrimSpace(content) != "" || strings.TrimSpace(reasoning) != "" {
					lastContent, lastReasoning = content, reasoning
				}
				answer, valErr := extractAndValidateAnswer(content, reasoning, effectiveKind, optionCount)
				if valErr == nil {
					result.Attempts = append(result.Attempts, AttemptRecord{
						Attempt: attemptNo, Stage: "agent", Model: answerModelLabel, Status: "ok",
						Answer: answer, RawContent: truncateText(content, 4000), Reasoning: truncateText(reasoning, 4000),
						LatencyMS: latency, Timestamp: attemptStart.Format("2006-01-02T15:04:05.000000"),
					})
					result.RawContent, result.Reasoning = content, reasoning
					result.Answer = answer
					return result, nil
				}
				result.Attempts = append(result.Attempts, AttemptRecord{
					Attempt: attemptNo, Stage: "agent", Model: answerModelLabel, Status: "invalid",
					RawContent: truncateText(content, 4000), Reasoning: truncateText(reasoning, 4000),
					Error: valErr.Error(), LatencyMS: latency, Timestamp: attemptStart.Format("2006-01-02T15:04:05.000000"),
				})
				result.AgentError = "Agent 作答失败，已回退：" + valErr.Error()
			}
		}
	}

	// 步骤 2：普通作答与异常重试循环，所有尝试记录在同一条答题记录中。
	baseMessages := []chatMessage{
		{Role: "system", Content: systemPromptForType(effectiveKind, optionCount, false)},
		{Role: "user", Content: userContent},
	}
	messages := baseMessages
	maxRetries := cfg.Answer.MaxRetries
	if maxRetries < 0 {
		maxRetries = 0
	}
	totalAttempts := 1 + maxRetries
	var finalErr error

	for i := 0; i < totalAttempts; i++ {
		attemptNo++
		slot, stage := "answer", "answer"
		if i > 0 {
			slot, stage = "retry", "retry"
		}
		attemptStart := time.Now()
		answerCtx := s.modelContext(ctx, slot, answerService, cfg.Answer.Answer)
		content, reasoning, callErr := s.models.chat(answerCtx, answerService, cfg.Answer.Answer, messages, cfg.Answer.Temperature, 0)
		latency := time.Since(attemptStart).Milliseconds()
		if callErr != nil {
			result.Attempts = append(result.Attempts, AttemptRecord{
				Attempt: attemptNo, Stage: stage, Model: answerModelLabel, Status: "error",
				Error: truncateText(callErr.Error(), 500), LatencyMS: latency,
				Timestamp: attemptStart.Format("2006-01-02T15:04:05.000000"),
			})
			finalErr = callErr
			if i+1 < totalAttempts && isRetryableModelError(ctx, callErr) {
				if waitErr := s.waitRetry(ctx, i+1); waitErr != nil {
					finalErr = waitErr
					break
				}
				continue
			}
			break
		}

		if strings.TrimSpace(content) != "" || strings.TrimSpace(reasoning) != "" {
			lastContent, lastReasoning = content, reasoning
		}
		answer, valErr := extractAndValidateAnswer(content, reasoning, effectiveKind, optionCount)
		if valErr == nil {
			result.Attempts = append(result.Attempts, AttemptRecord{
				Attempt: attemptNo, Stage: stage, Model: answerModelLabel, Status: "ok",
				Answer: answer, RawContent: truncateText(content, 4000), Reasoning: truncateText(reasoning, 4000),
				LatencyMS: latency, Timestamp: attemptStart.Format("2006-01-02T15:04:05.000000"),
			})
			result.RawContent, result.Reasoning = content, reasoning
			result.Answer = answer
			return result, nil
		}

		result.Attempts = append(result.Attempts, AttemptRecord{
			Attempt: attemptNo, Stage: stage, Model: answerModelLabel, Status: "invalid",
			RawContent: truncateText(content, 4000), Reasoning: truncateText(reasoning, 4000),
			Error: valErr.Error(), LatencyMS: latency, Timestamp: attemptStart.Format("2006-01-02T15:04:05.000000"),
		})
		finalErr = valErr
		if i+1 < totalAttempts && ctx.Err() == nil {
			messages = buildRetryMessages(baseMessages, content, valErr.Error(), effectiveKind, optionCount)
			continue
		}
		break
	}

	result.RawContent, result.Reasoning = lastContent, lastReasoning
	if finalErr == nil {
		finalErr = errEmptyAnswer
	}
	result.Answer = "ERROR: " + finalErr.Error()
	return result, finalErr
}

// modelLabel 返回回答模型的展示名，未配置时为空。
func modelLabel(service ServiceConfig, model ModelConfig) string {
	if model.Model == "" {
		return ""
	}
	name := service.Name
	if name == "" {
		name = "未知服务"
	}
	return name + " (" + model.Model + ")"
}

// captionLabel 返回图片转述模型的展示名，未配置时为空。
func captionLabel(service ServiceConfig, model ModelConfig) string {
	if model.Model == "" {
		return ""
	}
	return modelLabel(service, model)
}

// answerDisabled 判断回答模型是否未配置。
func answerDisabled(model ModelConfig) bool {
	return strings.TrimSpace(model.Model) == "" || strings.TrimSpace(model.Service) == ""
}

var (
	errAnswerMissing = errors.New("请先在设置中配置回答模型")
	errEmptyAnswer   = errors.New("模型未返回有效答案")
)
