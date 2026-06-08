package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

type ReadTool struct {
	guard pathGuard
}

func NewReadTool() *ReadTool {
	return &ReadTool{}
}

func NewReadToolWithRoots(roots ...string) *ReadTool {
	return &ReadTool{guard: newPathGuard(roots...)}
}

type ReadToolParam struct {
	Path string `json:"path"`
}

func (t *ReadTool) ToolName() AgentTool {
	return AgentToolRead
}

func (t *ReadTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolRead),
		Description: openai.String("read file content or list directory contents"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "the file path to read. Allowed roots: " + t.guard.describe(),
				},
			},
			"required": []string{"path"},
		},
	})
}

func (t *ReadTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := ReadToolParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	resolvedPath, err := t.guard.resolve(p.Path, false)
	if err != nil {
		return "", err
	}

	file, err := os.Open(resolvedPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return "", err
	}
	if fileInfo.IsDir() {
		return formatDirectoryListing(resolvedPath, file)
	}

	content, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

func formatDirectoryListing(path string, dir *os.File) (string, error) {
	entries, err := dir.Readdir(-1)
	if err != nil {
		return "", err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	result := fmt.Sprintf("directory: %s\n", path)
	result += "mode         size      modified              name\n"
	for _, entry := range entries {
		result += fmt.Sprintf("%-12s %-9d %-21s %s\n",
			entry.Mode().String(),
			entry.Size(),
			entry.ModTime().Format("2006-01-02 15:04:05"),
			entry.Name(),
		)
	}
	return result, nil
}
