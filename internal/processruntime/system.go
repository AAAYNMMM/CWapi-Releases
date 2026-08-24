package processruntime

import (
	"context"
	"errors"
	"os/exec"
	"sync"

	"github.com/AAAYNMMM/CWapi/internal/invocation"
	"github.com/AAAYNMMM/CWapi/internal/processscope"
)

type systemHandle struct {
	done     chan Completion
	finished chan struct{}
	cmd      *exec.Cmd
	scope    *processscope.Scope
	stopOnce sync.Once
	stopErr  error
}

func startSystem(ctx context.Context, final invocation.Final, tails *Tails) (Handle, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	command := exec.Command(final.Executable, final.Argv...)
	command.Dir = final.CWD
	command.Env = append([]string(nil), final.Environment...)
	command.Stdout = tails.Stdout
	command.Stderr = tails.Stderr
	if err := command.Start(); err != nil {
		return nil, err
	}
	scope, err := processscope.Attach(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_, _ = command.Process.Wait()
		return nil, err
	}
	handle := &systemHandle{
		done: make(chan Completion, 1), finished: make(chan struct{}),
		cmd: command, scope: scope,
	}
	go handle.wait()
	go func() {
		select {
		case <-ctx.Done():
			_ = handle.Stop()
		case <-handle.finished:
		}
	}()
	return handle, nil
}

func (h *systemHandle) Done() <-chan Completion { return h.done }

func (h *systemHandle) Stop() error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		h.stopErr = h.scope.Close()
		if h.cmd != nil && h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
	})
	return h.stopErr
}

func (h *systemHandle) wait() {
	err := h.cmd.Wait()
	_ = h.scope.Close()
	completion := Completion{}
	if h.cmd.ProcessState != nil {
		exitCode := h.cmd.ProcessState.ExitCode()
		completion.ExitCode = &exitCode
	}
	if err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			completion.Failure = &Failure{Code: FailureUnknown, Message: err.Error()}
		}
	}
	h.done <- completion
	close(h.done)
	close(h.finished)
}
