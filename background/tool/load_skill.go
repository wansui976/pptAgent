package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// LoadSkillTool loads full skill content into the conversation
type LoadSkillTool struct {
	pptMasterRoot string
}

// NewLoadSkillTool creates a new load_skill tool
func NewLoadSkillTool() *LoadSkillTool {
	return &LoadSkillTool{}
}

// NewLoadSkillToolWithRoot creates a load_skill tool that resolves skills under
// the given ppt-master root before falling back to legacy locations.
func NewLoadSkillToolWithRoot(pptMasterRoot string) *LoadSkillTool {
	return &LoadSkillTool{pptMasterRoot: pptMasterRoot}
}

type LoadSkillParam struct {
	Name string `json:"name"`
}

func (t *LoadSkillTool) ToolName() AgentTool {
	return AgentToolLoadSkill
}

func (t *LoadSkillTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolLoadSkill),
		Description: openai.String("Load the full content and instructions for a specific skill. Use this when you need detailed guidance for a task that matches a skill's purpose."),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{
					"type":        "string",
					"description": "The skill ID to load (e.g., 'code-review', 'debug')",
				},
			},
			"required": []string{"name"},
		},
	})
}

func (t *LoadSkillTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	p := LoadSkillParam{}
	err := json.Unmarshal([]byte(argumentsInJSON), &p)
	if err != nil {
		return "", err
	}

	if p.Name == "" {
		return "", fmt.Errorf("skill name is required")
	}

	skillPath, err := resolveSkillPath(p.Name, t.pptMasterRoot)
	if err != nil {
		return "", err
	}

	content, err := os.ReadFile(skillPath)
	if err != nil {
		return "", fmt.Errorf("failed to read skill file '%s': %w", skillPath, err)
	}

	skillDir := filepath.Dir(skillPath)
	text := strings.ReplaceAll(string(content), "${SKILL_DIR}", skillDir)
	return text, nil
}

func resolveSkillPath(name string, pptMasterRoot string) (string, error) {
	candidates := make([]string, 0, 3)
	if pptMasterRoot != "" {
		candidates = append(candidates, filepath.Join(pptMasterRoot, "skills", name, "SKILL.md"))
	}
	candidates = append(candidates, filepath.Join("/workspace", "ppt-master", "skills", name, "SKILL.md"))
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "..", "ppt-master", "skills", name, "SKILL.md"))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("skill '%s' not found in known skill directories", name)
}
