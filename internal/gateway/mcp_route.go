package gateway

import (
	"encoding/json"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/processcontract"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
)

const (
	processToolStart  = "process_start"
	processToolStatus = "process_status"
	processToolStop   = "process_stop"
)

func validateMCPRoute(request protocol.MCPRequest) (map[string]any, *protocol.MCPResponse) {
	params := map[string]any{}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return nil, routeError(request, "MCP_PARAMS_INVALID", "MCP params must be an object")
	}
	if _, supplied := params["threadId"]; supplied {
		return nil, routeError(request, "MCP_THREAD_ID_MANAGED", "threadId is owned by CWapi")
	}
	hasRepository := request.RepositoryURL != ""
	switch request.Method {
	case protocol.MCPMethodStatusList:
		if hasRepository || request.SystemToken != "" || len(params) != 0 {
			return nil, routeError(request, "MCP_ROUTE_INVALID", "status-list requires global scope and empty params")
		}
	case protocol.MCPMethodResourceRead:
		if request.SystemToken != "" || !onlyRouteKeys(params, "server", "uri") || len(params) != 2 || !nonemptyString(params["server"]) || !nonemptyString(params["uri"]) {
			return nil, routeError(request, "MCP_ROUTE_INVALID", "resource-read requires only server and uri")
		}
		if strings.TrimSpace(params["server"].(string)) == "cwapi" {
			return nil, routeError(request, "MCP_CWAPI_RESOURCE_UNAVAILABLE", "CWapi exposes no virtual resources")
		}
	case protocol.MCPMethodToolCall:
		if !onlyRouteKeys(params, "server", "tool", "arguments") || len(params) != 3 || !nonemptyString(params["server"]) || !nonemptyString(params["tool"]) {
			return nil, routeError(request, "MCP_ROUTE_INVALID", "tool-call outer params must contain only server, tool and arguments")
		}
		arguments, ok := params["arguments"].(map[string]any)
		if !ok {
			return nil, routeError(request, "MCP_PROCESS_ARGUMENTS_INVALID", "tool arguments must be an object")
		}
		if strings.TrimSpace(params["server"].(string)) != "cwapi" {
			if request.SystemToken != "" {
				return nil, routeError(request, "MCP_SYSTEM_TOKEN_INVALID", "system_token is only accepted by cwapi/process_start fallback")
			}
			break
		}
		tool := strings.TrimSpace(params["tool"].(string))
		switch tool {
		case processToolStart:
			if !hasRepository {
				return nil, routeError(request, "MCP_ROUTE_INVALID", "process_start requires repository_url and expected_commit")
			}
			if _, err := processcontract.DecodeStart(arguments); err != nil {
				return nil, routeError(request, routeCode(err), err.Error())
			}
		case processToolStatus, processToolStop:
			if hasRepository || request.SystemToken != "" {
				return nil, routeError(request, "MCP_ROUTE_INVALID", "process_status and process_stop require global scope without system_token")
			}
			if _, err := processcontract.DecodeProcessID(arguments); err != nil {
				return nil, routeError(request, routeCode(err), err.Error())
			}
		default:
			return nil, routeError(request, "MCP_CWAPI_TOOL_UNAVAILABLE", "CWapi exposes only process_start, process_status and process_stop")
		}
	default:
		return nil, routeError(request, "MCP_METHOD_UNAVAILABLE", "only v2 status-list, resource-read and tool-call methods are exposed")
	}
	return params, nil
}

func routeError(request protocol.MCPRequest, code, message string) *protocol.MCPResponse {
	response := mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed, code, "protocol", message)
	return &response
}

func routeCode(err error) string {
	if err == nil {
		return "MCP_ROUTE_INVALID"
	}
	code := err.Error()
	if boundary := strings.IndexAny(code, ": "); boundary >= 0 {
		code = code[:boundary]
	}
	if strings.HasPrefix(code, "MCP_") {
		return code
	}
	return "MCP_ROUTE_INVALID"
}

func nonemptyString(value any) bool {
	text, ok := value.(string)
	return ok && text == strings.TrimSpace(text) && text != ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func onlyRouteKeys(value map[string]any, allowed ...string) bool {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range value {
		if _, ok := set[key]; !ok {
			return false
		}
	}
	return true
}
