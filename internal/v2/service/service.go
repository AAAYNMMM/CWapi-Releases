package service

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/v2/agentbroker"
	"github.com/AAAYNMMM/CWapi/internal/v2/codextoolhost"
	"github.com/AAAYNMMM/CWapi/internal/v2/coding"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
	"github.com/AAAYNMMM/CWapi/internal/v2/mcpserver"
	"github.com/AAAYNMMM/CWapi/internal/v2/tunnel"
	"github.com/AAAYNMMM/CWapi/internal/v2/workspace"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type CodingLifecycle interface {
	CloseAll(context.Context) error
}

type CodingAccessProfile interface {
	SetAccessProfile(string) error
}

type AgentProviderLifecycle interface {
	Start(context.Context) error
	Close(context.Context) error
	Snapshot() agentbroker.ProviderSnapshot
	Endpoint() string
}

type TunnelLifecycle interface {
	Start(context.Context) error
	Close(context.Context) error
	Snapshot() tunnel.Snapshot
}

type Dependencies struct {
	Coding          mcpserver.CodingService
	CodingLifecycle CodingLifecycle
	CodingSnapshot  func() coding.RuntimeSnapshot
	WorkspaceIndex  func() workspace.IndexSnapshot
	CodexProbe      func() codextoolhost.RuntimeSnapshot
	Agent           mcpserver.AgentService
	AgentBroker     *agentbroker.Broker
	AgentProvider   AgentProviderLifecycle
	Tunnel          TunnelLifecycle
	AgentTunnel     TunnelLifecycle
}

type Snapshot struct {
	State              string                        `json:"state"`
	MCP                mcpserver.Snapshot            `json:"mcp"`
	CodexAccessProfile string                        `json:"codex_access_profile"`
	Codex              codextoolhost.RuntimeSnapshot `json:"codex"`
	Coding             coding.RuntimeSnapshot        `json:"coding"`
	Workspaces         workspace.IndexSnapshot       `json:"workspaces"`
	AgentEnabled       bool                          `json:"agent_enabled"`
	AgentProvider      agentbroker.ProviderSnapshot  `json:"agent_provider"`
	Agent              agentbroker.Snapshot          `json:"agent"`
	OpenAITunnel       tunnel.Snapshot               `json:"openai_tunnel"`
	AgentOpenAITunnel  tunnel.Snapshot               `json:"agent_openai_tunnel"`
	LastError          string                        `json:"last_error,omitempty"`
}

type Service struct {
	mu          sync.RWMutex
	lifecycleMu sync.Mutex

	configPath      string
	config          v2config.Config
	mcp             *mcpserver.Runtime
	codingLifecycle CodingLifecycle
	codingAccess    CodingAccessProfile
	codingSnapshot  func() coding.RuntimeSnapshot
	workspaceIndex  func() workspace.IndexSnapshot
	codexProbe      func() codextoolhost.RuntimeSnapshot
	agentBroker     *agentbroker.Broker
	agentProvider   AgentProviderLifecycle
	tunnel          TunnelLifecycle
	agentTunnel     TunnelLifecycle
	state           string
	lastError       string
	closed          bool
}

func New(configPath string, deps Dependencies) (*Service, error) {
	resolvedPath, err := resolveConfigPath(configPath)
	if err != nil {
		return nil, err
	}
	cfg, err := v2config.LoadOrCreate(resolvedPath)
	if err != nil {
		return nil, err
	}
	return newConfigured(resolvedPath, cfg, deps)
}

func resolveConfigPath(configPath string) (string, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		var err error
		configPath, err = v2config.DefaultPath()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return "", err
	}
	return filepath.Clean(absolute), nil
}

func newConfigured(configPath string, cfg v2config.Config, deps Dependencies) (*Service, error) {
	if deps.Coding == nil {
		return nil, errors.New("V2_CODING_SERVICE_REQUIRED")
	}
	if cfg.Agent.Enabled {
		if deps.Agent == nil {
			return nil, errors.New("V2_AGENT_SERVICE_REQUIRED")
		}
		if deps.AgentBroker == nil || deps.AgentProvider == nil {
			return nil, errors.New("V2_AGENT_RUNTIME_REQUIRED")
		}
	}
	if cfg.Tunnel.Enabled && deps.Tunnel == nil {
		return nil, errors.New("V2_TUNNEL_RUNTIME_REQUIRED")
	}
	if cfg.AgentTunnel.Enabled && deps.AgentTunnel == nil {
		return nil, errors.New("V2_AGENT_TUNNEL_RUNTIME_REQUIRED")
	}
	codingRegistrar := func(server *mcp.Server) error { return mcpserver.RegisterCoding(server, deps.Coding) }
	var agentRegistrar mcpserver.Registrar
	if cfg.Agent.Enabled {
		agentRegistrar = func(server *mcp.Server) error { return mcpserver.RegisterAgent(server, deps.Agent) }
	}
	gateway, err := mcpserver.New(cfg.MCP, codingRegistrar, agentRegistrar)
	if err != nil {
		return nil, err
	}
	codingAccess, _ := deps.Coding.(CodingAccessProfile)
	return &Service{
		configPath: configPath, config: cfg, mcp: gateway,
		codingLifecycle: deps.CodingLifecycle, codingAccess: codingAccess, codingSnapshot: deps.CodingSnapshot,
		workspaceIndex: deps.WorkspaceIndex, codexProbe: deps.CodexProbe,
		agentBroker: deps.AgentBroker, agentProvider: deps.AgentProvider,
		tunnel: deps.Tunnel, agentTunnel: deps.AgentTunnel,
		state: "stopped",
	}, nil
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil || s.mcp == nil {
		return errors.New("V2_SERVICE_UNAVAILABLE")
	}
	s.lifecycleMu.Lock()
	if s.closed {
		s.lifecycleMu.Unlock()
		return errors.New("V2_SERVICE_CLOSED")
	}
	if err := s.mcp.Start(nil); err != nil {
		s.failStartLocked(err)
		s.lifecycleMu.Unlock()
		return err
	}
	if s.config.Agent.Enabled && s.agentProvider != nil {
		if err := s.agentProvider.Start(nil); err != nil {
			closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.mcp.Close(closeCtx)
			cancel()
			s.failStartLocked(err)
			s.lifecycleMu.Unlock()
			return err
		}
	}
	var tunnelErrors []error
	if s.tunnel != nil {
		if err := s.tunnel.Start(nil); err != nil {
			tunnelErrors = append(tunnelErrors, err)
		}
	}
	if s.agentTunnel != nil {
		if err := s.agentTunnel.Start(nil); err != nil {
			tunnelErrors = append(tunnelErrors, err)
		}
	}
	s.mu.Lock()
	s.state = "running"
	if tunnelErr := errors.Join(tunnelErrors...); tunnelErr != nil {
		s.lastError = tunnelErr.Error()
	} else {
		s.lastError = ""
	}
	s.mu.Unlock()
	s.lifecycleMu.Unlock()
	if ctx != nil && ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = s.Close(closeCtx)
		}()
	}
	return nil
}

func (s *Service) failStartLocked(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = "failed"
	if err != nil {
		s.lastError = err.Error()
	}
}

func (s *Service) Close(ctx context.Context) error {
	if s == nil || s.mcp == nil {
		return nil
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.mu.Lock()
	s.state = "stopping"
	s.mu.Unlock()
	var tunnelErrors []error
	if s.tunnel != nil {
		if err := s.tunnel.Close(ctx); err != nil {
			tunnelErrors = append(tunnelErrors, err)
		}
	}
	if s.agentTunnel != nil {
		if err := s.agentTunnel.Close(ctx); err != nil {
			tunnelErrors = append(tunnelErrors, err)
		}
	}
	var agentErr error
	if s.agentProvider != nil {
		agentErr = s.agentProvider.Close(ctx)
	}
	var codingErr error
	if s.codingLifecycle != nil {
		codingErr = s.codingLifecycle.CloseAll(ctx)
	}
	// Stop request owners before shutting down the HTTP server. Otherwise an
	// active coding_exec handler can keep Shutdown waiting until the close
	// deadline, and the command is only canceled after that deadline expires.
	mcpErr := s.mcp.Close(ctx)
	err := errors.Join(errors.Join(tunnelErrors...), agentErr, mcpErr, codingErr)
	s.mu.Lock()
	s.state = "stopped"
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
	}
	s.mu.Unlock()
	return err
}

func (s *Service) Snapshot() Snapshot {
	if s == nil {
		return Snapshot{State: "unavailable"}
	}
	s.mu.RLock()
	state, lastError := s.state, s.lastError
	cfg := s.config
	s.mu.RUnlock()
	provider := agentbroker.ProviderSnapshot{State: "disabled"}
	broker := agentbroker.Snapshot{BridgeState: "OFFLINE"}
	openAITunnel := tunnel.Snapshot{State: "disabled"}
	agentOpenAITunnel := tunnel.Snapshot{State: "disabled"}
	if s.agentProvider != nil {
		provider = s.agentProvider.Snapshot()
	}
	if s.agentBroker != nil {
		broker = s.agentBroker.Snapshot()
	}
	if s.tunnel != nil {
		openAITunnel = s.tunnel.Snapshot()
	}
	if s.agentTunnel != nil {
		agentOpenAITunnel = s.agentTunnel.Snapshot()
	}
	var codexState codextoolhost.RuntimeSnapshot
	if s.codexProbe != nil {
		codexState = s.codexProbe()
	} else {
		codexState = codextoolhost.RuntimeSnapshot{State: "unknown", AccessProfile: cfg.Codex.AccessProfile}
	}
	codexState.AccessProfile = cfg.Codex.AccessProfile
	codingState := coding.RuntimeSnapshot{State: "unknown"}
	if s.codingSnapshot != nil {
		codingState = s.codingSnapshot()
	}
	var workspaceState workspace.IndexSnapshot
	if s.workspaceIndex != nil {
		workspaceState = s.workspaceIndex()
	}
	return Snapshot{
		State: state, MCP: s.mcp.Snapshot(), CodexAccessProfile: cfg.Codex.AccessProfile,
		Codex: codexState, Coding: codingState, Workspaces: workspaceState,
		AgentEnabled: cfg.Agent.Enabled, AgentProvider: provider, Agent: broker,
		OpenAITunnel: openAITunnel, AgentOpenAITunnel: agentOpenAITunnel, LastError: lastError,
	}
}

func (s *Service) UpdateCodexAccessProfile(profile string) (Snapshot, error) {
	if s == nil {
		return Snapshot{State: "unavailable"}, errors.New("V2_SERVICE_UNAVAILABLE")
	}
	profile = strings.ToLower(strings.TrimSpace(profile))
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	if s.closed {
		return s.Snapshot(), errors.New("V2_SERVICE_CLOSED")
	}
	s.mu.RLock()
	before := s.config
	configPath := s.configPath
	setter := s.codingAccess
	s.mu.RUnlock()
	candidate := before
	candidate.Codex.AccessProfile = profile
	if err := v2config.Validate(candidate); err != nil {
		return s.Snapshot(), err
	}
	if candidate.Codex.AccessProfile == before.Codex.AccessProfile {
		return s.Snapshot(), nil
	}
	if setter == nil {
		return s.Snapshot(), errors.New("V2_CODING_ACCESS_PROFILE_UNAVAILABLE")
	}
	if err := v2config.SaveAtomic(configPath, candidate); err != nil {
		return s.Snapshot(), err
	}
	if err := setter.SetAccessProfile(candidate.Codex.AccessProfile); err != nil {
		rollbackErr := v2config.SaveAtomic(configPath, before)
		return s.Snapshot(), errors.Join(err, rollbackErr)
	}
	s.mu.Lock()
	s.config = candidate
	s.mu.Unlock()
	return s.Snapshot(), nil
}

func (s *Service) CodingEndpoint() string {
	if s == nil || s.mcp == nil {
		return ""
	}
	return s.mcp.CodingEndpoint()
}

func (s *Service) AgentEndpoint() string {
	if s == nil || s.mcp == nil {
		return ""
	}
	return s.mcp.AgentEndpoint()
}

func (s *Service) AgentProviderEndpoint() string {
	if s == nil || s.agentProvider == nil {
		return ""
	}
	return s.agentProvider.Endpoint()
}

func (s *Service) AgentAPIKey() string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Agent.APIKey
}
