package app

import (
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/config"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

func TestProtocolHelpExplainsDiscoveryAndConfiguredProjectID(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		ID: "prj-0123456789abcdef01234567", DisplayName: "example-project", Repository: "example/example-project", LocalPath: `E:\private\example-project`,
	}}
	text := protocolHelpText(cfg, "CWAPIHELP4000")
	for _, required := range []string{"CWapi v1.6.0", "source_commit=", "projects/list", "project_id", "expected_commit", "command + argv", "powershell.exe", ".venv/Scripts/python.exe", "node_modules/.bin/tool.cmd", "C:/...", "CWAPIHELP4000", "prj-0123456789abcdef01234567", "example/example-project"} {
		if !strings.Contains(text, required) {
			t.Fatalf("help missing %q: %s", required, text)
		}
	}
	if strings.Contains(text, `E:\private`) {
		t.Fatalf("help leaked local path: %s", text)
	}
	if requestID := protocolHelpRequestID(slackcore.Message{MessageTS: "1724169600.123456"}); requestID != "CWAPIHELP1724169600123456" {
		t.Fatalf("help request id=%q", requestID)
	}
}
