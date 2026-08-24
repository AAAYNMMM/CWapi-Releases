package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

func (m *Manager) validateWorktreeRootLocked() (string, error) {
	root, exists, err := validateDirectoryRoot(filepath.Join(m.root, "git", "worktrees"), true)
	if err != nil || !exists {
		return "", errors.New("WORKSPACE_WORKTREE_ROOT_INTEGRITY_INVALID")
	}
	return root, nil
}

func validateDirectoryRoot(path string, create bool) (string, bool, error) {
	path = filepath.Clean(path)
	if create {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return "", false, err
		}
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil || !info.IsDir() || isReparsePoint(info) {
		return "", false, errors.New("directory integrity invalid")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false, err
	}
	expected, err := filepath.Abs(path)
	if err != nil {
		return "", false, err
	}
	actual, err := filepath.Abs(resolved)
	if err != nil || !strings.EqualFold(filepath.Clean(expected), filepath.Clean(actual)) {
		return "", false, errors.New("directory integrity invalid")
	}
	return filepath.Clean(expected), true, nil
}
