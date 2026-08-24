package processruntime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/observability"
)

func (r *Registry) trimLocked() {
	for len(r.terminalOrder) > MaxTerminal {
		oldest := r.terminalOrder[0]
		r.terminalOrder = r.terminalOrder[1:]
		delete(r.entries, oldest)
	}
}

func (r *Registry) newIDLocked() (string, error) {
	for attempt := 0; attempt < 8; attempt++ {
		payload := make([]byte, 12)
		if _, err := rand.Read(payload); err != nil {
			return "", fmt.Errorf("PROCESS_ID_CREATE_FAILED: %w", err)
		}
		id := "proc-" + hex.EncodeToString(payload)
		if _, exists := r.entries[id]; !exists {
			return id, nil
		}
	}
	return "", errors.New("PROCESS_ID_COLLISION")
}

func validateSpec(spec Spec) error {
	if spec.Backend != BackendCodex && spec.Backend != BackendSystem {
		return errors.New("PROCESS_BACKEND_INVALID")
	}
	if strings.TrimSpace(spec.Repository) == "" || strings.TrimSpace(spec.ExpectedCommit) == "" || spec.Launch == nil {
		return errors.New("PROCESS_SPEC_INVALID")
	}
	return nil
}

func normalizedFailure(completion Completion) *Failure {
	failure := completion.Failure
	if failure == nil {
		failure = &Failure{Code: FailureProgram, Message: "process exited unsuccessfully"}
	}
	switch failure.Code {
	case FailureProgram, FailurePermission, FailurePolicy, FailureUnknown:
	default:
		failure = &Failure{Code: FailureUnknown, Message: failure.Message}
	}
	message := map[string]string{
		FailureProgram: "process exited unsuccessfully", FailurePermission: "Codex sandbox denied the invocation",
		FailurePolicy: "permanent execution policy denied the invocation", FailureUnknown: "process backend failed",
	}[failure.Code]
	return &Failure{Code: failure.Code, Message: strings.ToValidUTF8(message, "?")}
}

func publicRecord(record Record, tails *Tails) Record {
	record.StdoutTail, record.StderrTail = tails.Snapshot()
	record.LatestStream, record.LatestOutputAt = tails.Latest()
	if record.Error != nil {
		record.Error = &Failure{Code: record.Error.Code, Message: observability.Redact(record.Error.Message)}
	}
	return record
}

func terminalState(state string) bool {
	return state == StateCompleted || state == StateFailed || state == StateStopped
}

func channelClosed(channel <-chan struct{}) bool {
	select {
	case <-channel:
		return true
	default:
		return false
	}
}
