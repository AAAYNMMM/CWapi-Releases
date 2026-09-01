package agentprotocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

func decodeJSON(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values")
		}
		return err
	}
	return nil
}

func cloneJSONMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var cloned map[string]any
	if err := decodeJSON(payload, &cloned); err != nil {
		return nil
	}
	return cloned
}

func canonicalJSONText(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(payload)
}

func canonicalJSONObject(value any, code string, kind ErrorKind) (map[string]any, error) {
	switch typed := value.(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return cloneJSONMap(typed), nil
	case string:
		var object map[string]any
		if err := decodeJSON([]byte(typed), &object); err != nil || object == nil {
			return nil, &CanonicalError{Code: code, Kind: kind}
		}
		return object, nil
	default:
		return nil, &CanonicalError{Code: code, Kind: kind}
	}
}

func canonicalContent(value any, code string, kind ErrorKind) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case map[string]any, []any, json.Number, float64, bool:
		payload, err := json.Marshal(typed)
		if err == nil {
			return string(payload), nil
		}
	}
	return "", &CanonicalError{Code: code, Kind: kind}
}
