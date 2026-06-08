package agent

const (
	EventError             = "error"
	EventReasoning         = "reasoning"
	EventContent           = "content"
	EventToolCall          = "tool_call"
	EventToolResult        = "tool_result"
	EventPPTProjectCreated = "ppt_project_created"
	EventPPTPageSVG        = "ppt_page_svg"
	EventPPTExported       = "ppt_exported"
)

// StreamEvent 是 agent 内部流式输出的事件类型，与传输层无关
type StreamEvent struct {
	Event            string
	Content          string
	ReasoningContent string
	ToolCallID       string
	ToolCall         string
	ToolArguments    string
	ToolResult       string
	PPTProjectName   string
	PPTProjectPath   string
	PPTPageIndex     int
	PPTFileName      string
	PPTSVGContent    string
	PPTPPTXURL       string
	// Stage 标识当前事件所属的 PPT 流水线阶段；非 PPT 会话或 legacy 模式留空。
	Stage string
}
