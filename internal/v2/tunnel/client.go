package tunnel

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
	"github.com/AAAYNMMM/CWapi/internal/processlaunch"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
)

const (
	profileDirectoryName = "tunnel"
	profileFileName      = "openai-tunnel.yaml"
	processExitError     = "OPENAI_TUNNEL_EXITED"
	defaultMaxRestarts   = 3
	defaultRestartDelay  = time.Second
	defaultStableRuntime = 30 * time.Second
)

type Snapshot struct {
	State         string `json:"state"`
	Configured    bool   `json:"configured"`
	TunnelID      string `json:"tunnel_id,omitempty"`
	APIKeyPresent bool   `json:"api_key_present"`
	LastError     string `json:"last_error,omitempty"`
}

type process interface {
	Wait() error
	Kill() error
}

type processStarter func(executable string, arguments []string, environment []string, directory string) (process, error)

type activeProcess struct {
	process   process
	done      chan struct{}
	startedAt time.Time
}

type commandProcess struct{ command *exec.Cmd }

func (p commandProcess) Wait() error { return p.command.Wait() }

func (p commandProcess) Kill() error {
	if p.command == nil || p.command.Process == nil {
		return errors.New("process is not running")
	}
	return p.command.Process.Kill()
}

type Manager struct {
	mu sync.RWMutex

	dataRoot         string
	profileDirectory string
	profileName      string
	executable       string
	config           v2config.TunnelConfig
	apiKey           string
	mcpURL           string
	start            processStarter

	process  *activeProcess
	closed   bool
	snapshot Snapshot

	restartToken  uint64
	restartCount  int
	maxRestarts   int
	restartDelay  time.Duration
	stableRuntime time.Duration
}

func New(dataRoot string, executable string, config v2config.TunnelConfig, apiKey string, mcpURL string) (*Manager, error) {
	return newManager(dataRoot, executable, config, apiKey, mcpURL, "")
}

// NewWithProfile creates an independent tunnel-client profile and working
// directory. Separate profiles are required when Coding and Agent each use a
// different OpenAI tunnel process.
func NewWithProfile(dataRoot string, executable string, config v2config.TunnelConfig, apiKey string, mcpURL string, profileName string) (*Manager, error) {
	return newManager(dataRoot, executable, config, apiKey, mcpURL, profileName)
}

func newManager(dataRoot string, executable string, config v2config.TunnelConfig, apiKey string, mcpURL string, profileName string) (*Manager, error) {
	if err := v2config.ValidateTunnel(config); err != nil {
		return nil, err
	}
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("OPENAI_TUNNEL_DATA_ROOT_INVALID")
	}
	profileName = strings.TrimSpace(profileName)
	if profileName != "" && !validProfileName(profileName) {
		return nil, errors.New("OPENAI_TUNNEL_PROFILE_NAME_INVALID")
	}
	if apiKey != "" {
		if err := validateAPIKey(apiKey); err != nil {
			return nil, err
		}
	}
	if config.Enabled && strings.TrimSpace(mcpURL) == "" {
		return nil, errors.New("OPENAI_TUNNEL_MCP_URL_REQUIRED")
	}
	profileDirectory := filepath.Join(dataRoot, profileDirectoryName)
	if profileName != "" {
		profileDirectory = filepath.Join(profileDirectory, profileName)
	}
	return &Manager{
		dataRoot: dataRoot, profileDirectory: profileDirectory, profileName: profileName,
		executable: strings.TrimSpace(executable), config: config,
		apiKey: apiKey, mcpURL: strings.TrimSpace(mcpURL), start: startProcess,
		snapshot: Snapshot{
			State:         initialState(config.Enabled),
			Configured:    config.Enabled,
			TunnelID:      config.TunnelID,
			APIKeyPresent: apiKey != "",
		},
		maxRestarts: defaultMaxRestarts, restartDelay: defaultRestartDelay, stableRuntime: defaultStableRuntime,
	}, nil
}

func validProfileName(value string) bool {
	if value == "" || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func initialState(enabled bool) string {
	if enabled {
		return "stopped"
	}
	return "disabled"
}

func (m *Manager) Snapshot() Snapshot {
	if m == nil {
		return Snapshot{State: "unavailable"}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.snapshot
}

func (m *Manager) Start(context.Context) error {
	if m == nil {
		return errors.New("OPENAI_TUNNEL_UNAVAILABLE")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("OPENAI_TUNNEL_CLOSED")
	}
	if !m.config.Enabled {
		m.snapshot = Snapshot{State: "disabled"}
		return nil
	}
	m.snapshot = m.configuredSnapshot("starting", "")
	if m.apiKey == "" {
		m.snapshot = m.configuredSnapshot("blocked", "OPENAI_TUNNEL_API_KEY_MISSING")
		return errors.New("OPENAI_TUNNEL_API_KEY_MISSING")
	}
	if err := validateAPIKey(m.apiKey); err != nil {
		m.snapshot = m.configuredSnapshot("blocked", "OPENAI_TUNNEL_API_KEY_INVALID")
		return err
	}
	if !isRegularFile(m.executable) {
		m.snapshot = m.configuredSnapshot("failed", "OPENAI_TUNNEL_RUNTIME_MISSING")
		return errors.New("OPENAI_TUNNEL_RUNTIME_MISSING")
	}
	if m.process != nil {
		m.snapshot = m.configuredSnapshot("running", "")
		return nil
	}
	m.restartToken++
	m.restartCount = 0
	if err := m.launchLocked(); err != nil {
		m.scheduleRestartLocked("OPENAI_TUNNEL_START_FAILED")
		return err
	}
	return nil
}

func (m *Manager) launchLocked() error {
	if err := m.writeProfile(); err != nil {
		m.snapshot = m.configuredSnapshot("failed", "OPENAI_TUNNEL_PROFILE_WRITE_FAILED")
		return fmt.Errorf("OPENAI_TUNNEL_PROFILE_WRITE_FAILED: %w", err)
	}
	arguments := []string{"run", "--profile-file", filepath.Join(m.profileDirectory, profileFileName)}
	child, err := m.start(m.executable, arguments, tunnelEnvironment(m.apiKey), m.profileDirectory)
	if err != nil {
		m.snapshot = m.configuredSnapshot("failed", "OPENAI_TUNNEL_START_FAILED")
		return fmt.Errorf("OPENAI_TUNNEL_START_FAILED: %w", err)
	}
	run := &activeProcess{process: child, done: make(chan struct{}), startedAt: time.Now()}
	m.process = run
	m.snapshot = m.configuredSnapshot("running", "")
	go m.wait(run)
	return nil
}

func (m *Manager) wait(run *activeProcess) {
	_ = run.process.Wait()
	m.mu.Lock()
	if m.process == run {
		m.process = nil
		if m.closed {
			m.snapshot = m.configuredSnapshot(initialState(m.config.Enabled), "")
		} else {
			if time.Since(run.startedAt) >= m.stableRuntime {
				m.restartCount = 0
			}
			m.scheduleRestartLocked(processExitError)
		}
	}
	close(run.done)
	m.mu.Unlock()
}

func (m *Manager) scheduleRestartLocked(lastError string) {
	if m.closed {
		return
	}
	if m.restartCount >= m.maxRestarts {
		m.snapshot = m.configuredSnapshot("failed", lastError)
		return
	}
	m.restartCount++
	delay := m.restartDelay
	for attempt := 1; attempt < m.restartCount; attempt++ {
		delay *= 2
	}
	m.restartToken++
	token := m.restartToken
	m.snapshot = m.configuredSnapshot("restarting", lastError)
	go m.restartAfter(token, delay)
}

func (m *Manager) restartAfter(token uint64, delay time.Duration) {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	<-timer.C
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.process != nil || token != m.restartToken {
		return
	}
	m.snapshot = m.configuredSnapshot("starting", processExitError)
	if err := m.launchLocked(); err != nil {
		m.scheduleRestartLocked("OPENAI_TUNNEL_RESTART_FAILED")
	}
}

func (m *Manager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.restartToken++
	run := m.process
	m.process = nil
	m.snapshot = m.configuredSnapshot(initialState(m.config.Enabled), "")
	m.mu.Unlock()
	if run == nil {
		return nil
	}
	if err := run.process.Kill(); err != nil {
		select {
		case <-run.done:
		case <-ctx.Done():
			return errors.New("OPENAI_TUNNEL_STOP_FAILED")
		default:
			return fmt.Errorf("OPENAI_TUNNEL_STOP_FAILED: %w", err)
		}
	}
	select {
	case <-run.done:
		return nil
	case <-ctx.Done():
		return errors.New("OPENAI_TUNNEL_STOP_FAILED")
	}
}

func (m *Manager) configuredSnapshot(state, lastError string) Snapshot {
	return Snapshot{
		State:         state,
		Configured:    m.config.Enabled,
		TunnelID:      m.config.TunnelID,
		APIKeyPresent: m.apiKey != "",
		LastError:     lastError,
	}
}

func (m *Manager) writeProfile() error {
	directory := m.profileDirectory
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	profile := fmt.Sprintf("config_version: 1\ncontrol_plane:\n  tunnel_id: %s\n  api_key: env:CONTROL_PLANE_API_KEY\nlog:\n  level: warn\n  format: struct-text\nhealth:\n  listen_addr: 127.0.0.1:0\nadmin_ui:\n  open_browser: false\nmcp:\n  server_urls:\n    - channel: main\n      url: %s\n", m.config.TunnelID, m.mcpURL)
	temporary, err := os.CreateTemp(directory, ".openai-tunnel-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.WriteString(temporary, profile); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	destination := filepath.Join(directory, profileFileName)
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(temporaryPath, destination); err != nil {
		return err
	}
	committed = true
	return nil
}

func validateAPIKey(value string) error {
	if value == "" || value != strings.TrimSpace(value) || strings.IndexByte(value, '\x00') >= 0 || strings.IndexAny(value, "\r\n\t ") >= 0 {
		return errors.New("OPENAI_TUNNEL_API_KEY_INVALID")
	}
	if len([]byte(value)) > 4096 {
		return errors.New("OPENAI_TUNNEL_API_KEY_INVALID")
	}
	return nil
}

func isRegularFile(path string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func tunnelEnvironment(apiKey string) []string {
	// The tunnel process receives the same bounded platform environment as
	// other CWapi children, plus the one secret it must use for control-plane
	// authentication. Do not inherit arbitrary host credentials or debug flags.
	environment := append([]string(nil), childenv.Canonical()...)
	environment = append(environment, "CONTROL_PLANE_API_KEY="+apiKey)
	return environment
}

func startProcess(executable string, arguments []string, environment []string, directory string) (process, error) {
	command := processlaunch.Command(executable, arguments...)
	command.Dir = directory
	command.Env = environment
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		return nil, err
	}
	return commandProcess{command: command}, nil
}
