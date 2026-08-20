package state

import (
	"context"
	"fmt"
)

func (s *Store) ExecutionEventsForTask(ctx context.Context, taskID string, limit int) ([]ExecutionEventRecord, error) {
	limit = boundedLimit(limit)
	rows, err := s.db.QueryContext(ctx, `SELECT id, timestamp, task_id, step_id, kind, status, message, duration_ms, data_json
		FROM execution_events WHERE task_id=? ORDER BY id DESC LIMIT ?`, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("STATE_TASK_EXECUTION_EVENTS_READ_FAILED: %w", err)
	}
	defer rows.Close()
	result := make([]ExecutionEventRecord, 0, limit)
	for rows.Next() {
		var record ExecutionEventRecord
		if err := rows.Scan(&record.ID, &record.Timestamp, &record.TaskID, &record.StepID, &record.Kind, &record.Status, &record.Message, &record.DurationMS, &record.DataJSON); err != nil {
			return nil, fmt.Errorf("STATE_TASK_EXECUTION_EVENT_SCAN_FAILED: %w", err)
		}
		result = append(result, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("STATE_TASK_EXECUTION_EVENTS_READ_FAILED: %w", err)
	}
	reverseExecutionEvents(result)
	return result, nil
}
