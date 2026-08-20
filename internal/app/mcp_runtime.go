package app

import (
	"context"
	"errors"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
)

type codexMCPCaller interface {
	CallMCP(context.Context, codex.MCPCall) (any, error)
}

type gatewayMCPToolhost struct {
	caller codexMCPCaller
}

func (h gatewayMCPToolhost) CallMCP(ctx context.Context, method string, params map[string]any, timeout time.Duration, execution gateway.MCPExecutionContext) (any, error) {
	if h.caller == nil {
		return nil, errors.New("APP_MCP_TOOLHOST_UNAVAILABLE")
	}
	return h.caller.CallMCP(ctx, codex.MCPCall{
		Method:  method,
		Params:  params,
		Timeout: timeout,
		CWD:     execution.CWD,
	})
}

func attachGatewayMCPRuntime(requestGateway *gateway.Gateway, caller codexMCPCaller, resolver gateway.MCPContextResolver) error {
	if requestGateway == nil || caller == nil {
		return errors.New("APP_MCP_RUNTIME_DEPENDENCY_INVALID")
	}
	return requestGateway.AttachMCPRuntime(gateway.MCPRuntime{
		Toolhost:        gatewayMCPToolhost{caller: caller},
		ContextResolver: resolver,
	})
}
