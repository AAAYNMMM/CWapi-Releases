package gateway

import (
	"strings"
	"unicode/utf8"
)

func mcpToolFailure(value any) (string, bool) {
	object, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	isError, ok := object["isError"].(bool)
	if !ok || !isError {
		return "", false
	}
	message := firstMCPText(object["content"])
	if message == "" {
		message = "MCP tool reported an execution error"
	}
	return boundedMCPText(message, 3000), true
}

func boundedMCPText(value string, maxBytes int) string {
	if maxBytes < 1 {
		return ""
	}
	for len(value) > maxBytes {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}

func firstMCPText(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok || item["type"] != "text" {
			continue
		}
		if text, ok := item["text"].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}
