//go:build windows

package gateway

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCWapiProcessServerResolvesWindowsExecutablePaths(t *testing.T) {
	workspace := filepath.Join(t.TempDir(), "exact workspace with spaces")
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		t.Fatal(err)
	}
	harness := startProcessServerHarness(t)
	commit := strings.Repeat("b", 40)

	venvPython := filepath.Join(workspace, ".venv", "Scripts", "python.exe")
	copyExecutable(t, harness.node, venvPython)
	spacedPython := filepath.Join(workspace, "tool chain", "Python 3.12", "python.exe")
	linkOrCopyExecutable(t, venvPython, spacedPython)

	tests := []struct {
		name         string
		command      string
		requestID    string
		marker       string
		resolution   string
		expectedPath string
	}{
		{
			name: "absolute backslash path with spaces", command: spacedPython,
			requestID: "REQABSPATHBACKSLASH001", marker: "CWAPI_ABSOLUTE_BACKSLASH_OK", resolution: "absolute_path", expectedPath: spacedPython,
		},
		{
			name: "absolute forward slash path with spaces", command: strings.ReplaceAll(spacedPython, `\`, "/"),
			requestID: "REQABSPATHFORWARDSLASH001", marker: "CWAPI_ABSOLUTE_FORWARD_SLASH_OK", resolution: "absolute_path", expectedPath: spacedPython,
		},
		{
			name: "venv relative backslash path", command: `.venv\Scripts\python.exe`,
			requestID: "REQVENVBCKSLASH001", marker: "CWAPI_VENV_BACKSLASH_OK", resolution: "working_directory_relative", expectedPath: `.venv\Scripts\python.exe`,
		},
		{
			name: "venv relative forward slash path", command: ".venv/Scripts/python.exe",
			requestID: "REQVENVFORWARDSLASH001", marker: "CWAPI_VENV_FORWARD_SLASH_OK", resolution: "working_directory_relative", expectedPath: `.venv\Scripts\python.exe`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result := runQuickCommand(t, harness, workspace, commit, test.requestID, test.command,
				"-e", fmt.Sprintf("console.log('%s')", test.marker))
			if result["state"] != "completed" || result["exit_code"] != float64(0) ||
				result["command_name"] != "python.exe" || result["command_resolution"] != test.resolution ||
				result["command_path"] != test.expectedPath || result["executable_kind"] != "native" ||
				!strings.Contains(fmt.Sprint(result["stdout_tail"]), test.marker) {
				t.Fatalf("result=%#v", result)
			}
		})
	}

	shim := filepath.Join(workspace, "node_modules", ".bin", "cwapi-path-test.cmd")
	if err := os.MkdirAll(filepath.Dir(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shim, []byte("@echo off\r\necho CWAPI_COMMAND_SHIM_OK:%~1\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shimResult := runQuickCommand(t, harness, workspace, commit, "REQCOMMANDSHIM001",
		`node_modules\.bin\cwapi-path-test.cmd`, "hello world")
	if shimResult["state"] != "completed" || shimResult["executable_kind"] != "windows_command_script" ||
		shimResult["command_resolution"] != "working_directory_relative" ||
		!strings.Contains(fmt.Sprint(shimResult["stdout_tail"]), "CWAPI_COMMAND_SHIM_OK:hello world") {
		t.Fatalf("shim result=%#v", shimResult)
	}

	missing := processArguments(workspace, commit, "REQCOMMANDMISSING001", `.venv\Scripts\missing.exe`, nil)
	requireToolError(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": missing}), "PROCESS_COMMAND_NOT_FOUND")
	quoted := processArguments(workspace, commit, "REQCOMMANDQUOTED001", `"C:\Python312\python.exe"`, nil)
	requireToolError(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": quoted}), "PROCESS_COMMAND_INVALID")
}

func runQuickCommand(t *testing.T, harness *processServerHarness, workspace, commit, requestID, command string, argv ...string) map[string]any {
	t.Helper()
	arguments := processArguments(workspace, commit, requestID, command, argv)
	result := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": arguments}))
	if result["state"] == "starting" || result["state"] == "running" {
		result = waitProcessTerminal(t, harness, fmt.Sprint(result["process_id"]), workspace, commit)
	}
	return result
}

func processArguments(workspace, commit, requestID, command string, argv []string) map[string]any {
	values := make([]any, len(argv))
	for index, value := range argv {
		values[index] = value
	}
	return map[string]any{
		"command": command, "argv": values, "cwd": ".",
		"_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": requestID,
	}
}

func linkOrCopyExecutable(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, destination); err == nil {
		return
	}
	copyExecutable(t, source, destination)
}

func copyExecutable(t *testing.T, source, destination string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	input, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		t.Fatal(err)
	}
	if err := output.Close(); err != nil {
		t.Fatal(err)
	}
}
