package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const stockProcessRuntimeTestEnv = "CWAPI_RUN_STOCK_CODEX_PROCESS_RUNTIME"

func TestStockCodexCWapiProcessServerRuntime(t *testing.T) {
	if os.Getenv(stockProcessRuntimeTestEnv) != "1" {
		t.Skip("stock Codex process MCP runtime gate is not enabled")
	}
	t.Setenv("CWAPI_TEST_SECRET_MUST_NOT_REACH_COMMAND", "must-not-leak-to-command")
	codexExecutable := strings.TrimSpace(os.Getenv(stockPermissionRuntimeExeEnv))
	nodeExecutable := strings.TrimSpace(os.Getenv("CWAPI_TEST_NODE_EXE"))
	processServer := strings.TrimSpace(os.Getenv("CWAPI_TEST_PROCESS_MCP"))
	for name, value := range map[string]string{
		stockPermissionRuntimeExeEnv: codexExecutable,
		"CWAPI_TEST_NODE_EXE":        nodeExecutable,
		"CWAPI_TEST_PROCESS_MCP":     processServer,
	} {
		if value == "" || !filepath.IsAbs(value) {
			t.Fatalf("%s must be an absolute path", name)
		}
	}

	root := t.TempDir()
	installRoot := filepath.Join(root, "install root with spaces")
	stagedNode := filepath.Join(installRoot, "runtime", "node", "node.exe")
	stagedServer := filepath.Join(installRoot, "runtime", "mcp", "cwapi", "process-server.cjs")
	copyRuntimeFile(t, nodeExecutable, stagedNode)
	copyRuntimeFile(t, processServer, stagedServer)
	copyRuntimeFile(t, filepath.Join(filepath.Dir(processServer), "process-invocation.cjs"), filepath.Join(filepath.Dir(stagedServer), "process-invocation.cjs"))
	copyRuntimeFile(t, filepath.Join(filepath.Dir(processServer), "process-output.cjs"), filepath.Join(filepath.Dir(stagedServer), "process-output.cjs"))

	dataRoot := filepath.Join(root, "data")
	projectRoot := filepath.Join(root, "project")
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		dataRoot: dataRoot, installRoot: installRoot, codexExe: codexExecutable,
		home: filepath.Join(dataRoot, "state", "codex-home"), stderrLog: filepath.Join(dataRoot, "logs", "codex-app-server.log"),
	}
	permission := PermissionConfig{ProfileID: PermissionProfileSafe, ProjectPaths: []string{projectRoot}}
	host := NewMCPHost(service, func() PermissionConfig { return permission })
	defer host.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	status, err := host.CallMCP(ctx, MCPCall{Method: mcpServerStatusListMethod, Timeout: 30 * time.Second, CWD: projectRoot})
	if err != nil {
		t.Fatalf("MCP server status: %v", err)
	}
	statusJSON, _ := json.Marshal(status)
	if !strings.Contains(string(statusJSON), "cwapi") {
		t.Fatalf("CWapi process server missing from stock catalog: %s", statusJSON)
	}

	commit := strings.Repeat("a", 40)
	started, err := host.CallMCP(ctx, MCPCall{
		Method: mcpToolCallMethod, Timeout: 30 * time.Second, CWD: projectRoot,
		Params: map[string]any{"server": "cwapi", "tool": "process_start", "arguments": map[string]any{
			"command": stagedNode, "argv": []any{"-e", "console.log('stock-codex-command-ready ' + (process.env.CWAPI_TEST_SECRET_MUST_NOT_REACH_COMMAND || 'secret-absent')); setInterval(() => {}, 1000)"}, "_cwapi_workspace": projectRoot,
			"_cwapi_expected_commit": commit, "_cwapi_request_id": "REQSTOCKPROCESSSTART",
		}},
	})
	if err != nil {
		t.Fatalf("process_start through stock Codex: %v", err)
	}
	startedRecord := decodeRuntimeProcessRecord(t, started)
	processID, _ := startedRecord["process_id"].(string)
	if processID == "" || startedRecord["state"] != "running" || startedRecord["invocation_kind"] != "command_argv" {
		t.Fatalf("started=%#v", startedRecord)
	}
	stdout := fmt.Sprint(startedRecord["stdout_tail"])
	if !strings.Contains(stdout, "secret-absent") || strings.Contains(stdout, "must-not-leak-to-command") {
		t.Fatalf("command environment was not bounded: %#v", startedRecord)
	}

	stopped, err := host.CallMCP(ctx, MCPCall{
		Method: mcpToolCallMethod, Timeout: 30 * time.Second, CWD: projectRoot,
		Params: map[string]any{"server": "cwapi", "tool": "process_stop", "arguments": map[string]any{
			"process_id": processID, "_cwapi_workspace": projectRoot,
			"_cwapi_expected_commit": commit, "_cwapi_request_id": "REQSTOCKPROCESSSTOP",
		}},
	})
	if err != nil {
		t.Fatalf("process_stop through stock Codex: %v", err)
	}
	if record := decodeRuntimeProcessRecord(t, stopped); record["state"] != "stopped" {
		t.Fatalf("stopped=%#v", record)
	}
}

func copyRuntimeFile(t *testing.T, source, destination string) {
	t.Helper()
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, data, 0o700); err != nil {
		t.Fatal(err)
	}
}

func decodeRuntimeProcessRecord(t *testing.T, value any) map[string]any {
	t.Helper()
	result, ok := value.(map[string]any)
	if !ok || result["isError"] == true {
		t.Fatalf("tool result=%#v", value)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("tool content=%#v", result)
	}
	item, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("tool content item=%#v", content[0])
	}
	text, _ := item["text"].(string)
	var record map[string]any
	if err := json.Unmarshal([]byte(text), &record); err != nil {
		t.Fatalf("tool text=%q err=%v", text, err)
	}
	return record
}
