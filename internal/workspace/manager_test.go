package workspace

import (
	"context"
	"errors"
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

func TestManagerRecreatesEvenCleanExactWorkspace(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(source, ".gitignore"), []byte("ignored.tmp\n"), 0o600); err != nil {
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
	ignored := filepath.Join(first.Path, "ignored.tmp")
	if err := os.WriteFile(ignored, []byte("request-private state"), 0o600); err != nil {
		t.Fatal(err)
	}
	if clean, err := IsClean(context.Background(), git, first.Path); err != nil || !clean {
		t.Fatalf("ignored fixture must leave Git clean: clean=%v err=%v", clean, err)
	}
	second, err := manager.Prepare(context.Background(), git, "owner/repo", source, "stable-key", sha)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Remove(context.Background(), git, second)
	if first.Path != second.Path || first.ActualSHA != second.ActualSHA {
		t.Fatalf("workspace identity changed unexpectedly: first=%#v second=%#v", first, second)
	}
	if _, err := os.Stat(ignored); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("clean mutable tree was reused: %v", err)
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

func TestGitHubCredentialHelperEnvironmentIsProcessLocalAndSecretFree(t *testing.T) {
	root := t.TempDir()
	manager, err := NewManager(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	gh := filepath.Join(root, "GitHub CLI", "gh.exe")
	if err := os.MkdirAll(filepath.Dir(gh), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gh, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(root, "gh-config")
	if err := manager.ConfigureGitHubCredentialHelper(gh, configDir); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"GH_TOKEN", "GITHUB_TOKEN", "GIT_TRACE", "GIT_CURL_VERBOSE", "GH_DEBUG", "CWAPI_SECRET", "SLACK_BOT_TOKEN", "OPENAI_API_KEY"} {
		t.Setenv(key, "must-not-leak")
	}
	environment := strings.Join(manager.gitEnvironment(), "\n")
	for _, expected := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_COUNT=1", "credential.https://github.com.helper", filepath.ToSlash(gh), "GH_CONFIG_DIR=" + configDir} {
		if !strings.Contains(environment, expected) {
			t.Fatalf("credential environment missing %q: %s", expected, environment)
		}
	}
	if strings.Contains(environment, "must-not-leak") {
		t.Fatalf("credential environment leaked host secret: %s", environment)
	}
}
