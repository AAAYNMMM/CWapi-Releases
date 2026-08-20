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
	if request.ProjectID != "" {
		data["project_id"] = request.ProjectID
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
	if request.Method != "mcpServer/tool/call" {
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
	if _, supported := cwapiProcessTools[tool]; !supported {
		return
	}
	var result map[string]any
	if json.Unmarshal(response.Result, &result) != nil {
		return
	}
	text := firstMCPText(result["content"])
	var record struct {
		ProcessID         string `json:"process_id"`
		State             string `json:"state"`
		InvocationKind    string `json:"invocation_kind"`
		Runtime           string `json:"runtime"`
		Entrypoint        string `json:"entrypoint"`
		CommandName       string `json:"command_name"`
		CommandPath       string `json:"command_path"`
		CommandResolution string `json:"command_resolution"`
		ExecutableKind    string `json:"executable_kind"`
		WorkingDirectory  string `json:"working_directory"`
	}
	if json.Unmarshal([]byte(text), &record) != nil {
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
		"invocation_kind": boundedMCPText(record.InvocationKind, 32),
	}
	if record.Runtime != "" {
		fields["runtime"] = boundedMCPText(record.Runtime, 32)
	}
	if record.Entrypoint != "" {
		fields["entrypoint"] = boundedMCPText(record.Entrypoint, 512)
	}
	if record.CommandName != "" {
		fields["command_name"] = boundedMCPText(record.CommandName, 256)
	}
	if record.CommandPath != "" {
		fields["command_path"] = boundedMCPText(record.CommandPath, 512)
	}
	if record.CommandResolution != "" {
		fields["command_resolution"] = boundedMCPText(record.CommandResolution, 32)
	}
	if record.ExecutableKind != "" {
		fields["executable_kind"] = boundedMCPText(record.ExecutableKind, 32)
	}
	if record.WorkingDirectory != "" {
		fields["working_directory"] = boundedMCPText(record.WorkingDirectory, 512)
	}
	g.emitMCPExecution(request, "process", record.State, "Project process state: "+record.State, startedAt, fields)
}
