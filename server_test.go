package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func setupServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	cfg, err := loadConfig(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := openStore(filepath.Join(dir, "ocs-tiku.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.close() })
	images, err := newImageProcessor(filepath.Join(dir, "images"))
	if err != nil {
		t.Fatal(err)
	}
	assets := filepath.Join(dir, "dist")
	if err := os.MkdirAll(assets, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(assets, "index.html"), []byte("<html>OCS app</html>"), 0600); err != nil {
		t.Fatal(err)
	}
	models := newModelClient()
	s := &Server{config: cfg, store: store, models: models, assets: assets, imageDir: images.dir}
	s.solver = &Solver{config: cfg, models: models, images: images}
	web := httptest.NewServer(s.handler())
	t.Cleanup(web.Close)
	return s, web, dir
}

func requestJSON(t *testing.T, web *httptest.Server, method, path string, value any, status int, out any) {
	t.Helper()
	var body io.Reader
	if value != nil {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, web.URL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if value != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: 状态 %d, 预期 %d, 内容 %s", method, path, resp.StatusCode, status, b)
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSearchAndRecords(t *testing.T) {
	s, web, _ := setupServer(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer demo" {
			t.Errorf("模型请求错误: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": "<answer>A#B</answer>", "reasoning_content": "思考"}}}})
	}))
	defer fake.Close()
	cfg := s.config.snapshot().Models.Text
	cfg.Provider, cfg.BaseURL, cfg.APIKey, cfg.Model = "openai_compat", fake.URL+"/v1", "demo", "test-text"
	if err := s.config.updateModel("text", cfg); err != nil {
		t.Fatal(err)
	}
	var response struct{ Question, Answer string }
	requestJSON(t, web, "POST", "/api/search", map[string]string{"title": "<b>题目</b>", "options": "选择一\n选择二", "type": "multiple"}, 200, &response)
	if response.Answer != "A#B" || response.Question != "<b>题目</b>" {
		t.Fatalf("搜索响应异常: %+v", response)
	}
	var records struct {
		Records []Record `json:"records"`
		Total   int      `json:"total"`
		Pages   int      `json:"pages"`
	}
	requestJSON(t, web, "GET", "/api/records?page=1&limit=1", nil, 200, &records)
	if records.Total != 1 || len(records.Records) != 1 || records.Records[0].TotalTimeMS < 0 || !strings.Contains(records.Records[0].FinalPrompt, "A. 选择一") || records.Records[0].Reasoning != "思考" {
		t.Fatalf("记录异常: %+v", records)
	}
	id := records.Records[0].ID
	var detail Record
	requestJSON(t, web, "GET", "/api/records/"+itoa(id), nil, 200, &detail)
	if detail.Answer != "A#B" || detail.Status != "complete" {
		t.Fatalf("详情异常: %+v", detail)
	}
	var recent []Record
	requestJSON(t, web, "GET", "/api/records/recent?since="+itoa(id), nil, 200, &recent)
	if len(recent) != 0 {
		t.Fatalf("recent 应为空: %+v", recent)
	}
	requestJSON(t, web, "GET", "/api/records?page=0", nil, 400, nil)
	requestJSON(t, web, "GET", "/api/records/999", nil, 404, nil)
	var stats Stats
	requestJSON(t, web, "GET", "/api/stats", nil, 200, &stats)
	if stats.Total != 1 || stats.Today != 1 || stats.SuccessRate != 100 || stats.TextModel != "OpenAI Compat (test-text)" {
		t.Fatalf("统计异常: %+v", stats)
	}
	var dashboard struct {
		Daily []DailyCount `json:"daily_counts"`
	}
	requestJSON(t, web, "GET", "/api/dashboard", nil, 200, &dashboard)
	if len(dashboard.Daily) != 1 || dashboard.Daily[0].Count != 1 {
		t.Fatalf("图表异常: %+v", dashboard)
	}
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

func TestImagesModesAndSettings(t *testing.T) {
	s, web, dir := setupServer(t)
	var mu sync.Mutex
	var calls []map[string]any
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		calls = append(calls, body)
		mu.Unlock()
		messages := body["messages"].([]any)
		answer := "<answer>B</answer>"
		if len(messages) == 1 {
			answer = "图中文字"
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": answer}}}})
	}))
	defer fake.Close()
	text := s.config.snapshot().Models.Text
	text.Provider, text.APIKey, text.Model, text.BaseURL = "openai_compat", "demo", "text", fake.URL
	vision := s.config.snapshot().Models.Vision
	vision.Provider, vision.APIKey, vision.Model, vision.BaseURL = "openai_vl", "demo", "vision", fake.URL
	requestJSON(t, web, "PUT", "/api/settings/text", text, 200, nil)
	requestJSON(t, web, "PUT", "/api/settings/vision", vision, 200, nil)
	requestJSON(t, web, "GET", "/api/test-text", nil, 200, nil)
	requestJSON(t, web, "GET", "/api/test-vision", nil, 200, nil)
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	s.solver.images.fetch = func(_ context.Context, _ string) ([]byte, error) { return pngData.Bytes(), nil }
	input := map[string]string{"title": "题目 https://example.org/a.png", "options": "答案甲\n答案乙", "type": "single"}
	var result struct{ Question, Answer string }
	requestJSON(t, web, "POST", "/api/search", input, 200, &result)
	if result.Answer != "B" || !strings.Contains(result.Question, "[图片: 图中文字]") {
		t.Fatalf("分步识别异常: %+v", result)
	}
	var item Record
	requestJSON(t, web, "GET", "/api/records/1", nil, 200, &item)
	if len(item.Images) != 1 || item.Images[0].Status != "done" || !strings.HasPrefix(item.Images[0].Src, "/images/") {
		t.Fatalf("图片记录异常: %+v", item.Images)
	}
	resp, err := http.Get(web.URL + item.Images[0].Src)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("图片读取失败: %d", resp.StatusCode)
	}
	cfg := s.config.snapshot().Answer
	cfg.Mode = "direct"
	requestJSON(t, web, "PUT", "/api/settings/answer", cfg, 200, nil)
	requestJSON(t, web, "POST", "/api/search", input, 200, &result)
	requestJSON(t, web, "GET", "/api/records/2", nil, 200, &item)
	if !strings.Contains(item.TextModel, "(direct)") || item.Images[0].Status != "done" || item.TotalTimeMS < 0 {
		t.Fatalf("直答记录异常: %+v", item)
	}
	mu.Lock()
	defer mu.Unlock()
	foundImage := false
	for _, call := range calls {
		messages := call["messages"].([]any)
		if len(messages) == 2 {
			user := messages[1].(map[string]any)
			if parts, ok := user["content"].([]any); ok {
				for _, part := range parts {
					if part.(map[string]any)["type"] == "image_url" {
						foundImage = true
					}
				}
			}
		}
	}
	if !foundImage {
		t.Fatal("直答模式没有发送图片")
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.yaml"))
	if err != nil || !strings.Contains(string(b), "mode: direct") {
		t.Fatalf("设置未保存: %v", err)
	}
}

func TestFailedSearchAndRoutes(t *testing.T) {
	_, web, _ := setupServer(t)
	var failure map[string]string
	requestJSON(t, web, "POST", "/api/search", map[string]string{"title": "未配置的模型"}, 500, &failure)
	if failure["answer"] != "" || !strings.Contains(failure["error"], "API Key") {
		t.Fatalf("错误响应异常: %v", failure)
	}
	var item Record
	requestJSON(t, web, "GET", "/api/records/1", nil, 200, &item)
	if !strings.HasPrefix(item.Answer, "ERROR:") {
		t.Fatalf("错误记录未保存: %+v", item)
	}
	var stats Stats
	requestJSON(t, web, "GET", "/api/stats", nil, 200, &stats)
	if stats.SuccessRate != 0 {
		t.Fatalf("失败计数异常: %+v", stats)
	}
	var conf []map[string]any
	requestJSON(t, web, "GET", "/api/config", nil, 200, &conf)
	if len(conf) != 1 || conf[0]["handler"] != "return (res)=>[res.question, res.answer]" {
		t.Fatalf("OCS 配置异常: %v", conf)
	}
	requestJSON(t, web, "GET", "/api/absent", nil, 404, nil)
	requestJSON(t, web, "GET", "/missing.js", nil, 404, nil)
	for _, pathname := range []string{"/", "/records", "/settings"} {
		resp, err := http.Get(web.URL + pathname)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || resp.Header.Get("Cache-Control") != "no-cache, no-store, must-revalidate" || !bytes.Contains(body, []byte("OCS app")) {
			t.Fatalf("SPA 路由 %s 异常: %d, %v", pathname, resp.StatusCode, err)
		}
	}
}

func TestOldDatabaseMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ocs-tiku.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE records (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp TEXT NOT NULL, type TEXT NOT NULL,
		raw_title TEXT DEFAULT '', raw_options TEXT DEFAULT '', processed_title TEXT DEFAULT '', processed_options TEXT DEFAULT '',
		images TEXT DEFAULT '[]', final_prompt TEXT DEFAULT '', answer TEXT DEFAULT '', text_model TEXT DEFAULT '',
		vision_model TEXT DEFAULT '', total_time_ms INTEGER DEFAULT 0);
		INSERT INTO records (timestamp,type,answer) VALUES (datetime('now'),'single','A')`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	store, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.close()
	r, err := store.get(context.Background(), 1)
	if err != nil || r.Answer != "A" || r.Status != "complete" {
		t.Fatalf("旧记录迁移失败: %+v %v", r, err)
	}
}

func TestLargeImageIsReduced(t *testing.T) {
	p, err := newImageProcessor(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 1400, 1400))
	noise := rand.New(rand.NewSource(42))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i] = byte(noise.Intn(256))
		img.Pix[i+1] = byte(noise.Intn(256))
		img.Pix[i+2] = byte(noise.Intn(256))
		img.Pix[i+3] = 255
	}
	var input bytes.Buffer
	if err := png.Encode(&input, img); err != nil {
		t.Fatal(err)
	}
	if input.Len() <= 5<<20 {
		t.Fatalf("测试图片过小: %d", input.Len())
	}
	p.http = &http.Client{Transport: imageRoundTripper{data: input.Bytes()}}
	result, err := p.download(context.Background(), "https://example.org/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if len(result) >= input.Len() {
		t.Fatal("大图未缩小")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(result))
	if err != nil || cfg.Width != 1024 || cfg.Height != 1024 {
		t.Fatalf("图片尺寸异常: %dx%d %v", cfg.Width, cfg.Height, err)
	}
}

type imageRoundTripper struct{ data []byte }

func (tr imageRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(tr.data))}, nil
}

func TestBlockedImageTargets(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1/p.png", "http://192.168.1.1/a", "http://localhost/a", "file:///tmp/a", "http://user:pass@example.org/a"} {
		u, _ := url.Parse(raw)
		if err := validateImageURL(u); err == nil {
			t.Errorf("未拦截 %s", raw)
		}
	}
}
