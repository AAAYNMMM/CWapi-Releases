package executionpolicy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckDirectRulesAndProtectedPaths(t *testing.T) {
	root := t.TempDir()
	repository := filepath.Join(root, "workspaces", "git", "worktrees", "request")
	dataRoot := filepath.Join(root, "data")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(root, "runtime", "node.exe")
	if err := os.MkdirAll(filepath.Dir(node), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(node, []byte("test"), 0o600); err != nil {
		t.Fatal(err)
	}
	allowed := Invocation{Executable: node, Argv: []string{"-e", `require("node:fs").writeFileSync("outside.txt","ok")`}, CWD: repository}
	if err := Check(allowed, repository, dataRoot); err != nil {
		t.Fatalf("nested script semantics must remain out of scope: %v", err)
	}
	for _, invocation := range []Invocation{
		{Executable: filepath.Join(root, "taskkill.exe"), CWD: repository},
		{Executable: filepath.Join(root, "git.exe"), Argv: []string{"commit", "-m", "x"}, CWD: repository},
		{Executable: filepath.Join(root, "git.exe"), Argv: []string{"branch", "-D", "main"}, CWD: repository},
		{Executable: filepath.Join(dataRoot, "state", "helper.exe"), CWD: repository},
		{Executable: node, Argv: []string{filepath.Join(dataRoot, "state", "cwapi.db")}, CWD: repository},
	} {
		if err := Check(invocation, repository, dataRoot); err == nil || !strings.HasPrefix(err.Error(), "PERMANENT_POLICY_DENIED") {
			t.Fatalf("invocation should be denied: %#v err=%v", invocation, err)
		}
	}
	if err := Check(Invocation{Executable: node, Argv: []string{filepath.Join(repository, "output.txt")}, CWD: repository}, repository, dataRoot); err != nil {
		t.Fatalf("owned repository path denied: %v", err)
	}
}

func TestCodexRulesComeFromMatcherDefinitions(t *testing.T) {
	rules := CodexRules()
	for _, expected := range []string{`pattern=["taskkill"]`, `pattern=["git", "commit"]`, `"--force"`} {
		if !strings.Contains(rules, expected) {
			t.Fatalf("generated rules missing %s", expected)
		}
	}
}
