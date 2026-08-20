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

func TestConfiguredProjectPreparesRemoteExactCommit(t *testing.T) {
	if os.Getenv("CWAPI_RUN_REMOTE_EXACT_COMMIT") != "1" {
		t.Skip("remote exact-commit integration gate is not enabled")
	}
	const projectID = "prj-cccccccccccccccccccccccc"
	repository := strings.TrimSpace(os.Getenv("CWAPI_REMOTE_EXACT_REPOSITORY"))
	remoteURL := strings.TrimSpace(os.Getenv("CWAPI_REMOTE_EXACT_URL"))
	commit := strings.TrimSpace(os.Getenv("CWAPI_REMOTE_EXACT_COMMIT"))
	if repository == "" || remoteURL == "" || commit == "" {
		t.Fatal("CWAPI_REMOTE_EXACT_REPOSITORY, CWAPI_REMOTE_EXACT_URL and CWAPI_REMOTE_EXACT_COMMIT are required")
	}
	root := t.TempDir()
	projectPath := filepath.Join(root, "configured-project")
	if err := os.MkdirAll(projectPath, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Projects = []config.Project{{
		ID: projectID, DisplayName: "remote-exact-project", Repository: repository,
		LocalPath: projectPath, RemoteURL: remoteURL,
	}}
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
	execution, release, err := resolver.PrepareMCPContext(context.Background(), "REQREMOTEEXACT", projectID, commit)
	if release != nil {
		defer release()
	}
	if err != nil {
		t.Fatal(err)
	}
	if execution.ProjectID != projectID || execution.Repository != repository || execution.ExpectedCommit != commit || !filepath.IsAbs(execution.CWD) {
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
