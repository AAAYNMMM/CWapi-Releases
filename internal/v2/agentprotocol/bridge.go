package agentprotocol

import (
	"encoding/json"
	"strings"
)

// EncodeBridgeRequest is the only canonical -> MCP bridge projection. Keeping
// this separate from external adapters prevents the broker from depending on a
// client protocol while preserving the 2.0 OpenAI-shaped agent_exchange body.
func EncodeBridgeRequest(conversation Conversation) (map[string]any, error) {
	if err := validateCanonicalConversation(conversation); err != nil {
		return nil, err
	}
	messages := make([]any, 0, len(conversation.Messages))
	for _, message := range conversation.Messages {
		encoded := map[string]any{"role": string(message.Role)}
		switch message.Role {
		case RoleTool:
			if message.ToolResult != nil {
				encoded["tool_call_id"] = message.ToolResult.CallID
				encoded["content"] = message.ToolResult.Content
				if message.ToolResult.Name != "" {
					encoded["name"] = message.ToolResult.Name
				}
			}
		default:
			encoded["content"] = message.Content
			if message.Name != "" {
				encoded["name"] = message.Name
			}
			if len(message.ToolCalls) > 0 {
				encoded["tool_calls"] = encodeToolCalls(message.ToolCalls)
			}
		}
		messages = append(messages, encoded)
	}
	result := map[string]any{"model": conversation.Model, "messages": messages}
	if len(conversation.Tools) > 0 {
		tools := make([]any, 0, len(conversation.Tools))
		for _, tool := range conversation.Tools {
			function := map[string]any{"name": tool.Name, "parameters": cloneJSONMap(tool.Parameters)}
			if tool.Description != "" {
				function["description"] = tool.Description
			}
			tools = append(tools, map[string]any{"type": "function", "function": function})
		}
		result["tools"] = tools
	}
	if conversation.ToolChoice.Mode != "" {
		result["tool_choice"] = conversation.ToolChoice.Mode
	} else if conversation.ToolChoice.Name != "" {
		result["tool_choice"] = map[string]any{"type": "function", "function": map[string]any{"name": conversation.ToolChoice.Name}}
	}
	if conversation.ResponseFormat.Type != "" {
		format := map[string]any{"type": conversation.ResponseFormat.Type}
		if conversation.ResponseFormat.Type == "json_schema" {
			format["json_schema"] = cloneJSONMap(conversation.ResponseFormat.JSONSchema)
		}
		result["response_format"] = format
	}
	if len(conversation.Metadata) > 0 {
		result["metadata"] = cloneJSONMap(conversation.Metadata)
	}
	if conversation.Stream {
		result["stream"] = true
	}
	return result, nil
}

func validateCanonicalConversation(conversation Conversation) error {
	if len(conversation.Messages) == 0 {
		return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "messages required"}
	}
	callIDs := make(map[string]struct{})
	for _, message := range conversation.Messages {
		switch message.Role {
		case RoleSystem, RoleDeveloper, RoleUser, RoleAssistant:
			if message.ToolResult != nil {
				return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "tool result role invalid"}
			}
		case RoleTool:
			if message.ToolResult == nil || strings.TrimSpace(message.ToolResult.CallID) == "" {
				return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "tool result call ID required"}
			}
			if _, found := callIDs[message.ToolResult.CallID]; !found {
				return &CanonicalError{Code: "AGENT_TOOL_RESULT_CALL_NOT_FOUND", Kind: ErrorToolMapping, Detail: message.ToolResult.CallID}
			}
		default:
			return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "invalid message role"}
		}
		for _, call := range message.ToolCalls {
			if message.Role != RoleAssistant || strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
				return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "invalid tool call"}
			}
			if _, duplicate := callIDs[call.ID]; duplicate {
				return &CanonicalError{Code: "AGENT_TOOL_CALL_ID_DUPLICATE", Kind: ErrorToolMapping, Detail: call.ID}
			}
			callIDs[call.ID] = struct{}{}
			if _, err := json.Marshal(call.Arguments); err != nil {
				return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "tool arguments are not JSON"}
			}
		}
	}
	toolNames := make(map[string]struct{}, len(conversation.Tools))
	for _, tool := range conversation.Tools {
		if strings.TrimSpace(tool.Name) == "" {
			return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "tool name required"}
		}
		if _, duplicate := toolNames[tool.Name]; duplicate {
			return &CanonicalError{Code: "AGENT_TOOL_NAME_DUPLICATE", Kind: ErrorToolMapping, Detail: tool.Name}
		}
		toolNames[tool.Name] = struct{}{}
		if _, err := json.Marshal(tool.Parameters); err != nil {
			return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "tool parameters are not JSON"}
		}
	}
	if _, err := json.Marshal(conversation.Metadata); err != nil {
		return &CanonicalError{Code: "AGENT_MCP_REQUEST_CONVERSION_FAILED", Kind: ErrorBridge, Detail: "metadata is not JSON"}
	}
	return nil
}

func DecodeBridgeCompletion(value map[string]any, request *Conversation) (Completion, error) {
	if value == nil {
		return Completion{}, &CanonicalError{Code: "AGENT_RESPONSE_INVALID", Kind: ErrorWebGPT}
	}
	message := value
	if nested, ok := value["message"].(map[string]any); ok {
		message = nested
	}
	content, hasContent := message["content"]
	toolCallsValue, hasTools := message["tool_calls"]
	normalizedContent := ""
	if hasContent && content != nil {
		var err error
		normalizedContent, err = canonicalContent(content, "AGENT_RESPONSE_CONTENT_INVALID", ErrorWebGPT)
		if err != nil {
			return Completion{}, err
		}
	}
	var toolCalls []ToolCall
	if hasTools {
		var err error
		toolCalls, err = decodeToolCalls(toolCallsValue, ErrorWebGPT)
		if err != nil {
			return Completion{}, err
		}
	}
	if strings.TrimSpace(normalizedContent) == "" && len(toolCalls) == 0 {
		return Completion{}, &CanonicalError{Code: "AGENT_RESPONSE_MESSAGE_REQUIRED", Kind: ErrorWebGPT}
	}
	if request != nil {
		if err := validateCompletionTools(toolCalls, request.Tools); err != nil {
			return Completion{}, err
		}
		if err := enforceResponseFormat(request.ResponseFormat, normalizedContent); err != nil {
			return Completion{}, err
		}
	}
	finishReason := strings.TrimSpace(stringValue(value["finish_reason"]))
	if finishReason == "" {
		if len(toolCalls) > 0 {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}
	switch finishReason {
	case "stop", "tool_calls", "length", "content_filter":
	default:
		return Completion{}, &CanonicalError{Code: "AGENT_FINISH_REASON_INVALID", Kind: ErrorWebGPT, Detail: finishReason}
	}
	if len(toolCalls) > 0 && finishReason != "tool_calls" {
		return Completion{}, &CanonicalError{Code: "AGENT_TOOL_CALL_FINISH_REASON_INVALID", Kind: ErrorWebGPT}
	}
	return Completion{Content: normalizedContent, ToolCalls: toolCalls, FinishReason: finishReason, Status: CompletionCompleted}, nil
}

func validateCompletionTools(calls []ToolCall, tools []ToolDefinition) error {
	if len(calls) == 0 {
		return nil
	}
	if len(tools) == 0 {
		return &CanonicalError{Code: "AGENT_TOOL_CALL_UNMAPPABLE", Kind: ErrorToolMapping}
	}
	known := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		known[tool.Name] = struct{}{}
	}
	for _, call := range calls {
		if _, found := known[call.Name]; !found {
			return &CanonicalError{Code: "AGENT_TOOL_CALL_UNDECLARED", Kind: ErrorToolMapping, Detail: call.Name}
		}
	}
	return nil
}

func enforceResponseFormat(format ResponseFormat, content string) error {
	if format.Type != "json_object" && format.Type != "json_schema" {
		return nil
	}
	if !json.Valid([]byte(strings.TrimSpace(content))) {
		return &CanonicalError{Code: "AGENT_RESPONSE_JSON_INVALID", Kind: ErrorWebGPT}
	}
	return nil
}
