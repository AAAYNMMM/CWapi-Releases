package state

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestFreshStateSchemaPersistsV160State(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state", "cwapi.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	version, err := store.SchemaVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if version != schemaVersion {
		t.Fatalf("schema version = %q, want %q", version, schemaVersion)
	}

	now := int64(1000)
	request := MCPRequestRecord{
		RequestID:       "req-1",
		SourceIdentity:  "slack:C1:1000.1",
		SourceMessageID: "1000.1",
		Method:          "mcpServer/tool/call",
		ArgumentsHash:   "hash-1",
		RequestJSON:     `{}`,
		ExecutionState:  MCPExecutionReceived,
		DeliveryState:   MCPDeliveryPending,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	if _, inserted, err := store.InsertMCPRequestIfAbsent(ctx, request); err != nil || !inserted {
		t.Fatalf("insert MCP request: inserted=%v err=%v", inserted, err)
	}
	if _, err := store.AppendExecutionEvent(ctx, ExecutionEventRecord{Timestamp: now, TaskID: request.RequestID, Kind: "mcp.tool", Status: "running", Message: "calling stock MCP tool", DataJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendRuntimeLog(ctx, RuntimeLogRecord{Timestamp: now, Level: "info", Component: "state", Message: "ready", FieldsJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertErrorAggregate(ctx, ErrorAggregateRecord{Fingerprint: "fp-1", Component: "desktop", Operation: "refresh", Message: "failed", Count: 1, FirstSeen: now, LastSeen: now, Active: true}); err != nil {
		t.Fatal(err)
	}

	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()

	storedRequest, err := reopened.MCPRequestByID(ctx, request.RequestID)
	if err != nil || storedRequest.Method != request.Method {
		t.Fatalf("MCP request after reopen: %#v err=%v", storedRequest, err)
	}
	events, err := reopened.RecentExecutionEvents(ctx, 10)
	if err != nil || len(events) != 1 || events[0].TaskID != request.RequestID {
		t.Fatalf("execution events after reopen: %#v err=%v", events, err)
	}
	logs, err := reopened.RecentRuntimeLogs(ctx, 10)
	if err != nil || len(logs) != 1 || logs[0].Component != "state" {
		t.Fatalf("runtime logs after reopen: %#v err=%v", logs, err)
	}
	errors, err := reopened.RecentErrorAggregates(ctx, 10)
	if err != nil || len(errors) != 1 || errors[0].Count != 1 || !errors[0].Active {
		t.Fatalf("error aggregates after reopen: %#v err=%v", errors, err)
	}
}

func TestFreshStateSchemaDoesNotCreateLegacyRunnerTables(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	legacyTables := []string{
		"tasks",
		"steps",
		"workspaces",
		"results",
		"outbox",
		"protocol_messages",
		"task_specs",
	}
	for _, table := range legacyTables {
		var count int
		if err := store.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("legacy table %q exists in v1.6 schema", table)
		}
	}
}

func TestStateRejectsDifferentSchemaVersionInsteadOfMigrating(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cwapi.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE metadata SET value='1' WHERE key='schema_version'`); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = Open(path)
	if err == nil || !strings.Contains(err.Error(), "STATE_SCHEMA_VERSION_UNSUPPORTED") {
		t.Fatalf("expected unsupported schema rejection, got %v", err)
	}
}

func TestObservabilityRetentionKeepsNewestRecords(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	for index := 0; index < 7; index++ {
		if _, err := store.AppendExecutionEvent(ctx, ExecutionEventRecord{Timestamp: int64(index + 1), Kind: "step", Status: "running", Message: "event", DataJSON: `{}`}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.AppendRuntimeLog(ctx, RuntimeLogRecord{Timestamp: int64(index + 1), Level: "info", Component: "test", Message: "log", FieldsJSON: `{}`}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PruneObservability(ctx, 3, 4); err != nil {
		t.Fatal(err)
	}
	events, err := store.RecentExecutionEvents(ctx, 10)
	if err != nil || len(events) != 3 || events[0].Timestamp != 5 || events[2].Timestamp != 7 {
		t.Fatalf("retained events = %#v err=%v", events, err)
	}
	logs, err := store.RecentRuntimeLogs(ctx, 10)
	if err != nil || len(logs) != 4 || logs[0].Timestamp != 4 || logs[3].Timestamp != 7 {
		t.Fatalf("retained logs = %#v err=%v", logs, err)
	}
}

func TestErrorAggregateIncrementsInsteadOfCreatingDuplicates(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first := ErrorAggregateRecord{Fingerprint: "same", Component: "desktop", Operation: "snapshot", Message: "boom", Count: 1, FirstSeen: 10, LastSeen: 10, Active: true}
	if _, err := store.UpsertErrorAggregate(ctx, first); err != nil {
		t.Fatal(err)
	}
	first.LastSeen = 20
	result, err := store.UpsertErrorAggregate(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 || result.FirstSeen != 10 || result.LastSeen != 20 || !result.Active {
		t.Fatalf("aggregate = %#v", result)
	}
}
