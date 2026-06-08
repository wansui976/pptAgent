package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"lingxi/background/tool"
)

// PPTStage 表示新流水线的阶段标记。
// 状态机持久化在 ChatMessage.PPTStage 列；每条 SSE 通过 StreamEvent.Stage 透传给前端。
type PPTStage string

const (
	StageNone     PPTStage = ""       // 非 PPT 会话，或老 SKILL.md 自驱模式
	StageIntake   PPTStage = "intake" // 等用户选页数档位
	StageResearch PPTStage = "research"
	StageOutline  PPTStage = "outline" // 金字塔架构师阶段
	StageLayout   PPTStage = "layout"  // 选 layout_id（轻量）
	StageRender   PPTStage = "render"  // SVG 生成 + speaker notes（沿用旧 Executor）
	StageExport   PPTStage = "export"
	StageLegacy   PPTStage = "legacy" // 校验失败 2 次后回退；走旧 SKILL.md prompt 自驱
)

// PPTPipelineState 是单次会话的流水线状态快照。
// 后端 service 层负责：会话开始时从最近的 ChatMessage 读出最新 state，
// agent loop 结束后把 NextStage 持久化回数据库。
type PPTPipelineState struct {
	Stage          PPTStage `json:"stage"`
	Topic          string   `json:"topic,omitempty"` // intake 阶段从 [INTAKE] JSON 解析出的标准化主题；后续阶段用它当 Topic，避免被 query 覆盖
	ProjectName    string   `json:"project_name,omitempty"`
	PageRange      string   `json:"page_range,omitempty"`     // intake 阶段用户选定的档位，如 "15-20"
	ResearchBrief  string   `json:"research_brief,omitempty"` // research 阶段摘要（落库前已截断）
	OutlineJSON    string   `json:"outline_json,omitempty"`
	LayoutPlanJSON string   `json:"layout_plan_json,omitempty"`
	FailureCount   int      `json:"failure_count,omitempty"` // 连续 stage 校验失败次数；>=2 切到 StageLegacy
}

// MarshalState 把 state 序列化成 JSON 字符串，方便落到 ChatMessage 的 PPTStage 列旁边。
func MarshalState(s PPTPipelineState) string {
	if s.Stage == "" {
		return ""
	}
	b, err := json.Marshal(s)
	if err != nil {
		return ""
	}
	return string(b)
}

// UnmarshalState 反序列化 state 字符串，失败时返回空 state（视为新会话）。
func UnmarshalState(raw string) PPTPipelineState {
	if strings.TrimSpace(raw) == "" {
		return PPTPipelineState{}
	}
	var s PPTPipelineState
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return PPTPipelineState{}
	}
	return s
}

// fallToLegacy 把 state 切到 StageLegacy 的同时清理 v2 流水线的中间产物。
// 兼容模式走旧 SKILL.md 自驱，不会消费这些字段，留着只占库列空间，且可能误导后续路由。
// 保留 Topic / PageRange：legacy 模式仍可能在 prompt 注入上下文（service.injectPipelineContext 用得到）。
func fallToLegacy(state PPTPipelineState) PPTPipelineState {
	state.Stage = StageLegacy
	state.ResearchBrief = ""
	state.OutlineJSON = ""
	state.LayoutPlanJSON = ""
	state.FailureCount = 0
	return state
}

// IsActiveStage 表示某 stage 受新流水线管控（即需要工具白名单 + tag 校验）。
// StageNone 和 StageLegacy 都走旧 SKILL.md 自驱，无须管控。
func IsActiveStage(stage PPTStage) bool {
	switch stage {
	case StageIntake, StageResearch, StageOutline, StageLayout:
		return true
	default:
		return false
	}
}

// stageToolWhitelist 返回每个 stage 允许暴露给 LLM 的工具集。
// StageRender/StageExport 沿用现有 Executor 全工具，所以不在此列表（agent.go 走旧分支）。
var stageToolWhitelist = map[PPTStage]map[tool.AgentTool]bool{
	StageIntake:   {},
	StageResearch: {tool.AgentToolWebSearch: true, tool.AgentToolWrite: true},
	StageOutline:  {tool.AgentToolRead: true, tool.AgentToolWrite: true},
	StageLayout:   {tool.AgentToolListPPTTemplates: true, tool.AgentToolRead: true},
}

// AllowedToolForStage 判断给定阶段下某工具是否允许。
// stage 不在表里时返回 true（即 render/export/legacy 不裁剪工具）。
func AllowedToolForStage(stage PPTStage, name tool.AgentTool) bool {
	whitelist, ok := stageToolWhitelist[stage]
	if !ok {
		return true
	}
	return whitelist[name]
}

// allowedToolNamesForStage 按字母序返回某 stage 的工具白名单（仅用于 prompt 拼接的可读性）。
// stage 不在表里时返回 nil；intake 这种"零工具"阶段返回空切片。
func allowedToolNamesForStage(stage PPTStage) []string {
	whitelist, ok := stageToolWhitelist[stage]
	if !ok {
		return nil
	}
	names := make([]string, 0, len(whitelist))
	for name := range whitelist {
		names = append(names, string(name))
	}
	sort.Strings(names)
	return names
}

// stageTagPatterns 是每个 stage 期望从模型输出里提取的 [TAG]...[/TAG] 块。
// 用于服务端硬校验：未找到对应 tag 则 FailureCount++。
var stageTagPatterns = map[PPTStage]*regexp.Regexp{
	StageIntake:  regexp.MustCompile(`(?s)\[INTAKE\](.*?)\[/INTAKE\]`),
	StageOutline: regexp.MustCompile(`(?s)\[PPT_OUTLINE\](.*?)\[/PPT_OUTLINE\]`),
	StageLayout:  regexp.MustCompile(`(?s)\[LAYOUT_PLAN\](.*?)\[/LAYOUT_PLAN\]`),
}

// ExtractStageTag 从 LLM 输出里提取标签包裹的 JSON payload（去除前后空白）。
// stage 没有 tag 要求（如 research）时返回空字符串、ok=true，调用方按"无校验"处理。
func ExtractStageTag(stage PPTStage, content string) (string, bool) {
	pattern, has := stageTagPatterns[stage]
	if !has {
		return "", true
	}
	match := pattern.FindStringSubmatch(content)
	if len(match) < 2 {
		return "", false
	}
	return strings.TrimSpace(match[1]), true
}

// ValidateStageJSON 校验 stage 输出是否是合法 JSON 对象。
// 返回 false 时调用方应递增 FailureCount；连续两次失败切到 StageLegacy。
func ValidateStageJSON(stage PPTStage, payload string) bool {
	if payload == "" {
		return false
	}
	var probe map[string]any
	return json.Unmarshal([]byte(payload), &probe) == nil
}

// PageRangeRecommendation 是 intake 阶段返回给前端的页数档位选项。
type PageRangeRecommendation struct {
	Key       string `json:"key"`       // "compact" / "standard" / "detailed"
	Label     string `json:"label"`     // "简版"
	Range     string `json:"range"`     // "8-12 页"
	Suitable  string `json:"suitable"`  // "适合 ___"
	Recommend bool   `json:"recommend"` // ★ 标记
}

// DefaultPageRecommendations 给 intake prompt 注入的默认 3 档建议。
// 模型可以基于主题微调"适合"描述，但档位本身固定，避免数量发散。
func DefaultPageRecommendations() []PageRangeRecommendation {
	return []PageRangeRecommendation{
		{Key: "compact", Label: "简版", Range: "8-12 页", Suitable: "汇报简报、产品介绍速览", Recommend: false},
		{Key: "standard", Label: "标准", Range: "15-20 页", Suitable: "常规课题汇报、行业分析", Recommend: true},
		{Key: "detailed", Label: "详版", Range: "25-35 页", Suitable: "深度研究、咨询报告", Recommend: false},
	}
}

// StageSystemPrompt 返回每个 stage 注入到 LLM 的 system prompt。
// 这是双约束的"软"一半：硬约束是工具白名单 + tag 校验。
func StageSystemPrompt(stage PPTStage, ctx StagePromptContext) string {
	switch stage {
	case StageIntake:
		return intakePrompt(ctx)
	case StageResearch:
		return researchPrompt(ctx)
	case StageOutline:
		return outlinePrompt(ctx)
	case StageLayout:
		return layoutPrompt(ctx)
	default:
		return ""
	}
}

// StagePromptContext 是给 prompt 模板填值用的上下文。
type StagePromptContext struct {
	Topic         string
	PageRange     string
	ResearchBrief string
	OutlineJSON   string
	WorkspaceRoot string
}

func intakePrompt(_ StagePromptContext) string {
	rec := DefaultPageRecommendations()
	recJSON, _ := json.Marshal(rec)
	return `你是 PPT 需求顾问，目标是把用户主题转成可执行的需求清单。

## 输出要求
1. 不要调用任何工具。
2. 输出包在 [INTAKE]...[/INTAKE] 标签里，标签内必须是合法 JSON。
3. JSON 字段：
   - topic: 字符串，标准化后的主题
   - category: 主题归类（biography / industry_research / product_launch / data_report / teaching / other）
   - audience: 假设的目标受众（一句话）
   - language: 输出语言（zh / en）
   - page_options: 页数档位数组，必须是 3 项；可基于主题微调 suitable 字段，但 range/key/label 维持下面的默认值
4. page_options 默认值（直接复用即可）：
` + string(recJSON) + `
5. 标签外可以写一句过渡性说明，但不要再展开页数推荐——前端只读 JSON。`
}

func researchPrompt(c StagePromptContext) string {
	briefPath := workspaceFor(c) + "/" + safeBriefFileName(c.Topic) + ".research.md"
	return `你正在 PPT 流水线的【调研】阶段。

## 任务
基于用户主题进行联网调研，产出一份 4K tokens 以内的中文资料摘要。

## 工具
本阶段唯一允许的检索工具是 web_search（DashScope 联网搜索）。
- 至少调用 1 次，最多 3 次；每次 query 都要不同侧面（背景 / 现状 / 数据 / 争议）。
- 调用后把结果整合写入 ` + briefPath + `（用 write 工具保存）。

## 严格禁止
- 不要进入大纲设计或版式设计——那是后续阶段。
- 不要直接输出 [PPT_OUTLINE] 等其他阶段的标签。

## 用户主题
` + c.Topic + `

## 用户已选页数档位
` + c.PageRange
}

// safeBriefFileName 把主题压成可作文件名的 ASCII / 中文片段。
// 单纯防止 c.Topic 含 "/"、空格、引号等让 write 工具拒收路径。
func safeBriefFileName(topic string) string {
	trimmed := strings.TrimSpace(topic)
	if trimmed == "" {
		return "research_brief"
	}
	var b strings.Builder
	for _, r := range trimmed {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		case r >= 0x4e00 && r <= 0x9fff: // CJK Unified
			b.WriteRune(r)
		case r == ' ' || r == '/' || r == '\\' || r == '|':
			b.WriteRune('_')
		}
	}
	name := b.String()
	if name == "" {
		return "research_brief"
	}
	if utf8RuneCount := len([]rune(name)); utf8RuneCount > 40 {
		name = string([]rune(name)[:40])
	}
	return name
}

// outlinePrompt 嵌入用户给的"金字塔架构师" prompt（1.0 版基础 + 我们的输入上下文 + 严格禁止）。
func outlinePrompt(c StagePromptContext) string {
	pageReq := c.PageRange
	if pageReq == "" {
		pageReq = "15-20 页"
	}
	return `# Role: 顶级的PPT结构架构师

## Profile
- 版本：2.0 (Context-Aware)
- 专业：PPT逻辑结构设计
- 特长：运用金字塔原理，结合背景调研信息构建清晰的演示逻辑

## Goals
基于用户提供的 PPT主题 和 背景调研信息 (Context)，设计一份逻辑严密、层次清晰的PPT大纲。

## Core Methodology: 金字塔原理
1. 结论先行：每个部分以核心观点开篇
2. 以上统下：上层观点是下层内容的总结
3. 归类分组：同一层级的内容属于同一逻辑范畴
4. 逻辑递进：内容按照某种逻辑顺序展开

## 输入上下文
- 主题：` + c.Topic + `
- 页数档位：` + pageReq + `
- 调研摘要（来自 research_brief，已截断到 4K tokens）：

` + truncateBrief(c.ResearchBrief) + `

## 输出规范
请严格按照以下 JSON 格式输出，结果用 [PPT_OUTLINE] 和 [/PPT_OUTLINE] 包裹：

[PPT_OUTLINE]
{
  "ppt_outline": {
    "cover": {"title": "...", "sub_title": "...", "content": []},
    "table_of_contents": {"title": "目录", "content": ["第一部分标题", "..."]},
    "parts": [
      {"part_title": "第一部分：...", "pages": [{"title": "...", "content": []}]}
    ],
    "end_page": {"title": "总结与展望", "content": []}
  }
}
[/PPT_OUTLINE]

## 严格禁止
- 不要调用任何工具，本阶段只产出 [PPT_OUTLINE] JSON。
- 不要进入版式 / SVG 设计——那是下一阶段的事。
- 不要在 JSON 之外重复大段调研内容。`
}

func layoutPrompt(c StagePromptContext) string {
	return `你正在 PPT 流水线的【版式规划】阶段。

## 任务
对 outline 中的每一页，从 ppt-master layouts_index.json 中选一个 layout_id（轻量草稿，不做坐标级排版）。

## 工具
- list_ppt_templates：列出全部可用 layout 名称、关键词、风格描述。先调一次。
- read：必要时按需读取具体 layout 的 design_spec。

## 选择约束
- cover / end_page 用 cover 类布局
- 含数据 / 对比 / 流程的页 → 优先 chart 类布局
- 同一 part 内不要连续两页用同一 layout_id（除非内容真的同质）

## 输入大纲（来自 outline 阶段）
` + c.OutlineJSON + `

## 输出规范
[LAYOUT_PLAN]
{
  "pages": [
    {"page_index": 1, "title": "...", "layout_id": "...", "reason": "..."}
  ]
}
[/LAYOUT_PLAN]

## 严格禁止
- 不要直接生成 SVG 或调用 bash。
- 不要在标签外重复 outline。`
}

// workspaceFor 优先取上下文里的 WorkspaceRoot；为空时退回 server 端的统一默认。
// 集中在一处定义，避免和 service.DefaultWorkspaceRoot / agent.pptRuntimeOverrides 各写一份导致 drift。
func workspaceFor(c StagePromptContext) string {
	if c.WorkspaceRoot != "" {
		return c.WorkspaceRoot
	}
	return defaultWorkspaceRoot
}

// defaultWorkspaceRoot 与 server.DefaultWorkspaceRoot 保持一致；
// agent 包不能反向 import server，所以这里复制一份常量，单点对齐由 main 装配时注入 a.workspaceRoot 兜底。
const defaultWorkspaceRoot = "/workspace/lingxi/workspace"

// truncateBrief 给 outline 阶段的 brief 补一道保险（web_search 已经做过截断，这里防意外）。
func truncateBrief(s string) string {
	const limit = 6000
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "\n\n...(truncated)"
}

// nextStageOnSuccess 描述各阶段顺利产出 tag 后应转移到的下一阶段。
// 这是状态机的"happy path"边。
func nextStageOnSuccess(stage PPTStage) PPTStage {
	switch stage {
	case StageIntake:
		return StageResearch
	case StageResearch:
		return StageOutline
	case StageOutline:
		return StageLayout
	case StageLayout:
		return StageRender
	default:
		return stage
	}
}

// StageBoundaryReminder 在每轮 RunStage 进入 LLM 之前，作为 system 消息插在 history 与本轮 user 之间。
// 目的：history 里可能含有前一阶段的 web_search/write 等工具调用记录，
// 模型容易误以为本阶段还能继续调用，导致出现"工具不在白名单"的错误循环。
// 这里显式声明：上面是历史，本阶段为 X，仅这些工具可用。
func StageBoundaryReminder(stage PPTStage) string {
	allowed := allowedToolNamesForStage(stage)
	var toolLine string
	switch {
	case allowed == nil:
		toolLine = "（本阶段不裁剪工具列表）"
	case len(allowed) == 0:
		toolLine = "本阶段禁止调用任何工具，请直接输出对应标签的 JSON。"
	default:
		toolLine = "本阶段仅允许以下工具：" + strings.Join(allowed, ", ") + "。其他工具即使出现在历史里也不再可用。"
	}
	return "## 阶段边界\n" +
		"以上历史消息可能来自上一阶段（例如调研阶段的 web_search 调用记录）。\n" +
		"本阶段为：" + string(stage) + "（" + FormatStageBanner(stage) + "）。\n" +
		toolLine
}

// FormatStageBanner 给前端 SSE 提示的轻量描述（用于进度条 tooltip）。
func FormatStageBanner(stage PPTStage) string {
	switch stage {
	case StageIntake:
		return "需求确认"
	case StageResearch:
		return "联网调研"
	case StageOutline:
		return "大纲架构"
	case StageLayout:
		return "版式规划"
	case StageRender:
		return "页面生成"
	case StageExport:
		return "导出 PPTX"
	case StageLegacy:
		return "兼容模式"
	default:
		return ""
	}
}

// ensureNextStage 是给 service 层用的辅助函数：根据当前 stage + LLM 输出更新 state。
//
// 调用方负责：
//   - content：本轮 assistant 最终文本（用于 tag 提取）
//   - usedWebSearch：本轮 roundMessages 里是否至少有一次 web_search tool call（research 阶段硬约束）
//
// 失败计数规则：tag 校验失败、JSON 不合法、research 未联网 → FailureCount++；连续 >=2 切 StageLegacy。
func ensureNextStage(state PPTPipelineState, content string, usedWebSearch bool) PPTPipelineState {
	if !IsActiveStage(state.Stage) {
		return state
	}

	// research 阶段没有 tag 校验，但要求至少调用过一次 web_search；
	// 摘要直接取 finalContent（已经走过 LLM 整合，比读 markdown 文件更可靠）。
	if state.Stage == StageResearch {
		if !usedWebSearch {
			state.FailureCount++
			if state.FailureCount >= 2 {
				return fallToLegacy(state)
			}
			return state
		}
		state.ResearchBrief = truncateBrief(strings.TrimSpace(content))
		state.FailureCount = 0
		state.Stage = nextStageOnSuccess(state.Stage)
		return state
	}

	tag, hasTag := stageTagPatterns[state.Stage]
	if !hasTag {
		// 兜底：未来若新增没 tag 的阶段，默认放行。
		state.Stage = nextStageOnSuccess(state.Stage)
		state.FailureCount = 0
		return state
	}
	match := tag.FindStringSubmatch(content)
	if len(match) < 2 || !ValidateStageJSON(state.Stage, strings.TrimSpace(match[1])) {
		state.FailureCount++
		if state.FailureCount >= 2 {
			return fallToLegacy(state)
		}
		return state
	}
	payload := strings.TrimSpace(match[1])
	switch state.Stage {
	case StageIntake:
		// intake 完成后必须等用户选页数档位，不能立即推进；
		// 顺手把模型解析出的 topic 写入 state.Topic，避免下一轮被 "我选标准 15-20 页" 覆盖。
		if topic := extractIntakeTopic(payload); topic != "" {
			state.Topic = topic
		}
		state.FailureCount = 0
		return state
	case StageOutline:
		state.OutlineJSON = payload
	case StageLayout:
		state.LayoutPlanJSON = payload
	}
	state.FailureCount = 0
	state.Stage = nextStageOnSuccess(state.Stage)
	return state
}

// extractIntakeTopic 从 [INTAKE] JSON 里读 topic 字段；解析失败返回空串（调用方会保留旧 Topic）。
func extractIntakeTopic(payload string) string {
	var probe struct {
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal([]byte(payload), &probe); err != nil {
		return ""
	}
	return strings.TrimSpace(probe.Topic)
}

// stageDebugString 给日志用的简短描述。
func stageDebugString(s PPTPipelineState) string {
	return fmt.Sprintf("stage=%s failures=%d project=%q", s.Stage, s.FailureCount, s.ProjectName)
}
