package server

import (
	"encoding/json"

	"lingxi/background/shared"
)

// buildHistory 根据 parent_message_id 沿树向上追溯路径，
// 将路径上每条消息的 rounds 拼接成 LLM history。
// allMsgs 为该会话下的全部消息，parentMessageID 为本次请求的父消息 ID。
func buildHistory(allMsgs []ChatMessage, parentMessageID string) []shared.OpenAIMessage {
	if parentMessageID == "" {
		return nil
	}

	// 构建 id -> message 索引
	index := make(map[string]*ChatMessage, len(allMsgs))
	for i := range allMsgs {
		index[allMsgs[i].MessageID] = &allMsgs[i]
	}

	// 从 parentMessageID 向根节点追溯，收集路径（顺序：根 -> parent）
	path := make([]*ChatMessage, 0)
	cur := parentMessageID
	for cur != "" {
		msg, ok := index[cur]
		if !ok {
			break
		}
		path = append(path, msg)
		cur = msg.ParentMessageID
	}

	// 反转：变为根 -> parent 顺序
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}

	// 拼接每条消息的 rounds
	history := make([]shared.OpenAIMessage, 0)
	for _, msg := range path {
		if msg.Rounds == "" {
			continue
		}
		var rounds []shared.OpenAIMessage
		if err := json.Unmarshal([]byte(msg.Rounds), &rounds); err != nil {
			continue
		}
		history = append(history, sanitizeOpenAIHistory(rounds)...)
	}
	return history
}

func sanitizeOpenAIHistory(rounds []shared.OpenAIMessage) []shared.OpenAIMessage {
	cleaned := make([]shared.OpenAIMessage, 0, len(rounds))

	for i := 0; i < len(rounds); i++ {
		msg := rounds[i]
		if msg.OfAssistant != nil && len(msg.OfAssistant.ToolCalls) > 0 {
			ids := make(map[string]struct{}, len(msg.OfAssistant.ToolCalls))
			for _, tc := range msg.OfAssistant.ToolCalls {
				if tc.OfFunction != nil {
					ids[tc.OfFunction.ID] = struct{}{}
				}
			}
			if !hasImmediateToolResults(rounds, i+1, ids) {
				msg.OfAssistant.ToolCalls = nil
				cleaned = append(cleaned, msg)
				continue
			}
			cleaned = append(cleaned, msg)
			for len(ids) > 0 && i+1 < len(rounds) && rounds[i+1].OfTool != nil {
				next := rounds[i+1]
				if _, ok := ids[next.OfTool.ToolCallID]; !ok {
					break
				}
				delete(ids, next.OfTool.ToolCallID)
				cleaned = append(cleaned, next)
				i++
			}
			continue
		}

		if msg.OfTool != nil {
			continue
		}

		cleaned = append(cleaned, msg)
	}

	return cleaned
}

func hasImmediateToolResults(rounds []shared.OpenAIMessage, start int, ids map[string]struct{}) bool {
	if len(ids) == 0 {
		return false
	}
	remaining := make(map[string]struct{}, len(ids))
	for id := range ids {
		remaining[id] = struct{}{}
	}
	for i := start; i < len(rounds) && len(remaining) > 0; i++ {
		msg := rounds[i]
		if msg.OfTool == nil {
			return false
		}
		if _, ok := remaining[msg.OfTool.ToolCallID]; !ok {
			return false
		}
		delete(remaining, msg.OfTool.ToolCallID)
	}
	return len(remaining) == 0
}
