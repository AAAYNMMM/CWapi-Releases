package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/repository"
	"github.com/AAAYNMMM/CWapi/internal/workspace"
)

type mcpContextResolver struct {
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
	resolver := &mcpContextResolver{
		workspaces:    workspaces,
		gitExecutable: resolveGitExecutable(),
	}
	return resolver, nil
}

func (r *mcpContextResolver) sweep(ctx context.Context) []error {
	if r == nil || r.workspaces == nil {
		return []error{errors.New("MCP_CONTEXT_RESOLVER_UNAVAILABLE")}
	}
	return r.workspaces.Sweep(ctx, r.gitExecutable)
}

func (r *mcpContextResolver) PrepareMCPContext(ctx context.Context, requestID, repositoryURL, expectedCommit string) (gateway.MCPExecutionContext, func(), error) {
	if r == nil || r.workspaces == nil {
		return gateway.MCPExecutionContext{}, nil, errors.New("MCP_CONTEXT_RESOLVER_UNAVAILABLE")
	}
	if strings.TrimSpace(r.gitExecutable) == "" {
		return gateway.MCPExecutionContext{}, nil, errors.New("MCP_GIT_RUNTIME_UNAVAILABLE")
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
		return gateway.MCPExecutionContext{}, nil, err
	}
	release := func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = r.workspaces.Remove(cleanup, r.gitExecutable, prepared)
	}
	return gateway.MCPExecutionContext{
		RepositoryURL:  identity.NormalizedURL,
		ExpectedCommit: prepared.ActualSHA,
		Repository:     identity.Repository,
		CWD:            prepared.Path,
	}, release, nil
}

// configureRepositoryCredentialHelper keeps private repository authentication
// lazy and request-scoped. It never runs a GitHub CLI version/auth probe and
// does not create a separate readiness state; Git reports credential failures.
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
