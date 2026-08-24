package executionpolicy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var forbiddenPrograms = []string{"format", "diskpart", "bcdedit", "regedit", "taskkill", "stop-process"}

var forbiddenGitCommands = []string{
	"add", "commit", "checkout", "switch", "restore", "reset", "clean",
	"merge", "rebase", "cherry-pick", "revert", "filter-branch", "push", "pull", "fetch",
}

var forbiddenGitBranchOptions = []string{"-d", "-D", "--delete", "-m", "-M", "--move", "-c", "-C", "--copy"}
var forbiddenGitTagOptions = []string{"-d", "--delete", "-f", "--force", "-a", "-s", "-u"}

type Invocation struct {
	Executable string
	Argv       []string
	CWD        string
}

func Check(invocation Invocation, repositoryRoot, dataRoot string) error {
	if !filepath.IsAbs(invocation.Executable) || !filepath.IsAbs(invocation.CWD) || !filepath.IsAbs(repositoryRoot) || !filepath.IsAbs(dataRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: invocation paths are not canonical")
	}
	if !pathWithin(invocation.CWD, repositoryRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: working directory is outside the owned repository")
	}
	program := normalizedProgram(invocation.Executable)
	if containsFold(forbiddenPrograms, program) {
		return fmt.Errorf("PERMANENT_POLICY_DENIED: %s is not permitted", program)
	}
	if program == "git" && gitMutation(invocation.Argv) {
		return errors.New("PERMANENT_POLICY_DENIED: Git mutation is not permitted")
	}
	if pathWithin(invocation.Executable, dataRoot) && !pathWithin(invocation.Executable, repositoryRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: internal CWapi files cannot be executed")
	}
	protected := protectedRoots(dataRoot)
	for _, argument := range invocation.Argv {
		candidate, ok := topLevelPath(argument, invocation.CWD)
		if !ok || pathWithin(candidate, repositoryRoot) {
			continue
		}
		for _, root := range protected {
			if pathWithin(candidate, root) {
				return errors.New("PERMANENT_POLICY_DENIED: protected path argument is not permitted")
			}
		}
	}
	return nil
}

// CodexRules renders the stock Codex base rules from the same command lists
// used by Check. The System path intentionally does not inspect nested shell,
// script, descendant, or batch-wrapper semantics.
func CodexRules() string {
	var builder strings.Builder
	builder.WriteString("# CWapi permanent base safety rules. Generated from the Go execution policy.\n")
	for _, program := range forbiddenPrograms {
		for _, name := range []string{program, program + ".exe", program + ".com"} {
			fmt.Fprintf(&builder, "prefix_rule(pattern=[%s], decision=\"forbidden\", justification=\"CWapi permanent execution policy.\")\n", quote(name))
		}
	}
	for _, command := range forbiddenGitCommands {
		fmt.Fprintf(&builder, "prefix_rule(pattern=[\"git\", %s], decision=\"forbidden\", justification=\"CWapi owns repository state and history.\")\n", quote(command))
	}
	fmt.Fprintf(&builder, "prefix_rule(pattern=[\"git\", \"branch\", [%s]], decision=\"forbidden\", justification=\"CWapi owns repository branches.\")\n", quoteList(forbiddenGitBranchOptions))
	fmt.Fprintf(&builder, "prefix_rule(pattern=[\"git\", \"tag\", [%s]], decision=\"forbidden\", justification=\"CWapi owns repository tags.\")\n", quoteList(forbiddenGitTagOptions))
	return builder.String()
}

func ProtectedFilesystemRoots() []string {
	return protectedRoots("")
}

func gitMutation(argv []string) bool {
	if len(argv) == 0 {
		return false
	}
	command := strings.ToLower(argv[0])
	if containsFold(forbiddenGitCommands, command) {
		return true
	}
	if command == "branch" {
		return containsAnyExact(argv[1:], forbiddenGitBranchOptions)
	}
	return command == "tag" && containsAnyExact(argv[1:], forbiddenGitTagOptions)
}

func protectedRoots(dataRoot string) []string {
	userProfile := strings.TrimSpace(os.Getenv("USERPROFILE"))
	values := []string{os.Getenv("SystemRoot"), os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), dataRoot}
	if userProfile != "" {
		values = append(values, filepath.Join(userProfile, "Downloads"))
	}
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || !filepath.IsAbs(value) {
			continue
		}
		cleaned := filepath.Clean(value)
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, cleaned)
	}
	sort.Slice(result, func(i, j int) bool { return strings.ToLower(result[i]) < strings.ToLower(result[j]) })
	return result
}

func topLevelPath(argument, cwd string) (string, bool) {
	if argument == "" || strings.ContainsAny(argument, "\r\n\x00") {
		return "", false
	}
	candidate := filepath.FromSlash(argument)
	if filepath.IsAbs(candidate) {
		return filepath.Clean(candidate), true
	}
	if strings.HasPrefix(candidate, "."+string(filepath.Separator)) || strings.HasPrefix(candidate, ".."+string(filepath.Separator)) {
		return filepath.Clean(filepath.Join(cwd, candidate)), true
	}
	return "", false
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

func containsFold(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}

func containsAnyExact(values, candidates []string) bool {
	for _, value := range values {
		for _, candidate := range candidates {
			if value == candidate {
				return true
			}
		}
	}
	return false
}

func pathWithin(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	if strings.EqualFold(path, root) {
		return true
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func quote(value string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
}

func quoteList(values []string) string {
	quoted := make([]string, len(values))
	for index, value := range values {
		quoted[index] = quote(value)
	}
	return strings.Join(quoted, ", ")
}
