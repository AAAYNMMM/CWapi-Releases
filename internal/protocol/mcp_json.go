package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

func canonicalJSONObject(raw json.RawMessage, maxBytes int, label string) (json.RawMessage, error) {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if len(raw) > maxBytes {
		return nil, fmt.Errorf("%s_TOO_LARGE: %d", label, len(raw))
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%s_INVALID: %w", label, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("%s_INVALID: trailing JSON value", label)
		}
		return nil, fmt.Errorf("%s_INVALID: %w", label, err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("%s_MUST_BE_OBJECT", label)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%s_ENCODE_FAILED: %w", label, err)
	}
	return encoded, nil
}

func CanonicalObject(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("MCP_CANONICAL_OBJECT_ENCODE_FAILED: %w", err)
	}
	return canonicalJSONObject(raw, MaxMCPBodyBytes, "MCP_CANONICAL_OBJECT")
}

func containsJSONKey(raw json.RawMessage, target string) bool {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return false
	}
	return valueContainsJSONKey(value, target)
}

func validateMCPSystemToken(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil
	}
	root, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	for key, child := range root {
		if key == "system_token" {
			token, ok := child.(string)
			if !ok || token != "" && !mcpSystemToken.MatchString(token) {
				return errors.New("MCP_SYSTEM_TOKEN_INVALID")
			}
			continue
		}
		if valueContainsJSONKey(child, "system_token") {
			return errors.New("MCP_SYSTEM_TOKEN_INVALID")
		}
	}
	return nil
}

func valueContainsJSONKey(value any, target string) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == target || valueContainsJSONKey(child, target) {
				return true
			}
		}
	case []any:
		for _, child := range typed {
			if valueContainsJSONKey(child, target) {
				return true
			}
		}
	}
	return false
}
