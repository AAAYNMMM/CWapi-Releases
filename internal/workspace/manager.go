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
	lease       *repositoryLease
}

type Manager struct {
	root         string
	ghExecutable string
	ghConfigDir  string
	blocked      error
	mu           sync.RWMutex
	locks        map[string]*repositoryLock
}

type repositoryLock struct {
	token chan struct{}
}

type repositoryLease struct {
	mu       sync.Mutex
	lock     *repositoryLock
	released bool
}

func newRepositoryLock() *repositoryLock {
	lock := &repositoryLock{token: make(chan struct{}, 1)}
	lock.token <- struct{}{}
	return lock
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
	return &Manager{root: filepath.Clean(dataRoot), locks: make(map[string]*repositoryLock)}, nil
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
	if ctx == nil {
		ctx = context.Background()
	}
	expectedSHA = strings.ToLower(expectedSHA)
	key := repositoryKey(repository)
	lease, err := m.acquireRepository(ctx, key)
	if err != nil {
		return Workspace{}, err
	}
	releaseOnError := true
	defer func() {
		if releaseOnError {
			_ = lease.release()
		}
	}()

	if blocked := m.blockedError(); blocked != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_ROOT_BLOCKED: %w", blocked)
	}
	repositoryRoot, err := m.validateRepositoryRootLocked()
	if err != nil {
		m.blockRoot(err)
		return Workspace{}, fmt.Errorf("WORKSPACE_ROOT_BLOCKED: %w", err)
	}
	mirrorRoot, exists, err := validateDirectoryRoot(filepath.Join(m.root, "git", "mirrors"), true)
	if err != nil || !exists {
		return Workspace{}, errors.New("WORKSPACE_MIRROR_ROOT_INTEGRITY_INVALID")
	}
	mirrorPath := filepath.Join(mirrorRoot, key+".git")
	workspacePath := filepath.Join(repositoryRoot, key)

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
			return Workspace{}, errors.New("WORKSPACE_MIRROR_INVALID")
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

	_, workspaceExists, workspaceErr := validateDirectoryRoot(workspacePath, false)
	if workspaceErr != nil {
		return Workspace{}, errors.New("WORKSPACE_REPOSITORY_INTEGRITY_INVALID")
	}
	if !workspaceExists {
		_, _ = m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "prune")
		if output, addErr := m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "add", "--detach", "--force", workspacePath, expectedSHA); addErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_ADD_FAILED: %w: %s", addErr, output)
		}
	} else {
		if !m.workspaceBelongsToMirror(ctx, gitExecutable, workspacePath, mirrorPath) {
			return Workspace{}, errors.New("WORKSPACE_REPOSITORY_GIT_IDENTITY_INVALID")
		}
		if output, checkoutErr := m.runGit(ctx, gitExecutable, "-C", workspacePath, "checkout", "--detach", "--force", expectedSHA); checkoutErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_SYNC_CHECKOUT_FAILED: %w: %s", checkoutErr, output)
		}
		if output, resetErr := m.runGit(ctx, gitExecutable, "-C", workspacePath, "reset", "--hard", expectedSHA); resetErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_SYNC_RESET_FAILED: %w: %s", resetErr, output)
		}
	}

	actual, err := m.runGit(ctx, gitExecutable, "-C", workspacePath, "rev-parse", "HEAD")
	if err != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_HEAD_READ_FAILED: %w", err)
	}
	actual = strings.ToLower(strings.TrimSpace(actual))
	if actual != expectedSHA {
		return Workspace{}, fmt.Errorf("WORKSPACE_COMMIT_MISMATCH: expected=%s actual=%s", expectedSHA, actual)
	}
	trackedClean, err := m.isTrackedClean(ctx, gitExecutable, workspacePath)
	if err != nil {
		return Workspace{}, err
	}
	if !trackedClean {
		return Workspace{}, errors.New("WORKSPACE_TRACKED_STATE_DIRTY")
	}
	releaseOnError = false
	return Workspace{
		Repository: repository, MirrorPath: mirrorPath, Path: workspacePath,
		ExpectedSHA: expectedSHA, ActualSHA: actual, CleanBefore: true, lease: lease,
	}, nil
}

func (m *Manager) acquireRepository(ctx context.Context, key string) (*repositoryLease, error) {
	m.mu.Lock()
	lock := m.locks[key]
	if lock == nil {
		lock = newRepositoryLock()
		m.locks[key] = lock
	}
	m.mu.Unlock()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("WORKSPACE_REPOSITORY_ACQUIRE_CANCELED: %w", ctx.Err())
	case <-lock.token:
		return &repositoryLease{lock: lock}, nil
	}
}

func (l *repositoryLease) release() error {
	if l == nil || l.lock == nil {
		return nil
	}
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return nil
	}
	l.released = true
	lock := l.lock
	l.mu.Unlock()
	lock.token <- struct{}{}
	return nil
}

func (m *Manager) Release(workspace Workspace) error {
	return workspace.lease.release()
}

// Sweep removes only direct children of the known process-lifetime repository
// workspace root. It never follows a symlink or Windows reparse point and
// preserves shared mirrors. Service startup calls Sweep before accepting tasks.
func (m *Manager) Sweep(ctx context.Context, gitExecutable string) []error {
	if ctx == nil {
		ctx = context.Background()
	}
	repositoryRoot, err := m.validateRepositoryRootLocked()
	if err != nil {
		m.blockRoot(err)
		return []error{fmt.Errorf("WORKSPACE_ROOT_BLOCKED: %w", err)}
	}
	entries, err := os.ReadDir(repositoryRoot)
	if err != nil {
		return []error{fmt.Errorf("WORKSPACE_SWEEP_READ_FAILED: %w", err)}
	}
	var failures []error
	for _, child := range entries {
		target := filepath.Join(repositoryRoot, child.Name())
		if err := removeTreeNoReparse(target, repositoryRoot); err != nil {
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

func (m *Manager) blockedError() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.blocked
}

func (m *Manager) blockRoot(err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	if m.blocked == nil {
		m.blocked = err
	}
	m.mu.Unlock()
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

func (m *Manager) workspaceBelongsToMirror(ctx context.Context, executable, workspacePath, mirrorPath string) bool {
	commonDir, err := m.runGit(ctx, executable, "-C", workspacePath, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(strings.TrimSpace(commonDir)), filepath.Clean(mirrorPath))
}

func (m *Manager) isTrackedClean(ctx context.Context, executable, path string) (bool, error) {
	output, err := m.runGit(ctx, executable, "-C", path, "status", "--porcelain=v1", "--untracked-files=no")
	if err != nil {
		return false, fmt.Errorf("WORKSPACE_STATUS_FAILED: %w", err)
	}
	return strings.TrimSpace(output) == "", nil
}

// Remove physically deletes a repository workspace. Normal request completion
// uses Release; Remove is reserved for explicit cleanup tests.
func (m *Manager) Remove(ctx context.Context, gitExecutable string, workspace Workspace) error {
	removeErr := m.removeLocked(ctx, gitExecutable, workspace.MirrorPath, workspace.Path)
	releaseErr := workspace.lease.release()
	if removeErr != nil {
		return removeErr
	}
	return releaseErr
}

func (m *Manager) removeLocked(ctx context.Context, gitExecutable, mirrorPath, workspacePath string) error {
	if strings.TrimSpace(mirrorPath) == "" || strings.TrimSpace(workspacePath) == "" {
		return nil
	}
	var first error
	if output, err := m.runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "remove", "--force", workspacePath); err != nil {
		first = fmt.Errorf("WORKSPACE_REMOVE_FAILED: %w: %s", err, output)
	}
	repositoryRoot := filepath.Join(m.root, "git", "repositories")
	if err := removeTreeNoReparse(workspacePath, repositoryRoot); err != nil && first == nil {
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
	m.mu.RLock()
	ghExecutable, ghConfigDir := m.ghExecutable, m.ghConfigDir
	m.mu.RUnlock()
	environment := childenv.Git(ghConfigDir)
	if ghExecutable != "" {
		helper := `!"` + filepath.ToSlash(ghExecutable) + `" auth git-credential`
		count, key, value := "1", "credential.https://github.com.helper", helper
		environment = childenv.Merge(environment, map[string]*string{
			"GIT_CONFIG_COUNT": &count, "GIT_CONFIG_KEY_0": &key, "GIT_CONFIG_VALUE_0": &value,
		})
	}
	return environment
}

func runGitWithEnvironment(ctx context.Context, executable string, environment []string, args ...string) (string, error) {
	gitArgs := make([]string, 0, len(args)+2)
	gitArgs = append(gitArgs, "-c", "core.longpaths=true")
	gitArgs = append(gitArgs, args...)
	cmd := exec.CommandContext(ctx, executable, gitArgs...)
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
