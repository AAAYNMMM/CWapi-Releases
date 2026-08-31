package codex

import (
	"encoding/json"
	"strconv"
)

func numberToInt64(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case json.Number:
		value, err := typed.Int64()
		return value, err == nil
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	case string:
		value, err := strconv.ParseInt(typed, 10, 64)
		return value, err == nil
	default:
		return 0, false
	}
}
