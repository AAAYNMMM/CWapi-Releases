package gateway

import (
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/protocol"
)

var cwapiProcessTools = map[string]struct{}{
	"process_start":  {},
	"process_status": {},
	"process_stop":   {},
}

func (g *Gateway) prepareCWapiProcessCall(request protocol.MCPRequest, execution MCPExecutionContext, params map[string]any) *protocol.MCPResponse {
	if request.Method != "mcpServer/tool/call" || strings.TrimSpace(stringValue(params["server"])) != "cwapi" {
		return nil
	}
	tool := strings.TrimSpace(stringValue(params["tool"]))
	if _, ok := cwapiProcessTools[tool]; !ok {
		response := mcpErrorResponse(request.RequestID, protocol.MCPStatusUnavailable,
			"MCP_CWAPI_TOOL_UNAVAILABLE", "tool", "CWapi process server exposes only process_start, process_status and process_stop")
		return &response
	}
	if request.ProjectID == "" || execution.CWD == "" || execution.ExpectedCommit == "" {
		response := mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_PROCESS_CONTEXT_REQUIRED", "workspace", "CWapi process tools require project_id and expected_commit. "+g.projectDiscoverySummary())
		return &response
	}
	arguments, ok := params["arguments"].(map[string]any)
	if !ok {
		response := mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_PROCESS_ARGUMENTS_INVALID", "tool", "CWapi process tool arguments must be an object")
		return &response
	}
	for _, key := range []string{"_cwapi_workspace", "_cwapi_expected_commit", "_cwapi_request_id"} {
		if _, supplied := arguments[key]; supplied {
			response := mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
				"MCP_PROCESS_CONTEXT_MANAGED", "relay", "CWapi process context fields are managed by CWapi")
			return &response
		}
	}
	arguments["_cwapi_workspace"] = execution.CWD
	arguments["_cwapi_expected_commit"] = execution.ExpectedCommit
	arguments["_cwapi_request_id"] = request.RequestID
	return nil
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
