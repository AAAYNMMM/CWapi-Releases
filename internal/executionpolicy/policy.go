// Package executionpolicy is the compatibility facade for callers that still
// use the 2.0 policy package name. Security ownership lives in internal/security.
package executionpolicy

import "github.com/AAAYNMMM/CWapi/internal/security"

const (
	AccessSafe = string(security.ProfileSafe)
	AccessFull = string(security.ProfileFull)
)

type Invocation = security.Invocation

func Check(invocation Invocation, repositoryRoot, dataRoot string) error {
	return security.Check(invocation, repositoryRoot, dataRoot)
}

func RequiresFullAccess(invocation Invocation) bool {
	return security.IsFull(invocation.AccessProfile)
}

// AllowsHostGitCredentials is retained for source compatibility. In 2.0.4,
// every FULL command receives the sanitized host development environment.
func AllowsHostGitCredentials(invocation Invocation) bool {
	return RequiresFullAccess(invocation)
}

func CodexRules() string { return security.CodexRules() }

func ProtectedFilesystemRoots() []string { return nil }
