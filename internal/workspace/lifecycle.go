package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Cleanup removes process-lifetime repository workspaces during normal
// shutdown. Unlike startup Sweep, Cleanup acquires each repository lease before
// deleting its directory so an in-flight stock MCP request cannot lose the
// workspace underneath it.
func (m *Manager) Cleanup(ctx context.Context, gitExecutable string) []error {
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
		return []error{fmt.Errorf("WORKSPACE_CLEANUP_READ_FAILED: %w", err)}
	}
	var failures []error
	for _, child := range entries {
		lease, acquireErr := m.acquireRepository(ctx, child.Name())
		if acquireErr != nil {
			failures = append(failures, fmt.Errorf("WORKSPACE_CLEANUP_ACQUIRE_FAILED: %s: %w", child.Name(), acquireErr))
			if ctx.Err() != nil {
				return failures
			}
			continue
		}
		target := filepath.Join(repositoryRoot, child.Name())
		removeErr := removeTreeNoReparse(target, repositoryRoot)
		_ = lease.release()
		if removeErr != nil {
			failures = append(failures, fmt.Errorf("WORKSPACE_CLEANUP_ITEM_FAILED: %s: %w", child.Name(), removeErr))
		}
	}
	if ctx.Err() != nil {
		return failures
	}
	return append(failures, m.pruneMirrors(ctx, gitExecutable)...)
}

func (m *Manager) pruneMirrors(ctx context.Context, gitExecutable string) []error {
	mirrorRoot, mirrorExists, mirrorErr := validateDirectoryRoot(filepath.Join(m.root, "git", "mirrors"), false)
	if mirrorErr == nil && !mirrorExists {
		return nil
	}
	if mirrorErr != nil {
		return []error{errors.New("WORKSPACE_MIRROR_ROOT_INTEGRITY_INVALID")}
	}
	mirrors, readErr := os.ReadDir(mirrorRoot)
	if readErr != nil {
		return []error{fmt.Errorf("WORKSPACE_MIRROR_READ_FAILED: %w", readErr)}
	}
	if !validExecutable(gitExecutable) {
		return []error{errors.New("WORKSPACE_GIT_RUNTIME_UNAVAILABLE")}
	}
	var failures []error
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
