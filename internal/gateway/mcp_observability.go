package gateway

import (
	"context"
	"encoding/json"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
)

func (g *Gateway) emitMCPExecution(request protocol.MCPRequest, stepID, status, message string, startedAt int64, extra map[string]any) {
	if g == nil || g.logs == nil {
		return
	}
	data := map[string]any{
		"request_id": request.RequestID,
		"method":     request.Method,
	}
	if request.RepositoryURL != "" {
		data["repository_url"] = request.RepositoryURL
		data["expected_commit"] = request.ExpectedCommit
	}
	for key, value := range mcpToolIdentity(request) {
		data[key] = value
	}
	for key, value := range extra {
		data[key] = value
	}
	duration := int64(0)
	if startedAt > 0 && status != "received" {
		duration = time.Now().UnixMilli() - startedAt
		if duration < 0 {
			duration = 0
		}
	}
	_, _ = g.logs.EmitExecution(context.Background(), observability.ExecutionInput{
		TaskID: request.RequestID, StepID: stepID, Kind: "mcp.request",
		Status: status, Message: message, DurationMS: duration, Data: data,
	})
}

func (g *Gateway) emitMCPDelivery(requestID, status, message string) {
	if g == nil || g.logs == nil {
		return
	}
	_, _ = g.logs.EmitExecution(context.Background(), observability.ExecutionInput{
		TaskID: requestID, StepID: "delivery", Kind: "mcp.delivery",
		Status: status, Message: message, Data: map[string]any{"request_id": requestID, "delivery_state": status},
	})
}

func mcpToolIdentity(request protocol.MCPRequest) map[string]any {
	if request.Method != protocol.MCPMethodToolCall {
		return nil
	}
	var params struct {
		Server string `json:"server"`
		Tool   string `json:"tool"`
	}
	if json.Unmarshal(request.Params, &params) != nil {
		return nil
	}
	result := map[string]any{}
	if params.Server != "" {
		result["server"] = params.Server
	}
	if params.Tool != "" {
		result["tool"] = params.Tool
	}
	return result
}

func (g *Gateway) emitMCPProcessState(request protocol.MCPRequest, response protocol.MCPResponse, startedAt int64) {
	identity := mcpToolIdentity(request)
	if identity["server"] != "cwapi" || response.Status != protocol.MCPStatusCompleted {
		return
	}
	tool, _ := identity["tool"].(string)
	if tool != processToolStart && tool != processToolStatus && tool != processToolStop {
		return
	}
	var record struct {
		ProcessID        string `json:"process_id"`
		State            string `json:"state"`
		Backend          string `json:"backend"`
		Repository       string `json:"repository"`
		WorkingDirectory string `json:"working_directory"`
	}
	if json.Unmarshal(response.Result, &record) != nil {
		return
	}
	allowedStates := map[string]struct{}{"starting": {}, "running": {}, "completed": {}, "failed": {}, "stopped": {}}
	if _, allowed := allowedStates[record.State]; !allowed {
		return
	}
	fields := map[string]any{
		"execution_state": "completed",
		"process_state":   record.State,
		"process_id":      boundedMCPText(record.ProcessID, 80),
	}
	if record.Backend != "" {
		fields["backend"] = boundedMCPText(record.Backend, 32)
	}
	if record.Repository != "" {
		fields["repository"] = boundedMCPText(record.Repository, 200)
	}
	if record.WorkingDirectory != "" {
		fields["working_directory"] = boundedMCPText(record.WorkingDirectory, 512)
	}
	g.emitMCPExecution(request, "process", record.State, "Process state: "+record.State, startedAt, fields)
}
