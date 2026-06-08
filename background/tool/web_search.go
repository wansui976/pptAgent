package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/openai/openai-go/v3"
	openaiShared "github.com/openai/openai-go/v3/shared"

	"lingxi/background/shared"
)

// researchBriefRuneLimit 是 web_search 返回内容的字符上限。
// 4K tokens ≈ 中文 6000-8000 字符（保守上限），与流水线 outline 阶段的上下文预算对齐。
// 超长会被截断并附 "...(truncated)" 标记，避免挤压后续 stage。
const researchBriefRuneLimit = 6000

// WebSearchTool 暴露给 agent loop 的联网搜索工具，底层走 Qwen DashScope。
type WebSearchTool struct {
	client *shared.QwenSearchClient
}

func NewWebSearchTool(client *shared.QwenSearchClient) *WebSearchTool {
	return &WebSearchTool{client: client}
}

type WebSearchParam struct {
	Query       string `json:"query"`
	Instruction string `json:"instruction,omitempty"`
	Freshness   string `json:"freshness,omitempty"`
	MaxResults  int    `json:"max_results,omitempty"`
}

func (t *WebSearchTool) ToolName() AgentTool { return AgentToolWebSearch }

func (t *WebSearchTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(openaiShared.FunctionDefinitionParam{
		Name: string(AgentToolWebSearch),
		Description: openai.String(
			"Search the public web via Qwen and return a structured Chinese summary with key facts and citations. " +
				"Use this during the PPT research stage to gather background before drafting the outline. " +
				"Do NOT use this for translation, math, or already-known facts.",
		),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Topic or question to research. Required.",
				},
				"instruction": map[string]any{
					"type":        "string",
					"description": "Optional output guidance, e.g. 'list 6-8 key facts with dates'.",
				},
				"freshness": map[string]any{
					"type":        "string",
					"description": "Optional time window in days, e.g. '7', '30', '180', '365'.",
				},
				"max_results": map[string]any{
					"type":        "integer",
					"description": "Optional hint, the underlying search API picks the actual count.",
				},
			},
			"required": []string{"query"},
		},
	})
}

func (t *WebSearchTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	if t.client == nil || !t.client.Configured() {
		// 中性错误：避免把 config.json 路径泄露进 LLM 上下文，可能被模型回显给前端用户。
		return "", fmt.Errorf("web search is currently unavailable")
	}

	var p WebSearchParam
	if err := json.Unmarshal([]byte(argumentsInJSON), &p); err != nil {
		return "", fmt.Errorf("invalid web_search arguments: %w", err)
	}
	query := strings.TrimSpace(p.Query)
	if query == "" {
		return "", fmt.Errorf("web_search.query is required")
	}

	opts := &shared.QwenSearchOptions{
		SearchStrategy: "turbo",
		EnableSource:   true,
	}
	if p.Freshness != "" {
		opts.Freshness = p.Freshness
	}

	res, err := t.client.Search(ctx, query, p.Instruction, opts)
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(res.Content)
	if utf8.RuneCountInString(content) > researchBriefRuneLimit {
		runes := []rune(content)
		content = string(runes[:researchBriefRuneLimit]) + "\n\n...(truncated, exceeded brief limit)"
	}
	return content, nil
}
