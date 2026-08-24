package app

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

func newDesktopWorkflowTestService(t *testing.T) *Service {
	t.Helper()
	root := t.TempDir()
	service, err := NewServiceWithPaths(
		filepath.Join(root, "config", "cwapi.json"),
		filepath.Join(root, "state", "cwapi.db"),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	return service
}

func TestProductionServiceUsesMCPOnlyRuntime(t *testing.T) {
	service := newDesktopWorkflowTestService(t)
	if service.gateway == nil || service.codexHost == nil || service.slack == nil {
		t.Fatalf("MCP runtime not initialized: %#v", service)
	}
	if stage := service.RuntimeSnapshot().Stage; stage != "v1.6.1" {
		t.Fatalf("runtime stage=%q", stage)
	}
}

func TestDesktopSnapshotUsesGoOwnedMCPState(t *testing.T) {
	ctx := context.Background()
	service := newDesktopWorkflowTestService(t)
	service.recordOperationalError("desktop", "snapshot.test", context.Canceled)
	snapshot, err := service.DesktopSnapshot(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Runtime.Stage != "v1.6.1" {
		t.Fatalf("runtime stage=%q", snapshot.Runtime.Stage)
	}
	if snapshot.Config.Schema == "" || len(snapshot.Components) == 0 || snapshot.Processes == nil {
		t.Fatalf("desktop snapshot missing authoritative MCP state: %#v", snapshot)
	}
	if snapshot.LatestRuntimeError == nil || snapshot.LatestRuntimeError.Level != "error" || snapshot.LatestRuntimeError.Component != "desktop" {
		t.Fatalf("desktop snapshot missing latest runtime error: %#v", snapshot.LatestRuntimeError)
	}
}

func TestDesktopSnapshotOmitsRoutineRuntimeInfo(t *testing.T) {
	service := newDesktopWorkflowTestService(t)
	service.runtimeInfo("slack", "socket.ready", map[string]any{"attempt": 1})
	snapshot, err := service.DesktopSnapshot(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LatestRuntimeError != nil {
		t.Fatalf("routine runtime info leaked into compact GUI: %#v", snapshot.LatestRuntimeError)
	}
}

func TestServiceStartupPersistsSafePermissionMode(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "cwapi.json")
	statePath := filepath.Join(root, "state", "cwapi.db")
	seed := config.Default()
	seed.PermissionMode = config.PermissionModeFullAccess
	seed.Slack.ChannelID = "C0123456789"
	if err := config.SaveAtomic(configPath, seed); err != nil {
		t.Fatal(err)
	}

	service, err := NewServiceWithPaths(configPath, statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if mode := service.ConfigSnapshot().PermissionMode; mode != config.PermissionModeSafe {
		t.Fatalf("runtime permission mode=%q", mode)
	}
	persisted, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.PermissionMode != config.PermissionModeSafe || persisted.Slack.ChannelID != seed.Slack.ChannelID {
		t.Fatalf("startup config=%#v", persisted)
	}
}

func TestServiceStartupDoesNotLoadPreviousRuntimeSession(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	statePath := filepath.Join(root, "state", "cwapi.db")
	store, err := state.Open(statePath)
	if err != nil {
		t.Fatal(err)
	}
	oldRequest := state.MCPRequestRecord{
		RequestID: "old-request", SourceIdentity: "slack:C1:1.000", SourceMessageID: "1.000",
		Method: "mcpServerStatus/list", ArgumentsHash: "old-hash", RequestJSON: `{}`,
		ExecutionState: "completed", DeliveryState: state.MCPDeliveryDelivered, ResponseJSON: `{}`,
		CreatedAt: 1, UpdatedAt: 2,
	}
	if _, inserted, err := store.InsertMCPRequestIfAbsent(ctx, oldRequest); err != nil || !inserted {
		t.Fatalf("seed old request: inserted=%v err=%v", inserted, err)
	}
	if _, err := store.AppendExecutionEvent(ctx, state.ExecutionEventRecord{Timestamp: 1, TaskID: oldRequest.RequestID, Kind: "old", Status: "completed", Message: "old event", DataJSON: `{}`}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMetadata(ctx, slackCursorKey, "1.000"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	service, err := NewServiceWithPaths(filepath.Join(root, "config", "cwapi.json"), statePath)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	requests, err := service.RecentMCPRequests(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	observability := service.ObservabilitySnapshot()
	if len(requests) != 0 || len(observability.StructuredExecution) != 0 {
		t.Fatalf("previous runtime session loaded: requests=%#v events=%#v", requests, observability.StructuredExecution)
	}
	if _, found, err := service.state.Metadata(ctx, slackCursorKey); err != nil || found {
		t.Fatalf("previous Slack cursor loaded: found=%v err=%v", found, err)
	}
}
