package codextoolhost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/invocation"
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
)

const (
	defaultTimeout = 120 * time.Second
	maxTimeout     = 180 * time.Second
	maxOutputBytes = 64 * 1024
)

// Host is a model-free bridge to the private Codex app-server command/exec
// development tool. It does not create Codex threads or turns and therefore
// does not use Codex account or model functionality.
type Host struct {
	service  *codex.Service
	resolver *invocation.Resolver

	profileMu     sync.RWMutex
	accessProfile string
}

type ExecInput struct {
	Command        string
	Argv           []string
	CWD            string
	TimeoutSeconds int
}

type ExecResult struct {
	State     string
	ExitCode  int
	Stdout    string
	Stderr    string
	Truncated bool
}

func New(dataRoot string, cfg v2config.CodexConfig) (*Host, error) {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("CODEX_TOOLHOST_DATA_ROOT_INVALID")
	}
	if err := v2config.ValidateCodex(cfg); err != nil {
		return nil, err
	}
	service, err := codex.NewCommandService(filepath.Clean(dataRoot), cfg.Executable)
	if err != nil {
		return nil, err
	}
	snapshot := service.Snapshot()
	if !snapshot.Configured {
		return nil, errors.New("CODEX_TOOLHOST_RUNTIME_UNAVAILABLE")
	}
	if !strings.EqualFold(snapshot.ExecutableSHA, codex.PinnedExecutableSHA256) {
		return nil, fmt.Errorf("CODEX_EXECUTABLE_SHA256_MISMATCH: expected=%s actual=%s", codex.PinnedExecutableSHA256, snapshot.ExecutableSHA)
	}
	resolver, err := invocation.New(commandEnvironment(filepath.Clean(dataRoot), snapshot.ExecutablePath))
	if err != nil {
		return nil, err
	}
	return &Host{service: service, resolver: resolver, accessProfile: cfg.AccessProfile}, nil
}

func (h *Host) SetAccessProfile(profile string) error {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile != v2config.AccessProfileSafe && profile != v2config.AccessProfileFull {
		return errors.New("CONFIG_CODEX_ACCESS_PROFILE_INVALID")
	}
	if h == nil {
		return errors.New("CODEX_TOOLHOST_UNAVAILABLE")
	}
	h.profileMu.Lock()
	h.accessProfile = profile
	h.profileMu.Unlock()
	return nil
}

func (h *Host) currentAccessProfile() string {
	h.profileMu.RLock()
	defer h.profileMu.RUnlock()
	return h.accessProfile
}

func (h *Host) Exec(ctx context.Context, workspaceRoot string, input ExecInput) (ExecResult, error) {
	if h == nil || h.service == nil || h.resolver == nil {
		return ExecResult{}, errors.New("CODEX_TOOLHOST_UNAVAILABLE")
	}
	workspaceRoot = strings.TrimSpace(workspaceRoot)
	if workspaceRoot == "" || !filepath.IsAbs(workspaceRoot) {
		return ExecResult{}, errors.New("CODEX_TOOLHOST_WORKSPACE_INVALID")
	}
	arguments, err := decodeArguments(input)
	if err != nil {
		return ExecResult{}, err
	}
	final, err := h.resolver.Resolve(filepath.Clean(workspaceRoot), arguments)
	if err != nil {
		return ExecResult{}, err
	}
	if final.BridgeCreated {
		defer func() { _ = os.Remove(final.BridgePath) }()
	}
	sandbox := codex.CommandSandboxWorkspaceWrite
	if h.currentAccessProfile() == v2config.AccessProfileFull {
		sandbox = codex.CommandSandboxFullAccess
	}
	timeout, err := commandTimeout(input.TimeoutSeconds)
	if err != nil {
		return ExecResult{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	processID, err := randomProcessID()
	if err != nil {
		return ExecResult{}, err
	}
	handle, err := h.service.StartCommand(execCtx, codex.CommandSpec{
		ProcessID: processID, Executable: final.Executable, Argv: final.Argv,
		CWD: final.CWD, WritableRoot: filepath.Clean(workspaceRoot),
		Environment: final.Environment, Sandbox: sandbox,
	})
	if err != nil {
		return ExecResult{}, err
	}
	select {
	case result := <-handle.Done():
		if result.Err != nil {
			return ExecResult{}, result.Err
		}
		stdout, stdoutTrimmed := boundedTail(result.Stdout, maxOutputBytes)
		stderr, stderrTrimmed := boundedTail(result.Stderr, maxOutputBytes)
		state := "completed"
		if result.ExitCode != 0 {
			state = "failed"
		}
		return ExecResult{
			State: state, ExitCode: result.ExitCode, Stdout: stdout, Stderr: stderr,
			Truncated: stdoutTrimmed || stderrTrimmed,
		}, nil
	case <-execCtx.Done():
		_ = handle.Stop()
		return ExecResult{}, fmt.Errorf("CODEX_TOOLHOST_COMMAND_TIMEOUT: %w", execCtx.Err())
	}
}

func decodeArguments(input ExecInput) (processcontract.StartArguments, error) {
	argv := make([]any, len(input.Argv))
	for index, value := range input.Argv {
		argv[index] = value
	}
	value := map[string]any{"command": input.Command}
	if len(argv) > 0 {
		value["argv"] = argv
	}
	if input.CWD != "" {
		value["cwd"] = input.CWD
	}
	return processcontract.DecodeStart(value)
}

func commandTimeout(seconds int) (time.Duration, error) {
	if seconds == 0 {
		return defaultTimeout, nil
	}
	timeout := time.Duration(seconds) * time.Second
	if timeout < time.Second || timeout > maxTimeout {
		return 0, errors.New("CODING_EXEC_TIMEOUT_INVALID")
	}
	return timeout, nil
}

func commandEnvironment(dataRoot, executable string) []string {
	base := childenv.Canonical()
	privateRoot := filepath.Dir(filepath.Dir(filepath.Clean(executable)))
	appRoot := filepath.Dir(filepath.Clean(dataRoot))
	paths := []string{
		filepath.Join(privateRoot, "bin"),
		filepath.Join(privateRoot, "codex-resources"),
		filepath.Join(privateRoot, "codex-path"),
		filepath.Join(appRoot, "runtime", "git", "cmd"),
	}
	available := make([]string, 0, len(paths)+1)
	for _, candidate := range paths {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			available = append(available, candidate)
		}
	}
	if inherited := childenv.Value(base, "PATH"); inherited != "" {
		available = append(available, inherited)
	}
	pathValue := strings.Join(available, string(os.PathListSeparator))
	prompt, interactive := "0", "Never"
	return childenv.Merge(base, map[string]*string{
		"PATH": &pathValue, "GIT_TERMINAL_PROMPT": &prompt, "GCM_INTERACTIVE": &interactive,
	})
}

func randomProcessID() (string, error) {
	var value [12]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("CODEX_TOOLHOST_PROCESS_ID_FAILED: %w", err)
	}
	return "proc-" + hex.EncodeToString(value[:]), nil
}

func boundedTail(value string, limit int) (string, bool) {
	if len(value) <= limit {
		return value, false
	}
	payload := []byte(value)
	start := len(payload) - limit
	for start < len(payload) && !utf8.RuneStart(payload[start]) {
		start++
	}
	return string(payload[start:]), true
}
