package processruntime

import (
	"context"

	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/invocation"
)

const SandboxBlockedExitCode = 197

type codexHandle struct {
	inner *codex.CommandHandle
	done  chan Completion
}

func (r *Runtime) startCodex(ctx context.Context, processID, repositoryRoot string, final invocation.Final, window *denialWindow, lateToken func() (string, error)) (Handle, error) {
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
				if sandboxBlockedExitCode(exitCode) {
					held, immediate := window.denied()
					completion.Failure = &Failure{Code: FailurePermission, Message: "Codex sandbox blocked the invocation"}
					completion.DiscardRecord = held && immediate
					if held && !immediate && lateToken != nil {
						if token, tokenErr := lateToken(); tokenErr == nil {
							completion.SystemToken = token
						}
					}
				}
			}
		}
		handle.done <- completion
		close(handle.done)
	}()
	return handle, nil
}

func sandboxBlockedExitCode(exitCode int) bool {
	return exitCode == 5 || exitCode == SandboxBlockedExitCode
}

func (h *codexHandle) Done() <-chan Completion { return h.done }
func (h *codexHandle) Stop() error             { return h.inner.Stop() }
