package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type WriteTool struct {
	guard pathGuard
}

func NewWriteTool() *WriteTool {
	return &WriteTool{}
}

func NewWriteToolWithRoot(root string) *WriteTool {
	return &WriteTool{guard: newPathGuard(root)}
}

type WriteToolParam struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

func (t *WriteTool) ToolName() AgentTool {
	return AgentToolWrite
}

func (t *WriteTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolWrite),
		Description: openai.String("write content to an absolute file path, creating parent directories when needed"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "file path to write. Allowed root: " + t.guard.describe(),
				},
				"content": map[string]any{
					"type":        "string",
					"description": "full file content to write",
				},
			},
			"required": []string{"path", "content"},
		},
	})
}

func (t *WriteTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	var p WriteToolParam
	if err := json.Unmarshal([]byte(argumentsInJSON), &p); err != nil {
		return "", err
	}
	resolvedPath, err := t.guard.resolve(p.Path, true)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(resolvedPath, []byte(p.Content), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("wrote %d bytes to %s", len([]byte(p.Content)), resolvedPath), nil
}
