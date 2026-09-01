package agentprotocol

import (
	"encoding/json"
	"strings"
)

type OpenAICompatibleAdapter struct{}

func NewOpenAICompatibleAdapter() OpenAICompatibleAdapter { return OpenAICompatibleAdapter{} }

func (OpenAICompatibleAdapter) Name() string { return "openai-compatible" }

func (OpenAICompatibleAdapter) Capabilities() Capabilities {
	return Capabilities{Streaming: true, Tools: true, ParallelTools: true, Images: false, Files: false}
}

func (adapter OpenAICompatibleAdapter) DecodeRequest(payload []byte) (Conversation, error) {
	var root map[string]any
	if err := decodeJSON(payload, &root); err != nil || root == nil {
		return Conversation{}, &CanonicalError{Code: "AGENT_REQUEST_JSON_INVALID", Kind: ErrorExternalRequest}
	}
	if _, present := root["attachments"]; present {
		return Conversation{}, &CanonicalError{Code: "AGENT_FILE_ATTACHMENTS_UNSUPPORTED", Kind: ErrorCapability}
	}
	if model, present := root["model"]; present && model != nil {
		if _, ok := model.(string); !ok {
			return Conversation{}, &CanonicalError{Code: "AGENT_MODEL_INVALID", Kind: ErrorExternalRequest}
		}
	}
	if stream, present := root["stream"]; present && stream != nil {
		if _, ok := stream.(bool); !ok {
			return Conversation{}, &CanonicalError{Code: "AGENT_STREAM_INVALID", Kind: ErrorExternalRequest}
		}
	}

	messagesValue, present := root["messages"]
	if !present || messagesValue == nil {
		return Conversation{}, &CanonicalError{Code: "AGENT_MESSAGES_REQUIRED", Kind: ErrorExternalRequest}
	}
	rawMessages, ok := messagesValue.([]any)
	if !ok || len(rawMessages) == 0 {
		return Conversation{}, &CanonicalError{Code: "AGENT_MESSAGES_INVALID", Kind: ErrorExternalRequest}
	}

	conversation := Conversation{Model: strings.TrimSpace(stringValue(root["model"])), Stream: boolValue(root["stream"])}
	if conversation.Model == "" {
		conversation.Model = DefaultModel
	}
	knownCalls := make(map[string]struct{})
	for _, raw := range rawMessages {
		message, err := adapter.decodeMessage(raw, knownCalls)
		if err != nil {
			return Conversation{}, err
		}
		conversation.Messages = append(conversation.Messages, message)
		for _, call := range message.ToolCalls {
			if _, duplicate := knownCalls[call.ID]; duplicate {
				return Conversation{}, &CanonicalError{Code: "AGENT_TOOL_CALL_ID_DUPLICATE", Kind: ErrorToolMapping}
			}
			knownCalls[call.ID] = struct{}{}
		}
	}

	tools, err := decodeTools(root["tools"])
	if err != nil {
		return Conversation{}, err
	}
	conversation.Tools = tools
	choice, err := decodeToolChoice(root["tool_choice"])
	if err != nil {
		return Conversation{}, err
	}
	conversation.ToolChoice = choice
	if choice.Name != "" && !hasTool(conversation.Tools, choice.Name) {
		return Conversation{}, &CanonicalError{Code: "AGENT_TOOL_CHOICE_UNDECLARED", Kind: ErrorToolMapping, Detail: choice.Name}
	}
	format, err := decodeResponseFormat(root["response_format"])
	if err != nil {
		return Conversation{}, err
	}
	conversation.ResponseFormat = format
	metadata, err := decodeMetadata(root["metadata"])
	if err != nil {
		return Conversation{}, err
	}
	conversation.Metadata = metadata
	return conversation, nil
}

func (OpenAICompatibleAdapter) decodeMessage(raw any, knownCalls map[string]struct{}) (Message, error) {
	value, ok := raw.(map[string]any)
	if !ok {
		return Message{}, &CanonicalError{Code: "AGENT_MESSAGES_INVALID", Kind: ErrorExternalRequest}
	}
	role := Role(strings.TrimSpace(stringValue(value["role"])))
	switch role {
	case RoleSystem, RoleDeveloper, RoleUser, RoleAssistant, RoleTool:
	default:
		return Message{}, &CanonicalError{Code: "AGENT_MESSAGE_ROLE_INVALID", Kind: ErrorExternalRequest}
	}
	message := Message{Role: role, Name: strings.TrimSpace(stringValue(value["name"]))}
	if role != RoleTool {
		content, err := decodeMessageContent(value["content"])
		if err != nil {
			return Message{}, err
		}
		message.Content = content
	}

	if rawCalls, present := value["tool_calls"]; present && rawCalls != nil {
		if role != RoleAssistant {
			return Message{}, &CanonicalError{Code: "AGENT_TOOL_CALL_ROLE_INVALID", Kind: ErrorToolMapping}
		}
		calls, err := decodeToolCalls(rawCalls, ErrorExternalRequest)
		if err != nil {
			return Message{}, err
		}
		message.ToolCalls = calls
	}
	if role == RoleAssistant && message.Content == "" && len(message.ToolCalls) == 0 {
		return Message{}, &CanonicalError{Code: "AGENT_MESSAGE_CONTENT_REQUIRED", Kind: ErrorExternalRequest}
	}
	if role == RoleTool {
		callID := strings.TrimSpace(stringValue(value["tool_call_id"]))
		if callID == "" {
			return Message{}, &CanonicalError{Code: "AGENT_TOOL_RESULT_CALL_ID_REQUIRED", Kind: ErrorToolMapping}
		}
		if _, found := knownCalls[callID]; !found {
			return Message{}, &CanonicalError{Code: "AGENT_TOOL_RESULT_CALL_NOT_FOUND", Kind: ErrorToolMapping, Detail: callID}
		}
		var content string
		var err error
		if _, textParts := value["content"].([]any); textParts {
			content, err = decodeMessageContent(value["content"])
		} else {
			content, err = canonicalContent(value["content"], "AGENT_TOOL_RESULT_CONTENT_INVALID", ErrorCanonical)
		}
		if err != nil {
			return Message{}, err
		}
		message.Content = ""
		message.ToolResult = &ToolResult{CallID: callID, Name: message.Name, Content: content}
	}
	return message, nil
}

func decodeMessageContent(raw any) (string, error) {
	if raw == nil {
		return "", nil
	}
	if text, ok := raw.(string); ok {
		return text, nil
	}
	parts, ok := raw.([]any)
	if !ok {
		return "", &CanonicalError{Code: "AGENT_MESSAGE_CONTENT_UNSUPPORTED", Kind: ErrorCapability}
	}
	var text strings.Builder
	for _, rawPart := range parts {
		part, ok := rawPart.(map[string]any)
		if !ok {
			return "", &CanonicalError{Code: "AGENT_MESSAGE_CONTENT_UNSUPPORTED", Kind: ErrorCapability}
		}
		if strings.TrimSpace(stringValue(part["type"])) != "text" {
			return "", &CanonicalError{Code: "AGENT_MEDIA_INPUT_UNSUPPORTED", Kind: ErrorCapability}
		}
		partText, ok := part["text"].(string)
		if !ok {
			return "", &CanonicalError{Code: "AGENT_MESSAGE_CONTENT_UNSUPPORTED", Kind: ErrorCapability}
		}
		text.WriteString(partText)
	}
	return text.String(), nil
}

func decodeTools(raw any) ([]ToolDefinition, error) {
	if raw == nil {
		return nil, nil
	}
	values, ok := raw.([]any)
	if !ok {
		return nil, &CanonicalError{Code: "AGENT_TOOLS_INVALID", Kind: ErrorExternalRequest}
	}
	seen := make(map[string]struct{}, len(values))
	tools := make([]ToolDefinition, 0, len(values))
	for _, rawTool := range values {
		tool, ok := rawTool.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(tool["type"])) != "function" {
			return nil, &CanonicalError{Code: "AGENT_TOOL_INVALID", Kind: ErrorExternalRequest}
		}
		function, ok := tool["function"].(map[string]any)
		name := strings.TrimSpace(stringValue(function["name"]))
		if !ok || name == "" {
			return nil, &CanonicalError{Code: "AGENT_TOOL_FUNCTION_INVALID", Kind: ErrorExternalRequest}
		}
		if _, duplicate := seen[name]; duplicate {
			return nil, &CanonicalError{Code: "AGENT_TOOL_NAME_DUPLICATE", Kind: ErrorToolMapping, Detail: name}
		}
		seen[name] = struct{}{}
		parameters := map[string]any{"type": "object"}
		if rawParameters, present := function["parameters"]; present && rawParameters != nil {
			var valid bool
			parameters, valid = rawParameters.(map[string]any)
			if !valid {
				return nil, &CanonicalError{Code: "AGENT_TOOL_PARAMETERS_INVALID", Kind: ErrorExternalRequest, Detail: name}
			}
			parameters = cloneJSONMap(parameters)
		}
		tools = append(tools, ToolDefinition{Name: name, Description: stringValueUntrimmed(function["description"]), Parameters: parameters})
	}
	return tools, nil
}

func decodeToolCalls(raw any, kind ErrorKind) ([]ToolCall, error) {
	values, ok := raw.([]any)
	if !ok || len(values) == 0 {
		return nil, &CanonicalError{Code: "AGENT_TOOL_CALLS_INVALID", Kind: kind}
	}
	seen := make(map[string]struct{}, len(values))
	calls := make([]ToolCall, 0, len(values))
	for _, rawCall := range values {
		call, ok := rawCall.(map[string]any)
		if !ok || strings.TrimSpace(stringValue(call["type"])) != "function" {
			return nil, &CanonicalError{Code: "AGENT_TOOL_CALL_INVALID", Kind: kind}
		}
		id := strings.TrimSpace(stringValue(call["id"]))
		function, functionOK := call["function"].(map[string]any)
		name := strings.TrimSpace(stringValue(function["name"]))
		if id == "" || !functionOK || name == "" {
			return nil, &CanonicalError{Code: "AGENT_TOOL_CALL_INVALID", Kind: kind}
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, &CanonicalError{Code: "AGENT_TOOL_CALL_ID_DUPLICATE", Kind: ErrorToolMapping}
		}
		seen[id] = struct{}{}
		arguments, err := canonicalJSONObject(function["arguments"], "AGENT_TOOL_ARGUMENTS_INVALID", ErrorToolMapping)
		if err != nil {
			return nil, err
		}
		calls = append(calls, ToolCall{ID: id, Name: name, Arguments: arguments})
	}
	return calls, nil
}

func decodeToolChoice(raw any) (ToolChoice, error) {
	if raw == nil {
		return ToolChoice{}, nil
	}
	if mode, ok := raw.(string); ok {
		mode = strings.TrimSpace(mode)
		if mode == "none" || mode == "auto" || mode == "required" {
			return ToolChoice{Mode: mode}, nil
		}
		return ToolChoice{}, &CanonicalError{Code: "AGENT_TOOL_CHOICE_INVALID", Kind: ErrorExternalRequest}
	}
	object, ok := raw.(map[string]any)
	if !ok || strings.TrimSpace(stringValue(object["type"])) != "function" {
		return ToolChoice{}, &CanonicalError{Code: "AGENT_TOOL_CHOICE_INVALID", Kind: ErrorExternalRequest}
	}
	function, ok := object["function"].(map[string]any)
	name := strings.TrimSpace(stringValue(function["name"]))
	if !ok || name == "" {
		return ToolChoice{}, &CanonicalError{Code: "AGENT_TOOL_CHOICE_INVALID", Kind: ErrorExternalRequest}
	}
	return ToolChoice{Name: name}, nil
}

func decodeResponseFormat(raw any) (ResponseFormat, error) {
	if raw == nil {
		return ResponseFormat{}, nil
	}
	value, ok := raw.(map[string]any)
	if !ok {
		return ResponseFormat{}, &CanonicalError{Code: "AGENT_RESPONSE_FORMAT_INVALID", Kind: ErrorExternalRequest}
	}
	typeName := strings.TrimSpace(stringValue(value["type"]))
	switch typeName {
	case "text", "json_object":
		return ResponseFormat{Type: typeName}, nil
	case "json_schema":
		schema, ok := value["json_schema"].(map[string]any)
		if !ok || len(schema) == 0 {
			return ResponseFormat{}, &CanonicalError{Code: "AGENT_RESPONSE_FORMAT_INVALID", Kind: ErrorExternalRequest}
		}
		return ResponseFormat{Type: typeName, JSONSchema: cloneJSONMap(schema)}, nil
	default:
		return ResponseFormat{}, &CanonicalError{Code: "AGENT_RESPONSE_FORMAT_INVALID", Kind: ErrorExternalRequest}
	}
}

func decodeMetadata(raw any) (map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	metadata, ok := raw.(map[string]any)
	if !ok || len(metadata) > 32 {
		return nil, &CanonicalError{Code: "AGENT_METADATA_INVALID", Kind: ErrorExternalRequest}
	}
	result := make(map[string]any, len(metadata))
	for key, value := range metadata {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" || len(trimmed) > 64 {
			return nil, &CanonicalError{Code: "AGENT_METADATA_INVALID", Kind: ErrorExternalRequest}
		}
		switch item := value.(type) {
		case string:
			if len(item) > 512 {
				return nil, &CanonicalError{Code: "AGENT_METADATA_INVALID", Kind: ErrorExternalRequest}
			}
		case json.Number, float64, bool, nil:
		default:
			return nil, &CanonicalError{Code: "AGENT_METADATA_INVALID", Kind: ErrorExternalRequest}
		}
		if prior, duplicate := result[trimmed]; duplicate && canonicalJSONText(prior) != canonicalJSONText(value) {
			return nil, &CanonicalError{Code: "AGENT_METADATA_CONFLICT", Kind: ErrorCanonical, Detail: trimmed}
		}
		result[trimmed] = value
	}
	return result, nil
}

func (OpenAICompatibleAdapter) EncodeCompletion(completion Completion, metadata CompletionMetadata) (map[string]any, error) {
	if completion.Status != "" && completion.Status != CompletionCompleted {
		return nil, &CanonicalError{Code: "AGENT_COMPLETION_STATUS_UNSUPPORTED", Kind: ErrorCanonical, Detail: string(completion.Status)}
	}
	message := map[string]any{"role": string(RoleAssistant), "content": completion.Content}
	if len(completion.ToolCalls) > 0 {
		message["tool_calls"] = encodeToolCalls(completion.ToolCalls)
	}
	return map[string]any{
		"id": "chatcmpl-" + strings.TrimPrefix(metadata.ID, "request_"), "object": "chat.completion",
		"created": metadata.Created.Unix(), "model": metadata.Model,
		"choices": []any{map[string]any{"index": 0, "message": message, "finish_reason": completion.FinishReason}},
	}, nil
}

func (OpenAICompatibleAdapter) DecodeStreamChunk(payload []byte) (StreamChunk, error) {
	if strings.TrimSpace(string(payload)) == "[DONE]" {
		return StreamChunk{Done: true}, nil
	}
	var value map[string]any
	if err := decodeJSON(payload, &value); err != nil {
		return StreamChunk{}, &CanonicalError{Code: "AGENT_STREAM_CHUNK_INVALID", Kind: ErrorStream}
	}
	choices, ok := value["choices"].([]any)
	if !ok || len(choices) == 0 {
		return StreamChunk{}, &CanonicalError{Code: "AGENT_STREAM_CHUNK_INVALID", Kind: ErrorStream}
	}
	choice, ok := choices[0].(map[string]any)
	if !ok {
		return StreamChunk{}, &CanonicalError{Code: "AGENT_STREAM_CHUNK_INVALID", Kind: ErrorStream}
	}
	delta, _ := choice["delta"].(map[string]any)
	chunk := StreamChunk{Role: Role(stringValue(delta["role"])), ContentDelta: stringValueUntrimmed(delta["content"]), FinishReason: stringValue(choice["finish_reason"])}
	if rawCalls, present := delta["tool_calls"]; present {
		values, ok := rawCalls.([]any)
		if !ok {
			return StreamChunk{}, &CanonicalError{Code: "AGENT_STREAM_TOOL_CALL_INVALID", Kind: ErrorStream}
		}
		for _, raw := range values {
			call, ok := raw.(map[string]any)
			if !ok {
				return StreamChunk{}, &CanonicalError{Code: "AGENT_STREAM_TOOL_CALL_INVALID", Kind: ErrorStream}
			}
			function, _ := call["function"].(map[string]any)
			chunk.ToolCallDeltas = append(chunk.ToolCallDeltas, ToolCallDelta{
				Index: intValue(call["index"]), ID: stringValue(call["id"]), Name: stringValue(function["name"]), ArgumentsDelta: stringValueUntrimmed(function["arguments"]),
			})
		}
	}
	return chunk, nil
}

func (OpenAICompatibleAdapter) EncodeStreamChunk(chunk StreamChunk, metadata CompletionMetadata) (map[string]any, error) {
	if chunk.Done {
		return nil, nil
	}
	if chunk.Error != nil {
		return nil, chunk.Error
	}
	delta := map[string]any{}
	if chunk.Role != "" {
		delta["role"] = string(chunk.Role)
	}
	if chunk.ContentDelta != "" {
		delta["content"] = chunk.ContentDelta
	}
	if len(chunk.ToolCallDeltas) > 0 {
		calls := make([]any, 0, len(chunk.ToolCallDeltas))
		for _, call := range chunk.ToolCallDeltas {
			calls = append(calls, map[string]any{
				"index": call.Index, "id": call.ID, "type": "function",
				"function": map[string]any{"name": call.Name, "arguments": call.ArgumentsDelta},
			})
		}
		delta["tool_calls"] = calls
	}
	var finishReason any
	if chunk.FinishReason != "" {
		finishReason = chunk.FinishReason
	}
	return map[string]any{
		"id": "chatcmpl-" + strings.TrimPrefix(metadata.ID, "request_"), "object": "chat.completion.chunk",
		"created": metadata.Created.Unix(), "model": metadata.Model,
		"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finishReason}},
	}, nil
}

func encodeToolCalls(calls []ToolCall) []any {
	encoded := make([]any, 0, len(calls))
	for _, call := range calls {
		encoded = append(encoded, map[string]any{
			"id": call.ID, "type": "function",
			"function": map[string]any{"name": call.Name, "arguments": canonicalJSONText(call.Arguments)},
		})
	}
	return encoded
}

func stringValue(value any) string { return strings.TrimSpace(stringValueUntrimmed(value)) }

func stringValueUntrimmed(value any) string {
	text, _ := value.(string)
	return text
}

func boolValue(value any) bool {
	result, _ := value.(bool)
	return result
}

func intValue(value any) int {
	switch typed := value.(type) {
	case json.Number:
		integer, _ := typed.Int64()
		return int(integer)
	case float64:
		return int(typed)
	}
	return 0
}

func hasTool(tools []ToolDefinition, name string) bool {
	for _, tool := range tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

var _ Adapter = OpenAICompatibleAdapter{}
