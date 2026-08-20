package state

import (
	"context"
	"fmt"
)

func (s *Store) RecentMCPRequests(ctx context.Context, limit int) ([]MCPRequestRecord, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT request_id, source_identity, source_message_id, method, arguments_hash,
		request_json, execution_state, delivery_state, response_json, created_at, updated_at
		FROM mcp_requests ORDER BY updated_at DESC, created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("STATE_MCP_REQUESTS_RECENT_FAILED: %w", err)
	}
	defer rows.Close()

	records := []MCPRequestRecord{}
	for rows.Next() {
		var record MCPRequestRecord
		if err := rows.Scan(
			&record.RequestID, &record.SourceIdentity, &record.SourceMessageID, &record.Method, &record.ArgumentsHash,
			&record.RequestJSON, &record.ExecutionState, &record.DeliveryState, &record.ResponseJSON,
			&record.CreatedAt, &record.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("STATE_MCP_REQUESTS_RECENT_SCAN_FAILED: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("STATE_MCP_REQUESTS_RECENT_ROWS_FAILED: %w", err)
	}
	return records, nil
}
