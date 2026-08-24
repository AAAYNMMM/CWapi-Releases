package workspace

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
)

var fullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

type Workspace struct {
	Repository  string
	MirrorPath  string
	Path        string
	ExpectedSHA string
	ActualSHA   string
	CleanBefore bool
}

type Manager struct {
	root         string
	ghExecutable string
	ghConfigDir  string
	blocked      error
	mu           sync.Mutex
}

func (m *Manager) ConfigureGitHubCredentialHelper(executable, configDir string) error {
	executable = strings.TrimSpace(executable)
	if executable == "" {
		m.mu.Lock()
		m.ghExecutable, m.ghConfigDir = "", ""
		m.mu.Unlock()
		return nil
	}
	if !filepath.IsAbs(executable) {
		return errors.New("WORKSPACE_GH_EXECUTABLE_INVALID")
	}
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("WORKSPACE_GH_EXECUTABLE_INVALID")
	}
	if configDir != "" && !filepath.IsAbs(configDir) {
		return errors.New("WORKSPACE_GH_CONFIG_DIR_INVALID")
	}
	cleanConfigDir := ""
	if configDir != "" {
		cleanConfigDir = filepath.Clean(configDir)
	}
	m.mu.Lock()
	m.ghExecutable, m.ghConfigDir = filepath.Clean(executable), cleanConfigDir
	m.mu.Unlock()
	return nil
}

func NewManager(dataRoot string) (*Manager, error) {
	if strings.TrimSpace(dataRoot) == "" || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("WORKSPACE_DATA_ROOT_INVALID")
	}
	return &Manager{root: filepath.Clean(dataRoot)}, nil
}

func (m *Manager) Prepare(ctx context.Context, gitExecutable, repository, remoteURL, taskID, expectedSHA string) (Workspace, error) {
	if strings.TrimSpace(gitExecutable) == "" || !filepath.IsAbs(gitExecutable) {
		return Workspace{}, errors.New("WORKSPACE_GIT_EXECUTABLE_REQUIRED")
	}
	if info, err := os.Stat(gitExecutable); err != nil || !info.Mode().IsRegular() {
		return Workspace{}, errors.New("WORKSPACE_GIT_EXECUTABLE_INVALID")
	}
	if strings.TrimSpace(repository) == "" || strings.TrimSpace(remoteURL) == "" || strings.TrimSpace(taskID) == "" {
		return Workspace{}, errors.New("WORKSPACE_INPUT_REQUIRED")
	}
	if !fullSHA.MatchString(expectedSHA) {
		return Workspace{}, errors.New("WORKSPACE_EXPECTED_SHA_INVALID")
	}
	expectedSHA = strings.ToLower(expectedSHA)
	worktreePath := filepath.Join(m.root, "git", "worktrees", safeTaskID(taskID))

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blocked != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_ROOT_BLOCKED: %w", m.blocked)
	}
	if _, err := m.validateWorktreeRootLocked(); err != nil {
		m.blocked = err
		return Workspace{}, fmt.Errorf("WORKSPACE_ROOT_BLOCKED: %w", err)
	}
	mirrorRoot, exists, err := validateDirectoryRoot(filepath.Join(m.root, "git", "mirrors"), true)
	if err != nil || !exists {
		return Workspace{}, errors.New("WORKSPACE_MIRROR_ROOT_INTEGRITY_INVALID")
	}
	mirrorPath := filepath.Join(mirrorRoot, repositoryKey(repository)+".git")
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_WORKTREE_ROOT_CREATE_FAILED: %w", err)
	}

	_, mirrorExists, mirrorErr := validateDirectoryRoot(mirrorPath, false)
	if mirrorErr != nil {
		return Workspace{}, errors.New("WORKSPACE_MIRROR_INTEGRITY_INVALID")
	}
	if !mirrorExists {
		if output, runErr := m.runGit(ctx, gitExecutable, "clone", "--mirror", "--", remoteURL, mirrorPath); runErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_MIRROR_CLONE_FAILED: %w: %s", runErr, output)
		}
	} else {
		bare, runErr := m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "rev-parse", "--is-bare-repository")
		if runErr != nil || strings.TrimSpace(bare) != "true" {
			return Workspace{}, fmt.Errorf("WORKSPACE_MIRROR_INVALID")
		}
		if _, runErr := m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "remote", "set-url", "origin", remoteURL); runErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_REMOTE_UPDATE_FAILED: %w", runErr)
		}
	}
	if !m.isCommitObject(ctx, gitExecutable, mirrorPath, expectedSHA) {
		if output, fetchErr := m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "fetch", "--prune", "--tags", "origin"); fetchErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_FETCH_FAILED: %w: %s", fetchErr, output)
		}
		if !m.isCommitObject(ctx, gitExecutable, mirrorPath, expectedSHA) {
			return Workspace{}, fmt.Errorf("WORKSPACE_COMMIT_NOT_FOUND: %s", expectedSHA)
		}
	}

	if _, statErr := os.Lstat(worktreePath); statErr == nil {
		if removeErr := removeTreeNoReparse(worktreePath, filepath.Dir(worktreePath)); removeErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_STALE_REMOVE_FAILED: %w", removeErr)
		}
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return Workspace{}, fmt.Errorf("WORKSPACE_DIRECTORY_STAT_FAILED: %w", statErr)
	}
	_, _ = m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "prune")
	if output, err := m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "add", "--detach", "--force", worktreePath, expectedSHA); err != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_ADD_FAILED: %w: %s", err, output)
	}
	actual, err := m.runGit(ctx, gitExecutable, "-C", worktreePath, "rev-parse", "HEAD")
	if err != nil {
		_ = m.removeLocked(context.Background(), gitExecutable, mirrorPath, worktreePath)
		return Workspace{}, fmt.Errorf("WORKSPACE_HEAD_READ_FAILED: %w", err)
	}
	actual = strings.ToLower(strings.TrimSpace(actual))
	if actual != expectedSHA {
		_ = m.removeLocked(context.Background(), gitExecutable, mirrorPath, worktreePath)
		return Workspace{}, fmt.Errorf("WORKSPACE_COMMIT_MISMATCH: expected=%s actual=%s", expectedSHA, actual)
	}
	clean, err := IsClean(ctx, gitExecutable, worktreePath)
	if err != nil || !clean {
		_ = m.removeLocked(context.Background(), gitExecutable, mirrorPath, worktreePath)
		if err != nil {
			return Workspace{}, err
		}
		return Workspace{}, errors.New("WORKSPACE_NOT_CLEAN_BEFORE")
	}
	return Workspace{Repository: repository, MirrorPath: mirrorPath, Path: worktreePath, ExpectedSHA: expectedSHA, ActualSHA: actual, CleanBefore: true}, nil
}

// Sweep removes only direct children of the known ephemeral worktree root.
// It never follows a symlink or Windows reparse point and preserves mirrors.
func (m *Manager) Sweep(ctx context.Context, gitExecutable string) []error {
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	worktreeRoot, err := m.validateWorktreeRootLocked()
	if err != nil {
		m.blocked = err
		return []error{fmt.Errorf("WORKSPACE_ROOT_BLOCKED: %w", err)}
	}
	entries, err := os.ReadDir(worktreeRoot)
	if err != nil {
		return []error{fmt.Errorf("WORKSPACE_SWEEP_READ_FAILED: %w", err)}
	}
	var failures []error
	for _, child := range entries {
		target := filepath.Join(worktreeRoot, child.Name())
		if err := removeTreeNoReparse(target, worktreeRoot); err != nil {
			failures = append(failures, fmt.Errorf("WORKSPACE_SWEEP_ITEM_FAILED: %s: %w", child.Name(), err))
		}
	}
	mirrorRoot, mirrorExists, mirrorErr := validateDirectoryRoot(filepath.Join(m.root, "git", "mirrors"), false)
	if mirrorErr == nil && !mirrorExists {
		return failures
	}
	if mirrorErr != nil {
		return append(failures, errors.New("WORKSPACE_MIRROR_ROOT_INTEGRITY_INVALID"))
	}
	mirrors, readErr := os.ReadDir(mirrorRoot)
	if readErr != nil {
		return append(failures, fmt.Errorf("WORKSPACE_MIRROR_READ_FAILED: %w", readErr))
	}
	if !validExecutable(gitExecutable) {
		return append(failures, errors.New("WORKSPACE_GIT_RUNTIME_UNAVAILABLE"))
	}
	for _, mirror := range mirrors {
		path := filepath.Join(mirrorRoot, mirror.Name())
		_, exists, integrityErr := validateDirectoryRoot(path, false)
		if integrityErr != nil || !exists {
			failures = append(failures, fmt.Errorf("WORKSPACE_MIRROR_INTEGRITY_INVALID: %s", mirror.Name()))
			continue
		}
		if output, pruneErr := m.runGit(ctx, gitExecutable, "--git-dir", path, "worktree", "prune"); pruneErr != nil {
			failures = append(failures, fmt.Errorf("WORKSPACE_MIRROR_PRUNE_FAILED: %s: %s", mirror.Name(), output))
		}
	}
	return failures
}

func removeTreeNoReparse(target, root string) error {
	if !pathWithin(target, root) || strings.EqualFold(filepath.Clean(target), filepath.Clean(root)) {
		return errors.New("WORKSPACE_SWEEP_TARGET_INVALID")
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || isReparsePoint(info) {
		return os.Remove(target)
	}
	children, err := os.ReadDir(target)
	if err != nil {
		return err
	}
	for _, child := range children {
		if err := removeTreeNoReparse(filepath.Join(target, child.Name()), root); err != nil {
			return err
		}
	}
	return os.Remove(target)
}

func validExecutable(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func pathWithin(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	if strings.EqualFold(path, root) {
		return true
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func (m *Manager) isCommitObject(ctx context.Context, executable, mirrorPath, expectedSHA string) bool {
	objectType, err := m.runGit(ctx, executable, "--git-dir", mirrorPath, "cat-file", "-t", expectedSHA)
	return err == nil && strings.TrimSpace(objectType) == "commit"
}

func (m *Manager) Remove(ctx context.Context, gitExecutable string, workspace Workspace) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.removeLocked(ctx, gitExecutable, workspace.MirrorPath, workspace.Path)
}

func (m *Manager) removeLocked(ctx context.Context, gitExecutable, mirrorPath, worktreePath string) error {
	if strings.TrimSpace(mirrorPath) == "" || strings.TrimSpace(worktreePath) == "" {
		return nil
	}
	var first error
	if output, err := m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "remove", "--force", worktreePath); err != nil {
		first = fmt.Errorf("WORKSPACE_REMOVE_FAILED: %w: %s", err, output)
	}
	worktreeRoot := filepath.Join(m.root, "git", "worktrees")
	if err := removeTreeNoReparse(worktreePath, worktreeRoot); err != nil && first == nil {
		first = fmt.Errorf("WORKSPACE_DIRECTORY_REMOVE_FAILED: %w", err)
	}
	_, _ = m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "prune")
	return first
}

func IsClean(ctx context.Context, gitExecutable, path string) (bool, error) {
	output, err := runGit(ctx, gitExecutable, "-C", path, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return false, fmt.Errorf("WORKSPACE_STATUS_FAILED: %w", err)
	}
	return strings.TrimSpace(output) == "", nil
}

func Head(ctx context.Context, gitExecutable, path string) (string, error) {
	output, err := runGit(ctx, gitExecutable, "-C", path, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("WORKSPACE_HEAD_READ_FAILED: %w", err)
	}
	value := strings.ToLower(strings.TrimSpace(output))
	if !fullSHA.MatchString(value) {
		return "", errors.New("WORKSPACE_HEAD_INVALID")
	}
	return value, nil
}

func runGit(ctx context.Context, executable string, args ...string) (string, error) {
	return runGitWithEnvironment(ctx, executable, childenv.Git(""), args...)
}

func (m *Manager) runGit(ctx context.Context, executable string, args ...string) (string, error) {
	return runGitWithEnvironment(ctx, executable, m.gitEnvironment(), args...)
}

func (m *Manager) gitEnvironment() []string {
	environment := childenv.Git(m.ghConfigDir)
	if m.ghExecutable != "" {
		helper := `!"` + filepath.ToSlash(m.ghExecutable) + `" auth git-credential`
		count, key, value := "1", "credential.https://github.com.helper", helper
		environment = childenv.Merge(environment, map[string]*string{
			"GIT_CONFIG_COUNT": &count, "GIT_CONFIG_KEY_0": &key, "GIT_CONFIG_VALUE_0": &value,
		})
	}
	return environment
}

func runGitWithEnvironment(ctx context.Context, executable string, environment []string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = environment
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if len(text) > 4096 {
		text = text[len(text)-4096:]
	}
	return text, err
}

func repositoryKey(repository string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(repository))))
	return hex.EncodeToString(sum[:16])
}

func safeTaskID(taskID string) string {
	const maxPrefixBytes = 48
	var builder strings.Builder
	for _, r := range taskID {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			builder.WriteRune(r)
			if builder.Len() >= maxPrefixBytes {
				break
			}
		}
	}
	prefix := builder.String()
	if builder.Len() == 0 {
		prefix = "task"
	}
	digest := sha256.Sum256([]byte(taskID))
	return prefix + "-" + hex.EncodeToString(digest[:8])
}
