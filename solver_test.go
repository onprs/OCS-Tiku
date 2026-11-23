package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAnswerTemperature(t *testing.T) {
	for _, agent := range []bool{false, true} {
		for _, temperature := range []float64{0, 1.3} {
			t.Run(map[bool]string{false: "普通", true: "Agent"}[agent]+"/"+map[bool]string{true: "零温度", false: "自定义温度"}[temperature == 0], func(t *testing.T) {
				s, web, _ := setupServer(t)
				var actual atomic.Int64
				fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						Temperature *float64 `json:"temperature"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Temperature == nil {
						t.Errorf("请求缺少温度: %v", err)
					} else {
						actual.Store(int64(*body.Temperature * 1000))
					}
					jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "<answer>A</answer>"}, "finish_reason": "stop"}}})
				}))
				defer fake.Close()
				service := ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"}
				model := ModelConfig{Model: "answer-model", Vision: CapabilityOff, ToolCall: CapabilityOn}
				if agent {
					configureAgent(t, s, service, model, 3)
				} else {
					configureAnswer(t, s, service, model)
				}
				answer := s.config.snapshot().Answer
				answer.Temperature = temperature
				if err := s.config.updateAnswer(answer); err != nil {
					t.Fatal(err)
				}
				requestJSON(t, web, "POST", "/api/search", map[string]string{"title": "温度配置题目"}, 200, nil)
				if math.Abs(float64(actual.Load())/1000-temperature) > 0.001 {
					t.Fatalf("实际温度 %v，预期 %v", float64(actual.Load())/1000, temperature)
				}
			})
		}
	}
}

func TestEmptyAnswersAreFailures(t *testing.T) {
	for _, content := range []string{"", "<answer> </answer>"} {
		t.Run(content, func(t *testing.T) {
			s, web, _ := setupServer(t)
			fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content, "reasoning_content": "还需要进一步推导"}, "finish_reason": "length"}}})
			}))
			defer fake.Close()
			configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"}, ModelConfig{Model: "answer-model", Vision: CapabilityOff})
			requestJSON(t, web, "POST", "/api/search", map[string]string{"title": "空答案题目"}, 500, nil)
			stats, err := s.store.stats(t.Context())
			if err != nil || stats.Total != 1 || stats.SuccessRate != 0 {
				t.Fatalf("空答案不应计为成功: %+v %v", stats, err)
			}
			record, err := s.store.get(t.Context(), 1)
			if err != nil || record.Status != "complete" || !strings.HasPrefix(record.Answer, "ERROR:") || record.Reasoning == "" {
				t.Fatalf("失败记录未正确保存: %+v %v", record, err)
			}
		})
	}
}

func TestAgentReasoningWithoutAnswerFallsBack(t *testing.T) {
	s, web, _ := setupServer(t)
	var agentCalls, answerCalls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []any `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		message := map[string]string{"content": "<answer>B</answer>"}
		if len(body.Tools) > 0 {
			agentCalls.Add(1)
			message = map[string]string{"content": "", "reasoning_content": "推理尚未得出答案"}
		} else {
			answerCalls.Add(1)
		}
		jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": message, "finish_reason": "stop"}}})
	}))
	defer fake.Close()
	configureAgent(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"}, ModelConfig{Model: "answer-model", Vision: CapabilityOff, ToolCall: CapabilityOn}, 3)
	var result struct{ Answer string }
	requestJSON(t, web, "POST", "/api/search", map[string]string{"title": "推理未完成题目"}, 200, &result)
	if result.Answer != "B" || agentCalls.Load() != 1 || answerCalls.Load() != 1 {
		t.Fatalf("未正确回退: %+v，Agent=%d，普通=%d", result, agentCalls.Load(), answerCalls.Load())
	}
	record, err := s.store.get(t.Context(), 1)
	if err != nil || !strings.Contains(record.AgentError, "回退") {
		t.Fatalf("缺少回退原因: %+v %v", record, err)
	}
}

func TestMultilineOptionImageLabels(t *testing.T) {
	for _, options := range []string{
		"A. 第一项\n附图 https://images.example.com/a.png\n\nB. 第二项\n附图 https://images.example.com/b.png",
		"第一项 https://images.example.com/a.png\n\n第二项 https://images.example.com/b.png",
	} {
		parts := directContent("图片归属题目", options, "single", map[string][]byte{
			"https://images.example.com/a.png": []byte("第一张图"),
			"https://images.example.com/b.png": []byte("第二张图"),
		}).([]contentPart)
		labels := []string{}
		for _, part := range parts {
			if strings.HasPrefix(part.Text, "[选项") {
				labels = append(labels, part.Text)
			}
		}
		if strings.Join(labels, ",") != "[选项 A 的图片],[选项 B 的图片]" {
			t.Fatalf("选项图片标记异常: %v", labels)
		}
	}
}

func TestImageProcessingFailures(t *testing.T) {
	var imageData bytes.Buffer
	if err := png.Encode(&imageData, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	for _, failure := range []string{"download", "caption", "empty-caption", "partial"} {
		t.Run(failure, func(t *testing.T) {
			processor, err := newImageProcessor(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			processor.fetch = func(_ context.Context, url string) ([]byte, error) {
				if failure == "download" || failure == "partial" && strings.HasSuffix(url, "b.png") {
					return nil, errors.New("HTTP 404")
				}
				return imageData.Bytes(), nil
			}
			caption := &captionTask{run: func(context.Context, []byte) (string, error) {
				if failure == "caption" {
					return "", errors.New("转述模型失败")
				}
				return "  ", nil
			}}
			direct := failure == "download" || failure == "partial"
			_, _, infos, _, err := processor.process(t.Context(), "https://images.example.com/a.png", "https://images.example.com/b.png", caption, direct, nil)
			if err == nil || len(infos) != 2 || infos[1].Status != "failed" {
				t.Fatalf("图片失败未传播: %+v %v", infos, err)
			}
		})
	}
}

func TestMissingQuestionImageDoesNotCallAnswerModel(t *testing.T) {
	s, web, _ := setupServer(t)
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "<answer>A</answer>"}}}})
	}))
	defer fake.Close()
	configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"}, ModelConfig{Model: "answer-model", Vision: CapabilityOn})
	s.solver.images.fetch = func(context.Context, string) ([]byte, error) { return nil, errors.New("HTTP 404") }
	requestJSON(t, web, "POST", "/api/search", map[string]string{"title": "识别图形 https://images.example.com/question.png"}, 500, nil)
	if calls.Load() != 0 {
		t.Fatal("题图缺失时仍调用了回答模型")
	}
	record, err := s.store.get(t.Context(), 1)
	if err != nil || len(record.Images) != 1 || record.Images[0].Status != "failed" || !strings.HasPrefix(record.Answer, "ERROR:") {
		t.Fatalf("未保存图片失败详情: %+v %v", record, err)
	}
}

func TestExtractAndValidateAnswer(t *testing.T) {
	validCases := []struct {
		name        string
		kind        string
		optionCount int
		content     string
		reasoning   string
		want        string
	}{
		{"单选标准", "single", 4, "<answer>b</answer>", "", "B"},
		{"单选去括号", "single", 4, "<answer>(C)</answer>", "", "C"},
		{"单选六选项F", "single", 6, "<answer>F</answer>", "", "F"},
		{"单选思考中提取", "single", 4, "", "推导结果：<answer>D</answer>", "D"},
		{"单选取最后标签", "single", 4, "先想<answer>A</answer>，修正为<answer>B</answer>", "", "B"},
		{"多选排序去重", "multiple", 4, "<answer>D#A#C#A</answer>", "", "A#C#D"},
		{"多选逗号分隔", "multiple", 4, "<answer>B, A</answer>", "", "A#B"},
		{"多选连续字母", "multiple", 4, "<answer>CBA</answer>", "", "A#B#C"},
		{"判断对", "judgement", 0, "<answer>对</answer>", "", "对"},
		{"判断错", "judgement", 0, "<answer>错</answer>", "", "错"},
		{"判断同义词规范化", "judgement", 0, "<answer>正确</answer>", "", "对"},
		{"判断英文规范化", "judgement", 0, "<answer>False</answer>", "", "错"},
		{"判断符号规范化", "judgement", 0, "<answer>√</answer>", "", "对"},
		{"填空单空", "completion", 0, "<answer>TCP/IP</answer>", "", "TCP/IP"},
		{"填空多空", "completion", 0, "<answer> 卷积层 # 池化层 </answer>", "", "卷积层#池化层"},
		{"未知题型带选项", "unknown", 4, "<answer>C#A</answer>", "", "A#C"},
	}
	for _, tc := range validCases {
		t.Run("通过/"+tc.name, func(t *testing.T) {
			got, err := extractAndValidateAnswer(tc.content, tc.reasoning, tc.kind, tc.optionCount)
			if err != nil || got != tc.want {
				t.Fatalf("结果=(%q, %v)，期望=%q", got, err, tc.want)
			}
		})
	}

	invalidCases := []struct {
		name        string
		kind        string
		optionCount int
		content     string
	}{
		{"判断返回A", "judgement", 0, "<answer>A</answer>"},
		{"判断返回B", "judgement", 0, "<answer>B</answer>"},
		{"判断返回X占位符", "judgement", 0, "<answer>X</answer>"},
		{"判断返回解释文字", "judgement", 0, "这道题显然是对的"},
		{"单选越界", "single", 4, "<answer>E</answer>"},
		{"单选返回多选", "single", 4, "<answer>A#B</answer>"},
		{"单选返回对错", "single", 4, "<answer>对</answer>"},
		{"单选无标签解释", "single", 4, "根据题意应该选择A"},
		{"多选越界", "multiple", 4, "<answer>A#E</answer>"},
		{"多选返回对错", "multiple", 4, "<answer>错</answer>"},
		{"多选包含单词", "multiple", 4, "<answer>Option A and B</answer>"},
		{"多选连续重复字母", "multiple", 4, "<answer>AA</answer>"},
		{"填空缺少标签", "completion", 0, "直接输出无标签答案"},
		{"填空包含空分段", "completion", 0, "<answer>第一空##第二空</answer>"},
		{"空标签", "single", 4, "<answer> </answer>"},
	}
	for _, tc := range invalidCases {
		t.Run("拒绝/"+tc.name, func(t *testing.T) {
			got, err := extractAndValidateAnswer(tc.content, "", tc.kind, tc.optionCount)
			if err == nil {
				t.Fatalf("应当校验失败，却返回了 %q", got)
			}
		})
	}
}

func TestQuestionTypePromptGuidance(t *testing.T) {
	s, web, _ := setupServer(t)
	var mu sync.Mutex
	var captured []struct {
		System string
		User   string
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		sys, usr := "", ""
		for _, m := range body.Messages {
			if m.Role == "system" {
				sys = m.Content
			} else if m.Role == "user" {
				usr = m.Content
			}
		}
		mu.Lock()
		captured = append(captured, struct{ System, User string }{sys, usr})
		mu.Unlock()
		answer := "<answer>A</answer>"
		switch {
		case strings.Contains(sys, "判断题"):
			answer = "<answer>对</answer>"
		case strings.Contains(sys, "多选题"):
			answer = "<answer>A#C</answer>"
		case strings.Contains(sys, "填空题"):
			answer = "<answer>42</answer>"
		}
		jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
	}))
	defer fake.Close()
	configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"},
		ModelConfig{Model: "answer-model", Vision: CapabilityOff})

	// 1. 判断题不应附带 A. 对 / B. 错，且系统提示明确禁止字母。
	var res struct{ Answer string }
	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "地球围绕太阳公转。", "options": "对\n错", "type": "judgement",
	}, 200, &res)
	if res.Answer != "对" {
		t.Fatalf("判断题答案异常: %+v", res)
	}
	// 2. 单选题型但选项为 对/错（OCS 按判断题处理），应自动按判断题引导并返回“对”。
	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "水在标准大气压下100摄氏度沸腾。", "options": "正确\n错误", "type": "single",
	}, 200, &res)
	if res.Answer != "对" {
		t.Fatalf("对错选项单选题答案异常: %+v", res)
	}
	// 3. 多选题提示应明确范围和 # 分隔。
	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "以下属于质数的是", "options": "2\n4\n5\n9", "type": "multiple",
	}, 200, &res)
	if res.Answer != "A#C" {
		t.Fatalf("多选题答案异常: %+v", res)
	}
	// 4. 填空题提示应明确按填空引导且不含选项。
	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "6 乘以 7 等于 ____。", "options": "", "type": "completion",
	}, 200, &res)
	if res.Answer != "42" {
		t.Fatalf("填空题答案异常: %+v", res)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) != 4 {
		t.Fatalf("调用次数=%d，期望 4", len(captured))
	}
	for _, idx := range []int{0, 1} {
		if !strings.Contains(captured[idx].System, "判断题") || !strings.Contains(captured[idx].System, "严禁输出 A、B") {
			t.Fatalf("第 %d 条判断题系统提示缺少约束: %s", idx+1, captured[idx].System)
		}
		if strings.Contains(captured[idx].User, "A. 对") || strings.Contains(captured[idx].User, "A. 正确") {
			t.Fatalf("第 %d 条判断题用户提示不应给对/错加字母前缀: %s", idx+1, captured[idx].User)
		}
	}
	if !strings.Contains(captured[2].System, "多选题") || !strings.Contains(captured[2].System, "A-D") {
		t.Fatalf("多选题系统提示异常: %s", captured[2].System)
	}
	if !strings.Contains(captured[3].System, "填空题") || strings.Contains(captured[3].User, "选项：") {
		t.Fatalf("填空题提示异常: sys=%s user=%s", captured[3].System, captured[3].User)
	}
}

func TestNonImageLinksPreservedAsQuestionText(t *testing.T) {
	t.Run("普通网页、API、内网链接不下载且保留原文", func(t *testing.T) {
		s, web, _ := setupServer(t)
		var fetchCalls atomic.Int32
		s.solver.images.fetch = func(_ context.Context, raw string) ([]byte, error) {
			fetchCalls.Add(1)
			return nil, errors.New("HTTP 404")
		}
		var seenPrompts []string
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			seenPrompts = append(seenPrompts, body.Messages[len(body.Messages)-1].Content)
			jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "<answer>A</answer>"}}}})
		}))
		defer fake.Close()
		// 故意关闭视觉且不配置转述模型，验证普通链接不会触发“缺少图片转述模型”报错。
		configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"},
			ModelConfig{Model: "text-only-model", Vision: CapabilityOff})

		cases := []struct {
			title string
			link  string
		}{
			{"访问 https://api.example.com/v1/users 时常用的获取方法是什么？", "https://api.example.com/v1/users"},
			{"在浏览器中打开 https://www.example.com/docs/index.html 使用的协议是？", "https://www.example.com/docs/index.html"},
			{"本地测试访问 http://127.0.0.1:8080/logo.png 返回 404 的可能原因是？", "http://127.0.0.1:8080/logo.png"},
		}
		for _, tc := range cases {
			var res struct{ Question, Answer string }
			requestJSON(t, web, "POST", "/api/search", map[string]string{
				"title": tc.title, "options": "GET\nPOST\nPUT\nDELETE", "type": "single",
			}, 200, &res)
			if res.Answer != "A" {
				t.Fatalf("含普通链接的题目作答失败: %+v", res)
			}
			lastPrompt := seenPrompts[len(seenPrompts)-1]
			if !strings.Contains(lastPrompt, tc.link) || strings.Contains(lastPrompt, "[图]") {
				t.Fatalf("普通链接未原样保留: %s", lastPrompt)
			}
		}
		if fetchCalls.Load() != 0 {
			t.Fatalf("普通网页/内网链接不应发起图片下载，实际下载 %d 次", fetchCalls.Load())
		}
	})

	t.Run("代码或文字中的示例图片链接404或返回HTML时保留为文本", func(t *testing.T) {
		s, web, _ := setupServer(t)
		s.solver.images.fetch = func(_ context.Context, raw string) ([]byte, error) {
			if strings.Contains(raw, "html-page.png") {
				return nil, errNotImage
			}
			return nil, errors.New("HTTP 404")
		}
		var lastPrompt string
		fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			lastPrompt = body.Messages[len(body.Messages)-1].Content
			jsonResponse(w, 200, map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "<answer>B</answer>"}}}})
		}))
		defer fake.Close()
		configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"},
			ModelConfig{Model: "vision-model", Vision: CapabilityOn})

		titles := []struct {
			title string
			link  string
		}{
			{`在HTML中 &lt;img src=&quot;https://example.com/logo.png&quot;&gt; 缺少哪个无障碍属性？`, "https://example.com/logo.png"},
			{`当请求 https://example.com/not-found.png 时服务器返回 404 表示资源不存在吗？请选择正确状态码含义。`, "https://example.com/not-found.png"},
			{`如图所示的链接 https://example.com/html-page.png 实际指向网页而非图片。`, "https://example.com/html-page.png"},
		}
		for _, tc := range titles {
			var res struct{ Answer string }
			requestJSON(t, web, "POST", "/api/search", map[string]string{
				"title": tc.title, "options": "选项一\n选项二\n选项三", "type": "single",
			}, 200, &res)
			if res.Answer != "B" || !strings.Contains(lastPrompt, tc.link) {
				t.Fatalf("示例链接未降级保留: answer=%q prompt=%s", res.Answer, lastPrompt)
			}
		}
	})

	t.Run("紧邻拼接的两个网课图片URL能正确拆分", func(t *testing.T) {
		raw := "https://p.ananas.chaoxing.com/star3/origin/first.pnghttps://p.ananas.chaoxing.com/star3/origin/second.png"
		got := extractImageReferences(raw)
		want := []string{
			"https://p.ananas.chaoxing.com/star3/origin/first.png",
			"https://p.ananas.chaoxing.com/star3/origin/second.png",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("紧邻 URL 拆分错误: got=%v want=%v", got, want)
		}
	})

	t.Run("下载返回HTML识别为errNotImage且不重试", func(t *testing.T) {
		p, err := newImageProcessor(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		var attempts atomic.Int32
		p.http = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
			attempts.Add(1)
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader("<html>not image</html>")),
			}, nil
		})}
		_, err = p.download(t.Context(), "https://example.org/page.png")
		if !errors.Is(err, errNotImage) || attempts.Load() != 1 {
			t.Fatalf("非图片响应处理异常: err=%v attempts=%d", err, attempts.Load())
		}
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRetryOnInvalidAnswerRecordedInSingleRecord(t *testing.T) {
	s, web, _ := setupServer(t)
	s.solver.retryDelay = func(int) time.Duration { return 0 }
	var calls atomic.Int32
	var secondMessages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if n == 2 {
			secondMessages = body.Messages
		}
		content := "<answer>A</answer>"
		if n >= 2 {
			content = "<answer>对</answer>"
		}
		jsonResponse(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": "stop"}},
			"usage":   map[string]int{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	defer fake.Close()
	configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"},
		ModelConfig{Model: "answer-model", Vision: CapabilityOff})

	var res struct{ Answer string }
	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "TCP 是面向连接的传输层协议。", "options": "对\n错", "type": "judgement",
	}, 200, &res)
	if res.Answer != "对" || calls.Load() != 2 {
		t.Fatalf("判断题纠错重试异常: answer=%q calls=%d", res.Answer, calls.Load())
	}
	if len(secondMessages) != 4 || secondMessages[2].Role != "assistant" || secondMessages[2].Content != "<answer>A</answer>" ||
		secondMessages[3].Role != "user" || !strings.Contains(secondMessages[3].Content, "不能输出选项字母") {
		t.Fatalf("重试请求未包含上次错误回复与纠错指引: %+v", secondMessages)
	}

	var list struct {
		Records []RecordSummary `json:"records"`
		Total   int             `json:"total"`
	}
	requestJSON(t, web, "GET", "/api/records", nil, 200, &list)
	if list.Total != 1 || len(list.Records) != 1 || list.Records[0].RetryCount != 1 {
		t.Fatalf("重试应合并在同一条记录中: %+v", list)
	}

	var detail Record
	requestJSON(t, web, "GET", "/api/records/1", nil, 200, &detail)
	if detail.Answer != "对" || detail.RetryCount != 1 || len(detail.Attempts) != 2 {
		t.Fatalf("重试详情异常: %+v", detail)
	}
	if detail.Attempts[0].Status != "invalid" || !strings.Contains(detail.Attempts[0].Error, "不能输出选项字母") || detail.Attempts[0].RawContent != "<answer>A</answer>" {
		t.Fatalf("第 1 次失败尝试未准确记录: %+v", detail.Attempts[0])
	}
	if detail.Attempts[1].Stage != "retry" || detail.Attempts[1].Status != "ok" || detail.Attempts[1].Answer != "对" {
		t.Fatalf("第 2 次重试尝试未准确记录: %+v", detail.Attempts[1])
	}
	if len(detail.Calls) != 2 || detail.Calls[0].Slot != "answer" || detail.Calls[1].Slot != "retry" {
		t.Fatalf("模型调用记录未归属到同一条记录: %+v", detail.Calls)
	}
	var stats Stats
	requestJSON(t, web, "GET", "/api/stats", nil, 200, &stats)
	if stats.Total != 1 || stats.SuccessRate != 100 || stats.TotalTokens != 30 {
		t.Fatalf("统计与 Token 累计异常: %+v", stats)
	}
}

func TestRetryOnTransientCallErrorRecordedInSingleRecord(t *testing.T) {
	s, web, _ := setupServer(t)
	s.solver.retryDelay = func(int) time.Duration { return 0 }
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`{"error":{"message":"service temporarily unavailable"}}`))
			return
		}
		jsonResponse(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<answer>C</answer>"}, "finish_reason": "stop"}},
		})
	}))
	defer fake.Close()
	configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"},
		ModelConfig{Model: "answer-model", Vision: CapabilityOff})

	var res struct{ Answer string }
	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "瞬时故障恢复测试", "options": "甲\n乙\n丙\n丁", "type": "single",
	}, 200, &res)
	if res.Answer != "C" || calls.Load() != 2 {
		t.Fatalf("瞬时错误未重试恢复: answer=%q calls=%d", res.Answer, calls.Load())
	}
	var detail Record
	requestJSON(t, web, "GET", "/api/records/1", nil, 200, &detail)
	if detail.RetryCount != 1 || len(detail.Attempts) != 2 || detail.Attempts[0].Status != "error" || detail.Attempts[1].Status != "ok" {
		t.Fatalf("瞬时错误重试详情异常: %+v", detail)
	}
	if len(detail.Calls) != 2 || detail.Calls[0].Status != "error" || detail.Calls[1].Status != "ok" {
		t.Fatalf("调用记录异常: %+v", detail.Calls)
	}
}

func TestExhaustedRetriesRecordedInSingleRecord(t *testing.T) {
	s, web, _ := setupServer(t)
	s.solver.retryDelay = func(int) time.Duration { return 0 }
	var calls atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		jsonResponse(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": "<answer>B</answer>"}, "finish_reason": "stop"}},
		})
	}))
	defer fake.Close()
	configureAnswer(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"},
		ModelConfig{Model: "answer-model", Vision: CapabilityOff})

	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "始终返回非法格式的判断题", "options": "对\n错", "type": "judgement",
	}, 500, nil)
	// 默认 max_retries = 2，共尝试 3 次且仅写入 1 条记录。
	if calls.Load() != 3 {
		t.Fatalf("尝试次数=%d，期望 3", calls.Load())
	}
	var list struct{ Total int }
	requestJSON(t, web, "GET", "/api/records", nil, 200, &list)
	if list.Total != 1 {
		t.Fatalf("重试耗尽后应只保留 1 条记录: %d", list.Total)
	}
	var detail Record
	requestJSON(t, web, "GET", "/api/records/1", nil, 200, &detail)
	if !strings.HasPrefix(detail.Answer, "ERROR:") || detail.RetryCount != 2 || len(detail.Attempts) != 3 {
		t.Fatalf("耗尽重试记录异常: %+v", detail)
	}
}

func TestAgentInvalidAnswerFallsBackAndRecordsAttempts(t *testing.T) {
	s, web, _ := setupServer(t)
	s.solver.retryDelay = func(int) time.Duration { return 0 }
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Tools []any `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		content := "<answer>错</answer>"
		if len(body.Tools) > 0 {
			content = "<answer>B</answer>"
		}
		jsonResponse(w, 200, map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": content}, "finish_reason": "stop"}},
		})
	}))
	defer fake.Close()
	configureAgent(t, s, ServiceConfig{Protocol: ProtocolOpenAIChat, BaseURL: fake.URL, APIKey: "test-key"},
		ModelConfig{Model: "agent-model", Vision: CapabilityOff, ToolCall: CapabilityOn}, 3)

	var res struct{ Answer string }
	requestJSON(t, web, "POST", "/api/search", map[string]string{
		"title": "HTTP 是有状态协议。", "options": "对\n错", "type": "judgement",
	}, 200, &res)
	if res.Answer != "错" {
		t.Fatalf("Agent 判断题校验回退异常: %+v", res)
	}
	var detail Record
	requestJSON(t, web, "GET", "/api/records/1", nil, 200, &detail)
	if !strings.Contains(detail.AgentError, "回退") || len(detail.Attempts) != 2 || detail.Attempts[0].Stage != "agent" || detail.Attempts[0].Status != "invalid" || detail.Attempts[1].Status != "ok" {
		t.Fatalf("Agent 校验回退明细异常: %+v", detail)
	}
}
