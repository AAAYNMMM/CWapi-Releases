package security

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var catastrophicPrograms = []string{"format", "diskpart", "bcdedit", "runas"}

type Invocation struct {
	Executable           string
	Argv                 []string
	CWD                  string
	AccessProfile        string
	TrustedGitExecutable string
	RemoteGitRewrite     bool
	ProtectedExecutables []string
}

// Check owns only profile-independent catastrophic safety, trusted-tool and
// protected CWapi identity checks. SAFE containment and FULL environment
// semantics are enforced by the profile/runtime layers, not by shell parsing.
func Check(invocation Invocation, repositoryRoot, dataRoot string) error {
	if !filepath.IsAbs(invocation.Executable) || !filepath.IsAbs(invocation.CWD) || !filepath.IsAbs(repositoryRoot) || !filepath.IsAbs(dataRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: invocation paths are not canonical")
	}
	if !PathWithin(invocation.CWD, repositoryRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: working directory is outside the owned repository")
	}
	if _, err := ParseProfile(invocation.AccessProfile); err != nil {
		return err
	}
	for _, protected := range invocation.ProtectedExecutables {
		if sameExecutable(invocation.Executable, protected) {
			return errors.New("PERMANENT_POLICY_DENIED: CWapi internal executable is not a Coding tool")
		}
	}
	program := normalizedProgram(invocation.Executable)
	for _, forbidden := range catastrophicPrograms {
		if strings.EqualFold(program, forbidden) {
			return fmt.Errorf("PERMANENT_POLICY_DENIED: %s is not permitted", program)
		}
	}
	if powershellElevation(program, invocation.Argv) {
		return errors.New("PERMANENT_POLICY_DENIED: automatic elevation is not permitted")
	}
	if PathWithin(invocation.Executable, dataRoot) && !PathWithin(invocation.Executable, repositoryRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: internal CWapi files cannot be executed")
	}
	if err := ValidateGit(invocation, filepath.Clean(repositoryRoot), filepath.Clean(dataRoot)); err != nil {
		return err
	}
	for _, argument := range invocation.Argv {
		candidate, ok := topLevelPath(argument, invocation.CWD)
		if ok && protectedPath(candidate, dataRoot) {
			return errors.New("PERMANENT_POLICY_DENIED: protected CWapi path argument is not permitted")
		}
	}
	return nil
}

func powershellElevation(program string, argv []string) bool {
	if program != "powershell" && program != "pwsh" {
		return false
	}
	for index, value := range argv {
		lower := strings.ToLower(strings.TrimSpace(value))
		if lower == "-verb" && index+1 < len(argv) && strings.EqualFold(strings.TrimSpace(argv[index+1]), "runas") {
			return true
		}
		if strings.Contains(lower, "-verb runas") || strings.Contains(lower, "-verb\trunas") {
			return true
		}
	}
	return false
}

func CodexRules() string {
	var builder strings.Builder
	builder.WriteString("# CWapi permanent catastrophic safety rules. Generated from internal/security.\n")
	for _, program := range catastrophicPrograms {
		for _, name := range []string{program, program + ".exe", program + ".com"} {
			fmt.Fprintf(&builder, "prefix_rule(pattern=[%s], decision=\"forbidden\", justification=\"CWapi permanent safety guard.\")\n", quote(name))
		}
	}
	return builder.String()
}

func normalizedProgram(executable string) string {
	name := strings.ToLower(filepath.Base(executable))
	for _, extension := range []string{".exe", ".com", ".cmd", ".bat"} {
		if strings.HasSuffix(name, extension) {
			return strings.TrimSuffix(name, extension)
		}
	}
	return name
}

func sameExecutable(actual, trusted string) bool {
	return filepath.IsAbs(actual) && filepath.IsAbs(trusted) && strings.EqualFold(filepath.Clean(actual), filepath.Clean(trusted))
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}
