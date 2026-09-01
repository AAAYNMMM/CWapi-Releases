package executionpolicy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	AccessSafe = "safe"
	AccessFull = "full"
)

var forbiddenPrograms = []string{
	"format", "diskpart", "bcdedit", "regedit", "taskkill", "stop-process",
	"git-credential-manager", "git-credential-manager-core", "git-credential-wincred",
	"invoke-expression",
}

// These Git operations are destructive, rewrite repository state, or let a
// caller reach credential helpers. They remain forbidden in every profile.
var permanentlyForbiddenGitCommands = []string{
	"checkout", "switch", "restore", "reset", "clean", "merge", "rebase",
	"cherry-pick", "revert", "filter-branch", "pull", "fetch", "credential",
}

// These Git metadata operations remain gated to direct FULL invocations and
// receive additional argument validation. FULL itself selects the upstream
// dangerFullAccess sandbox for every command that passes the permanent policy.
var fullGitCommands = []string{"add", "commit", "push"}

var forbiddenGitBranchOptions = []string{"-d", "--delete", "-m", "--move", "-c", "--copy"}
var forbiddenGitTagOptions = []string{"-d", "--delete", "-f", "--force", "-a", "-s", "-u"}
var allowedGitConfigKeys = []string{"core.longpaths", "user.name", "user.email"}
var allowedGitGlobalFlags = []string{"--help", "--version", "--no-pager", "--no-replace-objects", "--no-optional-locks", "-p"}

type Invocation struct {
	Executable           string
	Argv                 []string
	CWD                  string
	AccessProfile        string
	TrustedGitExecutable string
}

// Check enforces CWapi's in-process command boundary against the resolved
// target executable and argv. AccessProfile defaults to SAFE so callers cannot
// accidentally obtain Git metadata access by omitting it.
func Check(invocation Invocation, repositoryRoot, dataRoot string) error {
	if !filepath.IsAbs(invocation.Executable) || !filepath.IsAbs(invocation.CWD) || !filepath.IsAbs(repositoryRoot) || !filepath.IsAbs(dataRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: invocation paths are not canonical")
	}
	if !pathWithin(invocation.CWD, repositoryRoot) {
		return errors.New("PERMANENT_POLICY_DENIED: working directory is outside the owned repository")
	}
	profile := strings.ToLower(strings.TrimSpace(invocation.AccessProfile))
	if profile == "" {
		profile = AccessSafe
	}
	if profile != AccessSafe && profile != AccessFull {
		return errors.New("PERMANENT_POLICY_DENIED: access profile is invalid")
	}
	if err := checkCommand(invocation.Executable, invocation.Argv, profile, false); err != nil {
		return err
	}
	if normalizedProgram(invocation.Executable) == "git" {
		command, invalidConfig := gitCommand(invocation.Argv)
		if !invalidConfig && containsFold(fullGitCommands, command) {
			if !sameExecutable(invocation.Executable, invocation.TrustedGitExecutable) {
				return errors.New("PERMANENT_POLICY_DENIED: Git metadata operation requires the trusted Git executable")
			}
			if err := validateElevatedGitArguments(command, gitSubcommandArgs(invocation.Argv, command), invocation.CWD, repositoryRoot); err != nil {
				return err
			}
		}
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

// RequiresFullAccess reports whether a validated invocation selected FULL.
// Permanent policy checks still run first, so FULL changes the upstream sandbox
// rather than bypassing CWapi's destructive-command and protected-path rules.
func RequiresFullAccess(invocation Invocation) bool {
	return strings.ToLower(strings.TrimSpace(invocation.AccessProfile)) == AccessFull
}

func sameExecutable(actual, trusted string) bool {
	return filepath.IsAbs(actual) && filepath.IsAbs(trusted) && strings.EqualFold(filepath.Clean(actual), filepath.Clean(trusted))
}

// AllowsHostGitCredentials limits temporary host Git credential discovery to
// a direct FULL push. SAFE never receives it.
func AllowsHostGitCredentials(invocation Invocation) bool {
	if !RequiresFullAccess(invocation) || normalizedProgram(invocation.Executable) != "git" ||
		!sameExecutable(invocation.Executable, invocation.TrustedGitExecutable) {
		return false
	}
	command, invalidConfig := gitCommand(invocation.Argv)
	return !invalidConfig && command == "push"
}

func checkCommand(executable string, argv []string, profile string, nested bool) error {
	program := normalizedProgram(executable)
	if containsFold(forbiddenPrograms, program) {
		return fmt.Errorf("PERMANENT_POLICY_DENIED: %s is not permitted", program)
	}
	if program == "git" {
		command, invalidConfig := gitCommand(argv)
		if invalidConfig {
			return errors.New("PERMANENT_POLICY_DENIED: Git configuration override is not permitted")
		}
		if containsFold(permanentlyForbiddenGitCommands, command) || forbiddenGitRefMutation(command, argv) {
			return errors.New("PERMANENT_POLICY_DENIED: destructive Git operation is not permitted")
		}
		if containsFold(fullGitCommands, command) && (profile != AccessFull || nested) {
			return errors.New("PERMANENT_POLICY_DENIED: Git metadata operation requires a direct FULL invocation")
		}
	}
	for _, command := range nestedShellCommands(program, argv) {
		if len(command) == 0 {
			continue
		}
		if err := checkCommand(command[0], command[1:], profile, true); err != nil {
			return err
		}
	}
	return nil
}

// CodexRules remains defense in depth. The Go Check above is authoritative;
// these rules intentionally contain only profile-independent denials because
// FULL Git metadata operations are selected per command by Host.Exec.
func CodexRules() string {
	var builder strings.Builder
	builder.WriteString("# CWapi permanent base safety rules. Generated from the Go execution policy.\n")
	for _, program := range forbiddenPrograms {
		for _, name := range []string{program, program + ".exe", program + ".com"} {
			fmt.Fprintf(&builder, "prefix_rule(pattern=[%s], decision=\"forbidden\", justification=\"CWapi permanent execution policy.\")\n", quote(name))
		}
	}
	for _, command := range permanentlyForbiddenGitCommands {
		fmt.Fprintf(&builder, "prefix_rule(pattern=[\"git\", %s], decision=\"forbidden\", justification=\"CWapi protects repository state and credentials.\")\n", quote(command))
	}
	fmt.Fprintf(&builder, "prefix_rule(pattern=[\"git\", \"branch\", [%s]], decision=\"forbidden\", justification=\"CWapi protects repository branches.\")\n", quoteList(forbiddenGitBranchOptions))
	fmt.Fprintf(&builder, "prefix_rule(pattern=[\"git\", \"tag\", [%s]], decision=\"forbidden\", justification=\"CWapi protects repository tags.\")\n", quoteList(forbiddenGitTagOptions))
	return builder.String()
}

func ProtectedFilesystemRoots() []string {
	return protectedRoots("")
}

func gitCommand(argv []string) (string, bool) {
	for index := 0; index < len(argv); index++ {
		value := strings.TrimSpace(argv[index])
		lower := strings.ToLower(value)
		switch {
		case value == "":
			continue
		case value == "-c":
			if index+1 >= len(argv) || !allowedGitConfig(argv[index+1]) {
				return "", true
			}
			index++
			continue
		case strings.HasPrefix(value, "-c") && len(value) > 2:
			if !allowedGitConfig(value[2:]) {
				return "", true
			}
			continue
		case lower == "--config-env" || strings.HasPrefix(lower, "--config-env="):
			return "", true
		case value == "-C" || lower == "--git-dir" || lower == "--work-tree" || lower == "--namespace" || lower == "--exec-path":
			return "", true
		case strings.HasPrefix(value, "-C") || strings.HasPrefix(lower, "--git-dir=") || strings.HasPrefix(lower, "--work-tree=") || strings.HasPrefix(lower, "--namespace=") || strings.HasPrefix(lower, "--exec-path="):
			return "", true
		case containsFold(allowedGitGlobalFlags, lower):
			continue
		case strings.HasPrefix(lower, "-"):
			return "", true
		default:
			return lower, false
		}
	}
	return "", false
}

func allowedGitConfig(value string) bool {
	key, _, ok := strings.Cut(value, "=")
	if !ok {
		return false
	}
	return containsFold(allowedGitConfigKeys, strings.TrimSpace(key))
}

func forbiddenGitRefMutation(command string, argv []string) bool {
	options := make([]string, 0, len(argv))
	for _, value := range argv {
		options = append(options, strings.ToLower(strings.TrimSpace(value)))
	}
	if command == "branch" {
		return containsAnyExact(options, forbiddenGitBranchOptions)
	}
	return command == "tag" && containsAnyExact(options, forbiddenGitTagOptions)
}

func gitSubcommandArgs(argv []string, command string) []string {
	for index, value := range argv {
		if strings.EqualFold(strings.TrimSpace(value), command) {
			return argv[index+1:]
		}
	}
	return nil
}

func validateElevatedGitArguments(command string, argv []string, cwd, repositoryRoot string) error {
	switch command {
	case "add":
		pathsOnly := false
		for _, value := range argv {
			if pathsOnly {
				if !gitPathWithinRepository(value, cwd, repositoryRoot) {
					return errors.New("PERMANENT_POLICY_DENIED: elevated Git path escapes the repository")
				}
				continue
			}
			lower := strings.ToLower(value)
			switch {
			case value == "--":
				pathsOnly = true
			case value == "-A" || value == "-u" || value == "-n" || value == "-v" || value == "-f" || value == "-N" ||
				lower == "--all" || lower == "--update" || lower == "--dry-run" || lower == "--verbose" || lower == "--force" || lower == "--intent-to-add":
				continue
			default:
				if strings.HasPrefix(value, "-") {
					return errors.New("PERMANENT_POLICY_DENIED: unsupported elevated git add option")
				}
				if !gitPathWithinRepository(value, cwd, repositoryRoot) {
					return errors.New("PERMANENT_POLICY_DENIED: elevated Git path escapes the repository")
				}
			}
		}
	case "commit":
		pathsOnly := false
		for index := 0; index < len(argv); index++ {
			value := argv[index]
			if pathsOnly {
				if !gitPathWithinRepository(value, cwd, repositoryRoot) {
					return errors.New("PERMANENT_POLICY_DENIED: elevated Git path escapes the repository")
				}
				continue
			}
			lower := strings.ToLower(value)
			switch {
			case lower == "--":
				pathsOnly = true
			case lower == "-m" || lower == "--message" || lower == "--author" || lower == "--date":
				if index+1 >= len(argv) {
					return errors.New("PERMANENT_POLICY_DENIED: elevated git commit option is incomplete")
				}
				index++
			case strings.HasPrefix(lower, "-m") && len(value) > 2,
				strings.HasPrefix(lower, "--message="), strings.HasPrefix(lower, "--author="), strings.HasPrefix(lower, "--date="):
				continue
			case lower == "-a" || lower == "--all" || lower == "--allow-empty" || lower == "--allow-empty-message" || lower == "--no-verify" || lower == "--quiet" || lower == "--verbose":
				continue
			default:
				if strings.HasPrefix(value, "-") {
					return errors.New("PERMANENT_POLICY_DENIED: unsupported elevated git commit option")
				}
				if !gitPathWithinRepository(value, cwd, repositoryRoot) {
					return errors.New("PERMANENT_POLICY_DENIED: elevated Git path escapes the repository")
				}
			}
		}
	case "push":
		for _, value := range argv {
			lower := strings.ToLower(strings.TrimSpace(value))
			switch lower {
			case "-u", "--set-upstream", "--porcelain", "-q", "--quiet", "-v", "--verbose", "--dry-run", "--atomic", "--follow-tags":
				continue
			}
			if strings.HasPrefix(value, "-") || strings.HasPrefix(value, "+") || strings.HasPrefix(value, ":") {
				return errors.New("PERMANENT_POLICY_DENIED: unsafe or unsupported elevated git push argument")
			}
			native := filepath.FromSlash(value)
			if filepath.IsAbs(native) || filepath.VolumeName(native) != "" || strings.HasPrefix(value, ".") || strings.HasPrefix(lower, "file:") || strings.HasPrefix(lower, "ext::") {
				return errors.New("PERMANENT_POLICY_DENIED: elevated git push target is not permitted")
			}
		}
	}
	return nil
}

func gitPathWithinRepository(value, cwd, repositoryRoot string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\x00") {
		return false
	}
	if strings.HasPrefix(value, ":(") {
		return true
	}
	candidate := filepath.FromSlash(value)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(cwd, candidate)
	}
	return pathWithin(candidate, repositoryRoot)
}

func nestedShellCommands(program string, argv []string) [][]string {
	var payload []string
	switch program {
	case "cmd":
		for index, value := range argv {
			if strings.EqualFold(value, "/c") || strings.EqualFold(value, "/k") {
				payload = argv[index+1:]
				break
			}
		}
	case "powershell", "pwsh":
		for index, value := range argv {
			lower := strings.ToLower(strings.TrimSpace(value))
			if lower == "-encodedcommand" || lower == "-enc" || lower == "-ec" || lower == "-encodedarguments" {
				return [][]string{{"invoke-expression"}}
			}
			if lower == "-command" || lower == "-c" || lower == "/command" {
				payload = argv[index+1:]
				break
			}
		}
	}
	if len(payload) == 0 {
		return nil
	}
	script := strings.Join(payload, " ")
	if program == "cmd" && strings.ContainsAny(script, "%!") {
		return [][]string{{"invoke-expression"}}
	}
	if (program == "powershell" || program == "pwsh") && dynamicPowerShellInvocation(script) {
		return [][]string{{"invoke-expression"}}
	}
	if len(payload) > 1 && !containsShellSyntax(payload[0]) {
		return [][]string{append([]string(nil), payload...)}
	}
	return splitShellCommands(script)
}

func dynamicPowerShellInvocation(script string) bool {
	compact := strings.NewReplacer(" ", "", "\t", "", "\r", "", "\n", "").Replace(script)
	return strings.Contains(compact, "&(") || strings.Contains(compact, "&$")
}

func containsShellSyntax(value string) bool {
	return strings.ContainsAny(value, " \t;&|(){}\r\n\"'")
}

func splitShellCommands(script string) [][]string {
	segments := splitShellSegments(script)
	commands := make([][]string, 0, len(segments))
	for _, segment := range segments {
		words := shellWords(segment)
		for len(words) > 0 && (words[0] == "&" || strings.EqualFold(words[0], "call")) {
			words = words[1:]
		}
		if len(words) == 0 {
			continue
		}
		if strings.HasPrefix(words[0], "$(&") || strings.Contains(words[0], "%") {
			commands = append(commands, []string{"invoke-expression"})
			continue
		}
		if strings.EqualFold(words[0], "start") || strings.EqualFold(words[0], "start-process") || strings.EqualFold(words[0], "invoke-expression") || strings.EqualFold(words[0], "iex") {
			commands = append(commands, []string{"invoke-expression"})
			continue
		}
		commands = append(commands, words)
	}
	return commands
}

func splitShellSegments(value string) []string {
	var segments []string
	var builder strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if segment := strings.TrimSpace(builder.String()); segment != "" {
			segments = append(segments, segment)
		}
		builder.Reset()
	}
	for _, char := range value {
		if escaped {
			builder.WriteRune(char)
			escaped = false
			continue
		}
		if char == '^' || char == '`' {
			escaped = true
			continue
		}
		if quote != 0 {
			builder.WriteRune(char)
			if char == quote {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			builder.WriteRune(char)
			continue
		}
		if strings.ContainsRune(";&|\r\n", char) {
			flush()
			continue
		}
		builder.WriteRune(char)
	}
	flush()
	return segments
}

func shellWords(value string) []string {
	var words []string
	var builder strings.Builder
	var quote rune
	escaped := false
	flush := func() {
		if builder.Len() > 0 {
			words = append(words, builder.String())
			builder.Reset()
		}
	}
	for _, char := range value {
		if escaped {
			builder.WriteRune(char)
			escaped = false
			continue
		}
		if char == '^' || char == '`' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			} else {
				builder.WriteRune(char)
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if char == '&' && builder.Len() == 0 {
			flush()
			words = append(words, "&")
			continue
		}
		if char == ' ' || char == '\t' || char == '(' || char == ')' || char == '{' || char == '}' {
			flush()
			continue
		}
		builder.WriteRune(char)
	}
	flush()
	return words
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
			if strings.EqualFold(value, candidate) {
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
