package tool

import (
	"context"

	"github.com/openai/openai-go/v3"
)

type AgentTool = string

const (
	AgentToolRead             AgentTool = "read"
	AgentToolWrite            AgentTool = "write"
	AgentToolEdit             AgentTool = "edit"
	AgentToolBash             AgentTool = "bash"
	AgentToolLoadStorage      AgentTool = "load_storage"
	AgentToolLoadSkill        AgentTool = "load_skill"
	AgentToolListPPTTemplates AgentTool = "list_ppt_templates"
	AgentToolWebSearch        AgentTool = "web_search"
)

type Tool interface {
	ToolName() AgentTool
	Info() openai.ChatCompletionToolUnionParam
	Execute(ctx context.Context, argumentsInJSON string) (string, error)
}

type HostPreferredTool interface {
	HostTool() Tool
}
