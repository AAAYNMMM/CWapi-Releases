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
	if subject != "[CWapi/MCP/2][MCP_REQUEST][REQ123]" {
		t.Fatalf("subject = %q", subject)
	}
	parsed, err := ParseMCPSubject(subject)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Family != MCPFamilyRequest || parsed.RequestID != "REQ123" {
		t.Fatalf("parsed = %#v", parsed)
	}
	if _, err := ParseMCPSubject("[CWapi/MCP/2][MCP_UNKNOWN][REQ123]"); err == nil {
		t.Fatal("unknown MCP family must fail")
	}
	if _, err := ParseMCPSubject("[CWapi/MCP/1][MCP_REQUEST][REQ123]"); err == nil || !strings.Contains(err.Error(), "USE_V2") {
		t.Fatalf("v1 subject should return v2 guidance, got %v", err)
	}
}

func TestDecodeMCPRequestCanonicalizesParamsAndFingerprint(t *testing.T) {
	first := []byte(`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"mcpServer/tool/call","params":{"z":{"b":2,"a":1},"a":true}}`)
	second := []byte(`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"mcpServer/tool/call","params":{"a":true,"z":{"a":1,"b":2}}}`)

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

func TestMCPRequestRepositoryContextParticipatesInFingerprint(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	request, err := DecodeMCPRequest([]byte(`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","repository_url":"https://github.com/Owner/Repo.git","expected_commit":"` + strings.ToUpper(sha) + `","method":"mcpServer/tool/call","params":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if request.ExpectedCommit != sha {
		t.Fatalf("expected commit not canonicalized: %q", request.ExpectedCommit)
	}
	if request.RepositoryURL != "https://github.com/Owner/Repo" {
		t.Fatalf("repository URL not canonicalized: %q", request.RepositoryURL)
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
	if _, err := DecodeMCPRequest([]byte(`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","repository_url":"https://github.com/o/r","method":"mcpServer/tool/call","params":{}}`)); err == nil {
		t.Fatal("partial repository context must fail")
	}
}

func TestFingerprintUsesNormalizedIdentityAndExcludesRequestAndToken(t *testing.T) {
	base := MCPRequest{
		RequestID: "REQONE", RepositoryURL: "https://github.com/Owner/Repo", ExpectedCommit: strings.Repeat("a", 40),
		Method: MCPMethodToolCall, Params: json.RawMessage(`{"server":"cwapi","tool":"process_start","arguments":{"command":"node"}}`),
		SystemToken: strings.Repeat("b", 64),
	}
	first, err := base.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	base.RequestID = "REQTWO"
	base.RepositoryURL = "https://github.com/owner/repo.git"
	base.SystemToken = strings.Repeat("c", 64)
	second, err := base.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("normalized equivalent requests differ: %s %s", first, second)
	}
}

func TestDecodeMCPRequestRejectsInvalidSurface(t *testing.T) {
	cases := []string{
		`{"schema":"cwapi.mcp.request.v0","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"mcpServer/tool/call","params":{}}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"wrong","request_id":"REQ123","method":"mcpServer/tool/call","params":{}}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"bad id","method":"mcpServer/tool/call","params":{}}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"TOOLS CALL","params":{}}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"projects/list","params":{}}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"mcpServer/tool/call","params":[]}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","method":"mcpServer/tool/call","params":{},"unexpected":true}`,
	}
	for _, input := range cases {
		if _, err := DecodeMCPRequest([]byte(input)); err == nil {
			t.Fatalf("expected failure for %s", input)
		}
	}
}

func TestSystemTokenLocationAndResponseContract(t *testing.T) {
	token := strings.Repeat("a", 64)
	request, err := DecodeMCPRequest([]byte(`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQTOKEN","repository_url":"https://github.com/o/r","expected_commit":"0123456789abcdef0123456789abcdef01234567","method":"mcpServer/tool/call","params":{"server":"cwapi","tool":"process_start","arguments":{}},"system_token":"` + token + `"}`))
	if err != nil || request.SystemToken != token {
		t.Fatalf("valid top-level Token rejected: request=%#v err=%v", request, err)
	}
	for _, body := range []string{
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQTOKEN","method":"mcpServer/tool/call","params":{"system_token":"` + token + `"}}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQTOKEN","method":"mcpServer/tool/call","params":{},"system_token":"` + strings.ToUpper(token) + `"}`,
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQTOKEN","method":"mcpServer/tool/call","params":{},"system_token":null}`,
	} {
		if _, err := DecodeMCPRequest([]byte(body)); err == nil || !strings.Contains(err.Error(), "MCP_SYSTEM_TOKEN_INVALID") {
			t.Fatalf("invalid Token should use stable code: %v", err)
		}
	}
	response := MCPResponse{
		Schema: MCPResponseSchema, ProtocolVersion: MCPProtocolVersion, RequestID: "REQTOKEN",
		Status: MCPStatusBlocked, Error: &MCPError{Code: "PERMISSION_DENIED", Category: "permission", Message: "blocked"}, SystemToken: token,
	}
	payload, _ := json.Marshal(response)
	if _, err := DecodeMCPResponse(payload); err != nil {
		t.Fatalf("valid Token response rejected: %v", err)
	}
	response.Status = MCPStatusFailed
	payload, _ = json.Marshal(response)
	if _, err := DecodeMCPResponse(payload); err == nil {
		t.Fatal("Token outside blocked PERMISSION_DENIED response must fail")
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
	event, err := DecodeMCPEvent([]byte(`{"schema":"cwapi.mcp.event.v2","protocol_version":"cwapi-mcp/2","request_id":"REQ123","sequence":2,"event":"tool.progress","data":{"percent":50}}`))
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
