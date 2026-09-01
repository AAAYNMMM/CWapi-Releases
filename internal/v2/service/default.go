package service

import (
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/AAAYNMMM/CWapi/internal/credentials"
	"github.com/AAAYNMMM/CWapi/internal/v2/agentbroker"
	"github.com/AAAYNMMM/CWapi/internal/v2/codextoolhost"
	"github.com/AAAYNMMM/CWapi/internal/v2/coding"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
	"github.com/AAAYNMMM/CWapi/internal/v2/tunnel"
	"github.com/AAAYNMMM/CWapi/internal/v2/workspace"
)

type tunnelCredentialReader interface {
	ReadOpenAITunnelAPIKey() (string, bool, error)
	ReadOpenAITunnelAgentAPIKey() (string, bool, error)
}

func NewDefault(configPath string) (*Service, error) {
	resolvedPath, err := resolveConfigPath(configPath)
	if err != nil {
		return nil, err
	}
	cfg, err := v2config.LoadOrCreate(resolvedPath)
	if err != nil {
		return nil, err
	}
	dataRoot := dataRootFromConfigPath(resolvedPath)
	manager, err := workspace.NewManager(dataRoot, discoverGitExecutable(dataRoot))
	if err != nil {
		return nil, err
	}
	codingService, err := coding.NewLazy(manager, dataRoot, cfg.Codex)
	if err != nil {
		return nil, err
	}
	var probeOnce sync.Once
	var cachedProbe codextoolhost.RuntimeSnapshot
	codexProbe := func() codextoolhost.RuntimeSnapshot {
		probeOnce.Do(func() { cachedProbe = codextoolhost.Probe(dataRoot, cfg.Codex) })
		return cachedProbe
	}
	deps := Dependencies{
		Coding:          codingService,
		CodingLifecycle: codingService,
		CodingSnapshot:  codingService.RuntimeSnapshot,
		WorkspaceIndex:  manager.Index,
		CodexProbe:      codexProbe,
	}
	codingTunnelAPIKey, agentTunnelAPIKey, err := readTunnelCredentials(cfg, credentials.New())
	if err != nil {
		return nil, err
	}
	codingTunnelURL := "http://127.0.0.1:" + strconv.Itoa(cfg.MCP.Port) + "/mcp/coding/" + cfg.MCP.CodingToken
	tunnelManager, err := tunnel.NewWithProfile(dataRoot, discoverTunnelExecutable(dataRoot), cfg.Tunnel, codingTunnelAPIKey, codingTunnelURL, "coding")
	if err != nil {
		return nil, err
	}
	deps.Tunnel = tunnelManager
	agentTunnelURL := "http://127.0.0.1:" + strconv.Itoa(cfg.MCP.Port) + "/mcp/agent/" + cfg.MCP.AgentToken
	agentTunnelManager, err := tunnel.NewWithProfile(dataRoot, discoverTunnelExecutable(dataRoot), cfg.AgentTunnel, agentTunnelAPIKey, agentTunnelURL, "agent")
	if err != nil {
		return nil, err
	}
	deps.AgentTunnel = agentTunnelManager
	if cfg.Agent.Enabled {
		broker := agentbroker.New(agentbroker.Config{})
		provider, err := agentbroker.NewProvider(cfg.Agent, broker)
		if err != nil {
			broker.Shutdown()
			return nil, err
		}
		deps.Agent = broker
		deps.AgentBroker = broker
		deps.AgentProvider = provider
	}
	return newConfigured(resolvedPath, cfg, deps)
}

func readTunnelCredentials(cfg v2config.Config, reader tunnelCredentialReader) (string, string, error) {
	if !cfg.Tunnel.Enabled && !cfg.AgentTunnel.Enabled {
		return "", "", nil
	}
	if reader == nil {
		return "", "", errors.New("CREDENTIAL_STORE_UNAVAILABLE")
	}
	codingKey, agentKey := "", ""
	if cfg.Tunnel.Enabled {
		value, present, err := reader.ReadOpenAITunnelAPIKey()
		if err != nil {
			return "", "", err
		}
		if present {
			codingKey = value
		}
	}
	if cfg.AgentTunnel.Enabled {
		value, present, err := reader.ReadOpenAITunnelAgentAPIKey()
		if err != nil {
			return "", "", err
		}
		if present {
			agentKey = value
		}
	}
	return codingKey, agentKey, nil
}

func dataRootFromConfigPath(configPath string) string {
	configPath = filepath.Clean(configPath)
	directory := filepath.Dir(configPath)
	if strings.EqualFold(filepath.Base(directory), "config") {
		return filepath.Dir(directory)
	}
	return directory
}
