package state

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	MCPExecutionReceived  = "received"
	MCPExecutionPreparing = "preparing_workspace"
	MCPExecutionRunning   = "running"
	MCPDeliveryPending    = "pending"
	MCPDeliveryDelivered  = "delivered"
	MCPDeliveryAttention  = "attention"
)

// MCPRequestRecord stores one stable MCP request independently from Slack
// delivery attempts. RequestID is the idempotency key. ArgumentsHash detects a
// conflicting reuse of the same request ID with different semantics.
type MCPRequestRecord struct {
	RequestID       string `json:"request_id"`
	SourceIdentity  string `json:"source_identity"`
	SourceMessageID string `json:"source_message_id"`
	Method          string `json:"method"`
	ArgumentsHash   string `json:"arguments_hash"`
	RequestJSON     string `json:"request_json"`
	ExecutionState  string `json:"execution_state"`
	DeliveryState   string `json:"delivery_state"`
	ResponseJSON    string `json:"response_json"`
	CreatedAt       int64  `json:"created_at"`
	UpdatedAt       int64  `json:"updated_at"`
}

func (s *Store) InsertMCPRequestIfAbsent(ctx context.Context, record MCPRequestRecord) (MCPRequestRecord, bool, error) {
	result, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO mcp_requests(
		request_id, source_identity, source_message_id, method, arguments_hash, request_json,
		execution_state, delivery_state, response_json, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.RequestID, record.SourceIdentity, record.SourceMessageID, record.Method, record.ArgumentsHash,
		record.RequestJSON, record.ExecutionState, record.DeliveryState, record.ResponseJSON,
		record.CreatedAt, record.UpdatedAt)
	if err != nil {
		return MCPRequestRecord{}, false, fmt.Errorf("STATE_MCP_REQUEST_INSERT_FAILED: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return MCPRequestRecord{}, false, fmt.Errorf("STATE_MCP_REQUEST_INSERT_RESULT_FAILED: %w", err)
	}
	stored, err := s.MCPRequestByID(ctx, record.RequestID)
	if err != nil {
		return MCPRequestRecord{}, false, err
	}
	return stored, rows == 1, nil
}

func (s *Store) MCPRequestByID(ctx context.Context, requestID string) (MCPRequestRecord, error) {
	var record MCPRequestRecord
	err := s.db.QueryRowContext(ctx, `SELECT request_id, source_identity, source_message_id, method, arguments_hash,
		request_json, execution_state, delivery_state, response_json, created_at, updated_at
		FROM mcp_requests WHERE request_id=?`, requestID).Scan(
		&record.RequestID, &record.SourceIdentity, &record.SourceMessageID, &record.Method, &record.ArgumentsHash,
		&record.RequestJSON, &record.ExecutionState, &record.DeliveryState, &record.ResponseJSON,
		&record.CreatedAt, &record.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return MCPRequestRecord{}, fmt.Errorf("STATE_MCP_REQUEST_NOT_FOUND")
		}
		return MCPRequestRecord{}, fmt.Errorf("STATE_MCP_REQUEST_READ_FAILED: %w", err)
	}
	return record, nil
}

// CompleteMCPRequest writes the execution terminal fact and serialized response
// before any Slack protocol-message delivery is attempted.
func (s *Store) CompleteMCPRequest(ctx context.Context, requestID, executionState, responseJSON string, updatedAt int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE mcp_requests
		SET execution_state=?, response_json=?, updated_at=? WHERE request_id=?`,
		executionState, responseJSON, updatedAt, requestID)
	if err != nil {
		return fmt.Errorf("STATE_MCP_REQUEST_COMPLETE_FAILED: %w", err)
	}
	return requireOneMCPRow(result, "STATE_MCP_REQUEST_NOT_FOUND")
}

func (s *Store) UpdateMCPExecution(ctx context.Context, requestID, executionState string, updatedAt int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE mcp_requests SET execution_state=?, updated_at=? WHERE request_id=? AND response_json=''`,
		executionState, updatedAt, requestID)
	if err != nil {
		return fmt.Errorf("STATE_MCP_EXECUTION_UPDATE_FAILED: %w", err)
	}
	return requireOneMCPRow(result, "STATE_MCP_REQUEST_NOT_ACTIVE")
}

func (s *Store) UpdateMCPDelivery(ctx context.Context, requestID, deliveryState string, updatedAt int64) error {
	result, err := s.db.ExecContext(ctx, `UPDATE mcp_requests SET delivery_state=?, updated_at=? WHERE request_id=?`,
		deliveryState, updatedAt, requestID)
	if err != nil {
		return fmt.Errorf("STATE_MCP_DELIVERY_UPDATE_FAILED: %w", err)
	}
	return requireOneMCPRow(result, "STATE_MCP_REQUEST_NOT_FOUND")
}

func requireOneMCPRow(result sql.Result, notFound string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("STATE_MCP_REQUEST_RESULT_FAILED: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("%s", notFound)
	}
	return nil
}
