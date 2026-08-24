package app

import (
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/config"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

func TestProtocolHelpExplainsStrictV2Contract(t *testing.T) {
	text := protocolHelpText(config.Default(), "CWAPIHELP4000")
	for _, required := range []string{"source_commit=", "[CWapi/MCP/2]", "cwapi.mcp.request.v2", "cwapi-mcp/2", "repository_url", "expected_commit", "process_start", "System Token", "CWAPIHELP4000"} {
		if !strings.Contains(text, required) {
			t.Fatalf("help missing %q: %s", required, text)
		}
	}
	for _, retired := range []string{"projects/list", "project_id", "runtime=python", "entrypoint"} {
		if strings.Contains(text, retired) {
			t.Fatalf("help contains retired contract %q: %s", retired, text)
		}
	}
	if requestID := protocolHelpRequestID(slackcore.Message{MessageTS: "1724169600.123456"}); requestID != "CWAPIHELP1724169600123456" {
		t.Fatalf("help request id=%q", requestID)
	}
}
