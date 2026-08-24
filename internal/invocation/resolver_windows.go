//go:build windows

package invocation

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
	"golang.org/x/sys/windows"
)

const bridgeName = ".cwapi-process-bridge.cmd"

var executableExtensions = []string{".com", ".exe", ".cmd", ".bat"}

func New(environment []string) (*Resolver, error) {
	copyEnv := append([]string(nil), environment...)
	path := childenv.Value(copyEnv, "PATH")
	systemRoot := childenv.Value(copyEnv, "SystemRoot")
	if path == "" || systemRoot == "" || !filepath.IsAbs(systemRoot) {
		return nil, errors.New("INVOCATION_ENVIRONMENT_INVALID")
	}
	return &Resolver{environment: copyEnv, path: path, systemRoot: filepath.Clean(systemRoot)}, nil
}

func (r *Resolver) Resolve(repositoryRoot string, input processcontract.StartArguments) (Final, error) {
	if r == nil || !filepath.IsAbs(repositoryRoot) {
		return Final{}, errors.New("INVOCATION_REPOSITORY_ROOT_INVALID")
	}
	root, err := resolveDirectory(repositoryRoot)
	if err != nil {
		return Final{}, fmt.Errorf("INVOCATION_REPOSITORY_ROOT_INVALID: %w", err)
	}
	cwd, err := r.resolveCWD(root, input.CWD)
	if err != nil {
		return Final{}, err
	}
	target, err := r.resolveExecutable(cwd, input.Command)
	if err != nil {
		return Final{}, err
	}
	final := Final{
		Executable: target, Argv: append([]string(nil), input.Argv...), CWD: cwd,
		Environment: append([]string(nil), r.environment...), TargetExecutable: target,
		TargetArgv: append([]string(nil), input.Argv...),
	}
	extension := strings.ToLower(filepath.Ext(target))
	if extension == ".cmd" || extension == ".bat" {
		if err := r.wrapBatch(&final); err != nil {
			return Final{}, err
		}
	}
	if err := validateCommandLine(final.Executable, final.Argv); err != nil {
		return Final{}, err
	}
	return final, nil
}

func (r *Resolver) resolveCWD(root, remote string) (string, error) {
	if remote == "" {
		return root, nil
	}
	if driveRelative(remote) {
		return "", errors.New("INVOCATION_CWD_DRIVE_RELATIVE")
	}
	native := filepath.FromSlash(remote)
	candidate := native
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, candidate)
	}
	resolved, err := resolveDirectory(candidate)
	if err != nil || !pathWithin(resolved, root) {
		return "", errors.New("INVOCATION_CWD_OUTSIDE_REPOSITORY")
	}
	return resolved, nil
}

func (r *Resolver) resolveExecutable(cwd, remote string) (string, error) {
	if driveRelative(remote) {
		return "", errors.New("INVOCATION_EXECUTABLE_DRIVE_RELATIVE")
	}
	if strings.Contains(remote, "/") || strings.Contains(remote, ":") {
		native := filepath.FromSlash(remote)
		if !filepath.IsAbs(native) {
			native = filepath.Join(cwd, native)
		}
		return resolveExecutableFile(native)
	}
	extension := strings.ToLower(filepath.Ext(remote))
	if extension != "" && !allowedExtension(extension) {
		return "", errors.New("INVOCATION_EXECUTABLE_EXTENSION_INVALID")
	}
	names := []string{remote}
	if extension == "" {
		names = names[:0]
		for _, candidateExtension := range executableExtensions {
			names = append(names, remote+candidateExtension)
		}
	}
	for _, rawDirectory := range filepath.SplitList(r.path) {
		directory := strings.Trim(strings.TrimSpace(rawDirectory), `"`)
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		for _, name := range names {
			if resolved, err := resolveExecutableFile(filepath.Join(directory, name)); err == nil {
				return resolved, nil
			}
		}
	}
	return "", errors.New("INVOCATION_EXECUTABLE_NOT_FOUND")
}

func (r *Resolver) wrapBatch(final *Final) error {
	for _, argument := range final.TargetArgv {
		if strings.ContainsAny(argument, "\"\r\n") {
			return errors.New("INVOCATION_BATCH_ARGUMENT_UNREPRESENTABLE")
		}
	}
	payload, err := protocol.CanonicalObject(map[string]any{
		"argv": final.TargetArgv, "executable": final.TargetExecutable,
	})
	if err != nil {
		return err
	}
	binding := base64.RawURLEncoding.EncodeToString(payload)
	bridgePath := filepath.Join(final.CWD, bridgeName)
	bridgeBody := batchBridgeBody(len(final.TargetArgv))
	if err := ensureBridge(bridgePath, bridgeBody); err != nil {
		return err
	}
	cmdPath, err := resolveExecutableFile(filepath.Join(r.systemRoot, "System32", "cmd.exe"))
	if err != nil || !strings.EqualFold(filepath.Dir(cmdPath), filepath.Join(r.systemRoot, "System32")) {
		return errors.New("INVOCATION_SYSTEM_CMD_INVALID")
	}
	final.Executable = cmdPath
	final.Argv = []string{"/d", "/s", "/v:off", "/c", bridgeName, binding}
	final.BindingPayload = binding
	final.BridgePath = bridgePath
	overrides := map[string]*string{
		"CWAPI_INTERNAL_INVOCATION_B64": &binding,
		"CWAPI_INTERNAL_TARGET":         &final.TargetExecutable,
	}
	for index := range final.TargetArgv {
		key := fmt.Sprintf("CWAPI_INTERNAL_ARG_%03d", index)
		overrides[key] = &final.TargetArgv[index]
	}
	final.Environment = childenv.Merge(final.Environment, overrides)
	if environmentUTF16Length(final.Environment) >= 32767 {
		return errors.New("INVOCATION_ENVIRONMENT_TOO_LARGE")
	}
	return nil
}

func batchBridgeBody(argumentCount int) string {
	var builder strings.Builder
	builder.WriteString("@echo off\r\nsetlocal EnableDelayedExpansion\r\n\"!CWAPI_INTERNAL_TARGET!\"")
	for index := 0; index < argumentCount; index++ {
		fmt.Fprintf(&builder, " \"!CWAPI_INTERNAL_ARG_%03d!\"", index)
	}
	builder.WriteString("\r\n")
	return builder.String()
}

func ensureBridge(path, expected string) error {
	if payload, err := os.ReadFile(path); err == nil {
		if string(payload) != expected {
			return errors.New("INVOCATION_BRIDGE_CHANGED")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("INVOCATION_BRIDGE_READ_FAILED: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("INVOCATION_BRIDGE_CREATE_FAILED: %w", err)
	}
	if _, err := file.WriteString(expected); err != nil {
		_ = file.Close()
		return fmt.Errorf("INVOCATION_BRIDGE_WRITE_FAILED: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("INVOCATION_BRIDGE_CLOSE_FAILED: %w", err)
	}
	return nil
}

func resolveDirectory(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return "", errors.New("not a directory")
	}
	return filepath.Clean(absolute), nil
}

func resolveExecutableFile(path string) (string, error) {
	extension := strings.ToLower(filepath.Ext(path))
	if !allowedExtension(extension) {
		return "", errors.New("INVOCATION_EXECUTABLE_EXTENSION_INVALID")
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("INVOCATION_EXECUTABLE_NOT_REGULAR")
	}
	return filepath.Clean(absolute), nil
}

func validateCommandLine(executable string, argv []string) error {
	line := windows.ComposeCommandLine(append([]string{executable}, argv...))
	if len(utf16.Encode([]rune(line)))+1 >= 32767 {
		return errors.New("INVOCATION_COMMAND_LINE_TOO_LARGE")
	}
	return nil
}

func environmentUTF16Length(entries []string) int {
	total := 1
	for _, entry := range entries {
		total += len(utf16.Encode([]rune(entry))) + 1
	}
	return total
}

func allowedExtension(extension string) bool {
	for _, allowed := range executableExtensions {
		if strings.EqualFold(extension, allowed) {
			return true
		}
	}
	return false
}

func driveRelative(path string) bool {
	return len(path) >= 2 && path[1] == ':' && (len(path) == 2 || path[2] != '/')
}

func pathWithin(path, root string) bool {
	if strings.EqualFold(path, root) {
		return true
	}
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}
