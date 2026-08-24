package codex

import "github.com/AAAYNMMM/CWapi/internal/childenv"

func appServerEnvironment() []string {
	return childenv.Canonical()
}
