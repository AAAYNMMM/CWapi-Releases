package agentbroker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const DefaultModel = "cwapi-web-gpt"

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       json.RawMessage `json:"messages"`
	Tools          json.RawMessage `json:"tools,omitempty"`
	ToolChoice     json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat json.RawMessage `json:"response_format,omitempty"`
	Metadata       json.RawMessage `json:"metadata,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
}

func normalizeChatRequest(payload []byte) (map[string]any, string, bool, error) {
	var input chatRequest
	if err := json.Unmarshal(payload, &input); err != nil {
		return nil, "", false, errors.New("AGENT_REQUEST_JSON_INVALID")
	}
	if len(input.Messages) == 0 || string(input.Messages) == "null" {
		return nil, "", false, errors.New("AGENT_MESSAGES_REQUIRED")
	}
	var messages []any
	if err := json.Unmarshal(input.Messages, &messages); err != nil || len(messages) == 0 {
		return nil, "", false, errors.New("AGENT_MESSAGES_INVALID")
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, "", false, errors.New("AGENT_MESSAGES_INVALID")
		}
		role := strings.TrimSpace(stringValue(message["role"]))
		if role != "system" && role != "developer" && role != "user" && role != "assistant" && role != "tool" {
			return nil, "", false, errors.New("AGENT_MESSAGE_ROLE_INVALID")
		}
	}
	if err := validateTools(input.Tools); err != nil {
		return nil, "", false, err
	}
	if err := validateToolChoice(input.ToolChoice); err != nil {
		return nil, "", false, err
	}
	if err := validateResponseFormat(input.ResponseFormat); err != nil {
		return nil, "", false, err
	}
	if err := validateMetadata(input.Metadata); err != nil {
		return nil, "", false, err
	}
	model := strings.TrimSpace(input.Model)
	if model == "" {
		model = DefaultModel
	}
	var normalized map[string]any
	if err := json.Unmarshal(payload, &normalized); err != nil {
		return nil, "", false, errors.New("AGENT_REQUEST_JSON_INVALID")
	}
	allowed := map[string]bool{"model": true, "messages": true, "tools": true, "tool_choice": true, "response_format": true, "metadata": true, "stream": true}
	for key := range normalized {
		if !allowed[key] {
			delete(normalized, key)
		}
	}
	normalized["model"] = model
	return normalized, model, input.Stream, nil
}

func validateMetadata(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata == nil || len(metadata) > 32 {
		return errors.New("AGENT_METADATA_INVALID")
	}
	for key, value := range metadata {
		key = strings.TrimSpace(key)
		if key == "" || len(key) > 64 {
			return errors.New("AGENT_METADATA_INVALID")
		}
		switch item := value.(type) {
		case string:
			if len(item) > 512 {
				return errors.New("AGENT_METADATA_INVALID")
			}
		case float64, bool, nil:
		default:
			return errors.New("AGENT_METADATA_INVALID")
		}
	}
	return nil
}

func rejectTransferInputs(payload []byte) error {
	var root map[string]any
	if err := json.Unmarshal(payload, &root); err != nil {
		return errors.New("AGENT_REQUEST_JSON_INVALID")
	}
	if _, present := root["attachments"]; present {
		return errors.New("AGENT_FILE_ATTACHMENTS_UNSUPPORTED")
	}
	messages, _ := root["messages"].([]any)
	for _, raw := range messages {
		message, _ := raw.(map[string]any)
		parts, ok := message["content"].([]any)
		if !ok {
			continue
		}
		for _, rawPart := range parts {
			part, ok := rawPart.(map[string]any)
			if !ok {
				return errors.New("AGENT_MESSAGE_CONTENT_UNSUPPORTED")
			}
			if strings.TrimSpace(stringValue(part["type"])) != "text" {
				return errors.New("AGENT_MEDIA_INPUT_UNSUPPORTED")
			}
		}
	}
	return nil
}

func validateTools(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var tools []any
	if err := json.Unmarshal(raw, &tools); err != nil {
		return errors.New("AGENT_TOOLS_INVALID")
	}
	for _, value := range tools {
		tool, ok := value.(map[string]any)
		if !ok || stringValue(tool["type"]) != "function" {
			return errors.New("AGENT_TOOL_INVALID")
		}
		function, ok := tool["function"].(map[string]any)
		if !ok || strings.TrimSpace(stringValue(function["name"])) == "" {
			return errors.New("AGENT_TOOL_FUNCTION_INVALID")
		}
	}
	return nil
}

func validateToolChoice(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("AGENT_TOOL_CHOICE_INVALID")
	}
	if text, ok := value.(string); ok {
		if text == "none" || text == "auto" || text == "required" {
			return nil
		}
		return errors.New("AGENT_TOOL_CHOICE_INVALID")
	}
	object, ok := value.(map[string]any)
	if !ok || stringValue(object["type"]) != "function" {
		return errors.New("AGENT_TOOL_CHOICE_INVALID")
	}
	function, ok := object["function"].(map[string]any)
	if !ok || strings.TrimSpace(stringValue(function["name"])) == "" {
		return errors.New("AGENT_TOOL_CHOICE_INVALID")
	}
	return nil
}

func validateResponseFormat(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return errors.New("AGENT_RESPONSE_FORMAT_INVALID")
	}
	switch stringValue(value["type"]) {
	case "text", "json_object":
		return nil
	case "json_schema":
		if schema, ok := value["json_schema"].(map[string]any); !ok || len(schema) == 0 {
			return errors.New("AGENT_RESPONSE_FORMAT_INVALID")
		}
		return nil
	default:
		return errors.New("AGENT_RESPONSE_FORMAT_INVALID")
	}
}

func normalizeCompletion(value map[string]any, requestPayload map[string]any) (Completion, error) {
	if value == nil {
		return Completion{}, errors.New("AGENT_RESPONSE_INVALID")
	}
	message := value
	if nested, ok := value["message"].(map[string]any); ok {
		message = nested
	}
	content, hasContent := message["content"]
	toolCalls, hasTools := message["tool_calls"]
	if hasContent && content != nil {
		var err error
		content, err = normalizeResponseContent(content)
		if err != nil {
			return Completion{}, err
		}
	}
	if hasTools {
		var err error
		toolCalls, err = normalizeToolCalls(toolCalls)
		if err != nil {
			return Completion{}, err
		}
	}
	if (!hasContent || content == nil || strings.TrimSpace(stringValue(content)) == "") && !hasTools {
		return Completion{}, errors.New("AGENT_RESPONSE_MESSAGE_REQUIRED")
	}
	if err := enforceResponseFormat(requestPayload, content); err != nil {
		return Completion{}, err
	}
	finishReason := strings.TrimSpace(stringValue(value["finish_reason"]))
	if finishReason == "" {
		if hasTools {
			finishReason = "tool_calls"
		} else {
			finishReason = "stop"
		}
	}
	if finishReason != "stop" && finishReason != "tool_calls" && finishReason != "length" && finishReason != "content_filter" {
		return Completion{}, fmt.Errorf("AGENT_FINISH_REASON_INVALID: %s", finishReason)
	}
	if hasTools && finishReason != "tool_calls" {
		return Completion{}, errors.New("AGENT_TOOL_CALL_FINISH_REASON_INVALID")
	}
	return Completion{Content: content, ToolCalls: toolCalls, FinishReason: finishReason}, nil
}

func normalizeToolCalls(raw any) ([]any, error) {
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, errors.New("AGENT_TOOL_CALLS_INVALID")
	}
	var calls []any
	if err := json.Unmarshal(payload, &calls); err != nil || len(calls) == 0 {
		return nil, errors.New("AGENT_TOOL_CALLS_INVALID")
	}
	ids := map[string]struct{}{}
	normalized := make([]any, 0, len(calls))
	for _, value := range calls {
		call, ok := value.(map[string]any)
		if !ok || stringValue(call["type"]) != "function" {
			return nil, errors.New("AGENT_TOOL_CALL_INVALID")
		}
		id := strings.TrimSpace(stringValue(call["id"]))
		function, ok := call["function"].(map[string]any)
		if id == "" || !ok || strings.TrimSpace(stringValue(function["name"])) == "" {
			return nil, errors.New("AGENT_TOOL_CALL_INVALID")
		}
		if _, duplicate := ids[id]; duplicate {
			return nil, errors.New("AGENT_TOOL_CALL_ID_DUPLICATE")
		}
		ids[id] = struct{}{}
		arguments, err := normalizeToolArguments(function["arguments"])
		if err != nil {
			return nil, err
		}
		function["arguments"] = arguments
		call["function"] = function
		normalized = append(normalized, call)
	}
	return normalized, nil
}

func normalizeToolArguments(raw any) (string, error) {
	switch value := raw.(type) {
	case string:
		return canonicalJSONString(value)
	case map[string]any:
		payload, err := json.Marshal(value)
		if err != nil {
			return "", errors.New("AGENT_TOOL_ARGUMENTS_INVALID")
		}
		return string(payload), nil
	default:
		return "", errors.New("AGENT_TOOL_ARGUMENTS_INVALID")
	}
}

func normalizeResponseContent(raw any) (string, error) {
	if text, ok := raw.(string); ok {
		return text, nil
	}
	switch raw.(type) {
	case map[string]any, []any, float64, bool:
		payload, err := json.Marshal(raw)
		if err == nil {
			return string(payload), nil
		}
	}
	return "", errors.New("AGENT_RESPONSE_CONTENT_INVALID")
}

func canonicalJSONString(raw string) (string, error) {
	if !json.Valid([]byte(raw)) {
		return "", errors.New("AGENT_TOOL_ARGUMENTS_INVALID")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", errors.New("AGENT_TOOL_ARGUMENTS_INVALID")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return "", errors.New("AGENT_TOOL_ARGUMENTS_INVALID")
	}
	return string(payload), nil
}

func enforceResponseFormat(payload map[string]any, content any) error {
	format, _ := payload["response_format"].(map[string]any)
	typeName := stringValue(format["type"])
	if typeName != "json_object" && typeName != "json_schema" {
		return nil
	}
	text, ok := content.(string)
	if !ok || !json.Valid([]byte(strings.TrimSpace(text))) {
		return errors.New("AGENT_RESPONSE_JSON_INVALID")
	}
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}
