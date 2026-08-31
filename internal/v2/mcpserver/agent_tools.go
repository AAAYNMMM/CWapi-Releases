package mcpserver

import (
	"context"
	"errors"
	"net/url"

	"github.com/AAAYNMMM/CWapi/internal/v2/attachments"
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
	RequestID    string                 `json:"request_id"`
	Delivery     int                    `json:"delivery"`
	DeadlineAt   string                 `json:"deadline_at"`
	Request      map[string]any         `json:"request"`
	Attachments  []attachments.Metadata `json:"attachments,omitempty"`
	ContentItems []attachments.Item     `json:"-"`
}

type AgentExchangeOutput struct {
	State    string                 `json:"state"`
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
		Description: "Atomically submit results for a previous batch and wait for the next local software request batch on CWapi's single active bridge. " +
			"Process every returned request independently; delivery greater than one is a retry of the same request_id. Inline raster images may be returned as native MCP ImageContent; generic files are not supported.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input AgentExchangeInput) (*mcp.CallToolResult, AgentExchangeOutput, error) {
		if input.Capacity < 0 {
			return nil, AgentExchangeOutput{}, errors.New("AGENT_EXCHANGE_INPUT_INVALID")
		}
		output, err := service.Exchange(ctx, input)
		if err != nil {
			return nil, output, err
		}
		for _, request := range output.Requests {
			for _, item := range request.ContentItems {
				if item.Metadata.Kind != "image" {
					return nil, AgentExchangeOutput{}, errors.New("AGENT_IMAGE_ATTACHMENT_REQUIRED")
				}
			}
		}
		result := &mcp.CallToolResult{}
		for _, request := range output.Requests {
			for _, item := range request.ContentItems {
				uri := "cwapi://agent/" + url.PathEscape(request.RequestID) + "/" + url.PathEscape(item.Metadata.Name)
				result.Content = append(result.Content, attachments.MCPContent(item, "request_id="+request.RequestID, uri)...)
			}
		}
		return result, output, nil
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
