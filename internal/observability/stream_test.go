package observability

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/state"
)

func TestLiveStreamsAreIndependentBoundedAndNonBlocking(t *testing.T) {
	ctx := context.Background()
	store, err := state.Open(filepath.Join(t.TempDir(), "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	hub, err := New(ctx, store, 10, 20)
	if err != nil {
		t.Fatal(err)
	}

	execution, cancelExecution := hub.SubscribeExecution(2)
	defer cancelExecution()
	runtimeLogs, cancelRuntime := hub.SubscribeRuntime(2)
	defer cancelRuntime()

	for index := 1; index <= 4; index++ {
		if _, err := hub.EmitExecution(ctx, ExecutionInput{Timestamp: int64(index), Kind: "step", Status: "running", Message: "execution"}); err != nil {
			t.Fatal(err)
		}
		if _, err := hub.LogRuntime(ctx, RuntimeInput{Timestamp: int64(index), Level: "info", Component: "runner", Message: "runtime"}); err != nil {
			t.Fatal(err)
		}
	}

	firstExecution := <-execution
	secondExecution := <-execution
	if firstExecution.Timestamp != 3 || secondExecution.Timestamp != 4 {
		t.Fatalf("execution stream retained wrong records: %#v %#v", firstExecution, secondExecution)
	}
	firstRuntime := <-runtimeLogs
	secondRuntime := <-runtimeLogs
	if firstRuntime.Timestamp != 3 || secondRuntime.Timestamp != 4 {
		t.Fatalf("runtime stream retained wrong records: %#v %#v", firstRuntime, secondRuntime)
	}
	if firstExecution.Message == firstRuntime.Message {
		t.Fatal("independent streams collapsed into one payload surface")
	}
}
