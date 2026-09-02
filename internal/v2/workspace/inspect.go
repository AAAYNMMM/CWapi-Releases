package workspace

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/repository"
)

type Snapshot struct {
	Repository     string
	TargetRef      string
	ResolvedCommit string
	CurrentHead    string
	CurrentBranch  string
	Detached       bool
	TrackingHead   string
	TrackedDirty   bool
	Divergence     string
}

// Inspect reads only local Git/workspace state. It never fetches, checks out,
// resets, cleans or otherwise changes the durable workspace. TrackingHead is
// the local refs/remotes/origin/<target> value, not a live network query.
func (m *Manager) Inspect(ctx context.Context, repositoryURL string) (Snapshot, error) {
	if m == nil {
		return Snapshot{}, errors.New("WORKSPACE_MANAGER_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	identity, err := repository.Parse(strings.TrimSpace(repositoryURL))
	if err != nil {
		return Snapshot{}, err
	}
	container := filepath.Join(m.root, workspaceKey(identity.Repository))
	repoPath := filepath.Join(container, "repo")
	metadataPath := filepath.Join(container, "workspace.json")
	info, err := os.Lstat(repoPath)
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, errors.New("WORKSPACE_NOT_FOUND")
	}
	if err != nil {
		return Snapshot{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Snapshot{}, errors.New("WORKSPACE_REPOSITORY_PATH_INVALID")
	}
	if err := m.verifyRepository(ctx, repoPath, identity); err != nil {
		return Snapshot{}, err
	}
	meta, err := loadMetadata(metadataPath)
	if err != nil {
		return Snapshot{}, err
	}
	if meta.Repository != identity.Repository {
		return Snapshot{}, errors.New("WORKSPACE_METADATA_REPOSITORY_MISMATCH")
	}
	head, err := m.head(ctx, repoPath)
	if err != nil || head == "" {
		return Snapshot{}, errors.New("WORKSPACE_HEAD_UNAVAILABLE")
	}
	dirty, err := m.trackedDirty(ctx, repoPath)
	if err != nil {
		return Snapshot{}, err
	}
	currentBranch, detached, err := m.currentBranch(ctx, repoPath)
	if err != nil {
		return Snapshot{}, err
	}
	trackingHead, err := m.localTrackingHead(ctx, repoPath, meta.TargetRef)
	if err != nil {
		return Snapshot{}, err
	}
	relation, err := m.localDivergence(ctx, repoPath, head, trackingHead)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		Repository:     identity.Repository,
		TargetRef:      meta.TargetRef,
		ResolvedCommit: meta.ResolvedCommit,
		CurrentHead:    head,
		CurrentBranch:  currentBranch,
		Detached:       detached,
		TrackingHead:   trackingHead,
		TrackedDirty:   dirty,
		Divergence:     relation,
	}, nil
}

func (m *Manager) localTrackingHead(ctx context.Context, repoPath, targetRef string) (string, error) {
	branch := strings.TrimPrefix(strings.TrimSpace(targetRef), "refs/heads/")
	if branch == "" || branch == targetRef {
		return "", errors.New("WORKSPACE_TARGET_REF_INVALID")
	}
	output, code, err := m.runGit(ctx, "-C", repoPath, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/"+branch+"^{commit}")
	if code == 1 {
		return "", nil
	}
	if err != nil {
		return "", errors.New("WORKSPACE_TRACKING_HEAD_READ_FAILED")
	}
	trackingHead := strings.ToLower(strings.TrimSpace(output))
	if trackingHead != "" && !fullCommitPattern.MatchString(trackingHead) {
		return "", errors.New("WORKSPACE_TRACKING_HEAD_INVALID")
	}
	return trackingHead, nil
}

func (m *Manager) localDivergence(ctx context.Context, repoPath, currentHead, trackingHead string) (string, error) {
	if trackingHead == "" {
		return "unknown", nil
	}
	if currentHead == trackingHead {
		return "aligned", nil
	}
	trackingBehind, err := m.isAncestor(ctx, repoPath, trackingHead, currentHead)
	if err != nil {
		return "", err
	}
	if trackingBehind {
		return "ahead", nil
	}
	currentBehind, err := m.isAncestor(ctx, repoPath, currentHead, trackingHead)
	if err != nil {
		return "", err
	}
	if currentBehind {
		return "behind", nil
	}
	return "diverged", nil
}
