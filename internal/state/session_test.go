package state

import (
	"context"
	"path/filepath"
	"testing"
)

func TestResetRuntimeSessionClearsOldRecordsAndKeepsSchema(t *testing.T) {
	ctx := context.Background()
	store, err := Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	request := MCPRequestRecord{
		RequestID: "old-request", SourceIdentity: "slack:C1:1.000", SourceMessageID: "1.000",
		Method: "mcpServerStatus/list", ArgumentsHash: "old-hash", RequestJSON: `{}`,
		ExecutionState: "completed", DeliveryState: MCPDeliveryDelivered, ResponseJSON: `{}`,
		CreatedAt: 1, UpdatedAt: 2,
	}
	if _, inserted, err := store.InsertMCPRequestIfAbsent(ctx, request); err != nil || !inserted {
		t.Fatalf("insert old request: inserted=%v err=%v", inserted, err)
	}
	if _, err := store.AppendExecutionEvent(ctx, ExecutionEventRecord{Timestamp: 1, TaskID: request.RequestID, Kind: "mcp.tool", Status: "completed", Message: "old event", DataJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendRuntimeLog(ctx, RuntimeLogRecord{Timestamp: 1, Level: "info", Component: "old", Message: "old log", FieldsJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMetadata(ctx, "slack.last_successful_message_ts", "1.000"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMetadata(ctx, "persistent.example", "keep"); err != nil {
		t.Fatal(err)
	}

	if err := store.ResetRuntimeSession(ctx); err != nil {
		t.Fatal(err)
	}
	requests, requestErr := store.RecentMCPRequests(ctx, 10)
	events, eventErr := store.RecentExecutionEvents(ctx, 10)
	logs, logErr := store.RecentRuntimeLogs(ctx, 10)
	if requestErr != nil || eventErr != nil || logErr != nil {
		t.Fatalf("read reset session: request=%v event=%v log=%v", requestErr, eventErr, logErr)
	}
	if len(requests) != 0 || len(events) != 0 || len(logs) != 0 {
		t.Fatalf("old session survived reset: requests=%d events=%d logs=%d", len(requests), len(events), len(logs))
	}
	if _, found, err := store.Metadata(ctx, "slack.last_successful_message_ts"); err != nil || found {
		t.Fatalf("old Slack cursor survived reset: found=%v err=%v", found, err)
	}
	if value, found, err := store.Metadata(ctx, "persistent.example"); err != nil || !found || value != "keep" {
		t.Fatalf("persistent metadata was cleared: value=%q found=%v err=%v", value, found, err)
	}
	if version, err := store.SchemaVersion(ctx); err != nil || version != schemaVersion {
		t.Fatalf("schema after reset=%q err=%v", version, err)
	}
}
