package codex

import "time"

type MCPCall struct {
	Method  string
	Params  map[string]any
	Timeout time.Duration
	CWD     string
}
