package app

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"

	"github.com/AAAYNMMM/CWapi/internal/buildinfo"
	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/processruntime"
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
	config          *config.Manager
	state           *state.Store
	observability   *observability.Hub
	gateway         *gateway.Gateway
	slack           *slackRuntime
	codexHost       *codex.MCPHost
	processRuntime  *processruntime.Runtime
	contextResolver *mcpContextResolver
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
	if manager.Snapshot().PermissionMode != config.PermissionModeSafe {
		if _, err := manager.Update(func(candidate *config.Config) error {
			candidate.PermissionMode = config.PermissionModeSafe
			return nil
		}); err != nil {
			return nil, fmt.Errorf("CONFIG_STARTUP_SAFE_FAILED: %w", err)
		}
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
		return codex.PermissionConfig{ProfileID: profileID}
	})
	processRuntime, err := processruntime.NewRuntime(codexService, manager, dataRoot)
	if err != nil {
		codexHost.Close()
		return fail(err)
	}
	contextResolver, err := newMCPContextResolver(manager, dataRoot)
	if err != nil {
		processRuntime.Close()
		codexHost.Close()
		return fail(err)
	}
	slackRuntime := newSlackRuntime(manager, hub)
	requestGateway, err := gateway.NewMCP(manager, stateStore, gatewaySlackPoster{runtime: slackRuntime}, hub)
	if err != nil {
		processRuntime.Close()
		codexHost.Close()
		return fail(err)
	}
	slackRuntime.SetProtocolHandler(stateStore, requestGateway.HandleMCPMessage)

	hub.SetComponent("config", "healthy", "authoritative config ready")
	hub.SetComponent("mcp-relay", "healthy", "MCP-only Slack relay ready")
	codexRuntime := codexService.Snapshot()
	if codexRuntime.Configured {
		hub.SetComponent("codex", "healthy", "stock Codex runtime verified")
	} else {
		hub.SetComponent("codex", "degraded", "Codex runtime missing or unverified")
	}
	if err := attachGatewayMCPRuntime(requestGateway, codexHost, contextResolver, processRuntime); err != nil {
		processRuntime.Close()
		codexHost.Close()
		return fail(err)
	}
	hub.SetComponent("codex-mcp", "healthy", "stock MCP relay and Core process runtime attached")
	sweepFailures := contextResolver.sweep(context.Background())
	if len(sweepFailures) == 0 {
		hub.SetComponent("workspace", "healthy", "ephemeral worktree root swept")
	} else {
		hub.SetComponent("workspace", "degraded", "one or more stale workspace items need attention")
		for _, sweepErr := range sweepFailures {
			_, _ = hub.LogRuntime(context.Background(), observability.RuntimeInput{
				Level: "error", Component: "workspace", Message: "startup.sweep: " + sweepErr.Error(),
				Fields: map[string]any{"operation": "startup.sweep"},
			})
		}
	}
	hub.SetComponent("desktop", "healthy", "Wails desktop workflow bindings ready")
	hub.SetComponent("slack", "setup_required", "Slack credentials and channel are not ready")
	if _, err := hub.LogRuntime(context.Background(), observability.RuntimeInput{
		Level:     "info",
		Component: "core",
		Message:   "Go Core MCP relay, stock Codex app-server and Slack initialized",
		Fields:    map[string]any{"stage": "v1.6.1"},
	}); err != nil {
		processRuntime.Close()
		codexHost.Close()
		return fail(err)
	}
	return &Service{
		config: manager, state: stateStore, observability: hub,
		gateway: requestGateway, slack: slackRuntime, codexHost: codexHost,
		processRuntime: processRuntime, contextResolver: contextResolver,
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
	if s.processRuntime != nil {
		s.processRuntime.Close()
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
	s.recordOperationalError("core", operation, err)
}

func (s *Service) RuntimeSnapshot() RuntimeSnapshot {
	return RuntimeSnapshot{
		Version: buildinfo.Version, SourceCommit: buildinfo.Commit(),
		Architecture: "go-core+wails-v2+react-typescript", Core: "go", Desktop: "wails-v2",
		Platform: runtime.GOOS + "/" + runtime.GOARCH, Stage: "v1.6.1",
	}
}
