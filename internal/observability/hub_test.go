package observability

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/state"
)

func TestHubKeepsStructuredAndRuntimeLogsSeparateAndBounded(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub, err := New(ctx, store, 2, 3)
	if err != nil {
		t.Fatal(err)
	}

	for index := 1; index <= 3; index++ {
		if _, err := hub.EmitExecution(ctx, ExecutionInput{Timestamp: int64(index), TaskID: "task", Kind: "step", Status: "running", Message: "structured"}); err != nil {
			t.Fatal(err)
		}
		if _, err := hub.LogRuntime(ctx, RuntimeInput{Timestamp: int64(index), Level: "info", Component: "runner", Message: "runtime"}); err != nil {
			t.Fatal(err)
		}
	}

	snapshot := hub.Snapshot()
	if len(snapshot.StructuredExecution) != 2 || snapshot.StructuredExecution[0].Timestamp != 2 || snapshot.StructuredExecution[1].Timestamp != 3 {
		t.Fatalf("structured live buffer = %#v", snapshot.StructuredExecution)
	}
	if len(snapshot.RuntimeLogs) != 2 || snapshot.RuntimeLogs[0].Timestamp != 2 || snapshot.RuntimeLogs[1].Timestamp != 3 {
		t.Fatalf("runtime live buffer = %#v", snapshot.RuntimeLogs)
	}
	for _, event := range snapshot.StructuredExecution {
		if event.Message != "structured" {
			t.Fatalf("runtime data leaked into structured surface: %#v", event)
		}
	}
	for _, record := range snapshot.RuntimeLogs {
		if record.Message != "runtime" {
			t.Fatalf("structured data leaked into runtime surface: %#v", record)
		}
	}
}

func TestHubRedactsBeforePersistenceAndPresentation(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub, err := New(ctx, store, 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	secret := "xoxb-123456789-secretvalue"
	if _, err := hub.EmitExecution(ctx, ExecutionInput{
		TaskID:  "task",
		Kind:    "step",
		Status:  "failed",
		Message: "request token=" + secret,
		Data:    map[string]any{"authorization": "Bearer abcdef123456"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.LogRuntime(ctx, RuntimeInput{Level: "error", Component: "slack", Message: "bot token=" + secret}); err != nil {
		t.Fatal(err)
	}

	snapshot := hub.Snapshot()
	joined := snapshot.StructuredExecution[0].Message + snapshot.StructuredExecution[0].DataJSON + snapshot.RuntimeLogs[0].Message
	if strings.Contains(joined, secret) || strings.Contains(joined, "abcdef123456") {
		t.Fatalf("secret reached live snapshot: %s", joined)
	}
	if !strings.Contains(joined, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", joined)
	}

	events, err := store.RecentExecutionEvents(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	logs, err := store.RecentRuntimeLogs(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	persisted := events[0].Message + events[0].DataJSON + logs[0].Message
	if strings.Contains(persisted, secret) || strings.Contains(persisted, "abcdef123456") {
		t.Fatalf("secret reached SQLite: %s", persisted)
	}
}

func TestUnserializableFieldsProduceValidRedactionJSON(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub, err := New(ctx, store, 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	record, err := hub.EmitExecution(ctx, ExecutionInput{
		Kind:    "step",
		Status:  "failed",
		Message: "unserializable fields",
		Data:    map[string]any{"callback": func() {}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid([]byte(record.DataJSON)) {
		t.Fatalf("fallback is not valid JSON: %q", record.DataJSON)
	}
	if record.DataJSON != "{\"redaction_error\":\"unserializable fields\"}" {
		t.Fatalf("unexpected fallback JSON: %q", record.DataJSON)
	}
}

func TestPersistentErrorIsAggregatedAndResolvable(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub, err := New(ctx, store, 10, 10)
	if err != nil {
		t.Fatal(err)
	}

	first, err := hub.RecordError(ctx, ErrorInput{Timestamp: 10, Component: "desktop", Operation: "observability.snapshot", Message: "backend unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := hub.RecordError(ctx, ErrorInput{Timestamp: 20, Component: "desktop", Operation: "observability.snapshot", Message: "backend unavailable"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint != second.Fingerprint || second.Count != 2 || second.FirstSeen != 10 || second.LastSeen != 20 {
		t.Fatalf("dedup aggregate mismatch: first=%#v second=%#v", first, second)
	}
	snapshot := hub.Snapshot()
	if len(snapshot.Errors) != 1 || snapshot.Errors[0].Count != 2 || !snapshot.Errors[0].Active {
		t.Fatalf("snapshot errors = %#v", snapshot.Errors)
	}
	if err := hub.ResolveError(ctx, second.Fingerprint); err != nil {
		t.Fatal(err)
	}
	if hub.Snapshot().Errors[0].Active {
		t.Fatal("resolved error remained active")
	}
}

func TestComponentSnapshotIsGoOwnedAndRedacted(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub, err := New(ctx, store, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	hub.SetComponent("runner", "healthy", "authorization=Bearer should-not-leak")

	snapshot := hub.Snapshot()
	if snapshot.StateSchema != "3" || snapshot.StatePath == "" {
		t.Fatalf("state snapshot incomplete: %#v", snapshot)
	}
	found := false
	for _, component := range snapshot.Components {
		if component.Name == "runner" {
			found = true
			if strings.Contains(component.Detail, "should-not-leak") || !strings.Contains(component.Detail, "[REDACTED]") {
				t.Fatalf("component detail was not redacted: %q", component.Detail)
			}
		}
	}
	if !found {
		t.Fatal("runner component missing")
	}
}
