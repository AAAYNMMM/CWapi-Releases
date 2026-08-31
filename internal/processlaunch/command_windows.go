//go:build windows

package processlaunch

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

func configure(command *exec.Cmd) {
	if command == nil {
		return
	}
	attributes := command.SysProcAttr
	if attributes == nil {
		attributes = &syscall.SysProcAttr{}
	}
	attributes.HideWindow = true
	attributes.CreationFlags |= createNoWindow
	command.SysProcAttr = attributes
}
