package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// fallbackPPTLayoutsIndexPath 在没有显式配置 ppt-master root 时使用，
// 与容器里 docker bash 工具挂载的 /workspace/ppt-master 对齐。
const fallbackPPTLayoutsIndexPath = "/workspace/ppt-master/skills/ppt-master/templates/layouts/layouts_index.json"

type ListPPTTemplatesTool struct {
	indexPath string
}

func NewListPPTTemplatesTool() *ListPPTTemplatesTool {
	return &ListPPTTemplatesTool{indexPath: fallbackPPTLayoutsIndexPath}
}

func NewListPPTTemplatesToolWithRoot(pptMasterRoot string) *ListPPTTemplatesTool {
	if pptMasterRoot == "" {
		return NewListPPTTemplatesTool()
	}
	return &ListPPTTemplatesTool{
		indexPath: filepath.Join(pptMasterRoot, "skills/ppt-master/templates/layouts/layouts_index.json"),
	}
}

func (t *ListPPTTemplatesTool) ToolName() AgentTool {
	return AgentToolListPPTTemplates
}

func (t *ListPPTTemplatesTool) Info() openai.ChatCompletionToolUnionParam {
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        string(AgentToolListPPTTemplates),
		Description: openai.String("List available ppt-master layout templates with category, label, summary, tone, theme mode, and keywords. Use this tool before discussing available PPT templates."),
		Parameters: openai.FunctionParameters{
			"type":       "object",
			"properties": map[string]any{},
		},
	})
}

func (t *ListPPTTemplatesTool) Execute(ctx context.Context, argumentsInJSON string) (string, error) {
	_ = ctx
	_ = argumentsInJSON

	indexPath := t.indexPath
	if indexPath == "" {
		indexPath = fallbackPPTLayoutsIndexPath
	}
	content, err := os.ReadFile(indexPath)
	if err != nil {
		return "", fmt.Errorf("failed to read ppt templates index %q: %w", indexPath, err)
	}

	var index struct {
		Meta struct {
			Total         int      `json:"total"`
			Formats       []string `json:"formats"`
			Updated       string   `json:"updated"`
			StandardFiles []string `json:"standardFiles"`
		} `json:"meta"`
		Categories map[string]struct {
			Label   string   `json:"label"`
			Layouts []string `json:"layouts"`
		} `json:"categories"`
		QuickLookup map[string][]string `json:"quickLookup"`
		Layouts     map[string]struct {
			Label     string   `json:"label"`
			Summary   string   `json:"summary"`
			Tone      string   `json:"tone"`
			ThemeMode string   `json:"themeMode"`
			Keywords  []string `json:"keywords"`
			Assets    []string `json:"assets"`
		} `json:"layouts"`
	}
	if err := json.Unmarshal(content, &index); err != nil {
		return "", fmt.Errorf("failed to parse ppt templates index: %w", err)
	}

	categoryByLayout := make(map[string][]string)
	for categoryKey, category := range index.Categories {
		label := category.Label
		if label == "" {
			label = categoryKey
		}
		for _, layout := range category.Layouts {
			categoryByLayout[layout] = append(categoryByLayout[layout], label)
		}
	}

	type templateInfo struct {
		Name       string   `json:"name"`
		Label      string   `json:"label"`
		Categories []string `json:"categories,omitempty"`
		Summary    string   `json:"summary,omitempty"`
		Tone       string   `json:"tone,omitempty"`
		ThemeMode  string   `json:"theme_mode,omitempty"`
		Keywords   []string `json:"keywords,omitempty"`
		Assets     []string `json:"assets,omitempty"`
		PreviewDir string   `json:"preview_dir,omitempty"`
	}

	names := make([]string, 0, len(index.Layouts))
	for name := range index.Layouts {
		names = append(names, name)
	}
	sort.Strings(names)

	templates := make([]templateInfo, 0, len(names))
	for _, name := range names {
		layout := index.Layouts[name]
		categories := append([]string(nil), categoryByLayout[name]...)
		sort.Strings(categories)
		templates = append(templates, templateInfo{
			Name:       name,
			Label:      layout.Label,
			Categories: categories,
			Summary:    layout.Summary,
			Tone:       layout.Tone,
			ThemeMode:  layout.ThemeMode,
			Keywords:   layout.Keywords,
			Assets:     layout.Assets,
			PreviewDir: filepath.Join(filepath.Dir(indexPath), name),
		})
	}

	result := map[string]any{
		"meta": map[string]any{
			"total":          index.Meta.Total,
			"formats":        index.Meta.Formats,
			"updated":        index.Meta.Updated,
			"standard_files": index.Meta.StandardFiles,
			"index_path":     indexPath,
		},
		"quick_lookup": index.QuickLookup,
		"templates":    templates,
	}

	pretty, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return string(pretty), nil
}
