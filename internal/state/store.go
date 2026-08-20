package state

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const schemaVersion = "3"

// Store is the single Go-owned SQLite state database for v1.6.0.
// v1.6.0 uses only its current MCP relay/runtime schema. Older CWapi databases
// are intentionally not imported or migrated.
type Store struct {
	path string
	db   *sql.DB
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("STATE_PATH_REQUIRED")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("STATE_DIRECTORY_CREATE_FAILED: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("STATE_OPEN_FAILED: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{path: path, db: db}
	ctx := context.Background()
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Path() string { return s.path }

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	if err := s.db.PingContext(ctx); err != nil {
		return fmt.Errorf("STATE_PING_FAILED: %w", err)
	}
	return nil
}

func (s *Store) configure(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("STATE_PRAGMA_FAILED: %s: %w", statement, err)
		}
	}
	return nil
}

func (s *Store) initialize(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("STATE_SCHEMA_BEGIN_FAILED: %w", err)
	}
	defer tx.Rollback()

	statements := []string{
		`CREATE TABLE IF NOT EXISTS metadata (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS execution_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			task_id TEXT NOT NULL DEFAULT '',
			step_id TEXT NOT NULL DEFAULT '',
			kind TEXT NOT NULL,
			status TEXT NOT NULL,
			message TEXT NOT NULL,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			data_json TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_execution_events_timestamp ON execution_events(timestamp DESC, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_execution_events_task ON execution_events(task_id, id DESC)`,
		`CREATE TABLE IF NOT EXISTS mcp_requests (
			request_id TEXT PRIMARY KEY,
			source_identity TEXT NOT NULL,
			source_message_id TEXT NOT NULL,
			method TEXT NOT NULL,
			arguments_hash TEXT NOT NULL,
			request_json TEXT NOT NULL,
			execution_state TEXT NOT NULL,
			delivery_state TEXT NOT NULL,
			response_json TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_requests_execution ON mcp_requests(execution_state, updated_at)`,
		`CREATE INDEX IF NOT EXISTS idx_mcp_requests_delivery ON mcp_requests(delivery_state, updated_at)`,
		`CREATE TABLE IF NOT EXISTS runtime_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp INTEGER NOT NULL,
			level TEXT NOT NULL,
			component TEXT NOT NULL,
			message TEXT NOT NULL,
			fields_json TEXT NOT NULL DEFAULT '{}',
			fingerprint TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runtime_logs_timestamp ON runtime_logs(timestamp DESC, id DESC)`,
		`CREATE TABLE IF NOT EXISTS error_aggregates (
			fingerprint TEXT PRIMARY KEY,
			component TEXT NOT NULL,
			operation TEXT NOT NULL,
			message TEXT NOT NULL,
			count INTEGER NOT NULL,
			first_seen INTEGER NOT NULL,
			last_seen INTEGER NOT NULL,
			active INTEGER NOT NULL CHECK(active IN (0,1))
		)`,
		`CREATE INDEX IF NOT EXISTS idx_error_aggregates_active_seen ON error_aggregates(active, last_seen DESC)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("STATE_SCHEMA_CREATE_FAILED: %w", err)
		}
	}

	var existing string
	err = tx.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='schema_version'`).Scan(&existing)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `INSERT INTO metadata(key, value) VALUES('schema_version', ?)`, schemaVersion); err != nil {
			return fmt.Errorf("STATE_SCHEMA_VERSION_WRITE_FAILED: %w", err)
		}
	case err != nil:
		return fmt.Errorf("STATE_SCHEMA_VERSION_READ_FAILED: %w", err)
	case existing != schemaVersion:
		return fmt.Errorf("STATE_SCHEMA_VERSION_UNSUPPORTED: got=%s want=%s", existing, schemaVersion)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("STATE_SCHEMA_COMMIT_FAILED: %w", err)
	}
	return nil
}

func (s *Store) SchemaVersion(ctx context.Context) (string, error) {
	var version string
	if err := s.db.QueryRowContext(ctx, `SELECT value FROM metadata WHERE key='schema_version'`).Scan(&version); err != nil {
		return "", fmt.Errorf("STATE_SCHEMA_VERSION_READ_FAILED: %w", err)
	}
	return version, nil
}
