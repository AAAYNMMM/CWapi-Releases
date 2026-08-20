package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/workspace"
)

type mcpContextResolver struct {
	config        *config.Manager
	workspaces    *workspace.Manager
	gitExecutable string
}

func newMCPContextResolver(manager *config.Manager, dataRoot string) (*mcpContextResolver, error) {
	if manager == nil {
		return nil, errors.New("MCP_CONTEXT_CONFIG_REQUIRED")
	}
	workspaces, err := workspace.NewManager(filepath.Join(dataRoot, "workspaces"))
	if err != nil {
		return nil, err
	}
	return &mcpContextResolver{
		config:        manager,
		workspaces:    workspaces,
		gitExecutable: resolveGitExecutable(),
	}, nil
}

func (r *mcpContextResolver) PrepareMCPContext(ctx context.Context, _ string, projectID, expectedCommit string) (gateway.MCPExecutionContext, func(), error) {
	if r == nil || r.config == nil || r.workspaces == nil {
		return gateway.MCPExecutionContext{}, nil, errors.New("MCP_CONTEXT_RESOLVER_UNAVAILABLE")
	}
	if strings.TrimSpace(r.gitExecutable) == "" {
		return gateway.MCPExecutionContext{}, nil, errors.New("MCP_GIT_RUNTIME_UNAVAILABLE")
	}
	cfg := r.config.Snapshot()
	var selected *config.Project
	for index := range cfg.Projects {
		if cfg.Projects[index].ID == projectID {
			project := cfg.Projects[index]
			selected = &project
			break
		}
	}
	if selected == nil {
		return gateway.MCPExecutionContext{}, nil, fmt.Errorf("MCP_PROJECT_NOT_FOUND: %s", projectID)
	}

	prepared, err := r.workspaces.Prepare(
		ctx,
		r.gitExecutable,
		selected.Repository,
		selected.RemoteURL,
		selected.ID+":"+strings.ToLower(expectedCommit),
		expectedCommit,
	)
	if err != nil {
		return gateway.MCPExecutionContext{}, nil, err
	}
	// Exact-commit workspaces are managed caches. Keeping their stable path lets
	// the Codex MCP context preserve state across consecutive tool calls.
	return gateway.MCPExecutionContext{
		ProjectID:      selected.ID,
		ExpectedCommit: prepared.ActualSHA,
		Repository:     selected.Repository,
		CWD:            prepared.Path,
	}, nil, nil
}

func resolveGitExecutable() string {
	if executable, err := os.Executable(); err == nil {
		installRoot := filepath.Dir(executable)
		for _, candidate := range []string{
			filepath.Join(installRoot, "runtime", "git", "cmd", "git.exe"),
			filepath.Join(installRoot, "runtime", "git", "current", "cmd", "git.exe"),
			filepath.Join(installRoot, "runtime", "git", "bin", "git.exe"),
		} {
			if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
				return candidate
			}
		}
	}
	if executable, err := exec.LookPath("git"); err == nil {
		return executable
	}
	return ""
}
