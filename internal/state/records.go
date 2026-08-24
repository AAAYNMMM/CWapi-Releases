package state

import (
	"context"
	"fmt"
)

func (s *Store) AppendExecutionEvent(ctx context.Context, record ExecutionEventRecord) (ExecutionEventRecord, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO execution_events(timestamp, task_id, step_id, kind, status, message, duration_ms, data_json)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		record.Timestamp, record.TaskID, record.StepID, record.Kind, record.Status, record.Message, record.DurationMS, record.DataJSON)
	if err != nil {
		return ExecutionEventRecord{}, fmt.Errorf("STATE_EXECUTION_EVENT_APPEND_FAILED: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return ExecutionEventRecord{}, fmt.Errorf("STATE_EXECUTION_EVENT_ID_FAILED: %w", err)
	}
	record.ID = id
	return record, nil
}

func (s *Store) RecentExecutionEvents(ctx context.Context, limit int) ([]ExecutionEventRecord, error) {
	limit = boundedLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, timestamp, task_id, step_id, kind, status, message, duration_ms, data_json
		FROM execution_events ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("STATE_EXECUTION_EVENTS_READ_FAILED: %w", err)
	}
	defer rows.Close()
	result := make([]ExecutionEventRecord, 0, limit)
	for rows.Next() {
		var record ExecutionEventRecord
		if err := rows.Scan(&record.ID, &record.Timestamp, &record.TaskID, &record.StepID, &record.Kind, &record.Status, &record.Message, &record.DurationMS, &record.DataJSON); err != nil {
			return nil, fmt.Errorf("STATE_EXECUTION_EVENT_SCAN_FAILED: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("STATE_EXECUTION_EVENTS_READ_FAILED: %w", err)
	}
	reverseExecutionEvents(result)
	return result, nil
}

func (s *Store) AppendRuntimeLog(ctx context.Context, record RuntimeLogRecord) (RuntimeLogRecord, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO runtime_logs(timestamp, level, component, message, fields_json, fingerprint)
		VALUES(?, ?, ?, ?, ?, ?)`,
		record.Timestamp, record.Level, record.Component, record.Message, record.FieldsJSON, record.Fingerprint)
	if err != nil {
		return RuntimeLogRecord{}, fmt.Errorf("STATE_RUNTIME_LOG_APPEND_FAILED: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return RuntimeLogRecord{}, fmt.Errorf("STATE_RUNTIME_LOG_ID_FAILED: %w", err)
	}
	record.ID = id
	return record, nil
}

func (s *Store) RecentRuntimeLogs(ctx context.Context, limit int) ([]RuntimeLogRecord, error) {
	limit = boundedLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, timestamp, level, component, message, fields_json, fingerprint
		FROM runtime_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("STATE_RUNTIME_LOGS_READ_FAILED: %w", err)
	}
	defer rows.Close()
	result := make([]RuntimeLogRecord, 0, limit)
	for rows.Next() {
		var record RuntimeLogRecord
		if err := rows.Scan(&record.ID, &record.Timestamp, &record.Level, &record.Component, &record.Message, &record.FieldsJSON, &record.Fingerprint); err != nil {
			return nil, fmt.Errorf("STATE_RUNTIME_LOG_SCAN_FAILED: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("STATE_RUNTIME_LOGS_READ_FAILED: %w", err)
	}
	reverseRuntimeLogs(result)
	return result, nil
}

// PruneObservability bounds persistent event/log history without touching MCP
// request or workspace state.
func (s *Store) PruneObservability(ctx context.Context, maxEvents, maxLogs int) error {
	if maxEvents < 1 || maxLogs < 1 {
		return fmt.Errorf("STATE_RETENTION_INVALID")
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM execution_events WHERE id NOT IN (
		SELECT id FROM execution_events ORDER BY id DESC LIMIT ?
	)`, maxEvents); err != nil {
		return fmt.Errorf("STATE_EXECUTION_EVENT_PRUNE_FAILED: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM runtime_logs WHERE id NOT IN (
		SELECT id FROM runtime_logs ORDER BY id DESC LIMIT ?
	)`, maxLogs); err != nil {
		return fmt.Errorf("STATE_RUNTIME_LOG_PRUNE_FAILED: %w", err)
	}
	return nil
}

func boundedLimit(limit int) int {
	if limit < 1 {
		return 100
	}
	if limit > 5000 {
		return 5000
	}
	return limit
}

func reverseExecutionEvents(records []ExecutionEventRecord) {
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
}

func reverseRuntimeLogs(records []RuntimeLogRecord) {
	for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
		records[left], records[right] = records[right], records[left]
	}
}
