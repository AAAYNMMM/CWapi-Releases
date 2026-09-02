package security

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
)

type CommandRuntime struct {
	DataRoot         string
	WorkspaceRoot    string
	WorkspaceRuntime string
	ProcessRoot      string
	BridgeRoot       string
	AuthRoot         string
	profile          Profile
}

func PrepareCommandRuntime(dataRoot, workspaceRoot, processID, profileValue string) (*CommandRuntime, error) {
	profile, err := ParseProfile(profileValue)
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(dataRoot) || !filepath.IsAbs(workspaceRoot) || !validProcessID(processID) {
		return nil, errors.New("SECURITY_RUNTIME_INPUT_INVALID")
	}
	workspaceRuntime, err := WorkspaceRuntimeRoot(dataRoot, workspaceRoot)
	if err != nil {
		return nil, err
	}
	processBase := filepath.Join(filepath.Clean(dataRoot), "runtime", "process")
	processRoot := filepath.Join(processBase, processID)
	authRoot := filepath.Join(filepath.Clean(dataRoot), "auth", "github")
	for _, root := range []string{workspaceRuntime, processBase, authRoot} {
		if err := secureDirectory(root, filepath.Clean(dataRoot)); err != nil {
			return nil, err
		}
	}
	if err := os.Mkdir(processRoot, 0o700); err != nil {
		return nil, fmt.Errorf("SECURITY_PROCESS_ROOT_CREATE_FAILED: %w", err)
	}
	runtime := &CommandRuntime{
		DataRoot: filepath.Clean(dataRoot), WorkspaceRoot: filepath.Clean(workspaceRoot), WorkspaceRuntime: workspaceRuntime,
		ProcessRoot: processRoot, BridgeRoot: filepath.Join(processRoot, "bridge"), AuthRoot: authRoot, profile: profile,
	}
	directories := []string{filepath.Join(processRoot, "temp"), runtime.BridgeRoot}
	if profile == ProfileSafe {
		directories = append(directories,
			filepath.Join(processRoot, "profile"), filepath.Join(processRoot, "appdata"), filepath.Join(processRoot, "localappdata"), filepath.Join(processRoot, "xdg-config"),
			filepath.Join(workspaceRuntime, "cache", "go-build"), filepath.Join(workspaceRuntime, "cache", "go-mod"), filepath.Join(workspaceRuntime, "cache", "npm"),
			filepath.Join(workspaceRuntime, "cache", "pip"), filepath.Join(workspaceRuntime, "cache", "cargo"), filepath.Join(workspaceRuntime, "cache", "gradle"),
			filepath.Join(workspaceRuntime, "cache", "python"), filepath.Join(workspaceRuntime, "cache", "xdg"),
		)
	}
	for _, directory := range directories {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			runtime.Cleanup()
			return nil, fmt.Errorf("SECURITY_RUNTIME_DIR_CREATE_FAILED: %w", err)
		}
	}
	return runtime, nil
}

func WorkspaceRuntimeRoot(dataRoot, workspaceRoot string) (string, error) {
	if !filepath.IsAbs(dataRoot) || !filepath.IsAbs(workspaceRoot) {
		return "", errors.New("SECURITY_WORKSPACE_RUNTIME_INPUT_INVALID")
	}
	canonical, err := CanonicalPath(workspaceRoot, workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("SECURITY_WORKSPACE_RESOLVE_FAILED: %w", err)
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(canonical))))
	return filepath.Join(filepath.Clean(dataRoot), "runtime", "workspaces", hex.EncodeToString(sum[:])), nil
}

func (r *CommandRuntime) Environment(entries []string, sandbox string) ([]string, error) {
	if r == nil {
		return nil, errors.New("SECURITY_RUNTIME_UNAVAILABLE")
	}
	temp := filepath.Join(r.ProcessRoot, "temp")
	ghConfig := r.AuthRoot
	sandboxValue := sandbox
	base := append([]string(nil), entries...)
	trustedInternal := map[string]*string{}
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.HasPrefix(strings.ToUpper(key), "CWAPI_INTERNAL_") {
			copyValue := value
			trustedInternal[key] = &copyValue
		}
	}
	if r.profile == ProfileFull {
		base = os.Environ()
		preserve := map[string]*string{}
		for _, entry := range entries {
			key, value, ok := strings.Cut(entry, "=")
			if !ok {
				continue
			}
			upper := strings.ToUpper(key)
			if upper == "PATH" || strings.HasPrefix(upper, "CWAPI_INTERNAL_") {
				copyValue := value
				preserve[key] = &copyValue
				if strings.HasPrefix(upper, "CWAPI_INTERNAL_") {
					trustedInternal[key] = &copyValue
				}
			}
		}
		base = childenv.Merge(base, preserve)
	}
	overrides := map[string]*string{
		"TEMP": &temp, "TMP": &temp, "TMPDIR": &temp, "GH_CONFIG_DIR": &ghConfig,
		"CWAPI_CODEX_SANDBOX": &sandboxValue,
	}
	if r.profile == ProfileSafe {
		profile := filepath.Join(r.ProcessRoot, "profile")
		appData := filepath.Join(r.ProcessRoot, "appdata")
		localAppData := filepath.Join(r.ProcessRoot, "localappdata")
		cacheRoot := filepath.Join(r.WorkspaceRuntime, "cache")
		null, one, count, telemetry := os.DevNull, "1", "2", "off"
		hookKey, interactiveKey, interactiveValue := "core.hooksPath", "credential.interactive", "never"
		nodePreload := filepath.Join(r.ProcessRoot, "node-preload.cjs")
		if err := os.WriteFile(nodePreload, []byte(nodeSandboxPreload), 0o600); err != nil {
			return nil, fmt.Errorf("SECURITY_NODE_PRELOAD_CREATE_FAILED: %w", err)
		}
		nodeOptions := `--require="` + filepath.ToSlash(nodePreload) + `"`
		overrides["GOTMPDIR"] = &temp
		overrides["GOCACHE"] = pointer(filepath.Join(cacheRoot, "go-build"))
		overrides["GOMODCACHE"] = pointer(filepath.Join(cacheRoot, "go-mod"))
		overrides["NPM_CONFIG_CACHE"] = pointer(filepath.Join(cacheRoot, "npm"))
		overrides["NPM_CONFIG_USERCONFIG"] = &null
		overrides["PIP_CACHE_DIR"] = pointer(filepath.Join(cacheRoot, "pip"))
		overrides["PYTHONPYCACHEPREFIX"] = pointer(filepath.Join(cacheRoot, "python"))
		overrides["CARGO_HOME"] = pointer(filepath.Join(cacheRoot, "cargo"))
		overrides["GRADLE_USER_HOME"] = pointer(filepath.Join(cacheRoot, "gradle"))
		overrides["APPDATA"] = &appData
		overrides["LOCALAPPDATA"] = &localAppData
		overrides["USERPROFILE"] = &profile
		overrides["HOME"] = &profile
		overrides["XDG_CONFIG_HOME"] = pointer(filepath.Join(r.ProcessRoot, "xdg-config"))
		overrides["XDG_CACHE_HOME"] = pointer(filepath.Join(cacheRoot, "xdg"))
		overrides["GIT_CONFIG_NOSYSTEM"] = &one
		overrides["GIT_CONFIG_GLOBAL"] = &null
		overrides["GIT_CONFIG_COUNT"] = &count
		overrides["GIT_CONFIG_KEY_0"] = &hookKey
		overrides["GIT_CONFIG_VALUE_0"] = &null
		overrides["GIT_CONFIG_KEY_1"] = &interactiveKey
		overrides["GIT_CONFIG_VALUE_1"] = &interactiveValue
		overrides["GIT_TERMINAL_PROMPT"] = pointer("0")
		overrides["GCM_INTERACTIVE"] = pointer("Never")
		overrides["GOTELEMETRY"] = &telemetry
		overrides["NODE_OPTIONS"] = &nodeOptions
	}
	for _, entry := range base {
		key, _, ok := strings.Cut(entry, "=")
		if ok && sensitiveEnvironmentKey(key) {
			overrides[key] = nil
		}
	}
	for key, value := range trustedInternal {
		overrides[key] = value
	}
	return childenv.Merge(base, overrides), nil
}

func (r *CommandRuntime) WriteProcessState(value any) error {
	if r == nil {
		return errors.New("SECURITY_RUNTIME_UNAVAILABLE")
	}
	payload, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	return os.WriteFile(filepath.Join(r.ProcessRoot, "process.json"), payload, 0o600)
}

func (r *CommandRuntime) Cleanup() {
	if r != nil && r.ProcessRoot != "" {
		_ = os.RemoveAll(r.ProcessRoot)
	}
}

func sensitiveEnvironmentKey(key string) bool {
	upper := strings.ToUpper(strings.TrimSpace(key))
	return strings.HasPrefix(upper, "CWAPI_") || strings.HasPrefix(upper, "OPENAI_") || strings.HasPrefix(upper, "CODEX_") || upper == "CONTROL_PLANE_API_KEY"
}

func secureDirectory(path, dataRoot string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("SECURITY_RUNTIME_ROOT_CREATE_FAILED: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || !PathWithin(resolved, dataRoot) {
		return errors.New("SECURITY_RUNTIME_ROOT_INVALID")
	}
	return nil
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
