package app

import (
	"encoding/json"
	"strings"
	"testing"

	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

func TestSlackMessageSnapshotRedactsOnlyV2TopLevelSystemToken(t *testing.T) {
	token := strings.Repeat("a", 64)
	body := `{"request_id":"REQ1","system_token":"` + token +
		`","nested":{"system_token":"` + token + `"},"digest":"` + token + `"}`
	snapshot := slackMessageSnapshot(slackcore.Message{
		Subject: "[CWapi/MCP/2][MCP_RESPONSE][REQ1]",
		Body:    body,
	})
	if strings.Contains(snapshot.Body, `"system_token":"`+token+`"`) &&
		!strings.Contains(snapshot.Body, `"nested":{"system_token":"`+token+`"}`) {
		t.Fatal("top-level system token was not redacted")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(snapshot.Body), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["system_token"] != "[REDACTED]" {
		t.Fatalf("top-level system_token = %v", decoded["system_token"])
	}
	nested := decoded["nested"].(map[string]any)
	if nested["system_token"] != token || decoded["digest"] != token {
		t.Fatal("schema-aware redaction modified nested or unrelated 64-hex data")
	}
}

func TestSlackMessageSnapshotDoesNotRewriteOtherBodies(t *testing.T) {
	token := strings.Repeat("b", 64)
	for _, test := range []struct {
		name, subject, body string
	}{
		{"v1", "[CWapi/MCP/1][MCP_RESPONSE][REQ1]", `{"system_token":"` + token + `"}`},
		{"empty", "[CWapi/MCP/2][MCP_RESPONSE][REQ1]", `{"system_token":""}`},
		{"invalid", "[CWapi/MCP/2][MCP_RESPONSE][REQ1]", `{"system_token":`},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := slackMessageSnapshot(slackcore.Message{Subject: test.subject, Body: test.body}).Body
			if got != test.body {
				t.Fatalf("body changed: %q", got)
			}
		})
	}
}
