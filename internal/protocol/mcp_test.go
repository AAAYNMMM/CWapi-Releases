package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMCPSubjectRoundTrip(t *testing.T) {
	subject, err := BuildMCPSubject(MCPFamilyRequest, "REQ123")
	if err != nil {
		t.Fatal(err)
	}
	if subject != "[CWapi/MCP/1][MCP_REQUEST][REQ123]" {
		t.Fatalf("subject = %q", subject)
	}
	parsed, err := ParseMCPSubject(subject)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Family != MCPFamilyRequest || parsed.RequestID != "REQ123" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if _, err := ParseMCPSubject("[CWapi/MCP/1][MCP_UNKNOWN][REQ123]"); err == nil {
		t.Fatal("unknown MCP family must fail")
	}
}

func TestDecodeMCPRequestCanonicalizesParamsAndFingerprint(t *testing.T) {
	first := []byte(`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","method":"tools/call","params":{"z":{"b":2,"a":1},"a":true}}`)
	second := []byte(`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","method":"tools/call","params":{"a":true,"z":{"a":1,"b":2}}}`)

	a, err := DecodeMCPRequest(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := DecodeMCPRequest(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(a.Params) != `{"a":true,"z":{"a":1,"b":2}}` {
		t.Fatalf("canonical params = %s", a.Params)
	}
	af, err := a.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	bf, err := b.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if af != bf || len(af) != 64 {
		t.Fatalf("fingerprints: %q %q", af, bf)
	}
}

func TestMCPRequestProjectContextParticipatesInFingerprint(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	request, err := DecodeMCPRequest([]byte(`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","project_id":"prj-0123456789abcdef01234567","expected_commit":"` + strings.ToUpper(sha) + `","method":"mcpServer/tool/call","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.ExpectedCommit != sha {
		t.Fatalf("expected commit not canonicalized: %q", request.ExpectedCommit)
	}
	first, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	request.ExpectedCommit = "1123456789abcdef0123456789abcdef01234567"
	second, err := request.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("expected_commit must participate in request identity")
	}
	if _, err := DecodeMCPRequest([]byte(`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","project_id":"prj-0123456789abcdef01234567","method":"mcpServer/tool/call","params":{}}`)); err == nil {
		t.Fatal("partial project context must fail")
	}
}

func TestDecodeMCPRequestRejectsInvalidSurface(t *testing.T) {
	cases := []string{
		`{"schema":"cwapi.mcp.request.v0","protocol_version":"cwapi-mcp/1","request_id":"REQ123","method":"tools/call","params":{}}`,
		`{"schema":"cwapi.mcp.request.v1","protocol_version":"wrong","request_id":"REQ123","method":"tools/call","params":{}}`,
		`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"bad id","method":"tools/call","params":{}}`,
		`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","method":"TOOLS CALL","params":{}}`,
		`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","method":"tools/call","params":[]}`,
		`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","method":"tools/call","params":{},"unexpected":true}`,
	}
	for _, input := range cases {
		if _, err := DecodeMCPRequest([]byte(input)); err == nil {
			t.Fatalf("expected failure for %s", input)
		}
	}
}

func TestMCPResponseStatusAndErrorContract(t *testing.T) {
	completed := MCPResponse{
		Schema: MCPResponseSchema, ProtocolVersion: MCPProtocolVersion, RequestID: "REQ123",
		Status: MCPStatusCompleted, Result: json.RawMessage(`{"ok":true}`),
	}
	payload, _ := json.Marshal(completed)
	decoded, err := DecodeMCPResponse(payload)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Status != MCPStatusCompleted || string(decoded.Result) != `{"ok":true}` {
		t.Fatalf("decoded = %#v", decoded)
	}

	blocked := MCPResponse{
		Schema: MCPResponseSchema, ProtocolVersion: MCPProtocolVersion, RequestID: "REQ124",
		Status: MCPStatusBlocked,
		Error:  &MCPError{Code: "RUNTIME_MISSING", Category: "runtime", Message: "python is unavailable", Retryable: false, MissingRuntime: "python"},
	}
	payload, _ = json.Marshal(blocked)
	if _, err := DecodeMCPResponse(payload); err != nil {
		t.Fatal(err)
	}

	invalid := completed
	invalid.Error = &MCPError{Code: "BAD_ERROR", Category: "test", Message: "must not exist on completed"}
	payload, _ = json.Marshal(invalid)
	if _, err := DecodeMCPResponse(payload); err == nil {
		t.Fatal("completed response with error must fail")
	}

	missingError := completed
	missingError.Status = MCPStatusFailed
	payload, _ = json.Marshal(missingError)
	if _, err := DecodeMCPResponse(payload); err == nil {
		t.Fatal("non-completed response without structured error must fail")
	}
}

func TestMCPEventDecode(t *testing.T) {
	event, err := DecodeMCPEvent([]byte(`{"schema":"cwapi.mcp.event.v1","protocol_version":"cwapi-mcp/1","request_id":"REQ123","sequence":2,"event":"tool.progress","data":{"percent":50}}`))
	if err != nil {
		t.Fatal(err)
	}
	if event.Sequence != 2 || event.Event != "tool.progress" || string(event.Data) != `{"percent":50}` {
		t.Fatalf("event = %#v", event)
	}
}

func TestMCPResourceValidation(t *testing.T) {
	response := MCPResponse{
		Schema: MCPResponseSchema, ProtocolVersion: MCPProtocolVersion, RequestID: "REQ123", Status: MCPStatusCompleted,
		Result:    json.RawMessage(`{}`),
		Resources: []MCPResourceRef{{URI: "cwapi://requests/REQ123/stdout", SHA256: strings.Repeat("a", 64), SizeBytes: 10}},
	}
	payload, _ := json.Marshal(response)
	if _, err := DecodeMCPResponse(payload); err != nil {
		t.Fatal(err)
	}
	response.Resources[0].URI = "bad\nuri"
	payload, _ = json.Marshal(response)
	if _, err := DecodeMCPResponse(payload); err == nil {
		t.Fatal("multiline resource URI must fail")
	}
}
