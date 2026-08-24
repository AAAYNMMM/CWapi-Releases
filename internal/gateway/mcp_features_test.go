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
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

type featureConfigProvider struct{ value config.Config }

func (p featureConfigProvider) Snapshot() config.Config { return p.value.Clone() }

type featurePoster struct{ subjects, bodies []string }

func (p *featurePoster) Post(_ context.Context, subject, body, _ string) (PostedMessage, error) {
	p.subjects = append(p.subjects, subject)
	p.bodies = append(p.bodies, body)
	return PostedMessage{MessageID: "posted", MessageTS: "2.000"}, nil
}

type featureToolhost struct {
	result any
	params map[string]any
	calls  int
}

func (h *featureToolhost) CallMCP(_ context.Context, _ string, params map[string]any, _ time.Duration, _ MCPExecutionContext) (any, error) {
	h.calls++
	h.params = params
	return h.result, nil
}

type featureProcessRuntime struct {
	starts, statuses, stops int
	lastExecution           MCPExecutionContext
	lastToken               string
}

func (r *featureProcessRuntime) Start(_ context.Context, request protocol.MCPRequest, _ processcontract.StartArguments, execution MCPExecutionContext, release func()) (protocol.MCPResponse, bool) {
	r.starts++
	r.lastExecution, r.lastToken = execution, request.SystemToken
	if release != nil {
		release()
	}
	return protocol.MCPResponse{
		Schema: protocol.MCPResponseSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: request.RequestID, Status: protocol.MCPStatusCompleted,
		Result: json.RawMessage(`{"process_id":"proc-0123456789abcdef01234567","state":"running"}`),
	}, true
}

func (r *featureProcessRuntime) Status(_ context.Context, requestID, _ string) protocol.MCPResponse {
	r.statuses++
	return protocol.MCPResponse{Schema: protocol.MCPResponseSchema, ProtocolVersion: protocol.MCPProtocolVersion, RequestID: requestID, Status: protocol.MCPStatusCompleted, Result: json.RawMessage(`{"state":"running"}`)}
}

func (r *featureProcessRuntime) Stop(_ context.Context, requestID, _ string) protocol.MCPResponse {
	r.stops++
	return protocol.MCPResponse{Schema: protocol.MCPResponseSchema, ProtocolVersion: protocol.MCPProtocolVersion, RequestID: requestID, Status: protocol.MCPStatusCompleted, Result: json.RawMessage(`{"state":"stopped"}`)}
}

func (r *featureProcessRuntime) RevokeSystemToken(string) {}

type featureContextResolver struct {
	execution MCPExecutionContext
	err       error
	released  *bool
}

func (r featureContextResolver) PrepareMCPContext(context.Context, string, string, string) (MCPExecutionContext, func(), error) {
	var release func()
	if r.released != nil {
		release = func() { *r.released = true }
	}
	return r.execution, release, r.err
}

func newFeatureGateway(t *testing.T, host MCPToolhost) (*Gateway, *state.Store, *featurePoster) {
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
	gateway, err := NewMCP(featureConfigProvider{value: config.Default()}, store, poster, hub)
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
	request.Schema = protocol.MCPRequestSchema
	request.ProtocolVersion = protocol.MCPProtocolVersion
	if len(request.Params) == 0 {
		request.Params = json.RawMessage(`{}`)
	}
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

func TestStatusListDoesNotFabricateCWapiServer(t *testing.T) {
	host := &featureToolhost{result: map[string]any{"data": []any{}}}
	gateway, _, poster := newFeatureGateway(t, host)
	request := protocol.MCPRequest{RequestID: "REQSTATUS", Method: protocol.MCPMethodStatusList}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Status != protocol.MCPStatusCompleted || strings.Contains(string(response.Result), "cwapi") {
		t.Fatalf("status response=%s", response.Result)
	}
}

func TestInvalidRoutesAreRejectedBeforeClaim(t *testing.T) {
	tests := []protocol.MCPRequest{
		{RequestID: "REQSTATUSREPO", RepositoryURL: "https://github.com/o/r", ExpectedCommit: strings.Repeat("a", 40), Method: protocol.MCPMethodStatusList},
		{RequestID: "REQSTARTGLOBAL", Method: protocol.MCPMethodToolCall, Params: json.RawMessage(`{"server":"cwapi","tool":"process_start","arguments":{"command":"node"}}`)},
		{RequestID: "REQSTATUSREPO2", RepositoryURL: "https://github.com/o/r", ExpectedCommit: strings.Repeat("a", 40), Method: protocol.MCPMethodToolCall, Params: json.RawMessage(`{"server":"cwapi","tool":"process_status","arguments":{"process_id":"proc-0123456789abcdef01234567"}}`)},
		{RequestID: "REQOUTER", Method: protocol.MCPMethodToolCall, Params: json.RawMessage(`{"server":"playwright","tool":"x","arguments":{},"extra":true}`)},
		{RequestID: "REQRESOURCE", Method: protocol.MCPMethodResourceRead, Params: json.RawMessage(`{"server":"cwapi","uri":"cwapi://x"}`)},
	}
	for _, request := range tests {
		t.Run(request.RequestID, func(t *testing.T) {
			gateway, store, poster := newFeatureGateway(t, nil)
			if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
				t.Fatal(err)
			}
			response := decodeFeatureResponse(t, poster)
			if response.Status == protocol.MCPStatusCompleted || response.Error == nil {
				t.Fatalf("invalid route completed: %#v", response)
			}
			if _, err := store.MCPRequestByID(context.Background(), request.RequestID); err == nil {
				t.Fatal("invalid route consumed request_id")
			}
		})
	}
}

func TestRepositoryContextIsPreparedAndReleasedPerRequest(t *testing.T) {
	released := false
	host := &featureToolhost{result: map[string]any{"content": []any{}}}
	gateway, _, poster := newFeatureGateway(t, nil)
	execution := MCPExecutionContext{
		RepositoryURL: "https://github.com/Owner/Repo", Repository: "owner/repo",
		ExpectedCommit: strings.Repeat("a", 40), CWD: filepath.Join(t.TempDir(), "tree"),
	}
	if err := gateway.AttachMCPRuntime(MCPRuntime{Toolhost: host, ContextResolver: featureContextResolver{execution: execution, released: &released}}); err != nil {
		t.Fatal(err)
	}
	request := protocol.MCPRequest{
		RequestID: "REQREPOSITORY", RepositoryURL: execution.RepositoryURL, ExpectedCommit: execution.ExpectedCommit,
		Method: protocol.MCPMethodToolCall, Params: json.RawMessage(`{"server":"playwright","tool":"x","arguments":{}}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	if response := decodeFeatureResponse(t, poster); response.Status != protocol.MCPStatusCompleted || !released {
		t.Fatalf("response=%#v released=%v", response, released)
	}
}

func TestVirtualProcessToolsStayInCoreAndTransferWorkspaceOwnership(t *testing.T) {
	released := false
	host := &featureToolhost{}
	processes := &featureProcessRuntime{}
	gateway, _, poster := newFeatureGateway(t, nil)
	execution := MCPExecutionContext{
		RepositoryURL: "https://github.com/o/r", Repository: "o/r",
		ExpectedCommit: strings.Repeat("a", 40), CWD: filepath.Join(t.TempDir(), "tree"),
	}
	if err := gateway.AttachMCPRuntime(MCPRuntime{
		Toolhost: host, Process: processes,
		ContextResolver: featureContextResolver{execution: execution, released: &released},
	}); err != nil {
		t.Fatal(err)
	}
	request := protocol.MCPRequest{
		RequestID: "REQPROCESSCORE", RepositoryURL: execution.RepositoryURL, ExpectedCommit: execution.ExpectedCommit,
		Method: protocol.MCPMethodToolCall,
		Params: json.RawMessage(`{"server":"cwapi","tool":"process_start","arguments":{"command":"node"}}`),
	}
	message := featureMessage(t, request)
	if err := gateway.HandleMCPMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if response := decodeFeatureResponse(t, poster); response.Status != protocol.MCPStatusCompleted {
		t.Fatalf("response=%#v", response)
	}
	if processes.starts != 1 || host.calls != 0 || !released || processes.lastExecution.CWD != execution.CWD {
		t.Fatalf("routing mismatch: starts=%d host=%d released=%v execution=%#v", processes.starts, host.calls, released, processes.lastExecution)
	}
	if err := gateway.HandleMCPMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	if processes.starts != 1 {
		t.Fatalf("duplicate start executed %d times", processes.starts)
	}
}

func TestSystemTokenFallbackDoesNotPrepareAnotherWorkspace(t *testing.T) {
	host := &featureToolhost{}
	processes := &featureProcessRuntime{}
	prepareCalls := 0
	gateway, _, poster := newFeatureGateway(t, nil)
	resolver := featureCountingResolver{calls: &prepareCalls}
	if err := gateway.AttachMCPRuntime(MCPRuntime{Toolhost: host, Process: processes, ContextResolver: resolver}); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 64)
	request := protocol.MCPRequest{
		RequestID: "REQSYSTEMTOKEN", RepositoryURL: "https://github.com/o/r", ExpectedCommit: strings.Repeat("b", 40),
		SystemToken: token, Method: protocol.MCPMethodToolCall,
		Params: json.RawMessage(`{"server":"cwapi","tool":"process_start","arguments":{"command":"node"}}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	if response := decodeFeatureResponse(t, poster); response.Status != protocol.MCPStatusCompleted {
		t.Fatalf("response=%#v", response)
	}
	if prepareCalls != 0 || processes.starts != 1 || processes.lastExecution.CWD != "" || processes.lastToken != token || host.calls != 0 {
		t.Fatalf("fallback routing mismatch: prepare=%d process=%#v host=%d", prepareCalls, processes, host.calls)
	}
}

type featureCountingResolver struct{ calls *int }

func (r featureCountingResolver) PrepareMCPContext(context.Context, string, string, string) (MCPExecutionContext, func(), error) {
	(*r.calls)++
	return MCPExecutionContext{}, nil, errors.New("must not prepare")
}

func TestMismatchedPreparedRepositoryContextFails(t *testing.T) {
	gateway, _, poster := newFeatureGateway(t, nil)
	if err := gateway.AttachMCPRuntime(MCPRuntime{
		Toolhost: &featureToolhost{}, ContextResolver: featureContextResolver{execution: MCPExecutionContext{
			RepositoryURL: "https://github.com/other/repo", ExpectedCommit: strings.Repeat("b", 40), CWD: filepath.Join(t.TempDir(), "tree"),
		}},
	}); err != nil {
		t.Fatal(err)
	}
	request := protocol.MCPRequest{
		RequestID: "REQMISMATCH", RepositoryURL: "https://github.com/o/r", ExpectedCommit: strings.Repeat("a", 40),
		Method: protocol.MCPMethodToolCall, Params: json.RawMessage(`{"server":"playwright","tool":"x","arguments":{}}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	if response := decodeFeatureResponse(t, poster); response.Error == nil || response.Error.Code != "MCP_REPOSITORY_CONTEXT_MISMATCH" {
		t.Fatalf("response=%#v", response)
	}
}

func TestMCPRequestPersistsRealLifecycle(t *testing.T) {
	host := &featureToolhost{result: map[string]any{"data": []any{}}}
	gateway, store, poster := newFeatureGateway(t, host)
	request := protocol.MCPRequest{RequestID: "REQLIFECYCLE", Method: protocol.MCPMethodStatusList}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	if response := decodeFeatureResponse(t, poster); response.Status != protocol.MCPStatusCompleted {
		t.Fatalf("response=%#v", response)
	}
	record, err := store.MCPRequestByID(context.Background(), request.RequestID)
	if err != nil || record.ExecutionState != "completed" || record.DeliveryState != state.MCPDeliveryDelivered {
		t.Fatalf("record=%#v err=%v", record, err)
	}
}

func TestSlackEscapedToolArgumentReachesToolhost(t *testing.T) {
	host := &featureToolhost{result: map[string]any{"content": []any{}}}
	gateway, _, _ := newFeatureGateway(t, host)
	text := "+++\n[CWapi/MCP/2][MCP_REQUEST][REQEVALUATE]\n" +
		`{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"REQEVALUATE","method":"mcpServer/tool/call","params":{"server":"playwright","tool":"browser_evaluate","arguments":{"function":"() =&gt; 1"}}}` + "\n+++"
	subject, body, ok := slackcore.DecodeProtocol(text)
	if !ok {
		t.Fatal("Slack frame did not decode")
	}
	if err := gateway.HandleMCPMessage(context.Background(), slackcore.Message{MessageID: "m", ChannelID: "C12345678", MessageTS: "2", Subject: subject, Body: body}); err != nil {
		t.Fatal(err)
	}
	arguments, _ := host.params["arguments"].(map[string]any)
	if arguments["function"] != "() => 1" {
		t.Fatalf("arguments=%#v", arguments)
	}
}

func TestInvalidFramedRequestReturnsV2Guidance(t *testing.T) {
	gateway, _, poster := newFeatureGateway(t, nil)
	subject, _ := protocol.BuildMCPSubject(protocol.MCPFamilyRequest, "REQINVALID")
	message := slackcore.Message{MessageID: "m", ChannelID: "C12345678", MessageTS: "1", Subject: subject, Body: `{`}
	if err := gateway.HandleMCPMessage(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Error == nil || !strings.Contains(response.Error.Message, "v2") {
		t.Fatalf("response=%#v", response)
	}
}

func TestRepositoryPreparationErrorIsBounded(t *testing.T) {
	gateway, _, poster := newFeatureGateway(t, nil)
	if err := gateway.AttachMCPRuntime(MCPRuntime{Toolhost: &featureToolhost{}, ContextResolver: featureContextResolver{err: errors.New(strings.Repeat("x", 6000))}}); err != nil {
		t.Fatal(err)
	}
	request := protocol.MCPRequest{
		RequestID: "REQPREPFAIL", RepositoryURL: "https://github.com/o/r", ExpectedCommit: strings.Repeat("a", 40),
		Method: protocol.MCPMethodToolCall, Params: json.RawMessage(`{"server":"playwright","tool":"x","arguments":{}}`),
	}
	if err := gateway.HandleMCPMessage(context.Background(), featureMessage(t, request)); err != nil {
		t.Fatal(err)
	}
	response := decodeFeatureResponse(t, poster)
	if response.Error == nil || len(response.Error.Message) > protocol.MaxMCPErrorMessageBytes || !utf8.ValidString(response.Error.Message) {
		t.Fatalf("response=%#v", response)
	}
}

func TestMCPToolErrorBecomesBoundedValidFailure(t *testing.T) {
	text, failed := mcpToolFailure(map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": strings.Repeat("错", 2000)}}})
	if !failed || len(text) > 3000 || !utf8.ValidString(text) {
		t.Fatalf("failed=%v bytes=%d", failed, len(text))
	}
}
