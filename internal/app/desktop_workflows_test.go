package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

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
	if stage := service.RuntimeSnapshot().Stage; stage != "S2.4" {
		t.Fatalf("runtime stage=%q", stage)
	}
}

func TestDesktopSnapshotUsesGoOwnedMCPState(t *testing.T) {
	ctx := context.Background()
	service := newDesktopWorkflowTestService(t)
	snapshot, err := service.DesktopSnapshot(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Runtime.Stage != "S2.4" {
		t.Fatalf("runtime stage=%q", snapshot.Runtime.Stage)
	}
	if snapshot.Config.Schema == "" || snapshot.Observability.StatePath == "" {
		t.Fatalf("desktop snapshot missing authoritative MCP state: %#v", snapshot)
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
	snapshot, err := service.DesktopSnapshot(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.MCPRequests) != 0 || len(snapshot.Observability.StructuredExecution) != 0 {
		t.Fatalf("previous runtime session loaded: requests=%#v events=%#v", snapshot.MCPRequests, snapshot.Observability.StructuredExecution)
	}
	if _, found, err := service.state.Metadata(ctx, slackCursorKey); err != nil || found {
		t.Fatalf("previous Slack cursor loaded: found=%v err=%v", found, err)
	}
}

func TestDiagnosticsAndPersistentErrorResolutionStayAuthoritative(t *testing.T) {
	ctx := context.Background()
	service := newDesktopWorkflowTestService(t)
	service.recordOperationalError("desktop", "snapshot", errors.New("same persistent failure"))
	service.recordOperationalError("desktop", "snapshot", errors.New("same persistent failure"))

	before := service.ObservabilitySnapshot()
	if len(before.Errors) != 1 || !before.Errors[0].Active || before.Errors[0].Count != 2 {
		t.Fatalf("before resolve=%#v", before.Errors)
	}
	fingerprint := before.Errors[0].Fingerprint
	resolved, err := service.ResolveDesktopError(ctx, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.Errors) != 1 || resolved.Errors[0].Active || resolved.Errors[0].Count != 2 {
		t.Fatalf("resolved snapshot=%#v", resolved.Errors)
	}
	persisted, err := service.state.ErrorAggregate(ctx, fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Active {
		t.Fatalf("persistent error still active=%#v", persisted)
	}

	diagnostics, err := service.DiagnosticsSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostics.Stage != "S2.4" || diagnostics.StateSchema != "3" {
		t.Fatalf("diagnostics=%#v", diagnostics)
	}
	if diagnostics.ConfigPath != service.config.Path() || diagnostics.StatePath != service.state.Path() {
		t.Fatalf("diagnostic paths=%#v", diagnostics)
	}
	foundDesktop := false
	foundRelay := false
	for _, component := range diagnostics.Components {
		if component.Name == "desktop" && component.State == "healthy" {
			foundDesktop = true
		}
		if component.Name == "mcp-relay" && component.State == "healthy" {
			foundRelay = true
		}
	}
	if !foundDesktop || !foundRelay {
		t.Fatalf("expected desktop and MCP relay components: %#v", diagnostics.Components)
	}
}
