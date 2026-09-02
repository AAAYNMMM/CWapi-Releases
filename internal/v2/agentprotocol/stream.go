package agentprotocol

import (
	"fmt"
	"sort"
	"strings"
)

// AssembleStreamCompletion joins OpenAI-compatible streaming deltas before
// decoding function.arguments. Argument fragments are never JSON-decoded
// individually; each tool call is parsed exactly once after all fragments have
// been concatenated.
func AssembleStreamCompletion(chunks []StreamChunk) (Completion, error) {
	type pendingTool struct {
		id        string
		name      string
		arguments strings.Builder
	}

	var content strings.Builder
	tools := make(map[int]*pendingTool)
	finishReason := ""

	for _, chunk := range chunks {
		if chunk.Error != nil {
			return Completion{}, chunk.Error
		}
		if chunk.ContentDelta != "" {
			content.WriteString(chunk.ContentDelta)
		}
		if chunk.FinishReason != "" {
			if finishReason != "" && finishReason != chunk.FinishReason {
				return Completion{}, &CanonicalError{Code: "AGENT_STREAM_FINISH_REASON_CONFLICT", Kind: ErrorStream}
			}
			finishReason = chunk.FinishReason
		}
		for _, delta := range chunk.ToolCallDeltas {
			if delta.Index < 0 {
				return Completion{}, &CanonicalError{Code: "AGENT_STREAM_TOOL_CALL_INVALID", Kind: ErrorStream}
			}
			pending := tools[delta.Index]
			if pending == nil {
				pending = &pendingTool{}
				tools[delta.Index] = pending
			}
			if delta.ID != "" {
				if pending.id != "" && pending.id != delta.ID {
					return Completion{}, &CanonicalError{Code: "AGENT_STREAM_TOOL_CALL_CONFLICT", Kind: ErrorStream, Detail: fmt.Sprintf("index=%d id", delta.Index)}
				}
				pending.id = delta.ID
			}
			if delta.Name != "" {
				if pending.name != "" && pending.name != delta.Name {
					return Completion{}, &CanonicalError{Code: "AGENT_STREAM_TOOL_CALL_CONFLICT", Kind: ErrorStream, Detail: fmt.Sprintf("index=%d name", delta.Index)}
				}
				pending.name = delta.Name
			}
			pending.arguments.WriteString(delta.ArgumentsDelta)
		}
	}

	response := map[string]any{"content": content.String()}
	if len(tools) > 0 {
		indexes := make([]int, 0, len(tools))
		for index := range tools {
			indexes = append(indexes, index)
		}
		sort.Ints(indexes)
		calls := make([]any, 0, len(indexes))
		for _, index := range indexes {
			pending := tools[index]
			if strings.TrimSpace(pending.id) == "" || strings.TrimSpace(pending.name) == "" {
				return Completion{}, &CanonicalError{Code: "AGENT_STREAM_TOOL_CALL_INCOMPLETE", Kind: ErrorStream, Detail: fmt.Sprintf("index=%d", index)}
			}
			calls = append(calls, map[string]any{
				"id":   pending.id,
				"type": "function",
				"function": map[string]any{
					"name":      pending.name,
					"arguments": pending.arguments.String(),
				},
			})
		}
		response["tool_calls"] = calls
	}
	if finishReason != "" {
		response["finish_reason"] = finishReason
	}
	return DecodeBridgeCompletion(response, nil)
}
