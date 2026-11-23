package main

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	// internalCaptionPrompt 是系统内部使用的图片转述提示词，不对外暴露在设置界面。
	internalCaptionPrompt = "请精确识别图片中的内容。如果是数学公式，用LaTeX格式完整输出每个符号，例如：\\frac{1}{2}、\\sqrt{x}、\\int_{a}^{b}。如果是图表或文字，描述关键信息。如果图片空白或完全无法辨认，回复'空白'。只输出识别结果，不解释。"

	defaultMaxRetries = 2
	maxAllowedRetries = 5
)

// answerValidationError 表示模型返回了内容，但不符合当前题型的答案格式或范围要求。
type answerValidationError struct {
	Reason string
}

func (e *answerValidationError) Error() string {
	return e.Reason
}

var (
	judgementCorrectWords = map[string]bool{
		"对": true, "正确": true, "是": true, "确定": true,
		"√": true, "✓": true, "对的": true, "是的": true, "正确的": true,
		"true": true, "t": true, "yes": true,
	}
	judgementIncorrectWords = map[string]bool{
		"错": true, "错误": true, "否": true, "非": true,
		"×": true, "✗": true, "错的": true, "不对": true, "不正确": true,
		"不正确的": true, "不是": true, "不是的": true,
		"false": true, "f": true, "no": true,
	}
)

func questionLabel(kind string) string {
	switch kind {
	case "single":
		return "单选题"
	case "multiple":
		return "多选题"
	case "judgement":
		return "判断题"
	case "completion":
		return "填空题"
	case "unknown":
		return "未知题型"
	default:
		return kind
	}
}

func cleanJudgementToken(text string, stripOptionPrefix bool) string {
	text = strings.TrimSpace(text)
	if stripOptionPrefix {
		text = strings.TrimSpace(optionLabel.ReplaceAllString(text, ""))
	}
	text = strings.Trim(text, " \t\r\n.。!！,，;；:：()（）[]【】\"'“”‘’")
	return strings.ToLower(strings.TrimSpace(text))
}

func matchJudgementWord(text string, stripOptionPrefix bool) (string, bool) {
	token := cleanJudgementToken(text, stripOptionPrefix)
	if token == "" {
		return "", false
	}
	if judgementCorrectWords[token] {
		return "对", true
	}
	if judgementIncorrectWords[token] {
		return "错", true
	}
	return "", false
}

// isJudgementOptions 检测选项是否仅由“对”和“错”两个判断项构成，与 OCS 判断题识别逻辑保持一致。
func isJudgementOptions(groups []string) bool {
	if len(groups) != 2 {
		return false
	}
	var hasCorrect, hasIncorrect bool
	for _, group := range groups {
		val, ok := matchJudgementWord(group, true)
		if !ok {
			return false
		}
		if val == "对" {
			hasCorrect = true
		} else {
			hasIncorrect = true
		}
	}
	return hasCorrect && hasIncorrect
}

// effectiveQuestionType 计算实际用于引导与校验的题型。
// 当单选或未知题型的选项仅包含“对/错”两项时，OCS 会按判断题解析，需统一按判断题作答。
func effectiveQuestionType(kind, options string) string {
	switch kind {
	case "judgement":
		return "judgement"
	case "single", "unknown":
		if isJudgementOptions(splitOptions(options)) {
			return "judgement"
		}
		return kind
	case "multiple", "completion":
		return kind
	default:
		return "single"
	}
}

func optionRangeLabel(optionCount int) string {
	if optionCount <= 0 {
		return "A-Z"
	}
	if optionCount == 1 {
		return "A"
	}
	return "A-" + optionName(optionCount-1)
}

// systemPromptForType 根据题型构造专属的内部系统提示词，确保模型按目标题型格式作答。
func systemPromptForType(kind string, optionCount int, agent bool) string {
	var b strings.Builder
	switch kind {
	case "single":
		b.WriteString("你是严谨的大学生网课答题助手，当前题目为单选题。\n")
		b.WriteString("请仔细分析题干和各选项，选出唯一一个最正确的选项。\n")
		if optionCount > 0 {
			fmt.Fprintf(&b, "输出格式要求：必须在 <answer> 与 </answer> 标签内仅输出唯一一个正确选项的大写字母（范围：%s），例如 <answer>A</answer>。\n", optionRangeLabel(optionCount))
		} else {
			b.WriteString("输出格式要求：必须在 <answer> 与 </answer> 标签内仅输出唯一一个正确选项的大写字母，例如 <answer>A</answer>。\n")
		}
		b.WriteString("严禁输出多个字母、选项正文、对/错或任何额外解释文字。")
	case "multiple":
		b.WriteString("你是严谨的大学生网课答题助手，当前题目为多选题。\n")
		b.WriteString("请逐一甄别每个选项的正误，选出所有正确选项。\n")
		if optionCount > 0 {
			fmt.Fprintf(&b, "输出格式要求：必须在 <answer> 与 </answer> 标签内仅输出所有正确选项的大写字母，按字母升序排列并用 # 分隔（范围：%s），例如 <answer>A#B#C</answer>。\n", optionRangeLabel(optionCount))
		} else {
			b.WriteString("输出格式要求：必须在 <answer> 与 </answer> 标签内仅输出所有正确选项的大写字母，按字母升序排列并用 # 分隔，例如 <answer>A#B#C</answer>。\n")
		}
		b.WriteString("严禁输出重复字母、选项正文、对/错或任何额外解释文字。")
	case "judgement":
		b.WriteString("你是严谨的大学生网课答题助手，当前题目为判断题。\n")
		b.WriteString("请判断题目陈述是否正确。\n")
		b.WriteString("输出格式要求：必须在 <answer> 与 </answer> 标签内仅输出“对”或“错”，即 <answer>对</answer> 或 <answer>错</answer>。\n")
		b.WriteString("严禁输出 A、B 等选项字母，严禁输出 true、false、正确、错误或任何额外解释文字。")
	case "completion":
		b.WriteString("你是严谨的大学生网课答题助手，当前题目为填空题。\n")
		b.WriteString("请直接给出准确、简洁的填空内容，不要重复题干。\n")
		b.WriteString("输出格式要求：必须在 <answer> 与 </answer> 标签内输出填空内容；如果有多个空，按先后顺序用 # 分隔，例如 <answer>第一空#第二空</answer>。\n")
		b.WriteString("严禁在 <answer> 标签外输出额外分析、说明或多余标点。")
	default:
		b.WriteString("你是严谨的大学生网课答题助手。\n")
		b.WriteString("请根据题目内容和给定选项判断正确答案，并在 <answer> 与 </answer> 标签内直接输出最终答案。\n")
		if optionCount > 0 {
			fmt.Fprintf(&b, "若有选项（范围：%s），单选仅输出单个大写字母（如 <answer>A</answer>），多选按字母升序用 # 分隔（如 <answer>A#B</answer>）。\n", optionRangeLabel(optionCount))
		}
		b.WriteString("只输出 <answer> 标签及答案本身，禁止输出额外分析或解释。")
	}
	if agent {
		b.WriteString("\n如需参考历史相似题目，可调用工具查询；历史记录的字母仅对应原题选项，须结合当前题目重新确认，最终仍须严格遵守上述 <answer> 输出格式。")
	}
	return b.String()
}

// formatHintForType 返回附加在用户题目末尾的简短题型输出提醒。
func formatHintForType(kind string, optionCount int) string {
	switch kind {
	case "single":
		if optionCount > 0 {
			return fmt.Sprintf("请仅在 <answer> 标签内输出唯一正确选项的大写字母（范围：%s），例如 <answer>A</answer>。", optionRangeLabel(optionCount))
		}
		return "请仅在 <answer> 标签内输出唯一正确选项的大写字母，例如 <answer>A</answer>。"
	case "multiple":
		if optionCount > 0 {
			return fmt.Sprintf("请仅在 <answer> 标签内输出所有正确选项的大写字母并按字母升序用 # 分隔（范围：%s），例如 <answer>A#C</answer>。", optionRangeLabel(optionCount))
		}
		return "请仅在 <answer> 标签内输出所有正确选项的大写字母并按字母升序用 # 分隔，例如 <answer>A#C</answer>。"
	case "judgement":
		return "请判断上述陈述是否正确，仅输出 <answer>对</answer> 或 <answer>错</answer>，禁止输出选项字母。"
	case "completion":
		return "请仅在 <answer> 标签内输出填空内容，多个空按顺序用 # 分隔，不要包含解释。"
	default:
		return "请仅在 <answer> 标签内输出最终答案，不要包含解释。"
	}
}

// extractRawAnswer 优先从 content 的最后一个非空 <answer> 标签提取，其次从 reasoning 的 <answer> 标签提取。
func extractRawAnswer(content, reasoning string) (string, bool) {
	for _, value := range []string{content, reasoning} {
		matches := answerPattern.FindAllStringSubmatch(value, -1)
		if len(matches) == 0 {
			continue
		}
		for i := len(matches) - 1; i >= 0; i-- {
			if raw := strings.TrimSpace(matches[i][1]); raw != "" {
				return raw, true
			}
		}
		return "", true
	}
	return strings.TrimSpace(content), false
}

func isValidOptionIndex(letter byte, optionCount int) bool {
	if letter < 'A' || letter > 'Z' {
		return false
	}
	if optionCount <= 0 || optionCount > 26 {
		return true
	}
	return int(letter-'A') < optionCount
}

func stripSingleWrapper(text string) string {
	text = strings.TrimSpace(text)
	pairs := [][2]string{{"(", ")"}, {"（", "）"}, {"[", "]"}, {"【", "】"}, {"\"", "\""}, {"'", "'"}}
	for _, pair := range pairs {
		if strings.HasPrefix(text, pair[0]) && strings.HasSuffix(text, pair[1]) && len(text) > len(pair[0])+len(pair[1]) {
			text = strings.TrimSpace(text[len(pair[0]) : len(text)-len(pair[1])])
		}
	}
	text = strings.TrimRight(text, ".。、)）:：")
	return strings.TrimSpace(text)
}

func isASCIILetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func validateSingleAnswer(raw string, optionCount int) (string, error) {
	cleaned := stripSingleWrapper(raw)
	if cleaned == "" {
		return "", &answerValidationError{Reason: "单选题答案为空"}
	}
	if len(cleaned) == 1 && isASCIILetter(cleaned[0]) {
		letter := strings.ToUpper(cleaned)[0]
		if !isValidOptionIndex(letter, optionCount) {
			return "", &answerValidationError{Reason: fmt.Sprintf("单选题选项超出范围：实际返回 %c，可选范围为 %s", letter, optionRangeLabel(optionCount))}
		}
		return string(letter), nil
	}
	if val, ok := matchJudgementWord(cleaned, false); ok {
		return "", &answerValidationError{Reason: fmt.Sprintf("单选题不能返回判断题答案“%s”，请输出对应选项字母", val)}
	}
	if letters, ok := parseOptionLetters(cleaned); ok && len(letters) > 1 {
		return "", &answerValidationError{Reason: fmt.Sprintf("单选题只能选择一个选项，实际返回了多个选项“%s”", strings.Join(letters, "#"))}
	}
	return "", &answerValidationError{Reason: fmt.Sprintf("单选题答案格式无效：期望单个选项字母（%s），实际返回“%s”", optionRangeLabel(optionCount), truncateText(cleaned, 40))}
}

// parseOptionLetters 判断文本是否仅由选项字母及允许的分隔符组成。
func parseOptionLetters(raw string) ([]string, bool) {
	cleaned := strings.TrimSpace(raw)
	cleaned = strings.Trim(cleaned, "()（）[]【】\"'“”‘’")
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return nil, false
	}
	isSep := func(r rune) bool {
		switch r {
		case '#', ',', '，', '、', ';', '；', '|', '.', '。', ' ', '\t', '\r', '\n', '+', '/':
			return true
		default:
			return false
		}
	}
	for _, r := range cleaned {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || isSep(r) {
			continue
		}
		return nil, false
	}
	tokens := strings.FieldsFunc(cleaned, isSep)
	if len(tokens) == 0 {
		return nil, false
	}
	letters := make([]string, 0, len(tokens))
	if len(tokens) == 1 {
		token := strings.ToUpper(tokens[0])
		seen := make(map[byte]bool, len(token))
		for i := 0; i < len(token); i++ {
			ch := token[i]
			if seen[ch] {
				// 连续多字母串出现重复字母（如 "ALL"、"AA"）视为非合法选项组合。
				return nil, false
			}
			seen[ch] = true
			letters = append(letters, string(ch))
		}
		return letters, true
	}
	for _, token := range tokens {
		if len(token) != 1 {
			// 多段输入里出现单词（如 "Option A and B"）直接判定非法。
			return nil, false
		}
		letters = append(letters, strings.ToUpper(token))
	}
	return letters, true
}

func validateMultipleAnswer(raw string, optionCount int) (string, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "", &answerValidationError{Reason: "多选题答案为空"}
	}
	letters, ok := parseOptionLetters(cleaned)
	if !ok || len(letters) == 0 {
		if val, isJudge := matchJudgementWord(cleaned, false); isJudge {
			return "", &answerValidationError{Reason: fmt.Sprintf("多选题不能返回判断题答案“%s”，请输出选项字母并用 # 分隔", val)}
		}
		return "", &answerValidationError{Reason: fmt.Sprintf("多选题答案格式无效：期望以 # 分隔的选项字母（%s），实际返回“%s”", optionRangeLabel(optionCount), truncateText(cleaned, 40))}
	}
	unique := make(map[byte]bool, len(letters))
	result := make([]byte, 0, len(letters))
	for _, item := range letters {
		ch := item[0]
		if !isValidOptionIndex(ch, optionCount) {
			return "", &answerValidationError{Reason: fmt.Sprintf("多选题选项超出范围：包含 %c，可选范围为 %s", ch, optionRangeLabel(optionCount))}
		}
		if !unique[ch] {
			unique[ch] = true
			result = append(result, ch)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	formatted := make([]string, len(result))
	for i, ch := range result {
		formatted[i] = string(ch)
	}
	return strings.Join(formatted, "#"), nil
}

func validateJudgementAnswer(raw string) (string, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "", &answerValidationError{Reason: "判断题答案为空"}
	}
	if val, ok := matchJudgementWord(cleaned, false); ok {
		return val, nil
	}
	if letters, ok := parseOptionLetters(cleaned); ok && len(letters) > 0 {
		return "", &answerValidationError{Reason: fmt.Sprintf("判断题答案必须为“对”或“错”，不能输出选项字母“%s”", strings.Join(letters, "#"))}
	}
	return "", &answerValidationError{Reason: fmt.Sprintf("判断题答案必须为“对”或“错”，实际返回“%s”", truncateText(cleaned, 40))}
}

func validateCompletionAnswer(raw string, tagged bool) (string, error) {
	if !tagged {
		return "", &answerValidationError{Reason: "填空题回答缺少 <answer> 标签"}
	}
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return "", &answerValidationError{Reason: "填空题答案为空"}
	}
	if strings.Contains(cleaned, "#") {
		parts := strings.Split(cleaned, "#")
		normalized := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				return "", &answerValidationError{Reason: "填空题答案包含空项，请检查 # 分隔格式"}
			}
			normalized = append(normalized, part)
		}
		return strings.Join(normalized, "#"), nil
	}
	return cleaned, nil
}

// extractAndValidateAnswer 按题型从模型回复中提取并严格校验答案。
func extractAndValidateAnswer(content, reasoning, kind string, optionCount int) (string, error) {
	raw, tagged := extractRawAnswer(content, reasoning)
	if raw == "" {
		return "", &answerValidationError{Reason: errEmptyAnswer.Error()}
	}
	switch kind {
	case "single":
		return validateSingleAnswer(raw, optionCount)
	case "multiple":
		return validateMultipleAnswer(raw, optionCount)
	case "judgement":
		return validateJudgementAnswer(raw)
	case "completion":
		return validateCompletionAnswer(raw, tagged)
	case "unknown":
		if !tagged {
			return "", &answerValidationError{Reason: "回答缺少 <answer> 标签"}
		}
		if optionCount > 0 {
			if ans, err := validateMultipleAnswer(raw, optionCount); err == nil {
				return ans, nil
			}
		}
		return raw, nil
	default:
		return validateSingleAnswer(raw, optionCount)
	}
}

// buildRetryMessages 基于初始对话构造纠错重试消息，兼容 OpenAI/Anthropic/Gemini/Ollama 消息角色交替约束。
func buildRetryMessages(base []chatMessage, lastContent, reason, kind string, optionCount int) []chatMessage {
	feedback := fmt.Sprintf("你上一次的回答不符合本题型要求：%s。\n%s", reason, formatHintForType(kind, optionCount))
	messages := make([]chatMessage, 0, len(base)+2)
	messages = append(messages, base...)
	trimmed := strings.TrimSpace(lastContent)
	if trimmed != "" {
		messages = append(messages,
			chatMessage{Role: "assistant", Content: truncateText(trimmed, 1000)},
			chatMessage{Role: "user", Content: feedback},
		)
		return messages
	}
	// 上次正文为空时不插入空的 assistant 消息，直接把提醒附加到用户消息，避免部分协议报错。
	if len(messages) > 0 && messages[len(messages)-1].Role == "user" {
		last := messages[len(messages)-1]
		switch content := last.Content.(type) {
		case string:
			last.Content = content + "\n\n" + feedback
		case []contentPart:
			parts := make([]contentPart, len(content), len(content)+1)
			copy(parts, content)
			parts = append(parts, textPart(feedback))
			last.Content = parts
		}
		messages[len(messages)-1] = last
	}
	return messages
}

// isRetryableModelError 判断模型调用错误是否值得重试。
// 取消、缺少配置及 400/401/403/404 等确定性错误直接终止，避免无效等待。
func isRetryableModelError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := err.Error()
	for _, permanent := range []string{
		"模型密钥", "服务地址无效", "未知协议", "不支持工具调用", "消息格式无效", "图片数据无效",
	} {
		if strings.Contains(msg, permanent) {
			return false
		}
	}
	if match := statusCodePattern.FindString(msg); match != "" {
		switch match {
		case "408", "409", "425", "429", "500", "502", "503", "504", "520", "522", "524":
			return true
		default:
			if strings.HasPrefix(match, "5") {
				return true
			}
			return false
		}
	}
	return true
}
