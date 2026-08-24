package slack

import (
	"encoding/json"
	"testing"
)

func TestMCPProtocolRoundTripUsesSameThinSlackFrame(t *testing.T) {
	subject := "[CWapi/MCP/2][MCP_REQUEST][REQ123]"
	body := `{"schema":"cwapi.mcp.request.v2","request_id":"REQ123"}`
	text, err := EncodeProtocol(subject, body)
	if err != nil {
		t.Fatal(err)
	}
	decodedSubject, decodedBody, ok := DecodeProtocol(text)
	if !ok || decodedSubject != subject || decodedBody != body {
		t.Fatalf("decoded: subject=%q body=%q ok=%v", decodedSubject, decodedBody, ok)
	}
}

func TestProtocolRejectsUnknownCWapiPrefix(t *testing.T) {
	if _, err := EncodeProtocol("[CWapi/2][MCP_REQUEST][REQ123]", `{}`); err == nil {
		t.Fatal("unknown protocol prefix must be rejected")
	}
	if _, _, ok := DecodeProtocol("+++\n[CWapi/2][MCP_REQUEST][REQ123]\n{}\n+++"); ok {
		t.Fatal("unknown protocol prefix must not decode")
	}
}

func TestProtocolCandidateRecognizesMissingFrame(t *testing.T) {
	missingFrame := "[CWapi/MCP/1][MCP_REQUEST][GPTCWAPIFIXNOFRAME06]\n" +
		`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1"}`
	if _, _, ok := DecodeProtocol(missingFrame); ok {
		t.Fatal("missing frame must not decode")
	}
	if !IsProtocolCandidate(missingFrame) {
		t.Fatal("missing frame must be surfaced for a usage reply")
	}
	if IsProtocolCandidate("ordinary Slack conversation") {
		t.Fatal("ordinary conversation must remain ignored")
	}
}

func TestMCPProtocolRestoresSlackAutolinkedJSONURL(t *testing.T) {
	text := "+++\n[CWapi/MCP/2][MCP_REQUEST][REQ123]\n" +
		`{"params":{"arguments":{"url":"<https://example.com/path?a=1&b=2>"}}}` + "\n+++"
	_, body, ok := DecodeProtocol(text)
	if !ok {
		t.Fatal("Slack protocol message did not decode")
	}
	var decoded struct {
		Params struct {
			Arguments struct {
				URL string `json:"url"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Params.Arguments.URL != "https://example.com/path?a=1&b=2" {
		t.Fatalf("url = %q", decoded.Params.Arguments.URL)
	}
}

func TestMCPProtocolRestoresSlackEscapedJavaScriptAndHTML(t *testing.T) {
	text := "+++\n[CWapi/MCP/2][MCP_REQUEST][REQ123]\n" +
		`{"params":{"arguments":{"function":"() =&gt; '&lt;main&gt;&amp;'"}}}` + "\n+++"
	_, body, ok := DecodeProtocol(text)
	if !ok {
		t.Fatal("Slack protocol message did not decode")
	}
	var decoded struct {
		Params struct {
			Arguments struct {
				Function string `json:"function"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Params.Arguments.Function != "() => '<main>&'" {
		t.Fatalf("function=%q", decoded.Params.Arguments.Function)
	}
}
