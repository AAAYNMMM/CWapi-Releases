//go:build !windows

package processlaunch

import "os/exec"

func configure(*exec.Cmd) {}
