package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

type featureConfigProvider struct{ value config.Config }

func (p featureConfigProvider) Snapshot() config.Config { return p.value.Clone() }

type featurePoster struct {
	subjects []string
	bodies   []string
}

func (p *featurePoster) Post(_ context.Context, subject, body, _ string) (PostedMessage, error) {
	p.subjects = append(p.subjects, subject)
	p.bodies = append(p.bodies, body)
	return PostedMessage{MessageID: "posted", MessageTS: "2.000"}, nil
}

type featureToolhost struct{ result any }

func (h featureToolhost) CallMCP(context.Context, string, map[string]any, time.Duration, MCPExecutionContext) (any, error) {
	return h.result, nil
}

type captureToolhost struct{ params map[string]any }

func (h *captureToolhost) CallMCP(_ context.Context, _ string, params map[string]any, _ time.Duration, _ MCPExecutionContext) (any, error) {
	h.params = params
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": "1"}}}, nil
}

type featureContextResolver struct {
	execution MCPExecutionContext
	err       error
}

func (r featureContextResolver) PrepareMCPContext(context.Context, string, string, string) (MCPExecutionContext, func(), error) {
	return r.execution, nil, r.err
}

func newFeatureGateway(t *testing.T, cfg config.Config, host MCPToolhost) (*Gateway, *state.Store, *featurePoster) {
	t.Helper()
	store, err := state.Open(filepath.Join(t.TempDir(), "state", "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	hub, err := observability.New(context.Background(), store, 100, 500)
	if err != nil {
		t.Fatal(err)
	}
	poster := &featurePoster{}
	gateway, err := NewMCP(featureConfigProvider{value: cfg}, store, poster, hub)
	if err != nil {
		t.Fatal(err)
	}
	if host != nil {
		if err := gateway.AttachMCPRuntime(MCPRuntime{Toolhost: host}); err != nil {
			t.Fatal(err)
		}
	}
	return gateway, store, poster
}

func featureMessage(t *testing.T, request protocol.MCPRequest) slackcore.Message {
	t.Helper()
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := protocol.BuildMCPSubject(protocol.MCPFamilyRequest, request.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	return slackcore.Message{
		MessageID: "slack:C12345678:1.000", ChannelID: "C12345678", MessageTS: "1.000",
		Subject: subject, Body: string(body),
	}
}

func decodeFeatureResponse(t *testing.T, poster *featurePoster) protocol.MCPResponse {
	t.Helper()
	if len(poster.bodies) != 1 {
		t.Fatalf("posted responses=%d", len(poster.bodies))
	}
	response, err := protocol.DecodeMCPResponse([]byte(poster.bodies[0]))
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestProjectsListDiscoversStableIDsWithoutLocalPaths(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{
		{ID: "prj-bbbbbbbbbbbbbbbbbbbbbbbb", DisplayName: "Zulu", Repository: "owner/zulu", LocalPath: `E:\secret\zulu`},
		{ID: "prj-aaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "Alpha", Repository: "owner/alpha", LocalPath: `E:\secret\alpha`},
	}
	gateway, _, poster := newFeatureGateway(t, cfg, nil)
	request := protocol.MCPRequest{
		Schema: protocol.MCPRequestSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: "REQPROJECTSLIST", Method: "projects/list", Params: json.RawMessage(`{}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Status != protocol.MCPStatusCompleted {
		t.Fatalf("response=%#v", response)
	}
	var result struct {
		Schema   string                 `json:"schema"`
		Projects []projectDiscoveryItem `json:"projects"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Schema != "cwapi.projects.list.v1" || len(result.Projects) != 2 || result.Projects[0].ProjectID != "prj-aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("result=%#v", result)
	}
	if strings.Contains(string(response.Result), "secret") || strings.Contains(string(response.Result), "local_path") {
		t.Fatalf("project discovery leaked a local path: %s", response.Result)
	}
}

func TestMCPServerStatusIncludesCWapiProjectDiscovery(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		ID: "prj-aaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "example-project",
		Repository: "example/example-project", LocalPath: `E:\private\example-project`,
	}}
	gateway, _, poster := newFeatureGateway(t, cfg, featureToolhost{result: map[string]any{"data": []any{}}})
	request := protocol.MCPRequest{
		Schema: protocol.MCPRequestSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: "REQSTATUSDISCOVERY", Method: "mcpServerStatus/list", Params: json.RawMessage(`{}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	var result struct {
		Data  []any `json:"data"`
		CWapi struct {
			Schema              string                 `json:"schema"`
			SourceCommit        string                 `json:"source_commit"`
			Projects            []projectDiscoveryItem `json:"projects"`
			RequestMethods      []string               `json:"request_methods"`
			ProjectsListRequest struct {
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			} `json:"projects_list_request"`
		} `json:"cwapi"`
	}
	if err := json.Unmarshal(response.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result.Data == nil || result.CWapi.Schema != "cwapi.discovery.v1" || result.CWapi.SourceCommit == "" ||
		len(result.CWapi.Projects) != 1 || result.CWapi.Projects[0].ProjectID != "prj-aaaaaaaaaaaaaaaaaaaaaaaa" ||
		result.CWapi.ProjectsListRequest.Method != "projects/list" || !containsString(result.CWapi.RequestMethods, "projects/list") {
		t.Fatalf("result=%s", response.Result)
	}
	if strings.Contains(string(response.Result), `E:\private`) || strings.Contains(string(response.Result), "local_path") {
		t.Fatalf("status discovery leaked a local path: %s", response.Result)
	}
}

func TestMCPRequestPersistsAndEmitsRealLifecycle(t *testing.T) {
	gateway, store, poster := newFeatureGateway(t, config.Default(), featureToolhost{result: map[string]any{"servers": []any{}}})
	request := protocol.MCPRequest{
		Schema: protocol.MCPRequestSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: "REQLIFECYCLE", Method: "mcpServerStatus/list", Params: json.RawMessage(`{}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Status != protocol.MCPStatusCompleted {
		t.Fatalf("response=%#v", response)
	}
	record, err := store.MCPRequestByID(context.Background(), request.RequestID)
	if err != nil {
		t.Fatal(err)
	}
	if record.ExecutionState != "completed" || record.DeliveryState != state.MCPDeliveryDelivered {
		t.Fatalf("stored=%#v", record)
	}
	events, err := store.ExecutionEventsForTask(context.Background(), request.RequestID, 20)
	if err != nil {
		t.Fatal(err)
	}
	var statuses []string
	for _, event := range events {
		statuses = append(statuses, event.Status)
	}
	want := []string{state.MCPExecutionReceived, state.MCPExecutionRunning, "completed", state.MCPDeliveryDelivered}
	if strings.Join(statuses, ",") != strings.Join(want, ",") {
		t.Fatalf("lifecycle=%v want=%v", statuses, want)
	}
}

func TestInvalidFramedRequestRepliesWithDiscoveryUsage(t *testing.T) {
	gateway, _, poster := newFeatureGateway(t, config.Default(), nil)
	subject, err := protocol.BuildMCPSubject(protocol.MCPFamilyRequest, "REQINVALIDJSON")
	if err != nil {
		t.Fatal(err)
	}
	message := slackcore.Message{MessageID: "slack:C12345678:1.000", ChannelID: "C12345678", MessageTS: "1.000", Subject: subject, Body: `{`}
	if err := gateway.HandleMCPMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Error == nil || !strings.Contains(response.Error.Message, "projects/list") {
		t.Fatalf("response=%#v", response)
	}
}

func TestSlackEscapedBrowserEvaluateReachesToolhostAsJavaScript(t *testing.T) {
	host := &captureToolhost{}
	gateway, _, _ := newFeatureGateway(t, config.Default(), host)
	text := "+++\n[CWapi/MCP/1][MCP_REQUEST][REQEVALUATE]\n" +
		`{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"REQEVALUATE","method":"mcpServer/tool/call","params":{"server":"playwright","tool":"browser_evaluate","arguments":{"function":"() =&gt; 1"}}}` +
		"\n+++"
	subject, body, ok := slackcore.DecodeProtocol(text)
	if !ok {
		t.Fatal("Slack frame did not decode")
	}
	if err := gateway.HandleMCPMessage(context.Background(), slackcore.Message{
		MessageID: "slack:C12345678:2.000", ChannelID: "C12345678", MessageTS: "2.000", Subject: subject, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
	arguments, _ := host.params["arguments"].(map[string]any)
	if arguments["function"] != "() => 1" {
		t.Fatalf("toolhost function=%#v", arguments["function"])
	}
}

func TestPrepareCWapiProcessCallInjectsTrustedExactCommitContext(t *testing.T) {
	gateway, _, _ := newFeatureGateway(t, config.Default(), nil)
	request := protocol.MCPRequest{RequestID: "REQPROCESSSTART", Method: "mcpServer/tool/call", ProjectID: "prj-aaaaaaaaaaaaaaaaaaaaaaaa", ExpectedCommit: strings.Repeat("a", 40)}
	arguments := map[string]any{"runtime": "python", "entrypoint": "server.py"}
	params := map[string]any{"server": "cwapi", "tool": "process_start", "arguments": arguments}
	execution := MCPExecutionContext{CWD: `E:\managed\workspace`, ExpectedCommit: strings.Repeat("b", 40)}
	if response := gateway.prepareCWapiProcessCall(request, execution, params); response != nil {
		t.Fatalf("response=%#v", response)
	}
	if arguments["_cwapi_workspace"] != execution.CWD || arguments["_cwapi_expected_commit"] != execution.ExpectedCommit || arguments["_cwapi_request_id"] != request.RequestID {
		t.Fatalf("arguments=%#v", arguments)
	}
	spoofed := map[string]any{"_cwapi_workspace": `E:\other`}
	response := gateway.prepareCWapiProcessCall(request, execution, map[string]any{"server": "cwapi", "tool": "process_status", "arguments": spoofed})
	if response == nil || response.Error == nil || response.Error.Code != "MCP_PROCESS_CONTEXT_MANAGED" {
		t.Fatalf("spoof response=%#v", response)
	}
}

func TestProcessContextErrorIncludesConfiguredProjectID(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		ID: "prj-aaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "example-project",
		Repository: "example/example-project", LocalPath: `E:\private\example-project`,
	}}
	gateway, _, poster := newFeatureGateway(t, cfg, featureToolhost{})
	request := protocol.MCPRequest{
		Schema: protocol.MCPRequestSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: "REQPROCESSNOCONTEXT", Method: "mcpServer/tool/call",
		Params: json.RawMessage(`{"server":"cwapi","tool":"process_start","arguments":{"runtime":"python","entrypoint":"server.py"}}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Error == nil || response.Error.Code != "MCP_PROCESS_CONTEXT_REQUIRED" ||
		!strings.Contains(response.Error.Message, "project_id=prj-aaaaaaaaaaaaaaaaaaaaaaaa") ||
		!strings.Contains(response.Error.Message, "method=projects/list") {
		t.Fatalf("response=%#v", response)
	}
	if strings.Contains(response.Error.Message, `E:\private`) {
		t.Fatalf("context error leaked local path: %s", response.Error.Message)
	}
}

func TestUnknownProjectErrorIncludesCurrentDiscovery(t *testing.T) {
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		ID: "prj-aaaaaaaaaaaaaaaaaaaaaaaa", DisplayName: "example-project",
		Repository: "example/example-project", LocalPath: `E:\private\example-project`,
	}}
	gateway, _, poster := newFeatureGateway(t, cfg, nil)
	if err := gateway.AttachMCPRuntime(MCPRuntime{
		Toolhost: featureToolhost{}, ContextResolver: featureContextResolver{err: errors.New("MCP_PROJECT_NOT_FOUND: prj-bbbbbbbbbbbbbbbbbbbbbbbb")},
	}); err != nil {
		t.Fatal(err)
	}
	request := protocol.MCPRequest{
		Schema: protocol.MCPRequestSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: "REQUNKNOWNPROJECT", ProjectID: "prj-bbbbbbbbbbbbbbbbbbbbbbbb",
		ExpectedCommit: strings.Repeat("5", 40), Method: "mcpServerStatus/list", Params: json.RawMessage(`{}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Error == nil || response.Error.Code != "MCP_PROJECT_PREPARE_FAILED" ||
		!strings.Contains(response.Error.Message, "project_id=prj-aaaaaaaaaaaaaaaaaaaaaaaa") ||
		!strings.Contains(response.Error.Message, "example/example-project") {
		t.Fatalf("response=%#v", response)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestGatewayRejectsMismatchedPreparedExactCommitContext(t *testing.T) {
	gateway, _, poster := newFeatureGateway(t, config.Default(), nil)
	if err := gateway.AttachMCPRuntime(MCPRuntime{Toolhost: featureToolhost{}, ContextResolver: featureContextResolver{execution: MCPExecutionContext{
		ProjectID: "prj-bbbbbbbbbbbbbbbbbbbbbbbb", ExpectedCommit: strings.Repeat("b", 40), CWD: filepath.Join(t.TempDir(), "worktree"),
	}}}); err != nil {
		t.Fatal(err)
	}
	request := protocol.MCPRequest{
		Schema: protocol.MCPRequestSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: "REQCONTEXTMISMATCH", ProjectID: "prj-aaaaaaaaaaaaaaaaaaaaaaaa",
		ExpectedCommit: strings.Repeat("a", 40), Method: "mcpServerStatus/list", Params: json.RawMessage(`{}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Error == nil || response.Error.Code != "MCP_PROJECT_CONTEXT_MISMATCH" {
		t.Fatalf("response=%#v", response)
	}
}

func TestMCPToolErrorBecomesBoundedValidFailure(t *testing.T) {
	message := strings.Repeat("错", 2000)
	text, failed := mcpToolFailure(map[string]any{
		"isError": true,
		"content": []any{map[string]any{"type": "text", "text": message}},
	})
	if !failed || len(text) > 3000 || !utf8.ValidString(text) {
		t.Fatalf("failed=%v bytes=%d valid=%v", failed, len(text), utf8.ValidString(text))
	}
}

func TestStructuredLogIncludesActualCWapiProcessState(t *testing.T) {
	gateway, store, _ := newFeatureGateway(t, config.Default(), nil)
	request := protocol.MCPRequest{
		RequestID: "REQPROCESSSTATE", Method: "mcpServer/tool/call",
		Params: json.RawMessage(`{"server":"cwapi","tool":"process_start","arguments":{"runtime":"python","entrypoint":"server.py"}}`),
	}
	toolResult := map[string]any{"content": []any{map[string]any{
		"type": "text", "text": `{"process_id":"proc-aaaaaaaaaaaaaaaaaaaaaaaa","state":"running","runtime":"python","entrypoint":"server.py"}`,
	}}}
	encoded, err := json.Marshal(toolResult)
	if err != nil {
		t.Fatal(err)
	}
	gateway.emitMCPProcessState(request, protocol.MCPResponse{Status: protocol.MCPStatusCompleted, Result: encoded}, time.Now().Add(-time.Second).UnixMilli())
	events, err := store.ExecutionEventsForTask(context.Background(), request.RequestID, 10)
	if err != nil || len(events) != 1 || events[0].Status != "running" || !strings.Contains(events[0].DataJSON, `"process_state":"running"`) {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}

func TestStructuredLogIncludesGenericCommandIdentityWithoutArgv(t *testing.T) {
	gateway, store, _ := newFeatureGateway(t, config.Default(), nil)
	request := protocol.MCPRequest{
		RequestID: "REQCOMMANDSTATE", Method: "mcpServer/tool/call",
		Params: json.RawMessage(`{"server":"cwapi","tool":"process_start","arguments":{"command":"powershell.exe","argv":["-Command","secret-value"],"cwd":"."}}`),
	}
	toolResult := map[string]any{"content": []any{map[string]any{
		"type": "text", "text": `{"process_id":"proc-bbbbbbbbbbbbbbbbbbbbbbbb","state":"running","invocation_kind":"command_argv","command_name":"powershell.exe","command_path":"C:/Windows/System32/WindowsPowerShell/v1.0/powershell.exe","command_resolution":"absolute_path","executable_kind":"native","working_directory":"."}`,
	}}}
	encoded, err := json.Marshal(toolResult)
	if err != nil {
		t.Fatal(err)
	}
	gateway.emitMCPProcessState(request, protocol.MCPResponse{Status: protocol.MCPStatusCompleted, Result: encoded}, time.Now().Add(-time.Second).UnixMilli())
	events, err := store.ExecutionEventsForTask(context.Background(), request.RequestID, 10)
	if err != nil || len(events) != 1 || events[0].Status != "running" || !strings.Contains(events[0].DataJSON, `"invocation_kind":"command_argv"`) ||
		!strings.Contains(events[0].DataJSON, `"command_name":"powershell.exe"`) ||
		!strings.Contains(events[0].DataJSON, `"command_resolution":"absolute_path"`) ||
		!strings.Contains(events[0].DataJSON, `"executable_kind":"native"`) || strings.Contains(events[0].DataJSON, "secret-value") {
		t.Fatalf("events=%#v err=%v", events, err)
	}
}
