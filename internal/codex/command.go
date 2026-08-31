package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
)

type CommandSpec struct {
	ProcessID    string
	Executable   string
	Argv         []string
	CWD          string
	WritableRoot string
	Environment  []string
	Sandbox      string
}

const (
	CommandSandboxWorkspaceWrite = "workspace-write"
	CommandSandboxFullAccess     = "danger-full-access"
)

type CommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

type CommandHandle struct {
	done        chan CommandResult
	cancel      context.CancelFunc
	client      *Client
	home        string
	cleanupOnce sync.Once
	stopOnce    sync.Once
	stopErr     error
}

func (h *CommandHandle) Done() <-chan CommandResult {
	if h == nil {
		return nil
	}
	return h.done
}

func (h *CommandHandle) Stop() error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		h.cancel()
		h.stopErr = h.client.releaseProcessTree()
		h.client.Close()
	})
	return h.stopErr
}

func (s *Service) StartCommand(ctx context.Context, spec CommandSpec) (*CommandHandle, error) {
	if s == nil {
		return nil, errors.New("CODEX_COMMAND_SERVICE_UNAVAILABLE")
	}
	if err := validateCommandSpec(spec); err != nil {
		return nil, err
	}
	actualHash, err := hashFile(s.codexExe)
	if err != nil {
		return nil, fmt.Errorf("CODEX_RUNTIME_UNAVAILABLE: %w", err)
	}
	if !strings.EqualFold(actualHash, PinnedExecutableSHA256) {
		return nil, fmt.Errorf("CODEX_EXECUTABLE_SHA256_MISMATCH: expected=%s actual=%s", PinnedExecutableSHA256, actualHash)
	}

	executionRoot := filepath.Join(s.dataRoot, "temp", "codex-executions")
	home := filepath.Join(executionRoot, spec.ProcessID)
	if err := os.MkdirAll(executionRoot, 0o700); err != nil {
		return nil, fmt.Errorf("CODEX_EXECUTION_ROOT_CREATE_FAILED: %w", err)
	}
	if err := os.Mkdir(home, 0o700); err != nil {
		return nil, fmt.Errorf("CODEX_EXECUTION_HOME_CREATE_FAILED: %w", err)
	}
	if err := ensureCommandHome(home); err != nil {
		return nil, err
	}
	removeHome := true
	defer func() {
		if removeHome {
			_ = os.RemoveAll(home)
		}
	}()

	if ctx == nil {
		ctx = context.Background()
	}
	commandCtx, cancel := context.WithCancel(ctx)
	client, err := s.startCommandClient(commandCtx, home, spec.CWD, commandServerEnvironment(spec.Environment))
	if err != nil {
		cancel()
		return nil, err
	}
	handle := &CommandHandle{done: make(chan CommandResult, 1), cancel: cancel, client: client, home: home}
	removeHome = false
	go handle.run(commandCtx, commandParams(spec))
	return handle, nil
}

func (s *Service) startCommandClient(ctx context.Context, home, cwd string, environment []string) (*Client, error) {
	notifications := make(chan map[string]any, 8)
	newClient := func() *Client {
		return NewClient(s.codexExe, home, "", environment, 30*time.Second, func(message map[string]any) {
			select {
			case notifications <- message:
			default:
			}
		})
	}
	client := newClient()
	if err := startOwnedCommandClient(ctx, client); err != nil {
		return nil, err
	}
	readinessCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	readiness, err := client.request(readinessCtx, "windowsSandbox/readiness", nil, true)
	if err != nil {
		closeCommandClient(client)
		return nil, fmt.Errorf("CODEX_SANDBOX_READINESS_FAILED: %w", err)
	}
	if objectString(readiness, "status") == "ready" {
		return client, nil
	}
	if _, err := client.request(readinessCtx, "windowsSandbox/setupStart", map[string]any{"mode": "unelevated", "cwd": cwd}, true); err != nil {
		closeCommandClient(client)
		return nil, fmt.Errorf("CODEX_SANDBOX_SETUP_START_FAILED: %w", err)
	}
	for {
		select {
		case message := <-notifications:
			if objectString(message, "method") != "windowsSandbox/setupCompleted" {
				continue
			}
			params, _ := message["params"].(map[string]any)
			if success, _ := params["success"].(bool); !success {
				closeCommandClient(client)
				return nil, errors.New("CODEX_SANDBOX_SETUP_FAILED")
			}
			closeCommandClient(client)
			client = newClient()
			if err := startOwnedCommandClient(ctx, client); err != nil {
				return nil, err
			}
			verify, verifyErr := client.request(readinessCtx, "windowsSandbox/readiness", nil, true)
			if verifyErr != nil || objectString(verify, "status") != "ready" {
				closeCommandClient(client)
				return nil, errors.New("CODEX_SANDBOX_NOT_READY")
			}
			return client, nil
		case <-readinessCtx.Done():
			closeCommandClient(client)
			return nil, fmt.Errorf("CODEX_SANDBOX_SETUP_TIMEOUT: %w", readinessCtx.Err())
		}
	}
}

func startOwnedCommandClient(ctx context.Context, client *Client) error {
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.Start(startCtx); err != nil {
		client.Close()
		return err
	}
	if err := client.ownProcessTree(); err != nil {
		client.Close()
		return fmt.Errorf("CODEX_COMMAND_PROCESS_SCOPE_FAILED: %w", err)
	}
	return nil
}

func (h *CommandHandle) run(ctx context.Context, params map[string]any) {
	value, err := h.client.request(ctx, "command/exec", params, true)
	result := decodeCommandResult(value, err)
	h.finish()
	h.done <- result
	close(h.done)
}

func (h *CommandHandle) finish() {
	h.cleanupOnce.Do(func() {
		_ = h.Stop()
		select {
		case <-h.client.done:
		case <-time.After(3 * time.Second):
		}
		_ = os.RemoveAll(h.home)
	})
}

func closeCommandClient(client *Client) {
	if client == nil {
		return
	}
	_ = client.releaseProcessTree()
	client.Close()
	select {
	case <-client.done:
	case <-time.After(3 * time.Second):
	}
}

func commandParams(spec CommandSpec) map[string]any {
	command := make([]string, 0, len(spec.Argv)+1)
	command = append(command, spec.Executable)
	command = append(command, spec.Argv...)
	environment := make(map[string]any, len(spec.Environment)+3)
	for _, entry := range spec.Environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && key != "" {
			environment[key] = value
		}
	}
	for _, key := range []string{"CODEX_HOME", "RUST_LOG", "LOG_FORMAT"} {
		environment[key] = nil
	}
	sandboxPolicy := map[string]any{
		"type": "workspaceWrite", "writableRoots": []string{spec.WritableRoot},
		"networkAccess": true, "excludeSlashTmp": true, "excludeTmpdirEnvVar": true,
	}
	if spec.Sandbox == CommandSandboxFullAccess {
		sandboxPolicy = map[string]any{"type": "dangerFullAccess"}
	}
	return map[string]any{
		"command":        command,
		"cwd":            spec.CWD,
		"env":            environment,
		"sandboxPolicy":  sandboxPolicy,
		"disableTimeout": true,
	}
}

func decodeCommandResult(value any, requestErr error) CommandResult {
	if requestErr != nil {
		return CommandResult{Err: requestErr}
	}
	object, ok := value.(map[string]any)
	if !ok {
		return CommandResult{Err: errors.New("CODEX_COMMAND_RESPONSE_INVALID")}
	}
	exitCode, ok := numberToInt64(object["exitCode"])
	if !ok {
		return CommandResult{Err: errors.New("CODEX_COMMAND_EXIT_CODE_MISSING")}
	}
	stdout, _ := object["stdout"].(string)
	stderr, _ := object["stderr"].(string)
	return CommandResult{ExitCode: int(exitCode), Stdout: stdout, Stderr: stderr}
}

func commandServerEnvironment(entries []string) []string {
	overrides := make(map[string]*string)
	for _, entry := range entries {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(strings.ToUpper(key), "CWAPI_INTERNAL_") {
			overrides[key] = nil
		}
	}
	return childenv.Merge(entries, overrides)
}

func validateCommandSpec(spec CommandSpec) error {
	if !processcontract.ProcessIDPattern.MatchString(spec.ProcessID) {
		return errors.New("CODEX_COMMAND_PROCESS_ID_INVALID")
	}
	for _, path := range []string{spec.Executable, spec.CWD, spec.WritableRoot} {
		if !filepath.IsAbs(path) {
			return errors.New("CODEX_COMMAND_PATH_INVALID")
		}
	}
	if !pathWithin(spec.CWD, spec.WritableRoot) {
		return errors.New("CODEX_COMMAND_CWD_OUTSIDE_ROOT")
	}
	info, err := os.Stat(spec.Executable)
	if err != nil || !info.Mode().IsRegular() {
		return errors.New("CODEX_COMMAND_EXECUTABLE_INVALID")
	}
	if len(spec.Environment) == 0 {
		return errors.New("CODEX_COMMAND_ENVIRONMENT_REQUIRED")
	}
	if spec.Sandbox != "" && spec.Sandbox != CommandSandboxWorkspaceWrite && spec.Sandbox != CommandSandboxFullAccess {
		return errors.New("CODEX_COMMAND_SANDBOX_INVALID")
	}
	return nil
}

func objectString(value any, key string) string {
	object, _ := value.(map[string]any)
	text, _ := object[key].(string)
	return text
}
