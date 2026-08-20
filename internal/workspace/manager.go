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
	root string
	mu   sync.Mutex
}

func NewManager(dataRoot string) (*Manager, error) {
	if strings.TrimSpace(dataRoot) == "" || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("WORKSPACE_DATA_ROOT_INVALID")
	}
	return &Manager{root: filepath.Clean(dataRoot)}, nil
}

func (m *Manager) Prepare(ctx context.Context, gitExecutable, repository, remoteURL, taskID, expectedSHA string) (Workspace, error) {
	if strings.TrimSpace(gitExecutable) == "" {
		return Workspace{}, errors.New("WORKSPACE_GIT_EXECUTABLE_REQUIRED")
	}
	if strings.TrimSpace(repository) == "" || strings.TrimSpace(remoteURL) == "" || strings.TrimSpace(taskID) == "" {
		return Workspace{}, errors.New("WORKSPACE_INPUT_REQUIRED")
	}
	if !fullSHA.MatchString(expectedSHA) {
		return Workspace{}, errors.New("WORKSPACE_EXPECTED_SHA_INVALID")
	}
	expectedSHA = strings.ToLower(expectedSHA)
	mirrorPath := filepath.Join(m.root, "git", "mirrors", repositoryKey(repository)+".git")
	worktreePath := filepath.Join(m.root, "git", "worktrees", safeTaskID(taskID))

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(mirrorPath), 0o700); err != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_MIRROR_ROOT_CREATE_FAILED: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o700); err != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_WORKTREE_ROOT_CREATE_FAILED: %w", err)
	}

	if _, err := os.Stat(mirrorPath); errors.Is(err, os.ErrNotExist) {
		if output, runErr := runGit(ctx, gitExecutable, "clone", "--mirror", "--", remoteURL, mirrorPath); runErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_MIRROR_CLONE_FAILED: %w: %s", runErr, output)
		}
	} else if err != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_MIRROR_STAT_FAILED: %w", err)
	} else {
		bare, runErr := runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "rev-parse", "--is-bare-repository")
		if runErr != nil || strings.TrimSpace(bare) != "true" {
			return Workspace{}, fmt.Errorf("WORKSPACE_MIRROR_INVALID")
		}
		if _, runErr := runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "remote", "set-url", "origin", remoteURL); runErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_REMOTE_UPDATE_FAILED: %w", runErr)
		}
	}
	if _, err := runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "cat-file", "-e", expectedSHA+"^{commit}"); err != nil {
		if output, fetchErr := runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "fetch", "--prune", "--tags", "origin"); fetchErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_FETCH_FAILED: %w: %s", fetchErr, output)
		}
		if _, verifyErr := runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "cat-file", "-e", expectedSHA+"^{commit}"); verifyErr != nil {
			return Workspace{}, fmt.Errorf("WORKSPACE_COMMIT_NOT_FOUND: %s", expectedSHA)
		}
	}

	if info, statErr := os.Stat(worktreePath); statErr == nil && info.IsDir() {
		actual, headErr := Head(ctx, gitExecutable, worktreePath)
		clean, cleanErr := IsClean(ctx, gitExecutable, worktreePath)
		if headErr == nil && cleanErr == nil && clean && actual == expectedSHA {
			return Workspace{
				Repository: repository, MirrorPath: mirrorPath, Path: worktreePath,
				ExpectedSHA: expectedSHA, ActualSHA: actual, CleanBefore: true,
			}, nil
		}
		_ = m.removeLocked(ctx, gitExecutable, mirrorPath, worktreePath)
	} else if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return Workspace{}, fmt.Errorf("WORKSPACE_DIRECTORY_STAT_FAILED: %w", statErr)
	}
	_ = os.RemoveAll(worktreePath)
	_, _ = runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "prune")
	if output, err := runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "add", "--detach", "--force", worktreePath, expectedSHA); err != nil {
		return Workspace{}, fmt.Errorf("WORKSPACE_ADD_FAILED: %w: %s", err, output)
	}
	actual, err := runGit(ctx, gitExecutable, "-C", worktreePath, "rev-parse", "HEAD")
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
	if output, err := runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "remove", "--force", worktreePath); err != nil {
		first = fmt.Errorf("WORKSPACE_REMOVE_FAILED: %w: %s", err, output)
	}
	if err := os.RemoveAll(worktreePath); err != nil && first == nil {
		first = fmt.Errorf("WORKSPACE_DIRECTORY_REMOVE_FAILED: %w", err)
	}
	_, _ = runGit(ctx, gitExecutable, "--git-dir", mirrorPath, "worktree", "prune")
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
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Env = os.Environ()
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
