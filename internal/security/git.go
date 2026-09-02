package security

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
	"github.com/AAAYNMMM/CWapi/internal/processlaunch"
)

const maxSafetyRefs = 32

type GitInvocation struct {
	Command      string
	Args         []string
	EffectiveCWD string
	PathOptions  []string
}

func ParseGitInvocation(argv []string, cwd string) (GitInvocation, error) {
	result := GitInvocation{EffectiveCWD: filepath.Clean(cwd)}
	for index := 0; index < len(argv); index++ {
		value := strings.TrimSpace(argv[index])
		lower := strings.ToLower(value)
		if value == "" {
			continue
		}
		if value == "-C" {
			if index+1 >= len(argv) {
				return GitInvocation{}, errors.New("PERMANENT_POLICY_DENIED: Git -C is incomplete")
			}
			index++
			resolved, err := CanonicalPath(argv[index], result.EffectiveCWD)
			if err != nil {
				return GitInvocation{}, errors.New("PERMANENT_POLICY_DENIED: Git -C target is invalid")
			}
			result.EffectiveCWD = resolved
			result.PathOptions = append(result.PathOptions, resolved)
			continue
		}
		if strings.HasPrefix(value, "-C") && len(value) > 2 {
			resolved, err := CanonicalPath(value[2:], result.EffectiveCWD)
			if err != nil {
				return GitInvocation{}, errors.New("PERMANENT_POLICY_DENIED: Git -C target is invalid")
			}
			result.EffectiveCWD = resolved
			result.PathOptions = append(result.PathOptions, resolved)
			continue
		}
		if value == "-c" || lower == "--config-env" || lower == "--git-dir" || lower == "--work-tree" || lower == "--namespace" || lower == "--exec-path" {
			if index+1 >= len(argv) {
				return GitInvocation{}, errors.New("PERMANENT_POLICY_DENIED: Git global option is incomplete")
			}
			index++
			if lower == "--git-dir" || lower == "--work-tree" || lower == "--exec-path" {
				resolved, err := CanonicalPath(argv[index], result.EffectiveCWD)
				if err != nil {
					return GitInvocation{}, errors.New("PERMANENT_POLICY_DENIED: Git path option is invalid")
				}
				result.PathOptions = append(result.PathOptions, resolved)
			}
			continue
		}
		if strings.HasPrefix(lower, "--git-dir=") || strings.HasPrefix(lower, "--work-tree=") || strings.HasPrefix(lower, "--exec-path=") {
			pathValue := value[strings.Index(value, "=")+1:]
			resolved, err := CanonicalPath(pathValue, result.EffectiveCWD)
			if err != nil {
				return GitInvocation{}, errors.New("PERMANENT_POLICY_DENIED: Git path option is invalid")
			}
			result.PathOptions = append(result.PathOptions, resolved)
			continue
		}
		if strings.HasPrefix(value, "-") {
			continue
		}
		result.Command = strings.ToLower(value)
		result.Args = append([]string(nil), argv[index+1:]...)
		return result, nil
	}
	return result, nil
}

func ValidateGit(invocation Invocation, repositoryRoot, dataRoot string) error {
	if normalizedProgram(invocation.Executable) != "git" {
		return nil
	}
	if !sameExecutable(invocation.Executable, invocation.TrustedGitExecutable) {
		return errors.New("PERMANENT_POLICY_DENIED: Git requires the trusted executable")
	}
	parsed, err := ParseGitInvocation(invocation.Argv, invocation.CWD)
	if err != nil {
		return err
	}
	profile, _ := ParseProfile(invocation.AccessProfile)
	for _, candidate := range append([]string{parsed.EffectiveCWD}, parsed.PathOptions...) {
		if profile == ProfileSafe && !PathWithin(candidate, repositoryRoot) {
			return errors.New("PERMANENT_POLICY_DENIED: SAFE Git path escapes the workspace")
		}
		if protectedPath(candidate, dataRoot) {
			return errors.New("PERMANENT_POLICY_DENIED: Git path targets protected CWapi state")
		}
	}
	if parsed.Command == "push" {
		if err := validatePushArguments(parsed.Args, invocation.RemoteGitRewrite); err != nil {
			return err
		}
	}
	return nil
}

func validatePushArguments(argv []string, allowRewrite bool) error {
	for _, raw := range argv {
		value := strings.TrimSpace(raw)
		lower := strings.ToLower(value)
		if lower == "--receive-pack" || lower == "--exec" || strings.HasPrefix(lower, "--receive-pack=") || strings.HasPrefix(lower, "--exec=") {
			return errors.New("PERMANENT_POLICY_DENIED: custom Git receive-pack is not permitted")
		}
		if lower == "--mirror" || strings.Contains(lower, "refs/cwapi/") {
			return errors.New("PERMANENT_POLICY_DENIED: Git safety refs cannot be pushed")
		}
		if dangerousGitTransport(value) {
			return errors.New("PERMANENT_POLICY_DENIED: dangerous Git push transport is not permitted")
		}
		if !allowRewrite && (lower == "-f" || lower == "--force" || lower == "--delete" || lower == "-d" || lower == "--prune" || strings.HasPrefix(lower, "--force-with-lease") || strings.HasPrefix(lower, "--force-if-includes") || strings.HasPrefix(value, "+") || remoteDeleteRefspec(value)) {
			return errors.New("REMOTE_GIT_REWRITE_DISABLED: remote Git rewrite is disabled")
		}
	}
	return nil
}

func dangerousGitTransport(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	if strings.HasPrefix(lower, "file:") || strings.HasPrefix(lower, "ext::") {
		return true
	}
	native := filepath.FromSlash(value)
	return filepath.IsAbs(native) || strings.HasPrefix(value, "."+string(filepath.Separator)) || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../")
}

func remoteDeleteRefspec(value string) bool {
	if strings.HasPrefix(value, ":") {
		return true
	}
	left, _, ok := strings.Cut(value, ":")
	return ok && strings.TrimSpace(left) == ""
}

func NeedsGitRecovery(parsed GitInvocation) bool {
	switch parsed.Command {
	case "checkout", "switch", "restore", "reset", "clean", "merge", "rebase", "cherry-pick", "revert":
		if parsed.Command == "clean" {
			for _, value := range parsed.Args {
				lower := strings.ToLower(strings.TrimSpace(value))
				if lower == "-n" || lower == "--dry-run" {
					return false
				}
			}
		}
		return true
	case "branch":
		for _, value := range parsed.Args {
			lower := strings.ToLower(strings.TrimSpace(value))
			if lower == "-d" || lower == "--delete" || value == "-M" {
				return true
			}
		}
	}
	return false
}

type GitSafetyManager struct {
	git      string
	dataRoot string
}

func NewGitSafetyManager(gitExecutable, dataRoot string) *GitSafetyManager {
	return &GitSafetyManager{git: filepath.Clean(gitExecutable), dataRoot: filepath.Clean(dataRoot)}
}

func (m *GitSafetyManager) Before(ctx context.Context, invocation Invocation, repositoryRoot string) error {
	if m == nil || normalizedProgram(invocation.Executable) != "git" || !sameExecutable(invocation.Executable, m.git) {
		return nil
	}
	parsed, err := ParseGitInvocation(invocation.Argv, invocation.CWD)
	if err != nil {
		return err
	}
	if parsed.Command == "push" {
		if err := m.validateConfiguredPushRemote(ctx, parsed); err != nil {
			return err
		}
	}
	if !NeedsGitRecovery(parsed) {
		return nil
	}
	return m.createRecovery(ctx, parsed.EffectiveCWD, repositoryRoot)
}

func (m *GitSafetyManager) validateConfiguredPushRemote(ctx context.Context, parsed GitInvocation) error {
	remote := pushRemoteArgument(parsed.Args)
	if remote == "" {
		remote = "origin"
	}
	if strings.ContainsAny(remote, "/\\:") || strings.HasPrefix(remote, ".") {
		if dangerousGitTransport(remote) {
			return errors.New("PERMANENT_POLICY_DENIED: dangerous Git push transport is not permitted")
		}
		return nil
	}
	output, _, err := m.runGit(ctx, parsed.EffectiveCWD, "remote", "get-url", "--push", remote)
	if err != nil {
		return nil
	}
	if dangerousGitTransport(strings.TrimSpace(output)) {
		return errors.New("PERMANENT_POLICY_DENIED: dangerous configured Git push transport is not permitted")
	}
	return nil
}

func pushRemoteArgument(argv []string) string {
	for index := 0; index < len(argv); index++ {
		value := strings.TrimSpace(argv[index])
		lower := strings.ToLower(value)
		if value == "--" {
			if index+1 < len(argv) {
				return argv[index+1]
			}
			return ""
		}
		if lower == "--repo" {
			if index+1 < len(argv) {
				return argv[index+1]
			}
			return ""
		}
		if strings.HasPrefix(lower, "--repo=") {
			return value[strings.Index(value, "=")+1:]
		}
		if strings.HasPrefix(value, "-") {
			continue
		}
		return value
	}
	return ""
}

type recoveryMetadata struct {
	Schema    string `json:"schema"`
	Ref       string `json:"ref"`
	Commit    string `json:"commit"`
	Head      string `json:"head"`
	Branch    string `json:"branch,omitempty"`
	Dirty     bool   `json:"dirty"`
	CreatedAt string `json:"created_at"`
}

func (m *GitSafetyManager) createRecovery(ctx context.Context, gitCWD, repositoryRoot string) error {
	head, code, err := m.runGit(ctx, gitCWD, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil || code != 0 || strings.TrimSpace(head) == "" {
		return nil
	}
	head = strings.TrimSpace(head)
	branch, _, _ := m.runGit(ctx, gitCWD, "symbolic-ref", "--quiet", "--short", "HEAD")
	status, _, statusErr := m.runGit(ctx, gitCWD, "status", "--porcelain=v1", "--untracked-files=all")
	if statusErr != nil {
		return fmt.Errorf("GIT_SAFETY_SNAPSHOT_FAILED: %w", statusErr)
	}
	dirty := strings.TrimSpace(status) != ""
	commit := head
	if dirty {
		if stash, stashCode, stashErr := m.runGit(ctx, gitCWD, "stash", "create", "CWapi safety snapshot"); stashErr == nil && stashCode == 0 && strings.TrimSpace(stash) != "" {
			commit = strings.TrimSpace(stash)
		}
	}
	id, err := recoveryID()
	if err != nil {
		return err
	}
	ref := "refs/cwapi/safety/" + id
	if output, _, err := m.runGit(ctx, gitCWD, "update-ref", ref, commit); err != nil {
		return fmt.Errorf("GIT_SAFETY_SNAPSHOT_FAILED: %w: %s", err, strings.TrimSpace(output))
	}
	root, err := WorkspaceRuntimeRoot(m.dataRoot, repositoryRoot)
	if err != nil {
		return err
	}
	recoveryRoot := filepath.Join(root, "recovery")
	if err := os.MkdirAll(recoveryRoot, 0o700); err != nil {
		return fmt.Errorf("GIT_SAFETY_METADATA_CREATE_FAILED: %w", err)
	}
	metadata := recoveryMetadata{Schema: "cwapi.git-safety.v1", Ref: ref, Commit: commit, Head: head, Branch: strings.TrimSpace(branch), Dirty: dirty, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	payload, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(filepath.Join(recoveryRoot, id+".json"), payload, 0o600); err != nil {
		return fmt.Errorf("GIT_SAFETY_METADATA_WRITE_FAILED: %w", err)
	}
	return m.pruneRecoveries(ctx, gitCWD, recoveryRoot)
}

func (m *GitSafetyManager) pruneRecoveries(ctx context.Context, gitCWD, recoveryRoot string) error {
	output, _, err := m.runGit(ctx, gitCWD, "for-each-ref", "--format=%(refname)", "refs/cwapi/safety/")
	if err != nil {
		return nil
	}
	refs := strings.Fields(output)
	sort.Strings(refs)
	if len(refs) <= maxSafetyRefs {
		return nil
	}
	for _, ref := range refs[:len(refs)-maxSafetyRefs] {
		_, _, _ = m.runGit(ctx, gitCWD, "update-ref", "-d", ref)
		_ = os.Remove(filepath.Join(recoveryRoot, strings.TrimPrefix(ref, "refs/cwapi/safety/")+".json"))
	}
	return nil
}

func (m *GitSafetyManager) runGit(ctx context.Context, cwd string, args ...string) (string, int, error) {
	internalArgs := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "credential.interactive=never"}
	internalArgs = append(internalArgs, args...)
	command := processlaunch.CommandContext(ctx, m.git, internalArgs...)
	command.Dir = cwd
	authorName, authorEmail := "CWapi Safety", "cwapi-safety@localhost.invalid"
	command.Env = childenv.Merge(childenv.Git(""), map[string]*string{
		"GIT_AUTHOR_NAME": &authorName, "GIT_AUTHOR_EMAIL": &authorEmail,
		"GIT_COMMITTER_NAME": &authorName, "GIT_COMMITTER_EMAIL": &authorEmail,
	})
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	if err == nil {
		return stdout.String(), 0, nil
	}
	wrapped := fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode(), wrapped
	}
	return stdout.String(), -1, wrapped
}

func recoveryID() (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", fmt.Errorf("GIT_SAFETY_ID_FAILED: %w", err)
	}
	return time.Now().UTC().Format("20060102T150405.000000000Z") + "-" + hex.EncodeToString(suffix[:]), nil
}
