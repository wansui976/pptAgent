package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"

	"lingxi/background/shared"
	"lingxi/background/tool"
)

// stageWallClockTimeout 是单个 active stage 的 wall-clock 上限。
// research 阶段最坏 6 次工具调用 × 60s 网络超时 = 6 分钟，会让前端无任何反馈；
// 90s 在大多数 web_search/outline 场景里足够，超时计 failure 触发 v2 的两次回退路径。
const stageWallClockTimeout = 90 * time.Second

// RunStageResult 是单个 active stage 一轮的执行结果。
// NextState 已经吸收了 tag 校验 / failure_count 调整 / stage 转移。
// service 层只需把它落库即可。
type RunStageResult struct {
	Run       RunResult
	NextState PPTPipelineState
	// FellBackToLegacy 表示连续 2 次校验失败后强制切到旧管线。
	// service 层应据此往 SSE 写一条 system 提示，告知用户已切兼容模式。
	FellBackToLegacy bool
}

// RunStage 是新流水线的"单阶段"执行入口。
// 与 RunStreaming 的区别：
//   - 不加载 SKILL.md，不做 PPT auto-continue 启发式
//   - 工具列表按 stage 白名单裁剪
//   - 阶段 system prompt 来自 StageSystemPrompt
//   - 结束后自动校验 tag、推进 stage
//
// 仅当 state.Stage 是 active stage（intake/research/outline/layout）时调用本方法；
// render/export/legacy 阶段仍走 RunStreaming。
func (a *Agent) RunStage(
	ctx context.Context,
	history []openai.ChatCompletionMessageParamUnion,
	query string,
	state PPTPipelineState,
	eventCh chan<- StreamEvent,
) (RunStageResult, error) {
	if !IsActiveStage(state.Stage) {
		return RunStageResult{}, fmt.Errorf("RunStage requires active stage, got %q", state.Stage)
	}

	// 给整个 stage 套一个 wall-clock，避免单阶段卡死无反馈；
	// 超时单独处理，保留已积累的 contentBuilder/roundMessages，让 finalizeStage 有机会判 failure。
	stageCtx, cancelStage := context.WithTimeout(ctx, stageWallClockTimeout)
	defer cancelStage()
	ctx = stageCtx

	// Topic 优先取 intake 阶段写入的 state.Topic，避免 "我选标准 15-20 页" 这类切档回复覆盖真实主题。
	topic := strings.TrimSpace(state.Topic)
	if topic == "" {
		topic = query
	}
	stagePrompt := StageSystemPrompt(state.Stage, StagePromptContext{
		Topic:         topic,
		PageRange:     state.PageRange,
		ResearchBrief: state.ResearchBrief,
		OutlineJSON:   state.OutlineJSON,
		WorkspaceRoot: a.workspaceRoot,
	})
	system := a.systemPrompt + "\n\n## Pipeline Stage: " + string(state.Stage) + " (" + FormatStageBanner(state.Stage) + ")\n\n" + stagePrompt

	// 顺序：system(base + stage prompt) → history → stage boundary system → 本轮 user。
	// boundary 必须紧贴 user，让模型在最近上下文里看到"本阶段工具白名单"，
	// 否则面对一长串 history 的 web_search 记录，常会复用同样工具触发白名单拒绝。
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(history)+3)
	messages = append(messages, openai.SystemMessage(system))
	messages = append(messages, history...)
	if len(history) > 0 {
		messages = append(messages, openai.SystemMessage(StageBoundaryReminder(state.Stage)))
	}
	messages = append(messages, openai.UserMessage(query))

	roundMessages := []shared.OpenAIMessage{openai.UserMessage(query)}
	stageStr := string(state.Stage)

	// 通知前端进度条：本阶段开始。
	eventCh <- StreamEvent{Event: EventContent, Stage: stageStr, Content: ""}

	var contentBuilder strings.Builder
	var usage openai.CompletionUsage

	// active stage 内允许多轮 tool call（research 阶段可能多次 web_search），但保持单轮 LLM 调用为主。
	for iter := 0; iter < 6; iter++ {
		params := openai.ChatCompletionNewParams{
			Model:         a.model,
			Messages:      messages,
			Tools:         a.buildToolsForStage(state.Stage),
			StreamOptions: openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)},
		}

		stream := a.client.Chat.Completions.NewStreaming(ctx, params)
		acc := openai.ChatCompletionAccumulator{}

		for stream.Next() {
			chunk := stream.Current()
			acc.AddChunk(chunk)
			if len(chunk.Choices) > 0 {
				deltaRaw := chunk.Choices[0].Delta
				delta := deltaWithReasoning{}
				_ = json.Unmarshal([]byte(deltaRaw.RawJSON()), &delta)
				if delta.ReasoningContent != "" {
					eventCh <- StreamEvent{Event: EventReasoning, Stage: stageStr, ReasoningContent: delta.ReasoningContent}
				}
				if delta.Content != "" {
					eventCh <- StreamEvent{Event: EventContent, Stage: stageStr, Content: delta.Content}
				}
			}
		}
		if err := stream.Err(); err != nil {
			// stage 模式下不走 fallback HTTP 解析；超时和普通错误都丢给 finalizeStage 当 failure 处理，
			// 让连续 2 次失败自动回退到 legacy。超时再多 emit 一条提示，便于用户感知。
			if errors.Is(err, context.DeadlineExceeded) {
				eventCh <- StreamEvent{Event: EventError, Stage: stageStr, Content: "stage timeout (90s)"}
			} else {
				eventCh <- StreamEvent{Event: EventError, Stage: stageStr, Content: err.Error()}
			}
			result := a.finalizeStage(state, contentBuilder.String(), roundMessages, usage)
			// stage 校验若意外通过，强制把这一轮记一次 failure，避免空内容直接前进；
			// 同时清掉 nextState 上意外推进的 stage。
			result = forceFailureOnError(result, state)
			return result, err
		}
		if len(acc.Choices) == 0 {
			break
		}

		usage = acc.Usage
		message := acc.Choices[0].Message
		contentBuilder.WriteString(message.Content)

		assistantMsg := message.ToParam()
		messages = append(messages, assistantMsg)
		roundMessages = append(roundMessages, assistantMsg)

		if len(message.ToolCalls) == 0 {
			break
		}

		// stage 内执行 tool call。stage 工具白名单已经在 buildToolsForStage 阶段限制；
		// 这里再防一道：模型若伪造一个不在白名单的工具，直接报错短路。
		for _, toolCall := range message.ToolCalls {
			if !AllowedToolForStage(state.Stage, toolCall.Function.Name) {
				eventCh <- StreamEvent{
					Event:   EventError,
					Stage:   stageStr,
					Content: "tool not allowed in stage " + stageStr + ": " + toolCall.Function.Name,
				}
				continue
			}
			eventCh <- StreamEvent{
				Event:         EventToolCall,
				Stage:         stageStr,
				ToolCallID:    toolCall.ID,
				ToolCall:      toolCall.Function.Name,
				ToolArguments: toolCall.Function.Arguments,
			}
			toolResult, err := a.executeTool(ctx, toolCall, false)
			if err != nil {
				toolResult = err.Error()
				eventCh <- StreamEvent{Event: EventError, Stage: stageStr, Content: toolResult}
			}
			eventCh <- StreamEvent{
				Event:         EventToolResult,
				Stage:         stageStr,
				ToolCallID:    toolCall.ID,
				ToolCall:      toolCall.Function.Name,
				ToolArguments: toolCall.Function.Arguments,
				ToolResult:    toolResult,
			}
			toolMsg := openai.ToolMessage(toolResult, toolCall.ID)
			messages = append(messages, toolMsg)
			roundMessages = append(roundMessages, toolMsg)
		}
	}

	return a.finalizeStage(state, contentBuilder.String(), roundMessages, usage), nil
}

// finalizeStage 在 LLM 输出结束后做 tag 校验 + state 转移。
func (a *Agent) finalizeStage(
	state PPTPipelineState,
	finalContent string,
	rounds []shared.OpenAIMessage,
	usage openai.CompletionUsage,
) RunStageResult {
	prevStage := state.Stage

	// research 阶段需要硬证据：本轮 rounds 里至少有一次 web_search tool call，否则视为绕过联网。
	usedWebSearch := roundsContainToolCall(rounds, string(tool.AgentToolWebSearch))

	nextState := ensureNextStage(state, finalContent, usedWebSearch)
	fellBack := nextState.Stage == StageLegacy && prevStage != StageLegacy

	return RunStageResult{
		Run: RunResult{
			Response: finalContent,
			Rounds:   rounds,
			Usage:    usage,
		},
		NextState:        nextState,
		FellBackToLegacy: fellBack,
	}
}

// forceFailureOnError 处理 stage 在网络/超时报错时的 state：
// 即便意外提取到 tag、ensureNextStage 把 stage 推进了，也强制回退到原 stage 并 +1 failure，
// 让"两次失败回退 legacy"的合约对错误路径同样成立。
// 切到 legacy 时必须经 fallToLegacy 清掉中间产物，避免脏数据落库。
func forceFailureOnError(result RunStageResult, prev PPTPipelineState) RunStageResult {
	failed := prev
	failed.FailureCount++
	if failed.FailureCount >= 2 {
		failed = fallToLegacy(failed)
		result.FellBackToLegacy = true
	}
	result.NextState = failed
	return result
}

// roundsContainToolCall 扫描 assistant 消息里的 tool_calls，判断是否调用过指定工具。
// 用于 research 阶段强制 web_search 联网；不在意调用次数，只看有没有过。
func roundsContainToolCall(rounds []shared.OpenAIMessage, toolName string) bool {
	for _, msg := range rounds {
		if msg.OfAssistant == nil {
			continue
		}
		for _, tc := range msg.OfAssistant.ToolCalls {
			if tc.OfFunction != nil && tc.OfFunction.Function.Name == toolName {
				return true
			}
		}
	}
	return false
}
