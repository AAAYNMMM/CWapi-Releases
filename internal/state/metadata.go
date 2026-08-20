package state

import (
	"context"
	"database/sql"
	"fmt"
)

func (s *Store) Metadata(ctx context.Context, key string) (string, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key=?`, key).Scan(&value)
	if err != nil {
		if err == sql.ErrNoRows {
			return "", false, nil
		}
		return "", false, fmt.Errorf("STATE_METADATA_READ_FAILED: %w", err)
	}
	return value, true, nil
}

func (s *Store) SetMetadata(ctx context.Context, key, value string) error {
	if key == "" {
		return fmt.Errorf("STATE_METADATA_KEY_REQUIRED")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO metadata(key, value) VALUES(?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	if err != nil {
		return fmt.Errorf("STATE_METADATA_WRITE_FAILED: %w", err)
	}
	return nil
}
