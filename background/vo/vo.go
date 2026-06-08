package vo

// R 是统一的 JSON 响应包装
type R struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

func OK(data any) R {
	return R{Code: 0, Msg: "ok", Data: data}
}

func Err(code int, msg string) R {
	return R{Code: code, Msg: msg}
}

type AuthReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type AuthUserVO struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	CreatedAt int64  `json:"created_at"`
}

type AuthResp struct {
	Token     string     `json:"token"`
	ExpiresAt int64      `json:"expires_at"`
	User      AuthUserVO `json:"user"`
}

// CreateConversationReq POST /conversation 请求体
type CreateConversationReq struct {
	UserID string `json:"user_id"`
	Title  string `json:"title"`
}

// UpdateConversationReq PATCH /conversation/{id} 请求体
type UpdateConversationReq struct {
	Title string `json:"title" binding:"required"`
}

// CreateMessageReq POST /conversation/{id}/message 请求体
type CreateMessageReq struct {
	UserID          string `json:"user_id"`
	Query           string `json:"query" binding:"required"`
	ParentMessageID string `json:"parent_message_id"`
}

// ConversationVO GET /conversation 列表项
type ConversationVO struct {
	ConversationID string `json:"conversation_id"`
	UserID         string `json:"user_id"`
	Title          string `json:"title"`
	CreatedAt      int64  `json:"created_at"`
}

// RoundMessageVO 是一条 LLM round 消息的精简视图
type RoundMessageVO struct {
	Role      string       `json:"role"`                 // user / assistant / tool
	Content   string       `json:"content,omitempty"`    // 文本内容
	ToolCalls []ToolCallVO `json:"tool_calls,omitempty"` // assistant 发起的 tool call
	ToolName  string       `json:"tool_name,omitempty"`  // tool 消息的工具名
	ToolID    string       `json:"tool_id,omitempty"`    // tool 消息对应的 call_id
}

// ToolCallVO 是一次 tool call 的精简视图
type ToolCallVO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatMessageVO GET /conversation/{id}/message 列表项
type ChatMessageVO struct {
	MessageID       string           `json:"message_id"`
	ConversationID  string           `json:"conversation_id"`
	ParentMessageID string           `json:"parent_message_id"`
	Query           string           `json:"query"`
	Response        string           `json:"response"`
	Model           string           `json:"model"`
	CreatedAt       int64            `json:"created_at"`
	Rounds          []RoundMessageVO `json:"rounds,omitempty"`
}

type PPTTemplateVO struct {
	Name           string   `json:"name"`
	Label          string   `json:"label"`
	Categories     []string `json:"categories,omitempty"`
	Summary        string   `json:"summary,omitempty"`
	Tone           string   `json:"tone,omitempty"`
	ThemeMode      string   `json:"theme_mode,omitempty"`
	Keywords       []string `json:"keywords,omitempty"`
	PreviewSVGURL  string   `json:"preview_svg_url,omitempty"`
	PreviewSVGURLs []string `json:"preview_svg_urls,omitempty"`
	AssetURLs      []string `json:"asset_urls,omitempty"`
}
