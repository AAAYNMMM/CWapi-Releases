package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSweepDeletesOnlyEphemeralChildrenAndPreservesMirrors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	worktreeRoot := filepath.Join(root, "git", "worktrees")
	mirror := filepath.Join(root, "git", "mirrors", "repo.git")
	stale := filepath.Join(worktreeRoot, "stale", "nested")
	for _, directory := range []string{stale, mirror} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stale, "state.txt"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external")
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(external, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(worktreeRoot, "external-link")
	linked := createDirectoryLink(external, link) == nil

	failures := manager.Sweep(context.Background(), "")
	if _, err := os.Stat(filepath.Join(worktreeRoot, "stale")); !os.IsNotExist(err) {
		t.Fatalf("stale tree remained: %v", err)
	}
	if _, err := os.Stat(mirror); err != nil {
		t.Fatalf("mirror was removed: %v", err)
	}
	if len(failures) != 1 || failures[0].Error() != "WORKSPACE_GIT_RUNTIME_UNAVAILABLE" {
		t.Fatalf("unexpected degraded results: %#v", failures)
	}
	if linked {
		if _, err := os.Lstat(link); !os.IsNotExist(err) {
			t.Fatalf("reparse link remained: %v", err)
		}
		if payload, err := os.ReadFile(marker); err != nil || string(payload) != "keep" {
			t.Fatalf("sweep followed link: payload=%q err=%v", payload, err)
		}
	}
}

func TestSweepBlocksReparsedWorktreeRootWithoutFollowingIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	external := filepath.Join(t.TempDir(), "external")
	if err := os.MkdirAll(filepath.Join(root, "git"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(external, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	worktreeRoot := filepath.Join(root, "git", "worktrees")
	if err := createDirectoryLink(external, worktreeRoot); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	failures := manager.Sweep(context.Background(), "")
	if len(failures) != 1 || manager.blocked == nil {
		t.Fatalf("reparsed root was not blocked: failures=%#v blocked=%v", failures, manager.blocked)
	}
	if payload, err := os.ReadFile(marker); err != nil || string(payload) != "keep" {
		t.Fatalf("sweep followed root reparse point: payload=%q err=%v", payload, err)
	}
}

func TestPrepareRejectsReparsedMirrorWithoutFollowingIt(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspaces")
	external := filepath.Join(t.TempDir(), "external")
	if err := os.MkdirAll(filepath.Join(root, "git", "mirrors"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(external, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(external, "marker.txt")
	if err := os.WriteFile(marker, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "git", "mirrors", repositoryKey("owner/repo")+".git")
	if err := createDirectoryLink(external, link); err != nil {
		t.Skipf("directory link creation unavailable: %v", err)
	}
	gitExecutable, err := exec.LookPath("git.exe")
	if err != nil {
		gitExecutable, err = exec.LookPath("git")
	}
	if err != nil {
		t.Skipf("Git unavailable: %v", err)
	}
	gitExecutable, err = filepath.Abs(gitExecutable)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(root)
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Prepare(context.Background(), gitExecutable, "owner/repo", "https://github.com/owner/repo", "REQ", strings.Repeat("a", 40))
	if err == nil || !strings.Contains(err.Error(), "MIRROR_INTEGRITY_INVALID") {
		t.Fatalf("reparsed mirror accepted: %v", err)
	}
	if payload, readErr := os.ReadFile(marker); readErr != nil || string(payload) != "keep" {
		t.Fatalf("prepare followed mirror link: payload=%q err=%v", payload, readErr)
	}
}
