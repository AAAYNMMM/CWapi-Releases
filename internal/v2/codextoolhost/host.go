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
	"github.com/AAAYNMMM/CWapi/internal/executionpolicy"
	"github.com/AAAYNMMM/CWapi/internal/invocation"
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
)

const (
	defaultTimeout = 120 * time.Second
	maxTimeout     = 600 * time.Second
	maxOutputBytes = 64 * 1024
)

// Host is a model-free bridge to the private Codex app-server command/exec
// development tool. It does not create Codex threads or turns and therefore
// does not use Codex account or model functionality.
type Host struct {
	service  *codex.Service
	resolver *invocation.Resolver
	dataRoot string
	git      string

	policyMu      sync.RWMutex
	accessProfile string
	networkAccess bool
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
	gitExecutable, err := resolver.ResolvePATHExecutable("git")
	if err != nil {
		return nil, fmt.Errorf("CODEX_TOOLHOST_GIT_UNAVAILABLE: %w", err)
	}
	return &Host{
		service: service, resolver: resolver, dataRoot: filepath.Clean(dataRoot), git: gitExecutable,
		accessProfile: cfg.AccessProfile, networkAccess: cfg.NetworkAccess,
	}, nil
}

func (h *Host) SetAccessProfile(profile string) error {
	profile = strings.ToLower(strings.TrimSpace(profile))
	if profile != v2config.AccessProfileSafe && profile != v2config.AccessProfileFull {
		return errors.New("CONFIG_CODEX_ACCESS_PROFILE_INVALID")
	}
	if h == nil {
		return errors.New("CODEX_TOOLHOST_UNAVAILABLE")
	}
	h.policyMu.Lock()
	h.accessProfile = profile
	h.policyMu.Unlock()
	return nil
}

func (h *Host) SetNetworkAccess(allowed bool) error {
	if h == nil {
		return errors.New("CODEX_TOOLHOST_UNAVAILABLE")
	}
	h.policyMu.Lock()
	h.networkAccess = allowed
	h.policyMu.Unlock()
	return nil
}

func (h *Host) currentPolicy() (string, bool) {
	h.policyMu.RLock()
	defer h.policyMu.RUnlock()
	return h.accessProfile, h.networkAccess
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
	profile, networkAccess := h.currentPolicy()
	policyInvocation := executionpolicy.Invocation{
		Executable: final.TargetExecutable, Argv: final.TargetArgv, CWD: final.CWD, AccessProfile: profile,
		TrustedGitExecutable: h.git,
	}
	if err := executionpolicy.Check(policyInvocation, filepath.Clean(workspaceRoot), h.dataRoot); err != nil {
		return ExecResult{}, err
	}
	sandbox := codex.CommandSandboxWorkspaceWrite
	if executionpolicy.RequiresFullAccess(policyInvocation) {
		sandbox = codex.CommandSandboxFullAccess
	}
	if executionpolicy.AllowsHostGitCredentials(policyInvocation) && !networkAccess {
		return ExecResult{}, errors.New("CODING_NETWORK_ACCESS_REQUIRED: FULL git push requires explicit network access")
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
	environment, cleanupRuntime, err := commandRuntimeEnvironment(
		filepath.Clean(workspaceRoot), processID, final.Environment,
		executionpolicy.AllowsHostGitCredentials(policyInvocation), sandbox,
	)
	if err != nil {
		return ExecResult{}, err
	}
	defer cleanupRuntime()
	handle, err := h.service.StartCommand(execCtx, codex.CommandSpec{
		ProcessID: processID, Executable: final.Executable, Argv: final.Argv,
		CWD: final.CWD, WritableRoot: filepath.Clean(workspaceRoot),
		Environment: environment, Sandbox: sandbox, NetworkAccess: networkAccess,
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
	base := childenv.Git(filepath.Join(dataRoot, "temp", "gh-config"))
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
	prompt, interactive, noSystem, noGlobal := "0", "Never", "1", os.DevNull
	return childenv.Merge(base, map[string]*string{
		"PATH": &pathValue, "GIT_TERMINAL_PROMPT": &prompt, "GCM_INTERACTIVE": &interactive,
		"GIT_CONFIG_NOSYSTEM": &noSystem, "GIT_CONFIG_GLOBAL": &noGlobal,
	})
}

func commandRuntimeEnvironment(workspaceRoot, processID string, entries []string, allowHostGitCredentials bool, sandbox string) ([]string, func(), error) {
	runtimeRoot, err := createCommandRuntimeRoot(workspaceRoot, processID)
	if err != nil {
		return nil, func() {}, err
	}
	paths := map[string]string{
		"TEMP":             filepath.Join(runtimeRoot, "temp"),
		"GOCACHE":          filepath.Join(runtimeRoot, "cache", "go-build"),
		"GOMODCACHE":       filepath.Join(runtimeRoot, "cache", "go-mod"),
		"NPM_CONFIG_CACHE": filepath.Join(runtimeRoot, "cache", "npm"),
		"PIP_CACHE_DIR":    filepath.Join(runtimeRoot, "cache", "pip"),
		"CARGO_HOME":       filepath.Join(runtimeRoot, "cache", "cargo"),
		"APPDATA":          filepath.Join(runtimeRoot, "appdata"),
		"LOCALAPPDATA":     filepath.Join(runtimeRoot, "localappdata"),
		"USERPROFILE":      filepath.Join(runtimeRoot, "profile"),
		"GH_CONFIG_DIR":    filepath.Join(runtimeRoot, "gh-config"),
		"XDG_CONFIG_HOME":  filepath.Join(runtimeRoot, "xdg", "config"),
		"XDG_CACHE_HOME":   filepath.Join(runtimeRoot, "xdg", "cache"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(path, 0o700); err != nil {
			_ = os.RemoveAll(runtimeRoot)
			return nil, func() {}, fmt.Errorf("CODEX_TOOLHOST_RUNTIME_DIR_CREATE_FAILED: %w", err)
		}
	}
	nodePreload := filepath.Join(runtimeRoot, "node-preload.cjs")
	if err := os.WriteFile(nodePreload, []byte(nodeSandboxPreload), 0o600); err != nil {
		_ = os.RemoveAll(runtimeRoot)
		return nil, func() {}, fmt.Errorf("CODEX_TOOLHOST_NODE_PRELOAD_CREATE_FAILED: %w", err)
	}
	temp, profile := paths["TEMP"], paths["USERPROFILE"]
	npmUserConfig, gitNoSystem, gitGlobal := os.DevNull, "1", os.DevNull
	gitCount, hookKey, hookValue := "2", "core.hooksPath", os.DevNull
	interactiveKey, interactiveValue := "credential.interactive", "never"
	telemetry := "off"
	nodeOptions := `--require="` + filepath.ToSlash(nodePreload) + `"`
	overrides := map[string]*string{
		"TEMP": &temp, "TMP": &temp, "TMPDIR": &temp, "GOTMPDIR": &temp,
		"GOCACHE": pointer(paths["GOCACHE"]), "GOMODCACHE": pointer(paths["GOMODCACHE"]),
		"NPM_CONFIG_CACHE": pointer(paths["NPM_CONFIG_CACHE"]), "NPM_CONFIG_USERCONFIG": &npmUserConfig,
		"PIP_CACHE_DIR": pointer(paths["PIP_CACHE_DIR"]), "PYTHONPYCACHEPREFIX": pointer(filepath.Join(runtimeRoot, "cache", "python")),
		"CARGO_HOME": pointer(paths["CARGO_HOME"]), "GRADLE_USER_HOME": pointer(filepath.Join(runtimeRoot, "cache", "gradle")),
		"APPDATA": pointer(paths["APPDATA"]), "LOCALAPPDATA": pointer(paths["LOCALAPPDATA"]),
		"USERPROFILE": &profile, "HOME": &profile, "GH_CONFIG_DIR": pointer(paths["GH_CONFIG_DIR"]),
		"XDG_CONFIG_HOME": pointer(paths["XDG_CONFIG_HOME"]), "XDG_CACHE_HOME": pointer(paths["XDG_CACHE_HOME"]),
		"GIT_CONFIG_NOSYSTEM": &gitNoSystem, "GIT_CONFIG_GLOBAL": &gitGlobal,
		"GIT_CONFIG_COUNT": &gitCount, "GIT_CONFIG_KEY_0": &hookKey, "GIT_CONFIG_VALUE_0": &hookValue,
		"GIT_CONFIG_KEY_1": &interactiveKey, "GIT_CONFIG_VALUE_1": &interactiveValue,
		"GOTELEMETRY": &telemetry, "CWAPI_CODEX_SANDBOX": &sandbox, "NODE_OPTIONS": &nodeOptions,
	}
	for _, key := range []string{
		"OPENAI_API_KEY", "CODEX_API_KEY", "GH_TOKEN", "GITHUB_TOKEN",
		"GIT_TRACE", "GIT_CURL_VERBOSE", "GH_DEBUG",
	} {
		overrides[key] = nil
	}
	if sandbox == codex.CommandSandboxFullAccess {
		for _, key := range []string{"APPDATA", "LOCALAPPDATA", "USERPROFILE", "HOME"} {
			if value := strings.TrimSpace(os.Getenv(key)); value != "" {
				overrides[key] = &value
			} else {
				overrides[key] = nil
			}
		}
	}
	if allowHostGitCredentials {
		gitAllowProtocol, gitProtocolFromUser := "https:ssh", "0"
		overrides["GIT_CONFIG_NOSYSTEM"] = nil
		overrides["GIT_CONFIG_GLOBAL"] = nil
		overrides["GIT_ALLOW_PROTOCOL"] = &gitAllowProtocol
		overrides["GIT_PROTOCOL_FROM_USER"] = &gitProtocolFromUser
	}
	cleanup := func() {
		_ = os.RemoveAll(runtimeRoot)
		_ = os.Remove(filepath.Dir(runtimeRoot))
	}
	return childenv.Merge(entries, overrides), cleanup, nil
}

func createCommandRuntimeRoot(workspaceRoot, processID string) (string, error) {
	if !validProcessID(processID) {
		return "", errors.New("CODEX_TOOLHOST_PROCESS_ID_INVALID")
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(filepath.Clean(workspaceRoot))
	if err != nil {
		return "", fmt.Errorf("CODEX_TOOLHOST_WORKSPACE_RESOLVE_FAILED: %w", err)
	}
	resolvedWorkspace, err = filepath.Abs(resolvedWorkspace)
	if err != nil {
		return "", fmt.Errorf("CODEX_TOOLHOST_WORKSPACE_RESOLVE_FAILED: %w", err)
	}
	runtimeBase := filepath.Join(resolvedWorkspace, ".cwapi-runtime")
	if info, statErr := os.Lstat(runtimeBase); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", errors.New("CODEX_TOOLHOST_RUNTIME_ROOT_INVALID")
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", fmt.Errorf("CODEX_TOOLHOST_RUNTIME_ROOT_INVALID: %w", statErr)
	} else if err := os.Mkdir(runtimeBase, 0o700); err != nil {
		return "", fmt.Errorf("CODEX_TOOLHOST_RUNTIME_ROOT_CREATE_FAILED: %w", err)
	}
	resolvedBase, err := filepath.EvalSymlinks(runtimeBase)
	if err != nil || !runtimePathWithin(resolvedBase, resolvedWorkspace) {
		return "", errors.New("CODEX_TOOLHOST_RUNTIME_ROOT_OUTSIDE_WORKSPACE")
	}
	runtimeRoot := filepath.Join(runtimeBase, processID)
	if err := os.Mkdir(runtimeRoot, 0o700); err != nil {
		_ = os.Remove(runtimeBase)
		return "", fmt.Errorf("CODEX_TOOLHOST_RUNTIME_DIR_CREATE_FAILED: %w", err)
	}
	return runtimeRoot, nil
}

func validProcessID(value string) bool {
	if !strings.HasPrefix(value, "proc-") || len(value) != len("proc-")+24 {
		return false
	}
	for _, char := range value[len("proc-"):] {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func runtimePathWithin(path, root string) bool {
	path, root = filepath.Clean(path), filepath.Clean(root)
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func pointer(value string) *string { return &value }

const nodeSandboxPreload = `"use strict";
const childProcess = require("node:child_process");
const { EventEmitter } = require("node:events");
const { syncBuiltinESMExports } = require("node:module");
const originalExec = childProcess.exec;
childProcess.exec = function(command, ...args) {
  if (process.platform === "win32" && command === "net use") {
    const callback = [...args].reverse().find((value) => typeof value === "function");
    const child = new EventEmitter();
    process.nextTick(() => {
      const error = Object.assign(new Error("net use is unavailable in the CWapi SAFE sandbox"), { code: "EPERM" });
      if (callback) callback(error, "", "");
      child.emit("exit", 1, null);
      child.emit("close", 1, null);
    });
    return child;
  }
  return Reflect.apply(originalExec, this, [command, ...args]);
};
syncBuiltinESMExports();
`

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
