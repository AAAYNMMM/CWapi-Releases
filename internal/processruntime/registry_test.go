package processruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fakeHandle struct {
	done      chan Completion
	stopCount atomic.Int32
}

func newFakeHandle() *fakeHandle              { return &fakeHandle{done: make(chan Completion, 1)} }
func (h *fakeHandle) Done() <-chan Completion { return h.done }
func (h *fakeHandle) Stop() error {
	h.stopCount.Add(1)
	select {
	case h.done <- Completion{}:
	default:
	}
	return nil
}

func testSpec(handle *fakeHandle, cleanup *atomic.Int32) Spec {
	return Spec{
		Backend: BackendCodex, Repository: "owner/repo",
		ExpectedCommit: strings.Repeat("a", 40), WorkingDirectory: ".",
		Cleanup: func() { cleanup.Add(1) },
		Launch:  func(context.Context, string, *Tails) (Handle, error) { return handle, nil },
	}
}

func TestRegistryShortAndLongLifecycle(t *testing.T) {
	registry := NewRegistry()
	registry.observe = 15 * time.Millisecond
	defer registry.Close()

	var quickCleanup atomic.Int32
	quick := newFakeHandle()
	quick.done <- Completion{ExitCode: intPointer(0), Stdout: "ok", Stderr: "warning"}
	started, err := registry.Start(testSpec(quick, &quickCleanup))
	if err != nil {
		t.Fatal(err)
	}
	if started.Record.State != StateCompleted || started.Record.StdoutTail != "ok" || started.Record.StderrTail != "warning" {
		t.Fatalf("unexpected quick record: %#v", started.Record)
	}
	if started.Record.LatestStream != "stderr" || started.Record.LatestOutputAt == 0 {
		t.Fatalf("quick latest output metadata: %#v", started.Record)
	}
	if quickCleanup.Load() != 1 {
		t.Fatalf("quick cleanup count = %d", quickCleanup.Load())
	}

	var longCleanup atomic.Int32
	long := newFakeHandle()
	running, err := registry.Start(testSpec(long, &longCleanup))
	if err != nil {
		t.Fatal(err)
	}
	if running.Record.State != StateRunning {
		t.Fatalf("long state = %s", running.Record.State)
	}
	snapshot := registry.Snapshot(2)
	if len(snapshot) != 2 || snapshot[0].ProcessID != running.Record.ProcessID || snapshot[0].State != StateRunning || snapshot[1].State != StateCompleted {
		t.Fatalf("ordered public snapshot = %#v", snapshot)
	}
	stopped, err := registry.Stop(running.Record.ProcessID)
	if err != nil {
		t.Fatal(err)
	}
	if stopped.State != StateStopped || long.stopCount.Load() != 1 || longCleanup.Load() != 1 {
		t.Fatalf("stop mismatch: record=%#v stops=%d cleanups=%d", stopped, long.stopCount.Load(), longCleanup.Load())
	}
	retained, err := registry.Stop(running.Record.ProcessID)
	if err != nil || retained.State != StateStopped || long.stopCount.Load() != 1 {
		t.Fatalf("terminal stop mismatch: record=%#v err=%v stops=%d", retained, err, long.stopCount.Load())
	}
}

func TestRegistryLimitsAndTerminalTrim(t *testing.T) {
	registry := NewRegistry()
	registry.observe = time.Millisecond
	defer registry.Close()
	handles := make([]*fakeHandle, 0, MaxActive)
	ids := make([]string, 0, MaxActive)
	for index := 0; index < MaxActive; index++ {
		handle := newFakeHandle()
		handles = append(handles, handle)
		started, err := registry.Start(testSpec(handle, &atomic.Int32{}))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, started.Record.ProcessID)
	}
	if _, err := registry.Start(testSpec(newFakeHandle(), &atomic.Int32{})); !errors.Is(err, ErrLimitReached) {
		t.Fatalf("ninth active error = %v", err)
	}
	for _, handle := range handles {
		handle.done <- Completion{ExitCode: intPointer(0)}
	}
	for _, id := range ids {
		waitTerminal(t, registry, id)
	}

	registry.mu.Lock()
	first := registry.terminalOrder[0]
	registry.mu.Unlock()
	for index := MaxActive; index < MaxTerminal+5; index++ {
		handle := newFakeHandle()
		handle.done <- Completion{ExitCode: intPointer(0)}
		started, err := registry.Start(testSpec(handle, &atomic.Int32{}))
		if err != nil {
			t.Fatal(err)
		}
		if started.Record.State != StateCompleted {
			t.Fatalf("terminal %d state = %s", index, started.Record.State)
		}
	}
	if _, err := registry.Status(first); !errors.Is(err, ErrNotFound) {
		t.Fatalf("trimmed record error = %v", err)
	}
	registry.mu.Lock()
	count, terminalCount, active := len(registry.entries), len(registry.terminalOrder), registry.active
	registry.mu.Unlock()
	if count != MaxTerminal || terminalCount != MaxTerminal || active != 0 {
		t.Fatalf("registry limits mismatch: count=%d terminal=%d active=%d", count, terminalCount, active)
	}
}

func TestRegistryFailureDiscardAndPublicRedaction(t *testing.T) {
	registry := NewRegistry()
	registry.observe = 20 * time.Millisecond
	defer registry.Close()

	var cleanup atomic.Int32
	handle := newFakeHandle()
	handle.done <- Completion{
		ExitCode: intPointer(5), Stdout: strings.Repeat("x", TailBytes+50) + " token=secret-value",
		Stderr: "Authorization: Bearer abc.def", Failure: &Failure{Code: FailurePermission, Message: "password=hunter2"},
		DiscardRecord: true,
	}
	result, err := registry.Start(testSpec(handle, &cleanup))
	if err != nil {
		t.Fatal(err)
	}
	if result.Completion == nil || result.Completion.Failure.Code != FailurePermission || result.Record.ProcessID != "" {
		t.Fatalf("discard result = %#v", result)
	}
	if cleanup.Load() != 1 {
		t.Fatalf("discard cleanup count = %d", cleanup.Load())
	}

	normal := newFakeHandle()
	normal.done <- Completion{
		ExitCode: intPointer(7), Stdout: "api_key=visible", Stderr: "Bearer abc.def",
		Failure: &Failure{Code: "INTERNAL_CODE", Message: "secret=visible"},
	}
	failed, err := registry.Start(testSpec(normal, &atomic.Int32{}))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(failed.Record)
	text := string(encoded)
	if failed.Record.State != StateFailed || failed.Record.Error.Code != FailureUnknown ||
		strings.Contains(text, "visible") || strings.Contains(text, "abc.def") || strings.Contains(text, "LatestStream") ||
		strings.Contains(text, "latest_output") || len(failed.Record.StdoutTail) > TailBytes {
		t.Fatalf("public failure was not bounded/redacted: %s", text)
	}
}

func TestRegistryLaunchFailureAndCloseCleanupOnce(t *testing.T) {
	registry := NewRegistry()
	registry.observe = time.Millisecond
	var failedCleanup atomic.Int32
	failed, err := registry.Start(Spec{
		Backend: BackendCodex, Repository: "owner/repo", ExpectedCommit: strings.Repeat("b", 40), WorkingDirectory: ".",
		Cleanup: func() { failedCleanup.Add(1) },
		Launch:  func(context.Context, string, *Tails) (Handle, error) { return nil, errors.New("launch failed") },
	})
	if err != nil || failed.Record.State != StateFailed || failed.Record.Error.Code != FailureUnknown || failedCleanup.Load() != 1 {
		t.Fatalf("launch failure mismatch: result=%#v err=%v cleanups=%d", failed, err, failedCleanup.Load())
	}

	var cleanup atomic.Int32
	handle := newFakeHandle()
	running, err := registry.Start(testSpec(handle, &cleanup))
	if err != nil || running.Record.State != StateRunning {
		t.Fatalf("start before close: %#v %v", running, err)
	}
	registry.Close()
	if handle.stopCount.Load() != 1 || cleanup.Load() != 1 {
		t.Fatalf("close ownership mismatch: stops=%d cleanups=%d", handle.stopCount.Load(), cleanup.Load())
	}
	record, err := registry.Status(running.Record.ProcessID)
	if err != nil || record.State != StateStopped {
		t.Fatalf("closed record = %#v err=%v", record, err)
	}
}

func waitTerminal(t *testing.T, registry *Registry, processID string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		record, err := registry.Status(processID)
		if err == nil && terminalState(record.State) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(fmt.Sprintf("process %s did not become terminal", processID))
}

func intPointer(value int) *int { return &value }
