package workspace

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/processlaunch"
	"github.com/AAAYNMMM/CWapi/internal/repository"
)

const (
	metadataSchema = "cwapi.workspace.v1"
	maxGitOutput   = 1024 * 1024
)

var (
	fullCommitPattern           = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	errWorkspaceMetadataInvalid = errors.New("WORKSPACE_METADATA_INVALID")
)

type PrepareInput struct {
	RepositoryURL  string
	TargetRef      string
	ExpectedCommit string
	Resume         bool
}

type Result struct {
	Repository     string
	Path           string
	TargetRef      string
	ResolvedCommit string
	CurrentHead    string
	TrackedDirty   bool
	Resumed        bool
}

type metadata struct {
	Schema         string `json:"schema"`
	Repository     string `json:"repository"`
	NormalizedURL  string `json:"normalized_url"`
	TargetRef      string `json:"target_ref"`
	ResolvedCommit string `json:"resolved_commit"`
	UpdatedAt      string `json:"updated_at"`
}

type Manager struct {
	dataRoot string
	root     string
	git      string

	remoteURL     func(repository.Identity) string
	remoteMatches func(string, repository.Identity) bool
}

func NewManager(dataRoot, gitExecutable string) (*Manager, error) {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("WORKSPACE_DATA_ROOT_INVALID")
	}
	gitExecutable, err := resolveGitExecutable(gitExecutable)
	if err != nil {
		return nil, err
	}
	manager := &Manager{
		dataRoot: filepath.Clean(dataRoot),
		root:     filepath.Join(filepath.Clean(dataRoot), "workspaces"),
		git:      gitExecutable,
	}
	manager.remoteURL = func(identity repository.Identity) string { return identity.NormalizedURL }
	manager.remoteMatches = func(actual string, identity repository.Identity) bool {
		parsed, err := repository.Parse(strings.TrimSpace(actual))
		return err == nil && parsed.Repository == identity.Repository
	}
	return manager, nil
}

func (m *Manager) Prepare(ctx context.Context, input PrepareInput) (Result, error) {
	if m == nil {
		return Result{}, errors.New("WORKSPACE_MANAGER_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	identity, err := repository.Parse(strings.TrimSpace(input.RepositoryURL))
	if err != nil {
		return Result{}, err
	}
	targetRef, branch, err := m.canonicalTargetRef(ctx, input.TargetRef)
	if err != nil {
		return Result{}, err
	}
	expectedCommit := strings.ToLower(strings.TrimSpace(input.ExpectedCommit))
	if expectedCommit != "" && !fullCommitPattern.MatchString(expectedCommit) {
		return Result{}, errors.New("WORKSPACE_EXPECTED_COMMIT_INVALID")
	}

	container := filepath.Join(m.root, workspaceKey(identity.Repository))
	repoPath := filepath.Join(container, "repo")
	metadataPath := filepath.Join(container, "workspace.json")
	exists, err := m.ensureContainer(container, repoPath)
	if err != nil {
		return Result{}, err
	}

	if input.Resume {
		if !exists {
			return Result{}, errors.New("WORKSPACE_RESUME_NOT_FOUND")
		}
		if err := m.verifyRepository(ctx, repoPath, identity); err != nil {
			return Result{}, err
		}
		meta, err := loadMetadata(metadataPath)
		if err != nil {
			return Result{}, err
		}
		if meta.Repository != identity.Repository || meta.TargetRef != targetRef {
			return Result{}, errors.New("WORKSPACE_RESUME_CONTEXT_MISMATCH")
		}
		if expectedCommit != "" && expectedCommit != meta.ResolvedCommit {
			return Result{}, fmt.Errorf("WORKSPACE_EXPECTED_COMMIT_MISMATCH: expected=%s actual=%s", expectedCommit, meta.ResolvedCommit)
		}
		head, err := m.head(ctx, repoPath)
		if err != nil || head == "" {
			return Result{}, errors.New("WORKSPACE_HEAD_UNAVAILABLE")
		}
		dirty, err := m.trackedDirty(ctx, repoPath)
		if err != nil {
			return Result{}, err
		}
		return Result{
			Repository: identity.Repository, Path: repoPath, TargetRef: targetRef,
			ResolvedCommit: meta.ResolvedCommit, CurrentHead: head, TrackedDirty: dirty, Resumed: true,
		}, nil
	}

	if !exists {
		if err := os.MkdirAll(container, 0o700); err != nil {
			return Result{}, fmt.Errorf("WORKSPACE_DIRECTORY_CREATE_FAILED: %w", err)
		}
		remote := m.remoteURL(identity)
		if _, err := m.mustGit(ctx, "clone", "--origin", "origin", "--no-checkout", remote, repoPath); err != nil {
			return Result{}, fmt.Errorf("WORKSPACE_CLONE_FAILED: %w", err)
		}
		if err := m.verifyRepository(ctx, repoPath, identity); err != nil {
			return Result{}, err
		}
	} else {
		if err := m.verifyRepository(ctx, repoPath, identity); err != nil {
			return Result{}, err
		}
		dirty, err := m.trackedDirty(ctx, repoPath)
		if err != nil {
			return Result{}, err
		}
		if dirty {
			return Result{}, errors.New("WORKSPACE_DIRTY")
		}
	}

	meta, metaErr := loadMetadataIfPresent(metadataPath)
	if metaErr != nil {
		if !errors.Is(metaErr, errWorkspaceMetadataInvalid) {
			return Result{}, metaErr
		}
		// A crash while replacing metadata must not permanently strand an
		// otherwise valid, clean repository. Non-resume preparation already
		// verifies origin, dirty state and local commits before rewriting it.
		meta = nil
	}
	currentHead, err := m.head(ctx, repoPath)
	if err != nil {
		return Result{}, err
	}
	if exists && currentHead != "" && meta != nil && meta.TargetRef != targetRef {
		previousBranch := strings.TrimPrefix(meta.TargetRef, "refs/heads/")
		previousRemote, err := m.fetchAndResolve(ctx, repoPath, previousBranch)
		if err != nil {
			return Result{}, fmt.Errorf("WORKSPACE_PREVIOUS_TARGET_UNAVAILABLE: %w", err)
		}
		if err := m.ensureNoLocalCommits(ctx, repoPath, currentHead, previousRemote); err != nil {
			return Result{}, err
		}
	}

	resolved, err := m.fetchAndResolve(ctx, repoPath, branch)
	if err != nil {
		return Result{}, err
	}
	if expectedCommit != "" && expectedCommit != resolved {
		return Result{}, fmt.Errorf("WORKSPACE_EXPECTED_COMMIT_MISMATCH: expected=%s actual=%s", expectedCommit, resolved)
	}
	if exists && currentHead != "" && (meta == nil || meta.TargetRef == targetRef) {
		if err := m.ensureNoLocalCommits(ctx, repoPath, currentHead, resolved); err != nil {
			return Result{}, err
		}
	}

	if _, err := m.mustGit(ctx, "-C", repoPath, "checkout", "--detach", resolved); err != nil {
		return Result{}, fmt.Errorf("WORKSPACE_SYNC_FAILED: %w", err)
	}
	head, err := m.head(ctx, repoPath)
	if err != nil {
		return Result{}, err
	}
	if head != resolved {
		return Result{}, fmt.Errorf("WORKSPACE_COMMIT_MISMATCH: expected=%s actual=%s", resolved, head)
	}
	dirty, err := m.trackedDirty(ctx, repoPath)
	if err != nil {
		return Result{}, err
	}
	if dirty {
		return Result{}, errors.New("WORKSPACE_DIRTY_AFTER_SYNC")
	}
	if err := saveMetadata(metadataPath, metadata{
		Schema: metadataSchema, Repository: identity.Repository, NormalizedURL: identity.NormalizedURL,
		TargetRef: targetRef, ResolvedCommit: resolved, UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return Result{}, err
	}
	return Result{
		Repository: identity.Repository, Path: repoPath, TargetRef: targetRef,
		ResolvedCommit: resolved, CurrentHead: head, TrackedDirty: false,
	}, nil
}

func (m *Manager) canonicalTargetRef(ctx context.Context, raw string) (string, string, error) {
	branch := strings.TrimSpace(raw)
	if strings.HasPrefix(branch, "refs/heads/") {
		branch = strings.TrimPrefix(branch, "refs/heads/")
	}
	if branch == "" {
		return "", "", errors.New("WORKSPACE_TARGET_REF_REQUIRED")
	}
	output, err := m.mustGit(ctx, "check-ref-format", "--branch", branch)
	if err != nil || strings.TrimSpace(output) == "" {
		return "", "", errors.New("WORKSPACE_TARGET_REF_INVALID")
	}
	return "refs/heads/" + branch, branch, nil
}

func (m *Manager) ensureContainer(container, repoPath string) (bool, error) {
	if err := os.MkdirAll(m.root, 0o700); err != nil {
		return false, fmt.Errorf("WORKSPACE_ROOT_CREATE_FAILED: %w", err)
	}
	if info, err := os.Lstat(container); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return false, errors.New("WORKSPACE_CONTAINER_INVALID")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	info, err := os.Lstat(repoPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, errors.New("WORKSPACE_REPOSITORY_PATH_INVALID")
	}
	return true, nil
}

func (m *Manager) verifyRepository(ctx context.Context, repoPath string, identity repository.Identity) error {
	inside, err := m.mustGit(ctx, "-C", repoPath, "rev-parse", "--is-inside-work-tree")
	if err != nil || strings.TrimSpace(inside) != "true" {
		return errors.New("WORKSPACE_GIT_METADATA_INVALID")
	}
	origin, err := m.mustGit(ctx, "-C", repoPath, "remote", "get-url", "origin")
	if err != nil || !m.remoteMatches(strings.TrimSpace(origin), identity) {
		return errors.New("WORKSPACE_REPOSITORY_IDENTITY_MISMATCH")
	}
	return nil
}

func (m *Manager) fetchAndResolve(ctx context.Context, repoPath, branch string) (string, error) {
	refspec := "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
	if _, err := m.mustGit(ctx, "-C", repoPath, "fetch", "--prune", "origin", refspec); err != nil {
		return "", fmt.Errorf("WORKSPACE_FETCH_FAILED: %w", err)
	}
	resolved, err := m.mustGit(ctx, "-C", repoPath, "rev-parse", "--verify", "refs/remotes/origin/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("WORKSPACE_TARGET_COMMIT_NOT_FOUND: %w", err)
	}
	resolved = strings.ToLower(strings.TrimSpace(resolved))
	if !fullCommitPattern.MatchString(resolved) {
		return "", errors.New("WORKSPACE_TARGET_COMMIT_INVALID")
	}
	return resolved, nil
}

func (m *Manager) ensureNoLocalCommits(ctx context.Context, repoPath, current, remote string) error {
	if current == "" || current == remote {
		return nil
	}
	currentBehind, err := m.isAncestor(ctx, repoPath, current, remote)
	if err != nil {
		return err
	}
	if currentBehind {
		return nil
	}
	remoteBehind, err := m.isAncestor(ctx, repoPath, remote, current)
	if err != nil {
		return err
	}
	if remoteBehind {
		return errors.New("WORKSPACE_LOCAL_COMMITS")
	}
	return errors.New("WORKSPACE_DIVERGED")
}

func (m *Manager) isAncestor(ctx context.Context, repoPath, ancestor, descendant string) (bool, error) {
	_, code, err := m.runGit(ctx, "-C", repoPath, "merge-base", "--is-ancestor", ancestor, descendant)
	if code == 0 {
		return true, nil
	}
	if code == 1 {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("WORKSPACE_ANCESTRY_CHECK_FAILED: %w", err)
	}
	return false, errors.New("WORKSPACE_ANCESTRY_CHECK_FAILED")
}

func (m *Manager) head(ctx context.Context, repoPath string) (string, error) {
	output, code, err := m.runGit(ctx, "-C", repoPath, "rev-parse", "--verify", "--quiet", "HEAD^{commit}")
	if code == 1 {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("WORKSPACE_HEAD_READ_FAILED: %w", err)
	}
	head := strings.ToLower(strings.TrimSpace(output))
	if head != "" && !fullCommitPattern.MatchString(head) {
		return "", errors.New("WORKSPACE_HEAD_INVALID")
	}
	return head, nil
}

func (m *Manager) trackedDirty(ctx context.Context, repoPath string) (bool, error) {
	output, err := m.mustGit(ctx, "-C", repoPath, "status", "--porcelain=v1", "--untracked-files=no")
	if err != nil {
		return false, fmt.Errorf("WORKSPACE_STATUS_FAILED: %w", err)
	}
	return strings.TrimSpace(output) != "", nil
}

func (m *Manager) mustGit(ctx context.Context, args ...string) (string, error) {
	output, code, err := m.runGit(ctx, args...)
	if err != nil {
		return output, fmt.Errorf("git exit=%d: %w: %s", code, err, strings.TrimSpace(output))
	}
	return output, nil
}

func (m *Manager) runGit(ctx context.Context, args ...string) (string, int, error) {
	gitArgs := append([]string{"-c", "core.longpaths=true"}, args...)
	command := processlaunch.CommandContext(ctx, m.git, gitArgs...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never")
	buffer := &boundedBuffer{limit: maxGitOutput}
	command.Stdout = buffer
	command.Stderr = buffer
	err := command.Run()
	if err == nil {
		return buffer.String(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return buffer.String(), exitErr.ExitCode(), err
	}
	return buffer.String(), -1, err
}

func resolveGitExecutable(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		resolved, err := exec.LookPath("git")
		if err != nil {
			return "", errors.New("WORKSPACE_GIT_NOT_FOUND")
		}
		return resolved, nil
	}
	if filepath.IsAbs(value) {
		info, err := os.Stat(value)
		if err != nil || !info.Mode().IsRegular() {
			return "", errors.New("WORKSPACE_GIT_INVALID")
		}
		return filepath.Clean(value), nil
	}
	resolved, err := exec.LookPath(value)
	if err != nil {
		return "", errors.New("WORKSPACE_GIT_NOT_FOUND")
	}
	return resolved, nil
}

func workspaceKey(repositoryName string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(repositoryName)))
	return hex.EncodeToString(sum[:])
}

func loadMetadataIfPresent(path string) (*metadata, error) {
	payload, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("WORKSPACE_METADATA_READ_FAILED: %w", err)
	}
	var value metadata
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil || value.Schema != metadataSchema || value.Repository == "" || value.TargetRef == "" || !fullCommitPattern.MatchString(value.ResolvedCommit) {
		return nil, errWorkspaceMetadataInvalid
	}
	return &value, nil
}

func loadMetadata(path string) (metadata, error) {
	value, err := loadMetadataIfPresent(path)
	if err != nil {
		return metadata{}, err
	}
	if value == nil {
		return metadata{}, errors.New("WORKSPACE_METADATA_NOT_FOUND")
	}
	return *value, nil
}

func saveMetadata(path string, value metadata) error {
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("WORKSPACE_METADATA_ENCODE_FAILED: %w", err)
	}
	payload = append(payload, '\n')
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return fmt.Errorf("WORKSPACE_METADATA_WRITE_FAILED: %w", err)
	}
	return nil
}

type boundedBuffer struct {
	buffer bytes.Buffer
	limit  int
}

func (b *boundedBuffer) Write(payload []byte) (int, error) {
	original := len(payload)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if len(payload) > remaining {
			payload = payload[:remaining]
		}
		_, _ = b.buffer.Write(payload)
	}
	return original, nil
}

func (b *boundedBuffer) String() string { return b.buffer.String() }
