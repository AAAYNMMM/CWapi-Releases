package app

import (
	"context"
	"path/filepath"
	"runtime"

	"github.com/AAAYNMMM/CWapi/internal/buildinfo"
	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/projects"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

type RuntimeSnapshot struct {
	Version      string `json:"version"`
	SourceCommit string `json:"source_commit"`
	Architecture string `json:"architecture"`
	Core         string `json:"core"`
	Desktop      string `json:"desktop"`
	Platform     string `json:"platform"`
	Stage        string `json:"stage"`
}

type Service struct {
	config        *config.Manager
	projects      *projects.Registry
	state         *state.Store
	observability *observability.Hub
	gateway       *gateway.Gateway
	slack         *slackRuntime
	codexHost     *codex.MCPHost
}

func NewService() (*Service, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	return NewServiceWithConfigPath(path)
}

func NewServiceWithConfigPath(configPath string) (*Service, error) {
	configDir := filepath.Dir(configPath)
	dataRoot := configDir
	if filepath.Base(configDir) == "config" {
		dataRoot = filepath.Dir(configDir)
	}
	return NewServiceWithPaths(configPath, filepath.Join(dataRoot, "state", "cwapi.db"))
}

func NewServiceWithPaths(configPath, statePath string) (*Service, error) {
	manager, err := config.Open(configPath)
	if err != nil {
		return nil, err
	}
	stateStore, err := state.Open(statePath)
	if err != nil {
		return nil, err
	}
	fail := func(cause error) (*Service, error) {
		_ = stateStore.Close()
		return nil, cause
	}
	if err := stateStore.ResetRuntimeSession(context.Background()); err != nil {
		return fail(err)
	}
	hub, err := observability.New(context.Background(), stateStore, 250, 5000)
	if err != nil {
		return fail(err)
	}
	dataRoot := resolveDataRoot(configPath, statePath)
	codexService, err := codex.NewService(dataRoot)
	if err != nil {
		return fail(err)
	}
	codexHost := codex.NewMCPHost(codexService, func() codex.PermissionConfig {
		cfg := manager.Snapshot()
		profileID := codex.PermissionProfileSafe
		if config.EffectivePermissionMode(cfg.PermissionMode) == config.PermissionModeFullAccess {
			profileID = codex.PermissionProfileFullAccess
		}
		paths := make([]string, 0, len(cfg.Projects))
		for _, project := range cfg.Projects {
			paths = append(paths, project.LocalPath)
		}
		return codex.PermissionConfig{ProfileID: profileID, ProjectPaths: paths}
	})
	contextResolver, err := newMCPContextResolver(manager, dataRoot)
	if err != nil {
		codexHost.Close()
		return fail(err)
	}
	slackRuntime := newSlackRuntime(manager, hub)
	requestGateway, err := gateway.NewMCP(manager, stateStore, gatewaySlackPoster{runtime: slackRuntime}, hub)
	if err != nil {
		codexHost.Close()
		return fail(err)
	}
	slackRuntime.SetProtocolHandler(stateStore, requestGateway.HandleMCPMessage)

	hub.SetComponent("config", "healthy", "authoritative config ready")
	hub.SetComponent("projects", "healthy", "project registry ready")
	hub.SetComponent("mcp-relay", "healthy", "MCP-only Slack relay ready")
	codexRuntime := codexService.Snapshot()
	if codexRuntime.Configured {
		hub.SetComponent("codex", "healthy", "stock Codex runtime verified")
		if err := attachGatewayMCPRuntime(requestGateway, codexHost, contextResolver); err != nil {
			hub.SetComponent("codex-mcp", "degraded", "Codex MCP relay unavailable")
			_, _ = hub.RecordError(context.Background(), observability.ErrorInput{
				Component: "codex-mcp",
				Operation: "runtime.attach",
				Message:   err.Error(),
			})
		} else {
			hub.SetComponent("codex-mcp", "healthy", "stock Codex app-server MCP relay with exact-commit contexts attached")
		}
	} else {
		hub.SetComponent("codex", "degraded", "Codex runtime missing or unverified")
		hub.SetComponent("codex-mcp", "degraded", "Codex MCP relay unavailable")
	}
	hub.SetComponent("desktop", "healthy", "Wails desktop workflow bindings ready")
	hub.SetComponent("slack", "setup_required", "Slack credentials and channel are not ready")
	if _, err := hub.LogRuntime(context.Background(), observability.RuntimeInput{
		Level:     "info",
		Component: "core",
		Message:   "Go Core MCP relay, stock Codex app-server and Slack initialized",
		Fields:    map[string]any{"stage": "S2.4"},
	}); err != nil {
		codexHost.Close()
		return fail(err)
	}
	return &Service{
		config: manager, projects: projects.NewRegistry(manager), state: stateStore, observability: hub,
		gateway: requestGateway, slack: slackRuntime, codexHost: codexHost,
	}, nil
}

func resolveDataRoot(configPath, statePath string) string {
	stateDir := filepath.Dir(statePath)
	if filepath.Base(stateDir) == "state" {
		return filepath.Dir(stateDir)
	}
	configDir := filepath.Dir(configPath)
	if filepath.Base(configDir) == "config" {
		return filepath.Dir(configDir)
	}
	return stateDir
}

func (s *Service) Start(ctx context.Context) {
	if s == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.slack != nil {
		s.slack.Start(ctx)
	}
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	if s.slack != nil {
		s.slack.Close()
	}
	if s.codexHost != nil {
		s.codexHost.Close()
	}
	if s.state == nil {
		return nil
	}
	return s.state.Close()
}

func (s *Service) recordLifecycleError(operation string, err error) {
	if s == nil || s.observability == nil || err == nil {
		return
	}
	_, _ = s.observability.RecordError(context.Background(), observability.ErrorInput{
		Component: "core",
		Operation: operation,
		Message:   err.Error(),
	})
}

func (s *Service) RuntimeSnapshot() RuntimeSnapshot {
	return RuntimeSnapshot{
		Version: buildinfo.Version, SourceCommit: buildinfo.Commit(),
		Architecture: "go-core+wails-v2+react-typescript", Core: "go", Desktop: "wails-v2",
		Platform: runtime.GOOS + "/" + runtime.GOARCH, Stage: "S2.4",
	}
}
