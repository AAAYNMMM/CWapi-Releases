package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/workspace"
)

func TestRepositoryRequestPreparesRemoteExactCommit(t *testing.T) {
	if os.Getenv("CWAPI_RUN_REMOTE_EXACT_COMMIT") != "1" {
		t.Skip("remote exact-commit integration gate is not enabled")
	}
	const (
		repository = "aaaynmmm/cwapi-test"
		remoteURL  = "https://github.com/AAAYNMMM/CWapi-test.git"
		commit     = "5ff5dc1dd9563731e68b3d40da82314d93adbaa8"
	)
	root := t.TempDir()
	cfg := config.Default()
	configPath := filepath.Join(root, "config", "cwapi.json")
	if err := config.SaveAtomic(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	manager, err := config.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := newMCPContextResolver(manager, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if override := strings.TrimSpace(os.Getenv("CWAPI_TEST_GIT_EXE")); override != "" {
		resolver.gitExecutable = filepath.Clean(override)
	}
	execution, release, err := resolver.PrepareMCPContext(context.Background(), "REQREMOTEEXACT", remoteURL, commit)
	if release != nil {
		defer release()
	}
	if err != nil {
		t.Fatal(err)
	}
	if execution.RepositoryURL != "https://github.com/AAAYNMMM/CWapi-test" || execution.Repository != repository || execution.ExpectedCommit != commit || !filepath.IsAbs(execution.CWD) {
		t.Fatalf("execution=%#v", execution)
	}
	head, err := workspace.Head(context.Background(), resolver.gitExecutable, execution.CWD)
	if err != nil || !strings.EqualFold(head, commit) {
		t.Fatalf("head=%q err=%v", head, err)
	}
	if _, err := os.Stat(filepath.Join(execution.CWD, "server.py")); err != nil {
		t.Fatalf("exact commit server.py missing: %v", err)
	}
}
