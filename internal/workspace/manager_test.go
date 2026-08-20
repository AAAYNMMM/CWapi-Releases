package workspace

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerCreatesExactDetachedCleanWorktree(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		cmd := exec.Command(git, args...)
		cmd.Dir = source
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("git %v: %v\n%s", args, runErr, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "cwapi-test@example.invalid")
	run("config", "user.name", "CWapi Test")
	if err := os.WriteFile(filepath.Join(source, "value.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", "value.txt")
	run("commit", "-m", "first")
	sha := strings.ToLower(run("rev-parse", "HEAD"))

	manager, err := NewManager(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := manager.Prepare(context.Background(), git, "owner/repo", source, "TSK012345", sha)
	if err != nil {
		t.Fatal(err)
	}
	if workspace.ActualSHA != sha || !workspace.CleanBefore || workspace.Path == source {
		t.Fatalf("workspace=%#v", workspace)
	}
	if head, err := Head(context.Background(), git, workspace.Path); err != nil || head != sha {
		t.Fatalf("head=%s err=%v", head, err)
	}
	if clean, err := IsClean(context.Background(), git, workspace.Path); err != nil || !clean {
		t.Fatalf("clean=%v err=%v", clean, err)
	}
	if err := os.WriteFile(filepath.Join(workspace.Path, "dirty.txt"), []byte("dirty"), 0o600); err != nil {
		t.Fatal(err)
	}
	if clean, err := IsClean(context.Background(), git, workspace.Path); err != nil || clean {
		t.Fatalf("expected dirty worktree clean=%v err=%v", clean, err)
	}
	if err := manager.Remove(context.Background(), git, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace.Path); !os.IsNotExist(err) {
		t.Fatalf("worktree still exists: %v", err)
	}
}

func TestManagerFetchesNewExactCommitIntoExistingMirror(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	source := filepath.Join(t.TempDir(), "source")
	_ = os.MkdirAll(source, 0o700)
	run := func(args ...string) string {
		cmd := exec.Command(git, args...)
		cmd.Dir = source
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("git %v: %v\n%s", args, runErr, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "cwapi-test@example.invalid")
	run("config", "user.name", "CWapi Test")
	_ = os.WriteFile(filepath.Join(source, "value.txt"), []byte("one\n"), 0o600)
	run("add", ".")
	run("commit", "-m", "first")
	first := strings.ToLower(run("rev-parse", "HEAD"))
	manager, _ := NewManager(filepath.Join(t.TempDir(), "data"))
	ws1, err := manager.Prepare(context.Background(), git, "owner/repo", source, "TSK111", first)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Remove(context.Background(), git, ws1); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(source, "value.txt"), []byte("two\n"), 0o600)
	run("add", ".")
	run("commit", "-m", "second")
	second := strings.ToLower(run("rev-parse", "HEAD"))
	ws2, err := manager.Prepare(context.Background(), git, "owner/repo", source, "TSK222", second)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Remove(context.Background(), git, ws2)
	if ws2.ActualSHA != second || second == first {
		t.Fatalf("first=%s second=%s workspace=%#v", first, second, ws2)
	}
}

func TestManagerReusesCleanExactWorkspace(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable")
	}
	source := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(source, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		cmd := exec.Command(git, args...)
		cmd.Dir = source
		output, runErr := cmd.CombinedOutput()
		if runErr != nil {
			t.Fatalf("git %v: %v\n%s", args, runErr, output)
		}
		return strings.TrimSpace(string(output))
	}
	run("init", "--initial-branch=main")
	run("config", "user.email", "cwapi-test@example.invalid")
	run("config", "user.name", "CWapi Test")
	if err := os.WriteFile(filepath.Join(source, "value.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "first")
	sha := strings.ToLower(run("rev-parse", "HEAD"))
	manager, err := NewManager(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := manager.Prepare(context.Background(), git, "owner/repo", source, "stable-key", sha)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Prepare(context.Background(), git, "owner/repo", source, "stable-key", sha)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Remove(context.Background(), git, second)
	if first.Path != second.Path || first.ActualSHA != second.ActualSHA {
		t.Fatalf("workspace was not reused: first=%#v second=%#v", first, second)
	}
}

func TestSafeTaskIDKeepsDistinctProtocolIdentitiesDistinct(t *testing.T) {
	withColon := safeTaskID("REQ:workspace")
	withoutColon := safeTaskID("REQworkspace")
	withDot := safeTaskID("REQ.workspace")
	if withColon == withoutColon || withDot == withoutColon || withColon == withDot {
		t.Fatalf("workspace task IDs collided: colon=%q plain=%q dot=%q", withColon, withoutColon, withDot)
	}
}
