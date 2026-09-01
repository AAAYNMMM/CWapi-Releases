package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/credentials"
	"github.com/AAAYNMMM/CWapi/internal/tray"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
	v2service "github.com/AAAYNMMM/CWapi/internal/v2/service"
	"github.com/AAAYNMMM/CWapi/internal/v2/tunnel"
	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type ConnectionInfo struct {
	CodingMCP     string `json:"coding_mcp"`
	AgentMCP      string `json:"agent_mcp"`
	AgentProvider string `json:"agent_provider"`
}

type AgentCredential struct {
	ProviderURL string `json:"provider_url"`
	APIKey      string `json:"api_key"`
}

type OpenAITunnelInfo struct {
	Enabled       bool   `json:"enabled"`
	TunnelID      string `json:"tunnel_id"`
	APIKeyPresent bool   `json:"api_key_present"`
	State         string `json:"state"`
}

type App struct {
	mu            sync.RWMutex
	reconfigureMu sync.Mutex
	ctx           context.Context
	configPath    string
	service       *v2service.Service
	startupErr    error
	tray          *tray.Controller
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	configPath, err := appConfigPath()
	var service *v2service.Service
	if err == nil {
		service, err = v2service.NewDefault(configPath)
	}
	if err == nil {
		err = service.Start(ctx)
	}
	a.mu.Lock()
	a.ctx = ctx
	a.configPath = configPath
	a.service = service
	a.startupErr = err
	a.mu.Unlock()
	if err != nil {
		fmt.Println("CWapi 2.0.3 startup degraded:", err.Error())
	}

	a.tray = tray.New(a.showMainWindow, a.requestShutdown)
	if trayErr := a.tray.Start(); trayErr != nil {
		fmt.Println("CWapi tray startup failed:", trayErr.Error())
	}
}

func appConfigPath() (string, error) {
	if value := strings.TrimSpace(os.Getenv("CWAPI_V2_CONFIG")); value != "" {
		return value, nil
	}
	return v2config.DefaultPath()
}

func (a *App) shutdown(context.Context) {
	if a.tray != nil {
		_ = a.tray.Close()
	}
	service, _ := a.core()
	if service != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = service.Close(ctx)
	}
}

func (a *App) onSecondInstanceLaunch(_ options.SecondInstanceData) { a.showMainWindow() }

func (a *App) showMainWindow() {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return
	}
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.Show(ctx)
}

func (a *App) requestShutdown() {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx != nil {
		wailsruntime.Quit(ctx)
	}
}

func (a *App) RuntimeSnapshot() v2service.Snapshot {
	a.mu.RLock()
	service, startupErr := a.service, a.startupErr
	a.mu.RUnlock()
	if service != nil {
		return service.Snapshot()
	}
	snapshot := v2service.Snapshot{State: "failed"}
	if startupErr != nil {
		snapshot.LastError = startupErr.Error()
	}
	return snapshot
}

func (a *App) ConnectionInfo() ConnectionInfo {
	service, _ := a.core()
	if service == nil {
		return ConnectionInfo{}
	}
	return ConnectionInfo{
		CodingMCP:     service.CodingEndpoint(),
		AgentMCP:      service.AgentEndpoint(),
		AgentProvider: service.AgentProviderEndpoint(),
	}
}

func (a *App) AgentCredential() (AgentCredential, error) {
	service, err := a.core()
	if err != nil {
		return AgentCredential{}, err
	}
	endpoint := service.AgentProviderEndpoint()
	key := service.AgentAPIKey()
	if endpoint == "" || key == "" {
		return AgentCredential{}, errors.New("AGENT_DISABLED")
	}
	return AgentCredential{ProviderURL: endpoint, APIKey: key}, nil
}

func (a *App) OpenAITunnelInfo() OpenAITunnelInfo {
	service, _ := a.core()
	if service == nil {
		return OpenAITunnelInfo{}
	}
	return openAITunnelInfo(service.Snapshot().OpenAITunnel)
}

func (a *App) AgentOpenAITunnelInfo() OpenAITunnelInfo {
	service, _ := a.core()
	if service == nil {
		return OpenAITunnelInfo{}
	}
	return openAITunnelInfo(service.Snapshot().AgentOpenAITunnel)
}

func openAITunnelInfo(snapshot tunnel.Snapshot) OpenAITunnelInfo {
	return OpenAITunnelInfo{
		Enabled: snapshot.Configured, TunnelID: snapshot.TunnelID,
		APIKeyPresent: snapshot.APIKeyPresent, State: snapshot.State,
	}
}

func (a *App) ConfigureOpenAITunnel(tunnelID, apiKey string) (OpenAITunnelInfo, error) {
	tunnelID = strings.TrimSpace(tunnelID)
	if err := v2config.ValidateTunnelID(tunnelID); err != nil {
		return a.OpenAITunnelInfo(), err
	}
	if err := credentials.ValidateOpenAITunnelAPIKey(apiKey); err != nil {
		return a.OpenAITunnelInfo(), err
	}
	credentialManager := credentials.New()
	previousKey, previousPresent, err := credentialManager.ReadOpenAITunnelAPIKey()
	if err != nil {
		return a.OpenAITunnelInfo(), err
	}
	if err := credentialManager.WriteOpenAITunnelAPIKey(apiKey); err != nil {
		return a.OpenAITunnelInfo(), err
	}
	_, reconfigureErr := a.reconfigure(func(cfg *v2config.Config) {
		cfg.Tunnel.Enabled = true
		cfg.Tunnel.TunnelID = tunnelID
	})
	if reconfigureErr != nil {
		restoreErr := restoreOpenAITunnelKey(credentialManager, previousKey, previousPresent)
		return a.OpenAITunnelInfo(), errors.Join(reconfigureErr, restoreErr)
	}
	return a.OpenAITunnelInfo(), nil
}

func (a *App) ClearOpenAITunnel() (OpenAITunnelInfo, error) {
	credentialManager := credentials.New()
	previousKey, previousPresent, err := credentialManager.ReadOpenAITunnelAPIKey()
	if err != nil {
		return a.OpenAITunnelInfo(), err
	}
	if err := credentialManager.DeleteOpenAITunnelAPIKey(); err != nil {
		return a.OpenAITunnelInfo(), err
	}
	_, reconfigureErr := a.reconfigure(func(cfg *v2config.Config) {
		cfg.Tunnel.Enabled = false
		cfg.Tunnel.TunnelID = ""
	})
	if reconfigureErr != nil {
		restoreErr := restoreOpenAITunnelKey(credentialManager, previousKey, previousPresent)
		return a.OpenAITunnelInfo(), errors.Join(reconfigureErr, restoreErr)
	}
	return a.OpenAITunnelInfo(), nil
}

func (a *App) ConfigureAgentOpenAITunnel(tunnelID, apiKey string) (OpenAITunnelInfo, error) {
	tunnelID = strings.TrimSpace(tunnelID)
	if err := v2config.ValidateTunnelID(tunnelID); err != nil {
		return a.AgentOpenAITunnelInfo(), err
	}
	if err := credentials.ValidateOpenAITunnelAPIKey(apiKey); err != nil {
		return a.AgentOpenAITunnelInfo(), err
	}
	credentialManager := credentials.New()
	previousKey, previousPresent, err := credentialManager.ReadOpenAITunnelAgentAPIKey()
	if err != nil {
		return a.AgentOpenAITunnelInfo(), err
	}
	if err := credentialManager.WriteOpenAITunnelAgentAPIKey(apiKey); err != nil {
		return a.AgentOpenAITunnelInfo(), err
	}
	_, reconfigureErr := a.reconfigure(func(cfg *v2config.Config) {
		cfg.AgentTunnel.Enabled = true
		cfg.AgentTunnel.TunnelID = tunnelID
	})
	if reconfigureErr != nil {
		restoreErr := restoreOpenAITunnelAgentKey(credentialManager, previousKey, previousPresent)
		return a.AgentOpenAITunnelInfo(), errors.Join(reconfigureErr, restoreErr)
	}
	return a.AgentOpenAITunnelInfo(), nil
}

func (a *App) ClearAgentOpenAITunnel() (OpenAITunnelInfo, error) {
	credentialManager := credentials.New()
	previousKey, previousPresent, err := credentialManager.ReadOpenAITunnelAgentAPIKey()
	if err != nil {
		return a.AgentOpenAITunnelInfo(), err
	}
	if err := credentialManager.DeleteOpenAITunnelAgentAPIKey(); err != nil {
		return a.AgentOpenAITunnelInfo(), err
	}
	_, reconfigureErr := a.reconfigure(func(cfg *v2config.Config) {
		cfg.AgentTunnel.Enabled = false
		cfg.AgentTunnel.TunnelID = ""
	})
	if reconfigureErr != nil {
		restoreErr := restoreOpenAITunnelAgentKey(credentialManager, previousKey, previousPresent)
		return a.AgentOpenAITunnelInfo(), errors.Join(reconfigureErr, restoreErr)
	}
	return a.AgentOpenAITunnelInfo(), nil
}

func restoreOpenAITunnelKey(manager *credentials.Manager, value string, present bool) error {
	if present {
		return manager.WriteOpenAITunnelAPIKey(value)
	}
	return manager.DeleteOpenAITunnelAPIKey()
}

func restoreOpenAITunnelAgentKey(manager *credentials.Manager, value string, present bool) error {
	if present {
		return manager.WriteOpenAITunnelAgentAPIKey(value)
	}
	return manager.DeleteOpenAITunnelAgentAPIKey()
}

func (a *App) UpdateCodexAccessProfile(profile string) (v2service.Snapshot, error) {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile != v2config.AccessProfileSafe && profile != v2config.AccessProfileFull {
		return a.RuntimeSnapshot(), errors.New("CONFIG_CODEX_ACCESS_PROFILE_INVALID")
	}
	a.reconfigureMu.Lock()
	defer a.reconfigureMu.Unlock()
	current, err := a.core()
	if err != nil {
		return a.RuntimeSnapshot(), err
	}
	return current.UpdateCodexAccessProfile(profile)
}

func (a *App) UpdateCodexNetworkAccess(allowed bool) (v2service.Snapshot, error) {
	a.reconfigureMu.Lock()
	defer a.reconfigureMu.Unlock()
	current, err := a.core()
	if err != nil {
		return a.RuntimeSnapshot(), err
	}
	return current.UpdateCodexNetworkAccess(allowed)
}

func (a *App) SetAgentEnabled(enabled bool) (v2service.Snapshot, error) {
	return a.reconfigure(func(cfg *v2config.Config) { cfg.Agent.Enabled = enabled })
}

func (a *App) RegenerateAgentAPIKey() (AgentCredential, error) {
	_, err := a.reconfigure(func(cfg *v2config.Config) { cfg.Agent.APIKey = v2config.NewAgentAPIKey() })
	if err != nil {
		return AgentCredential{}, err
	}
	return a.AgentCredential()
}

func (a *App) RegenerateMCPIdentities() (ConnectionInfo, error) {
	_, err := a.reconfigure(func(cfg *v2config.Config) {
		cfg.MCP.CodingToken = v2config.NewCodingToken()
		cfg.MCP.AgentToken = v2config.NewAgentToken()
	})
	if err != nil {
		return ConnectionInfo{}, err
	}
	return a.ConnectionInfo(), nil
}

func (a *App) reconfigure(mutate func(*v2config.Config)) (v2service.Snapshot, error) {
	a.reconfigureMu.Lock()
	defer a.reconfigureMu.Unlock()

	current, err := a.core()
	if err != nil {
		return a.RuntimeSnapshot(), err
	}
	beforeSnapshot := current.Snapshot()
	if beforeSnapshot.Coding.Active > 0 {
		return beforeSnapshot, errors.New("CONFIG_BUSY_CODING")
	}
	if beforeSnapshot.Agent.Pending > 0 || beforeSnapshot.Agent.Claimed > 0 {
		return beforeSnapshot, errors.New("CONFIG_BUSY_AGENT")
	}

	a.mu.RLock()
	configPath, runtimeCtx := a.configPath, a.ctx
	a.mu.RUnlock()
	before, err := v2config.Load(configPath)
	if err != nil {
		return beforeSnapshot, err
	}
	candidate := before
	mutate(&candidate)
	if err := v2config.Validate(candidate); err != nil {
		return beforeSnapshot, err
	}

	// Keep the durable config untouched until the current runtime is fully
	// stopped. A Close failure can leave Service permanently closed, so in that
	// case immediately rebuild the previous runtime from the still-current
	// config instead of returning a dead Service pointer to the desktop shell.
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr := current.Close(closeCtx)
	closeCancel()
	if closeErr != nil {
		restoreErr := a.restartV2Service(configPath, runtimeCtx)
		return a.RuntimeSnapshot(), errors.Join(closeErr, restoreErr)
	}

	// Only commit the candidate after the old runtime has stopped cleanly. If
	// persistence fails, the file on disk is still the previous config and can
	// be used to restore service immediately.
	if err := v2config.SaveAtomic(configPath, candidate); err != nil {
		restoreErr := a.restartV2Service(configPath, runtimeCtx)
		return a.RuntimeSnapshot(), errors.Join(err, restoreErr)
	}

	next, startErr := v2service.NewDefault(configPath)
	if startErr == nil {
		startErr = next.Start(runtimeCtx)
	}
	if startErr != nil {
		var cleanupErr error
		if next != nil {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			cleanupErr = next.Close(cleanupCtx)
			cleanupCancel()
		}
		rollbackErr := v2config.SaveAtomic(configPath, before)
		if rollbackErr != nil {
			combined := errors.Join(startErr, cleanupErr, rollbackErr)
			a.mu.Lock()
			a.service = nil
			a.startupErr = combined
			a.mu.Unlock()
			return a.RuntimeSnapshot(), combined
		}
		restoreErr := a.restartV2Service(configPath, runtimeCtx)
		return a.RuntimeSnapshot(), errors.Join(startErr, cleanupErr, restoreErr)
	}

	a.mu.Lock()
	a.service = next
	a.startupErr = nil
	a.mu.Unlock()
	return next.Snapshot(), nil
}

func (a *App) core() (*v2service.Service, error) {
	if a == nil {
		return nil, errors.New("CORE_UNAVAILABLE")
	}
	a.mu.RLock()
	service, startupErr := a.service, a.startupErr
	a.mu.RUnlock()
	if service != nil {
		return service, nil
	}
	if startupErr != nil {
		return nil, fmt.Errorf("CORE_STARTUP_FAILED: %w", startupErr)
	}
	return nil, errors.New("CORE_NOT_STARTED")
}
