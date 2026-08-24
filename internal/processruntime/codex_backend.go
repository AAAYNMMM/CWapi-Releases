package processruntime

import (
	"context"

	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/invocation"
)

type codexHandle struct {
	inner *codex.CommandHandle
	done  chan Completion
}

func (r *Runtime) startCodex(ctx context.Context, processID, repositoryRoot string, final invocation.Final, window *denialWindow) (Handle, error) {
	inner, err := r.codex.StartCommand(ctx, codex.CommandSpec{
		ProcessID: processID, Executable: final.Executable, Argv: final.Argv,
		CWD: final.CWD, WritableRoot: repositoryRoot, Environment: final.Environment,
	})
	if err != nil {
		return nil, err
	}
	handle := &codexHandle{inner: inner, done: make(chan Completion, 1)}
	go func() {
		result, ok := <-inner.Done()
		completion := Completion{}
		if !ok {
			completion.Failure = &Failure{Code: FailureUnknown, Message: "Codex command backend closed"}
		} else {
			completion.Stdout, completion.Stderr = result.Stdout, result.Stderr
			if result.Err != nil {
				completion.Failure = &Failure{Code: FailureUnknown, Message: "Codex command backend failed"}
			} else {
				exitCode := result.ExitCode
				completion.ExitCode = &exitCode
				if exitCode == 5 {
					discard := window.denied()
					completion.Failure = &Failure{Code: FailurePermission, Message: "Codex sandbox denied the invocation"}
					completion.DiscardRecord = discard
				}
			}
		}
		handle.done <- completion
		close(handle.done)
	}()
	return handle, nil
}

func (h *codexHandle) Done() <-chan Completion { return h.done }
func (h *codexHandle) Stop() error             { return h.inner.Stop() }
