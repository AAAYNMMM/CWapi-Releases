package slack

import "testing"

func TestDecodeProtocolAllowsWhitespaceDelimitedSlackTrailer(t *testing.T) {
	const subject = "[CWapi/MCP/2][MCP_REQUEST][REQ123]"
	const body = `{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"mcpServerStatus/list","params":{}}`
	text := "+++\n" + subject + "\n" + body + "\n+++ *发送工具* <@U0123456789>"

	gotSubject, gotBody, ok := DecodeProtocol(text)
	if !ok {
		t.Fatal("protocol frame with out-of-band Slack trailer was rejected")
	}
	if gotSubject != subject || gotBody != body {
		t.Fatalf("decoded subject=%q body=%q", gotSubject, gotBody)
	}
}

func TestDecodeProtocolKeepsTrailerOutOfBody(t *testing.T) {
	const subject = "[CWapi/MCP/2][MCP_RESPONSE][REQ123]"
	text := "+++\n" + subject + "\nline1\nline2\n+++ metadata\nadditional trailer"

	gotSubject, gotBody, ok := DecodeProtocol(text)
	if !ok {
		t.Fatal("protocol frame with trailer was rejected")
	}
	if gotSubject != subject || gotBody != "line1\nline2" {
		t.Fatalf("decoded subject=%q body=%q", gotSubject, gotBody)
	}
}

func TestDecodeProtocolRejectsClosingSuffixWithoutWhitespaceBoundary(t *testing.T) {
	text := "+++\n[CWapi/MCP/2][MCP_REQUEST][REQ123]\nbody\n++++not-a-frame"
	if _, _, ok := DecodeProtocol(text); ok {
		t.Fatal("non-whitespace closing suffix must not terminate protocol frame")
	}
}

func TestDecodeProtocolRequiresOpeningFrameFirst(t *testing.T) {
	text := "preamble\n+++\n[CWapi/MCP/2][MCP_REQUEST][REQ123]\nbody\n+++"
	if _, _, ok := DecodeProtocol(text); ok {
		t.Fatal("protocol traffic with a preamble must be rejected")
	}
}
