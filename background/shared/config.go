package shared

import (
	"encoding/json"
	"os"
)

type AppConfig struct {
	LLMProviders struct {
		FrontModel ModelConfig `json:"front_model"`
		BackModel  ModelConfig `json:"back_model"`
		QwenSearch ModelConfig `json:"qwen_search"`
	} `json:"llm_providers"`
	Paths         PathsConfig `json:"paths"`
	PPTPipelineV2 bool        `json:"ppt_pipeline_v2"`
}

type PathsConfig struct {
	WorkspaceRoot string `json:"workspace_root"`
	FrontRoot     string `json:"front_root"`
	PPTistRoot    string `json:"pptist_root"`
	PPTMasterRoot string `json:"ppt_master_root"`
}

type ModelConfig struct {
	BaseURL string `json:"base_url"`
	ApiKey  string `json:"api_key"`
	Model   string `json:"model"`

	ContextWindow int `json:"context_window"`
}

func NewModelConfig() ModelConfig {
	return ModelConfig{
		BaseURL:       getEnvDefault("OPENAI_BASE_URL", "https://api.openai.com/v1"),
		ApiKey:        getEnvDefault("OPENAI_API_KEY", ""),
		Model:         getEnvDefault("OPENAI_MODEL", "gpt-5.2"),
		ContextWindow: 200000,
	}
}

func getEnvDefault(key, defaultValue string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return defaultValue
}

func LoadAppConfig(path string) (AppConfig, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return AppConfig{}, err
	}
	var config AppConfig
	err = json.Unmarshal(content, &config)
	if err != nil {
		return AppConfig{}, err
	}
	return config, nil
}
