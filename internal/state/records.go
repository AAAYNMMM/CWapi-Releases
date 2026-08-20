package state

import (
	"context"
	"database/sql"
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

func (s *Store) UpsertErrorAggregate(ctx context.Context, record ErrorAggregateRecord) (ErrorAggregateRecord, error) {
	active := 0
	if record.Active {
		active = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO error_aggregates(fingerprint, component, operation, message, count, first_seen, last_seen, active)
		VALUES(?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(fingerprint) DO UPDATE SET component=excluded.component, operation=excluded.operation,
		message=excluded.message, count=error_aggregates.count + 1, last_seen=excluded.last_seen, active=1`,
		record.Fingerprint, record.Component, record.Operation, record.Message, record.Count, record.FirstSeen, record.LastSeen, active)
	if err != nil {
		return ErrorAggregateRecord{}, fmt.Errorf("STATE_ERROR_AGGREGATE_UPSERT_FAILED: %w", err)
	}
	return s.ErrorAggregate(ctx, record.Fingerprint)
}

func (s *Store) ErrorAggregate(ctx context.Context, fingerprint string) (ErrorAggregateRecord, error) {
	var record ErrorAggregateRecord
	var active int
	err := s.db.QueryRowContext(ctx, `SELECT fingerprint, component, operation, message, count, first_seen, last_seen, active
		FROM error_aggregates WHERE fingerprint=?`, fingerprint).Scan(
		&record.Fingerprint, &record.Component, &record.Operation, &record.Message, &record.Count, &record.FirstSeen, &record.LastSeen, &active,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return ErrorAggregateRecord{}, fmt.Errorf("STATE_ERROR_AGGREGATE_NOT_FOUND")
		}
		return ErrorAggregateRecord{}, fmt.Errorf("STATE_ERROR_AGGREGATE_READ_FAILED: %w", err)
	}
	record.Active = active != 0
	return record, nil
}

func (s *Store) ResolveError(ctx context.Context, fingerprint string, timestamp int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE error_aggregates SET active=0, last_seen=? WHERE fingerprint=?`, timestamp, fingerprint)
	if err != nil {
		return fmt.Errorf("STATE_ERROR_RESOLVE_FAILED: %w", err)
	}
	return nil
}

func (s *Store) RecentErrorAggregates(ctx context.Context, limit int) ([]ErrorAggregateRecord, error) {
	limit = boundedLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT fingerprint, component, operation, message, count, first_seen, last_seen, active
		FROM error_aggregates ORDER BY active DESC, last_seen DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("STATE_ERROR_AGGREGATES_READ_FAILED: %w", err)
	}
	defer rows.Close()
	result := make([]ErrorAggregateRecord, 0, limit)
	for rows.Next() {
		var record ErrorAggregateRecord
		var active int
		if err := rows.Scan(&record.Fingerprint, &record.Component, &record.Operation, &record.Message, &record.Count, &record.FirstSeen, &record.LastSeen, &active); err != nil {
			return nil, fmt.Errorf("STATE_ERROR_AGGREGATE_SCAN_FAILED: %w", err)
		}
		record.Active = active != 0
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("STATE_ERROR_AGGREGATES_READ_FAILED: %w", err)
	}
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
