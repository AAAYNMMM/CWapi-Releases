package state

import (
	"context"
	"fmt"
)

// ResetRuntimeSession removes records owned by the previous CWapi process.
// Configuration, workspace caches and the schema version live outside this
// session boundary and remain available to the next process.
func (s *Store) ResetRuntimeSession(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("STATE_SESSION_RESET_BEGIN_FAILED: %w", err)
	}
	defer tx.Rollback()

	for _, statement := range []string{
		`DELETE FROM mcp_requests`,
		`DELETE FROM execution_events`,
		`DELETE FROM runtime_logs`,
		`DELETE FROM error_aggregates`,
		`DELETE FROM sqlite_sequence WHERE name IN ('execution_events', 'runtime_logs')`,
		`DELETE FROM metadata WHERE key = 'slack.last_successful_message_ts'`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("STATE_SESSION_RESET_FAILED: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("STATE_SESSION_RESET_COMMIT_FAILED: %w", err)
	}
	return nil
}
