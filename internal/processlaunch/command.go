package processlaunch

import (
	"context"
	"os/exec"
)

// Command creates a CWapi-owned background child process. Callers keep full
// control of stdin, stdout and stderr. Platform-specific configuration only
// changes how the child process is presented by the operating system.
func Command(name string, arg ...string) *exec.Cmd {
	command := exec.Command(name, arg...)
	configure(command)
	return command
}

// CommandContext is the context-aware form of Command.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, arg...)
	configure(command)
	return command
}
