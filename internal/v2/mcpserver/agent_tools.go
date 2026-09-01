package mcpserver

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolAgentOpen     = "agent_open"
	ToolAgentExchange = "agent_exchange"
	ToolAgentClose    = "agent_close"
)

type AgentOpenInput struct{}

type AgentOpenOutput struct {
	State       string `json:"state"`
	Resumed     bool   `json:"resumed"`
	MaxInflight int    `json:"max_inflight"`
	Revision    uint64 `json:"state_revision"`
}

type AgentExchangeResponse struct {
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response"`
}

type AgentExchangeInput struct {
	Responses []AgentExchangeResponse `json:"responses,omitempty"`
	Capacity  int                     `json:"capacity,omitempty"`
}

type AgentExchangeResult struct {
	RequestID string `json:"request_id,omitempty"`
	State     string `json:"state"`
	Error     string `json:"error,omitempty"`
}

type AgentExchangeRequest struct {
	RequestID       string         `json:"request_id"`
	TaskID          string         `json:"task_id,omitempty"`
	CorrelationID   string         `json:"correlation_id,omitempty"`
	State           string         `json:"state"`
	Delivery        int            `json:"delivery"`
	CreatedAt       string         `json:"created_at"`
	ClaimedAt       string         `json:"claimed_at"`
	LastDeliveredAt string         `json:"last_delivered_at"`
	DeadlineAt      string         `json:"deadline_at"`
	Request         map[string]any `json:"request"`
}

type AgentExchangeActivity struct {
	Revision     uint64 `json:"revision"`
	Changed      bool   `json:"changed"`
	Pending      int    `json:"pending"`
	Inflight     int    `json:"inflight"`
	Active       int    `json:"active"`
	IdleCount    int    `json:"idle_count"`
	WaitedMillis int64  `json:"waited_millis"`
	LastState    string `json:"last_state,omitempty"`
	LastError    string `json:"last_error,omitempty"`
	NextAction   string `json:"next_action"`
}

type AgentExchangeOutput struct {
	State    string                 `json:"state"`
	Activity AgentExchangeActivity  `json:"activity"`
	Results  []AgentExchangeResult  `json:"results,omitempty"`
	Requests []AgentExchangeRequest `json:"requests,omitempty"`
}

type AgentCloseInput struct{}

type AgentCloseOutput struct {
	State string `json:"state"`
}

type AgentService interface {
	Open(context.Context, AgentOpenInput) (AgentOpenOutput, error)
	Exchange(context.Context, AgentExchangeInput) (AgentExchangeOutput, error)
	Close(context.Context, AgentCloseInput) (AgentCloseOutput, error)
}

func RegisterAgent(server *mcp.Server, service AgentService) error {
	if server == nil || service == nil {
		return errors.New("AGENT_SERVICE_REQUIRED")
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolAgentOpen,
		Description: "Open or resume the single active Web GPT consumer bridge. The internal bridge generation remains CWapi-owned and is not exposed to Web GPT.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentOpenInput) (*mcp.CallToolResult, AgentOpenOutput, error) {
		output, err := service.Open(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: ToolAgentExchange,
		Description: "Atomically submit results for a previous batch on CWapi's single active bridge; non-tool terminal responses return immediately as state=responses, while tool_calls keep a bounded wait open for the local tool-result request batch. " +
			"Process every returned request independently; delivery greater than one is a retry of the same request_id, never a local process session. " +
			"A no_request result means only that the bounded wait ended with no OpenAI request; inspect activity and do not use it as evidence that local work is running. " +
			"Function arguments and JSON content may be supplied as native JSON values to avoid nested escaping. File and image transfer are not supported.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentExchangeInput) (*mcp.CallToolResult, AgentExchangeOutput, error) {
		if input.Capacity < 0 {
			return nil, AgentExchangeOutput{}, errors.New("AGENT_EXCHANGE_INPUT_INVALID")
		}
		output, err := service.Exchange(ctx, input)
		if err != nil {
			return nil, output, err
		}
		return nil, output, nil
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolAgentClose,
		Description: "Close CWapi's current single active Agent bridge and release its broker ownership. No bridge ID is required.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentCloseInput) (*mcp.CallToolResult, AgentCloseOutput, error) {
		output, err := service.Close(ctx, input)
		return nil, output, err
	})
	return nil
}
