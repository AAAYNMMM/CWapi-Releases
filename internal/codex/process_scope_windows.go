//go:build windows

package codex

import "github.com/AAAYNMMM/CWapi/internal/processscope"

type processScope = processscope.Scope

func attachProcessScope(pid int) (*processScope, error) {
	return processscope.Attach(pid)
}
