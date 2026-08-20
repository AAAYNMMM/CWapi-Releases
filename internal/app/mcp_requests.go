package app

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/protocol"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

type MCPRequestSnapshot struct {
	RequestID       string `json:"request_id"`
	SourceMessageID string `json:"source_message_id"`
	Method          string `json:"method"`
	ToolName        string `json:"tool_name"`
	ExecutionState  string `json:"execution_state"`
	DeliveryState   string `json:"delivery_state"`
	Terminal        bool   `json:"terminal"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
	ElapsedMS       int64  `json:"elapsed_ms"`
}

func (s *Service) RecentMCPRequests(ctx context.Context, limit int) ([]MCPRequestSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	records, err := s.state.RecentMCPRequests(ctx, limit)
	if err != nil {
		s.recordOperationalError("mcp-requests", "mcp.requests.recent", err)
		return nil, err
	}
	return mcpRequestSnapshots(records, time.Now().UnixMilli()), nil
}

func mcpRequestSnapshots(records []state.MCPRequestRecord, now int64) []MCPRequestSnapshot {
	snapshots := make([]MCPRequestSnapshot, len(records))
	for index, record := range records {
		terminal := record.ResponseJSON != ""
		end := now
		if terminal && record.UpdatedAt > 0 {
			end = record.UpdatedAt
		}
		elapsed := end - record.CreatedAt
		if elapsed < 0 {
			elapsed = 0
		}
		snapshots[index] = MCPRequestSnapshot{
			RequestID:       record.RequestID,
			SourceMessageID: record.SourceMessageID,
			Method:          record.Method,
			ToolName:        storedMCPToolName(record),
			ExecutionState:  record.ExecutionState,
			DeliveryState:   record.DeliveryState,
			Terminal:        terminal,
			CreatedAt:       record.CreatedAt,
			UpdatedAt:       record.UpdatedAt,
			ElapsedMS:       elapsed,
		}
	}
	return snapshots
}

func storedMCPToolName(record state.MCPRequestRecord) string {
	if record.Method != "mcpServer/tool/call" || strings.TrimSpace(record.RequestJSON) == "" {
		return ""
	}
	request, err := protocol.DecodeMCPRequest([]byte(record.RequestJSON))
	if err != nil {
		return ""
	}
	var params struct {
		Tool string `json:"tool"`
	}
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return ""
	}
	return strings.TrimSpace(params.Tool)
}
