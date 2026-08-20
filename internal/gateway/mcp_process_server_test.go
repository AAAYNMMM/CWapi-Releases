package gateway

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type processServerHarness struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  bytes.Buffer
	logRoot string
	node    string
	nextID  int
}

func startProcessServerHarness(t *testing.T) *processServerHarness {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is unavailable")
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "mcp", "cwapi", "process-server.cjs"))
	if err != nil {
		t.Fatal(err)
	}
	logRoot := filepath.Join(t.TempDir(), "logs")
	cmd := exec.Command(node, script)
	cmd.Env = append(os.Environ(), "CWAPI_PROCESS_LOG_ROOT="+logRoot)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	harness := &processServerHarness{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), logRoot: logRoot, node: node}
	cmd.Stderr = &harness.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = harness.stdin.Close()
		done := make(chan struct{})
		go func() {
			_ = harness.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = harness.cmd.Process.Kill()
			<-done
		}
	})
	return harness
}

func (h *processServerHarness) call(t *testing.T, method string, params map[string]any) map[string]any {
	t.Helper()
	h.nextID++
	payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": h.nextID, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.stdin.Write(append(payload, '\n')); err != nil {
		t.Fatalf("write MCP request: %v stderr=%s", err, h.stderr.String())
	}
	type readResult struct {
		line []byte
		err  error
	}
	ready := make(chan readResult, 1)
	go func() {
		line, err := h.stdout.ReadBytes('\n')
		ready <- readResult{line: line, err: err}
	}()
	select {
	case result := <-ready:
		if result.err != nil {
			t.Fatalf("read MCP response: %v stderr=%s", result.err, h.stderr.String())
		}
		var response map[string]any
		if err := json.Unmarshal(result.line, &response); err != nil {
			t.Fatalf("decode MCP response: %v line=%q", err, result.line)
		}
		if response["error"] != nil {
			t.Fatalf("MCP protocol error: %#v", response["error"])
		}
		return response
	case <-time.After(10 * time.Second):
		t.Fatalf("MCP response timeout stderr=%s", h.stderr.String())
		return nil
	}
}

func toolTextObject(t *testing.T, response map[string]any) map[string]any {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatalf("tool result=%#v", response["result"])
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("tool content=%#v", result)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("tool content item=%#v", content[0])
	}
	text, _ := first["text"].(string)
	var value map[string]any
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("tool text=%q err=%v", text, err)
	}
	return value
}

func requireToolError(t *testing.T, response map[string]any, code string) {
	t.Helper()
	result, ok := response["result"].(map[string]any)
	encoded, _ := json.Marshal(result)
	if !ok || result["isError"] != true || !bytes.Contains(encoded, []byte(code)) {
		t.Fatalf("tool error missing %s: %#v", code, response)
	}
}

func waitProcessTerminal(t *testing.T, harness *processServerHarness, processID, workspace, commit string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		status := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_status", "arguments": map[string]any{
			"process_id": processID, "_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQCOMMANDWAIT",
		}}))
		if status["state"] != "starting" && status["state"] != "running" {
			return status
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("process %s did not reach terminal state", processID)
	return nil
}

func TestCWapiProcessServerStartsReportsAndStopsOwnedEntrypoint(t *testing.T) {
	harness := startProcessServerHarness(t)
	initialized := harness.call(t, "initialize", map[string]any{"protocolVersion": "2024-11-05"})
	if fmt.Sprint(initialized["result"]) == "" {
		t.Fatal("initialize result missing")
	}
	listed := harness.call(t, "tools/list", nil)
	encodedTools, _ := json.Marshal(listed["result"])
	if !bytes.Contains(encodedTools, []byte(`"process_start"`)) || !bytes.Contains(encodedTools, []byte(`"command"`)) ||
		!bytes.Contains(encodedTools, []byte(`"argv"`)) || bytes.Contains(encodedTools, []byte(`"entrypoint_sha256"`)) {
		t.Fatalf("incomplete process schema: %s", encodedTools)
	}

	workspace := t.TempDir()
	entrypoint := filepath.Join(workspace, "server.cjs")
	if err := os.WriteFile(entrypoint, []byte("console.log('cwapi-process-ready xoxb-123456789-secretvalue api_key=abcdef123456'); setInterval(() => {}, 1000);\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	commit := strings.Repeat("a", 40)
	contextArgs := map[string]any{
		"_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQPROCESSSTART",
	}
	startArgs := map[string]any{"runtime": "node", "entrypoint": "server.cjs"}
	for key, value := range contextArgs {
		startArgs[key] = value
	}
	unsafeArgs := maps.Clone(startArgs)
	unsafeArgs["command"] = "node server.cjs"
	requireToolError(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": unsafeArgs}), "PROCESS_INVOCATION_CONFLICT")
	unsupportedRuntime := maps.Clone(startArgs)
	unsupportedRuntime["runtime"] = "powershell"
	requireToolError(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": unsupportedRuntime}), "PROCESS_RUNTIME_UNSUPPORTED")
	started := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": startArgs}))
	processID, _ := started["process_id"].(string)
	if processID == "" || started["state"] != "running" || started["expected_commit"] != commit {
		t.Fatalf("started=%#v", started)
	}
	if hash, _ := started["entrypoint_sha256"].(string); len(hash) != 64 {
		t.Fatalf("entrypoint hash=%q", hash)
	}

	statusArgs := map[string]any{"process_id": processID, "_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQPROCESSSTATUS"}
	status := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_status", "arguments": statusArgs}))
	stdoutTail := fmt.Sprint(status["stdout_tail"])
	if status["state"] != "running" || !strings.Contains(stdoutTail, "cwapi-process-ready") || !strings.Contains(stdoutTail, "[REDACTED]") ||
		strings.Contains(stdoutTail, "secretvalue") || strings.Contains(stdoutTail, "abcdef123456") {
		t.Fatalf("status=%#v", status)
	}
	logs, err := os.ReadDir(harness.logRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, log := range logs {
		content, err := os.ReadFile(filepath.Join(harness.logRoot, log.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(content, []byte("secretvalue")) || bytes.Contains(content, []byte("abcdef123456")) {
			t.Fatalf("process log contains an unredacted secret: %s", log.Name())
		}
	}

	stopArgs := map[string]any{"process_id": processID, "_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQPROCESSSTOP"}
	stopped := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_stop", "arguments": stopArgs}))
	if stopped["state"] != "stopped" {
		t.Fatalf("stopped=%#v", stopped)
	}

	commandArgs := map[string]any{
		"command": harness.node,
		"argv":    []any{"-e", "console.log('cwapi-command-ready xoxb-123456789-commandsecret'); setInterval(() => {}, 1000)"},
		"cwd":     ".", "_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQCOMMANDSTART",
	}
	commandStarted := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": commandArgs}))
	commandID, _ := commandStarted["process_id"].(string)
	if commandID == "" || commandStarted["state"] != "running" || commandStarted["invocation_kind"] != "command_argv" ||
		commandStarted["command_name"] != filepath.Base(harness.node) || len(fmt.Sprint(commandStarted["invocation_sha256"])) != 64 {
		t.Fatalf("command started=%#v", commandStarted)
	}
	commandStartedJSON, _ := json.Marshal(commandStarted)
	if bytes.Contains(commandStartedJSON, []byte("commandsecret")) {
		t.Fatalf("command result echoed argv: %s", commandStartedJSON)
	}
	commandStatusArgs := map[string]any{
		"process_id": commandID, "_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQCOMMANDSTATUS",
	}
	commandStatus := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_status", "arguments": commandStatusArgs}))
	if !strings.Contains(fmt.Sprint(commandStatus["stdout_tail"]), "cwapi-command-ready") ||
		!strings.Contains(fmt.Sprint(commandStatus["stdout_tail"]), "[REDACTED]") || strings.Contains(fmt.Sprint(commandStatus["stdout_tail"]), "commandsecret") {
		t.Fatalf("command status=%#v", commandStatus)
	}
	commandStopArgs := map[string]any{
		"process_id": commandID, "_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQCOMMANDSTOP",
	}
	commandStopped := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_stop", "arguments": commandStopArgs}))
	if commandStopped["state"] != "stopped" {
		t.Fatalf("command stopped=%#v", commandStopped)
	}

	quickArgs := map[string]any{
		"command": harness.node, "argv": []any{"-e", "console.log('cwapi-command-complete')"},
		"_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQCOMMANDQUICK",
	}
	quick := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": quickArgs}))
	if quick["state"] == "starting" || quick["state"] == "running" {
		quick = waitProcessTerminal(t, harness, fmt.Sprint(quick["process_id"]), workspace, commit)
	}
	if quick["state"] != "completed" || quick["exit_code"] != float64(0) || !strings.Contains(fmt.Sprint(quick["stdout_tail"]), "cwapi-command-complete") {
		t.Fatalf("quick command=%#v", quick)
	}
	escapeArgs := maps.Clone(quickArgs)
	escapeArgs["cwd"] = "../outside"
	requireToolError(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": escapeArgs}), "PROCESS_WORKING_DIRECTORY_INVALID")

	if powershell, err := exec.LookPath("powershell.exe"); err == nil {
		powershellArgs := map[string]any{
			"command": powershell, "argv": []any{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", "Write-Output 'cwapi-powershell-complete'"},
			"_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQPOWERSHELLQUICK",
		}
		powershellResult := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": powershellArgs}))
		if powershellResult["state"] == "starting" || powershellResult["state"] == "running" {
			powershellResult = waitProcessTerminal(t, harness, fmt.Sprint(powershellResult["process_id"]), workspace, commit)
		}
		if powershellResult["state"] != "completed" || !strings.Contains(fmt.Sprint(powershellResult["stdout_tail"]), "cwapi-powershell-complete") {
			t.Fatalf("powershell command=%#v", powershellResult)
		}
	}

	if _, err := exec.LookPath("python"); err == nil {
		if err := os.WriteFile(filepath.Join(workspace, "server.py"), []byte("import time\nprint('cwapi-python-ready', flush=True)\nwhile True: time.sleep(1)\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pythonArgs := map[string]any{
			"runtime": "python", "entrypoint": "server.py", "_cwapi_workspace": workspace,
			"_cwapi_expected_commit": commit, "_cwapi_request_id": "REQPYTHONSTART",
		}
		pythonStarted := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_start", "arguments": pythonArgs}))
		pythonID, _ := pythonStarted["process_id"].(string)
		if pythonID == "" || pythonStarted["state"] != "running" {
			t.Fatalf("python started=%#v", pythonStarted)
		}
		pythonStop := map[string]any{
			"process_id": pythonID, "_cwapi_workspace": workspace, "_cwapi_expected_commit": commit, "_cwapi_request_id": "REQPYTHONSTOP",
		}
		pythonStopped := toolTextObject(t, harness.call(t, "tools/call", map[string]any{"name": "process_stop", "arguments": pythonStop}))
		if pythonStopped["state"] != "stopped" || !strings.Contains(fmt.Sprint(pythonStopped["stdout_tail"]), "cwapi-python-ready") {
			t.Fatalf("python stopped=%#v", pythonStopped)
		}
	}
}
