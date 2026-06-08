package shared

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// QwenSearchOptions 控制 DashScope 联网搜索行为。
// 字段名映射 https://help.aliyun.com/zh/model-studio/web-search 中的 search_options。
type QwenSearchOptions struct {
	SearchStrategy        string   `json:"search_strategy,omitempty"`
	EnableSource          bool     `json:"enable_source,omitempty"`
	EnableCitation        bool     `json:"enable_citation,omitempty"`
	EnableSearchExtension bool     `json:"enable_search_extension,omitempty"`
	Freshness             string   `json:"freshness,omitempty"`
	AssignedSiteList      []string `json:"assigned_site_list,omitempty"`
}

// QwenSearchRequest 是给 DashScope OpenAI 兼容端点的请求体。
// 我们没用 openai-go SDK 直接调，因为 enable_search/search_options 是非标准扩展字段，
// SDK 的 strong-typed 结构体不接收，手写 HTTP 更直接。
type qwenSearchRequest struct {
	Model         string              `json:"model"`
	Messages      []QwenSearchMessage `json:"messages"`
	EnableSearch  bool                `json:"enable_search"`
	SearchOptions *QwenSearchOptions  `json:"search_options,omitempty"`
	Temperature   float64             `json:"temperature,omitempty"`
}

type QwenSearchMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// QwenSearchResult 是单次搜索调用的归一化输出。
// Content 是模型整合后的中文摘要；Sources 在 OpenAI 兼容模式下大概率为空，
// 已知限制详见 help.aliyun.com 的 "OpenAI 兼容不支持返回搜索来源" 提示。
type QwenSearchResult struct {
	Content string
	Sources []QwenSearchSource
}

type QwenSearchSource struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type qwenSearchResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// QwenSearchClient 封装 DashScope 联网搜索调用。
// modelConf.BaseURL 应指向 https://dashscope.aliyuncs.com/compatible-mode/v1。
type QwenSearchClient struct {
	conf    ModelConfig
	httpCli *http.Client
}

func NewQwenSearchClient(conf ModelConfig) *QwenSearchClient {
	return &QwenSearchClient{
		conf:    conf,
		httpCli: &http.Client{Timeout: 60 * time.Second},
	}
}

// Configured 判断 Qwen 搜索是否可用。
// api_key 为空意味着用户没在 config.json 里配 dashscope，调用方应跳过联网搜索。
func (c *QwenSearchClient) Configured() bool {
	if c == nil {
		return false
	}
	return strings.TrimSpace(c.conf.ApiKey) != ""
}

// Search 触发一次联网搜索 + 模型摘要。
// query 是用户原始诉求；instruction 是给模型的额外引导（例如"输出 5 条要点"）。
func (c *QwenSearchClient) Search(ctx context.Context, query string, instruction string, opts *QwenSearchOptions) (QwenSearchResult, error) {
	if !c.Configured() {
		return QwenSearchResult{}, fmt.Errorf("qwen search not configured")
	}

	model := strings.TrimSpace(c.conf.Model)
	if model == "" {
		model = "qwen-plus"
	}
	if opts == nil {
		opts = &QwenSearchOptions{SearchStrategy: "turbo"}
	}

	systemPrompt := strings.TrimSpace(instruction)
	if systemPrompt == "" {
		systemPrompt = "你是资料调研助手。请用中文给出结构化摘要，包含关键事实、数据、争议点；每条都附来源标题或网址。"
	}

	body := qwenSearchRequest{
		Model: model,
		Messages: []QwenSearchMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: query},
		},
		EnableSearch:  true,
		SearchOptions: opts,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return QwenSearchResult{}, err
	}

	endpoint := strings.TrimRight(c.conf.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return QwenSearchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.conf.ApiKey)

	resp, err := c.httpCli.Do(req)
	if err != nil {
		return QwenSearchResult{}, err
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return QwenSearchResult{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return QwenSearchResult{}, fmt.Errorf("qwen search http %d: %s", resp.StatusCode, string(respBytes))
	}

	var parsed qwenSearchResponse
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return QwenSearchResult{}, fmt.Errorf("qwen search decode: %w (body=%s)", err, string(respBytes))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return QwenSearchResult{}, fmt.Errorf("qwen search api error: %s", parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return QwenSearchResult{}, fmt.Errorf("qwen search returned no choices")
	}

	return QwenSearchResult{Content: parsed.Choices[0].Message.Content}, nil
}
