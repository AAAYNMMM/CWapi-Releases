package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/repository"
	"github.com/AAAYNMMM/CWapi/internal/workspace"
)

type mcpContextResolver struct {
	workspaces    *workspace.Manager
	gitExecutable string
	observability *observability.Hub
}

func newMCPContextResolver(manager *config.Manager, dataRoot string, hub *observability.Hub) (*mcpContextResolver, error) {
	if manager == nil {
		return nil, errors.New("MCP_CONTEXT_CONFIG_REQUIRED")
	}
	workspaces, err := workspace.NewManager(filepath.Join(dataRoot, "workspaces"))
	if err != nil {
		return nil, err
	}
	return &mcpContextResolver{
		workspaces:    workspaces,
		gitExecutable: resolveGitExecutable(),
		observability: hub,
	}, nil
}

func (r *mcpContextResolver) sweep(ctx context.Context) []error {
	if r == nil || r.workspaces == nil {
		return []error{errors.New("MCP_CONTEXT_RESOLVER_UNAVAILABLE")}
	}
	return r.workspaces.Sweep(ctx, r.gitExecutable)
}

func (r *mcpContextResolver) cleanup(ctx context.Context) []error {
	if r == nil || r.workspaces == nil {
		return []error{errors.New("MCP_CONTEXT_RESOLVER_UNAVAILABLE")}
	}
	return r.workspaces.Cleanup(ctx, r.gitExecutable)
}

func (r *mcpContextResolver) PrepareMCPContext(ctx context.Context, requestID, repositoryURL, expectedCommit string) (gateway.MCPExecutionContext, func(), error) {
	if r == nil || r.workspaces == nil {
		return gateway.MCPExecutionContext{}, nil, errors.New("MCP_CONTEXT_RESOLVER_UNAVAILABLE")
	}
	if strings.TrimSpace(r.gitExecutable) == "" {
		err := errors.New("MCP_GIT_RUNTIME_UNAVAILABLE")
		r.logWorkspaceError("repository.prepare", requestID, "", err)
		return gateway.MCPExecutionContext{}, nil, err
	}
	identity, err := repository.Parse(repositoryURL)
	if err != nil {
		return gateway.MCPExecutionContext{}, nil, err
	}
	r.configureRepositoryCredentialHelper()

	prepared, err := r.workspaces.Prepare(
		ctx,
		r.gitExecutable,
		identity.Repository,
		identity.NormalizedURL,
		requestID,
		expectedCommit,
	)
	if err != nil {
		r.logWorkspaceError("repository.prepare", requestID, identity.Repository, err)
		return gateway.MCPExecutionContext{}, nil, err
	}
	release := func() {
		if releaseErr := r.workspaces.Release(prepared); releaseErr != nil {
			r.logWorkspaceError("repository.release", requestID, identity.Repository, releaseErr)
		}
	}
	return gateway.MCPExecutionContext{
		RepositoryURL:  identity.NormalizedURL,
		ExpectedCommit: prepared.ActualSHA,
		Repository:     identity.Repository,
		CWD:            prepared.Path,
	}, release, nil
}

func (r *mcpContextResolver) logWorkspaceError(operation, requestID, repositoryName string, err error) {
	if r == nil || r.observability == nil || err == nil {
		return
	}
	fields := map[string]any{"operation": operation}
	if requestID = strings.TrimSpace(requestID); requestID != "" {
		fields["request_id"] = requestID
	}
	if repositoryName = strings.TrimSpace(repositoryName); repositoryName != "" {
		fields["repository"] = repositoryName
	}
	_, _ = r.observability.LogRuntime(context.Background(), observability.RuntimeInput{
		Level:     "error",
		Component: "workspace",
		Message:   operation + ": " + err.Error(),
		Fields:    fields,
	})
}

func (r *mcpContextResolver) configureRepositoryCredentialHelper() {
	if r == nil || r.workspaces == nil {
		return
	}
	executable := findGitHubCredentialHelper()
	configDir := ""
	if executable != "" {
		configDir = githubCredentialConfigDir()
	}
	_ = r.workspaces.ConfigureGitHubCredentialHelper(executable, configDir)
}

func findGitHubCredentialHelper() string {
	candidates := make([]string, 0, 6)
	if path, err := exec.LookPath("gh.exe"); err == nil {
		candidates = append(candidates, path)
	}
	appendCandidate := func(root string, elements ...string) {
		if root = strings.TrimSpace(root); root != "" {
			candidates = append(candidates, filepath.Join(append([]string{root}, elements...)...))
		}
	}
	appendCandidate(os.Getenv("ProgramFiles"), "GitHub CLI", "gh.exe")
	appendCandidate(os.Getenv("ProgramW6432"), "GitHub CLI", "gh.exe")
	appendCandidate(os.Getenv("ProgramFiles(x86)"), "GitHub CLI", "gh.exe")
	appendCandidate(os.Getenv("LOCALAPPDATA"), "Programs", "GitHub CLI", "gh.exe")
	appendCandidate(os.Getenv("LOCALAPPDATA"), "Microsoft", "WinGet", "Links", "gh.exe")

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		absolute, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		absolute = filepath.Clean(absolute)
		key := strings.ToLower(absolute)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		if info, err := os.Stat(absolute); err == nil && info.Mode().IsRegular() {
			return absolute
		}
	}
	return ""
}

func githubCredentialConfigDir() string {
	if configured := strings.TrimSpace(os.Getenv("GH_CONFIG_DIR")); configured != "" && filepath.IsAbs(configured) {
		return filepath.Clean(configured)
	}
	if appData := strings.TrimSpace(os.Getenv("APPDATA")); appData != "" {
		return filepath.Join(appData, "GitHub CLI")
	}
	return ""
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
	return ""
}
