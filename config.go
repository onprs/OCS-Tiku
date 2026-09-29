package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"gopkg.in/yaml.v3"
)

type ModelConfig struct {
	Provider        string `json:"provider" yaml:"provider"`
	Model           string `json:"model" yaml:"model"`
	APIKey          string `json:"api_key" yaml:"api_key"`
	BaseURL         string `json:"base_url" yaml:"base_url"`
	ReasoningEffort string `json:"reasoning_effort,omitempty" yaml:"reasoning_effort,omitempty"`
}

type AnswerConfig struct {
	Mode         string  `json:"mode" yaml:"mode"`
	Temperature  float64 `json:"temperature" yaml:"temperature"`
	SystemPrompt string  `json:"system_prompt" yaml:"system_prompt"`
}

type fileConfig struct {
	Server struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"server"`
	Models struct {
		Text   ModelConfig `yaml:"text"`
		Vision ModelConfig `yaml:"vision"`
	} `yaml:"models"`
	Answer AnswerConfig `yaml:"answer"`
}

type Config struct {
	mu   sync.RWMutex
	path string
	data fileConfig
}

func defaults() fileConfig {
	var c fileConfig
	c.Server.Host, c.Server.Port = "127.0.0.1", 8000
	c.Models.Text = ModelConfig{Provider: "deepseek", Model: "deepseek-v4-flash", BaseURL: "https://api.deepseek.com", ReasoningEffort: "max"}
	c.Models.Vision = ModelConfig{Model: "gpt-5.4-mini", BaseURL: "https://api.openai.com/v1"}
	c.Answer = AnswerConfig{Mode: "stepwise", Temperature: 0.1, SystemPrompt: "你是一个大学生网课答题助手。请根据题目和选项，在<answer>标签内输出正确答案。\n\n格式要求：\n单选题：<answer>A</answer>\n多选题：<answer>A#B#C</answer>\n判断题：<answer>对</answer> 或 <answer>错</answer>\n填空题：<answer>填空内容</answer>（多个空用#分隔）\n\n只输出<answer>标签，禁止任何额外文字、分析或解释。"}
	return c
}

func loadConfig(path string) (*Config, error) {
	c := &Config{path: path, data: defaults()}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, c.save()
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(b, &c.data); err != nil {
		return nil, fmt.Errorf("读取配置失败: %w", err)
	}
	if c.data.Server.Port < 1 || c.data.Server.Port > 65535 {
		return nil, fmt.Errorf("配置端口无效: %d", c.data.Server.Port)
	}
	return c, nil
}

func (c *Config) save() error {
	b, err := yaml.Marshal(c.data)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(c.path), ".config-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if _, err = f.Write(b); err != nil {
		return err
	}
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), c.path); err != nil {
		return fmt.Errorf("替换配置文件失败: %w", err)
	}
	return nil
}

func (c *Config) snapshot() fileConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v := c.data
	for key, field := range map[string]*string{
		"OCS_TEXT_API_KEY": &v.Models.Text.APIKey, "OCS_TEXT_BASE_URL": &v.Models.Text.BaseURL, "OCS_TEXT_MODEL": &v.Models.Text.Model,
		"OCS_VISION_API_KEY": &v.Models.Vision.APIKey, "OCS_VISION_BASE_URL": &v.Models.Vision.BaseURL, "OCS_VISION_MODEL": &v.Models.Vision.Model,
	} {
		if value := os.Getenv(key); value != "" {
			*field = value
		}
	}
	if value := os.Getenv("OCS_PORT"); value != "" {
		if port, err := strconv.Atoi(value); err == nil && port > 0 && port <= 65535 {
			v.Server.Port = port
		}
	}
	return v
}

func (c *Config) updateModel(kind string, v ModelConfig) error {
	if kind == "text" && v.Provider != "" && v.Provider != "deepseek" && v.Provider != "openai_compat" {
		return errors.New("未知文本模型提供方")
	}
	if kind == "vision" && v.Provider != "" && v.Provider != "openai_vl" {
		return errors.New("未知视觉模型提供方")
	}
	if kind != "text" && kind != "vision" {
		return errors.New("未知模型类型")
	}
	if v.Provider != "" && (v.Model == "" || v.BaseURL == "") {
		return errors.New("请填写模型名称和 API 地址")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.data
	if kind == "text" {
		c.data.Models.Text = v
	} else {
		c.data.Models.Vision = v
	}
	if err := c.save(); err != nil {
		c.data = old
		return err
	}
	return nil
}

func (c *Config) updateAnswer(v AnswerConfig) error {
	if v.Mode != "stepwise" && v.Mode != "direct" {
		return errors.New("未知答题模式")
	}
	if v.Temperature < 0 || v.Temperature > 2 {
		return errors.New("温度必须在 0 到 2 之间")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.data
	c.data.Answer = v
	if err := c.save(); err != nil {
		c.data = old
		return err
	}
	return nil
}
