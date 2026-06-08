package vo

const (
	SSETypeError             = "error"
	SSETypeReasoning         = "reasoning"
	SSETypeContent           = "content"
	SSETypeToolCall          = "tool_call"
	SSETypeToolResult        = "tool_result"
	SSETypePPTProjectCreated = "ppt_project_created"
	SSETypePPTPageSVG        = "ppt_page_svg"
	SSETypePPTExported       = "ppt_exported"
)

type SSEMessageVO struct {
	MessageID        string  `json:"message_id"`
	Event            string  `json:"event"`
	Stage            string  `json:"stage,omitempty"`
	Content          *string `json:"content,omitempty"`
	ReasoningContent *string `json:"reasoning_content,omitempty"`
	ToolCallID       *string `json:"tool_call_id,omitempty"`
	ToolCall         *string `json:"tool_call,omitempty"`
	ToolArguments    *string `json:"tool_arguments,omitempty"`
	ToolResult       *string `json:"tool_result,omitempty"`
	PPTProjectName   *string `json:"ppt_project_name,omitempty"`
	PPTProjectPath   *string `json:"ppt_project_path,omitempty"`
	PPTPageIndex     *int    `json:"ppt_page_index,omitempty"`
	PPTFileName      *string `json:"ppt_file_name,omitempty"`
	PPTSVGContent    *string `json:"ppt_svg_content,omitempty"`
	PPTPPTXURL       *string `json:"ppt_pptx_url,omitempty"`
}
