package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/observability"
)

func TestServiceObservabilitySnapshotSeparatesStreamsAndLogsErrors(t *testing.T) {
	root := t.TempDir()
	service, err := NewServiceWithPaths(
		filepath.Join(root, "config", "cwapi.json"),
		filepath.Join(root, "state", "cwapi.db"),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	if _, err := service.observability.EmitExecution(context.Background(), observability.ExecutionInput{
		Timestamp: 100,
		TaskID:    "task-1",
		Kind:      "function",
		Status:    "completed",
		Message:   "structured-only",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.observability.LogRuntime(context.Background(), observability.RuntimeInput{
		Timestamp: 200,
		Level:     "info",
		Component: "runner",
		Message:   "runtime-only",
	}); err != nil {
		t.Fatal(err)
	}
	service.recordOperationalError("desktop", "snapshot", errors.New("same persistent failure"))
	service.recordOperationalError("desktop", "snapshot", errors.New("same persistent failure"))

	snapshot := service.ObservabilitySnapshot()
	if snapshot.StateSchema != "3" || snapshot.StatePath != filepath.Join(root, "state", "cwapi.db") {
		t.Fatalf("state metadata mismatch: %#v", snapshot)
	}
	if len(snapshot.StructuredExecution) != 1 || snapshot.StructuredExecution[0].Message != "structured-only" {
		t.Fatalf("structured surface mismatch: %#v", snapshot.StructuredExecution)
	}
	foundRuntime := false
	for _, record := range snapshot.RuntimeLogs {
		if record.Message == "runtime-only" {
			foundRuntime = true
		}
		if record.Message == "structured-only" {
			t.Fatalf("structured event leaked into runtime logs: %#v", record)
		}
	}
	if !foundRuntime {
		t.Fatalf("runtime log missing: %#v", snapshot.RuntimeLogs)
	}
	errorCount := 0
	for _, record := range snapshot.RuntimeLogs {
		if record.Level == "error" && record.Message == "snapshot: same persistent failure" && record.Fingerprint != "" {
			errorCount++
		}
	}
	if errorCount != 2 {
		t.Fatalf("operational errors missing from runtime log: %#v", snapshot.RuntimeLogs)
	}
}
