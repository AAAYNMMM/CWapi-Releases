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
	Event     string         `json:"event,omitempty"`
	Response  map[string]any `json:"response"`
}

type AgentProgress struct {
	RequestID string `json:"request_id"`
	Message   string `json:"message"`
}

type AgentExchangeInput struct {
	Responses []AgentExchangeResponse `json:"responses,omitempty"`
	Progress  []AgentProgress         `json:"progress,omitempty"`
	Capacity  int                     `json:"capacity,omitempty"`
}

type AgentStructuredError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	RequestID  string `json:"request_id,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Retryable  bool   `json:"retryable"`
}

type AgentExchangeResult struct {
	RequestID string                `json:"request_id,omitempty"`
	State     string                `json:"state"`
	Error     string                `json:"error,omitempty"`
	Detail    *AgentStructuredError `json:"error_detail,omitempty"`
}

type AgentExchangeRequest struct {
	RequestID       string         `json:"request_id"`
	TaskID          string         `json:"task_id,omitempty"`
	CorrelationID   string         `json:"correlation_id,omitempty"`
	State           string         `json:"state"`
	LifecycleState  string         `json:"lifecycle_state"`
	Delivery        int            `json:"delivery"`
	PreviousState   string         `json:"previous_state,omitempty"`
	ResumeReason    string         `json:"resume_reason,omitempty"`
	CreatedAt       string         `json:"created_at"`
	ClaimedAt       string         `json:"claimed_at"`
	LastDeliveredAt string         `json:"last_delivered_at"`
	LastActivity    string         `json:"last_activity"`
	DeadlineAt      string         `json:"deadline_at"`
	Event           string         `json:"event,omitempty"`
	Request         map[string]any `json:"request"`
}

type AgentEvent struct {
	Type       string `json:"type"`
	RequestID  string `json:"request_id,omitempty"`
	ToolCallID string `json:"tool_call_id,omitempty"`
	ToolName   string `json:"tool_name,omitempty"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message,omitempty"`
	Retryable  bool   `json:"retryable,omitempty"`
	At         string `json:"at"`
}

type AgentExchangeActivity struct {
	Revision        uint64 `json:"revision"`
	Changed         bool   `json:"changed"`
	Pending         int    `json:"pending"`
	Inflight        int    `json:"inflight"`
	Active          int    `json:"active"`
	QueuedRequests  int    `json:"queued_requests"`
	ActiveRequests  int    `json:"active_requests"`
	IdleCount       int    `json:"idle_count"`
	WaitedMillis    int64  `json:"waited_millis"`
	LastState       string `json:"last_state,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
	LastProgress    string `json:"last_progress,omitempty"`
	NextAction      string `json:"next_action"`
}

type AgentExchangeOutput struct {
	State    string                 `json:"state"`
	Activity AgentExchangeActivity  `json:"activity"`
	Results  []AgentExchangeResult  `json:"results,omitempty"`
	Requests []AgentExchangeRequest `json:"requests,omitempty"`
	Events   []AgentEvent           `json:"events,omitempty"`
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
		Description: "Open or resume the logical Web GPT bridge. Active request state survives a temporary bridge detach and is redelivered with the same request_id.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentOpenInput) (*mcp.CallToolResult, AgentOpenOutput, error) {
		output, err := service.Open(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name: ToolAgentExchange,
		Description: "Submit structured completion/tool-call responses and optional progress, then receive queued or resumed requests. " +
			"delivery greater than one is the same request_id redelivered. Retryable parse/tool errors return error_detail and leave the request recoverable. " +
			"Heartbeat is runtime-owned; no_request only means the bounded exchange wait produced no request. File and image transfer are not supported.",
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
		Description: "Detach CWapi's current Agent bridge. Active requests are preserved for resume until they complete, fail finally, disconnect, or expire.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentCloseInput) (*mcp.CallToolResult, AgentCloseOutput, error) {
		output, err := service.Close(ctx, input)
		return nil, output, err
	})
	return nil
}
