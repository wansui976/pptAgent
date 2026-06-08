package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type EditTool struct {
	guard pathGuard
}

func NewEditTool() *EditTool {
	return &EditTool{}
}

func NewEditToolWithRoot(root string) *EditTool {
	return &EditTool{guard: newPathGuard(root)}
}

type EditToolParam struct {
	Path       string `json:"path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (t *EditTool) ToolName() AgentTool {
	return AgentToolEdit
}

func (t *EditTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolEdit),
		Description: openai.String("edit a file by replacing an existing string; old_string must be unique unless replace_all is true"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "file path to edit. Allowed root: " + t.guard.describe(),
				},
				"old_string": map[string]any{
					"type":        "string",
					"description": "existing text to replace",
				},
				"new_string": map[string]any{
					"type":        "string",
					"description": "replacement text",
				},
				"replace_all": map[string]any{
					"type":        "boolean",
					"description": "replace all matches instead of requiring a unique match",
				},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
	})
}

func (t *EditTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	var p EditToolParam
	if err := json.Unmarshal([]byte(argumentsInJSON), &p); err != nil {
		return "", err
	}
	if p.OldString == "" {
		return "", fmt.Errorf("old_string must not be empty")
	}

	resolvedPath, err := t.guard.resolve(p.Path, false)
	if err != nil {
		return "", err
	}

	contentBytes, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", err
	}
	content := string(contentBytes)
	matchCount := strings.Count(content, p.OldString)
	if matchCount == 0 {
		return "", fmt.Errorf("old_string not found in %s", resolvedPath)
	}
	if !p.ReplaceAll && matchCount != 1 {
		return "", fmt.Errorf("old_string must match exactly once in %s, found %d matches", resolvedPath, matchCount)
	}

	var updated string
	if p.ReplaceAll {
		updated = strings.ReplaceAll(content, p.OldString, p.NewString)
	} else {
		updated = strings.Replace(content, p.OldString, p.NewString, 1)
	}

	if err := os.WriteFile(resolvedPath, []byte(updated), 0o644); err != nil {
		return "", err
	}
	return fmt.Sprintf("edited %s (%d replacement%s)", resolvedPath, matchCount, pluralSuffix(matchCount)), nil
}

func pluralSuffix(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
