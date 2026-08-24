package processcontract

import (
	"strings"
	"testing"
)

func TestDecodeStartStrictSurface(t *testing.T) {
	value, err := DecodeStart(map[string]any{
		"command": "C:/Tools/node.exe", "argv": []any{"a", "b"}, "cwd": "sub/dir",
	})
	if err != nil || value.Command != "C:/Tools/node.exe" || len(value.Argv) != 2 || value.CWD != "sub/dir" {
		t.Fatalf("value=%#v err=%v", value, err)
	}
	for _, invalid := range []map[string]any{
		{},
		{"command": "node", "runtime": "node"},
		{"command": `C:\\Tools\\node.exe`},
		{"command": "node", "cwd": `sub\\dir`},
		{"command": `"C:/Program Files/node.exe"`},
		{"command": " node"},
		{"command": "node", "cwd": "../outside"},
		{"command": "node", "cwd": "a/./b"},
		{"command": "node", "cwd": "C:/outside"},
		{"command": "node", "cwd": "/outside"},
		{"command": "node", "argv": []any{1}},
		{"command": "node", "argv": make([]any, MaxArgvItems+1)},
		{"command": strings.Repeat("x", MaxCommandBytes+1)},
	} {
		if _, err := DecodeStart(invalid); err == nil {
			t.Fatalf("invalid start accepted: %#v", invalid)
		}
	}
}

func TestDecodeProcessIDStrict(t *testing.T) {
	want := "proc-0123456789abcdef01234567"
	if got, err := DecodeProcessID(map[string]any{"process_id": want}); err != nil || got != want {
		t.Fatalf("got=%q err=%v", got, err)
	}
	for _, value := range []map[string]any{
		{}, {"process_id": "proc-ABC"}, {"process_id": want, "extra": true},
	} {
		if _, err := DecodeProcessID(value); err == nil {
			t.Fatalf("invalid process id accepted: %#v", value)
		}
	}
}
