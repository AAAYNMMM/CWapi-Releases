//go:build windows

package processruntime

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
)

func TestV161ProcessRuntimeIntegration(t *testing.T) {
	if os.Getenv("CWAPI_RUN_V161_PROCESS_RUNTIME") != "1" {
		t.Skip("set CWAPI_RUN_V161_PROCESS_RUNTIME=1 for the packaged runtime gate")
	}
	codexExecutable := requireRuntimeExecutable(t, "CWAPI_TEST_CODEX_EXE")
	nodeExecutable := requireRuntimeExecutable(t, "CWAPI_TEST_NODE_EXE")
	t.Setenv("SLACK_BOT_TOKEN", "xoxb-runtime-secret-canary")
	t.Setenv("OPENAI_API_KEY", "sk-runtime-secret-canary")
	t.Setenv("GITHUB_TOKEN", "github-runtime-secret-canary")

	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	manager, err := config.Open(filepath.Join(dataRoot, "config", "cwapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := codex.NewServiceWithInstallRoot(dataRoot, runtimeInstallRoot(codexExecutable))
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(service, manager, dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	commit := strings.Repeat("a", 40)
	repositoryURL := "https://github.com/owner/repo"

	t.Run("short-safe-process-and-minimal-public-record", func(t *testing.T) {
		repositoryRoot := filepath.Join(root, "short-repository")
		if err := os.MkdirAll(repositoryRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		var releases atomic.Int32
		source := `const keys=["SLACK_BOT_TOKEN","OPENAI_API_KEY","GITHUB_TOKEN","CODEX_HOME"];const found={};for(const key of keys){if(process.env[key]!==undefined)found[key]=process.env[key]}process.stdout.write(JSON.stringify(found));process.stderr.write("short-stderr")`
		response, owned := runtime.Start(context.Background(), processRequest("RUNTIME-SHORT-1", repositoryURL, commit, ""), processcontract.StartArguments{
			Command: filepath.ToSlash(nodeExecutable), Argv: []string{"-e", source},
		}, executionContext(repositoryURL, commit, repositoryRoot), func() { releases.Add(1) })
		if !owned || response.Status != protocol.MCPStatusCompleted {
			t.Fatalf("short response=%#v owned=%v", response, owned)
		}
		record := decodeProcessRecord(t, response)
		if record.Backend != BackendCodex || record.State != StateCompleted || record.StdoutTail != "{}" || record.StderrTail != "short-stderr" || record.WorkingDirectory != "." {
			t.Fatalf("short record=%#v", record)
		}
		encoded := string(response.Result)
		for _, forbidden := range []string{repositoryRoot, nodeExecutable, "SLACK_BOT_TOKEN", "OPENAI_API_KEY"} {
			if strings.Contains(encoded, forbidden) {
				t.Fatalf("public record leaked %q: %s", forbidden, encoded)
			}
		}
		if releases.Load() != 1 {
			t.Fatalf("short releases=%d", releases.Load())
		}
	})

	t.Run("long-process-status-stop-and-job-cleanup", func(t *testing.T) {
		repositoryRoot := filepath.Join(root, "long-repository")
		if err := os.MkdirAll(repositoryRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		heartbeat := filepath.Join(repositoryRoot, "heartbeat.txt")
		source := `const fs=require("node:fs");const p=process.argv[1];setInterval(()=>fs.writeFileSync(p,String(Date.now())),75)`
		var releases atomic.Int32
		response, _ := runtime.Start(context.Background(), processRequest("RUNTIME-LONG-1", repositoryURL, commit, ""), processcontract.StartArguments{
			Command: filepath.ToSlash(nodeExecutable), Argv: []string{"-e", source, heartbeat},
		}, executionContext(repositoryURL, commit, repositoryRoot), func() { releases.Add(1) })
		record := decodeProcessRecord(t, response)
		if record.State != StateRunning {
			t.Fatalf("long start=%#v", record)
		}
		waitForFile(t, heartbeat, 10*time.Second)
		status := decodeProcessRecord(t, runtime.Status(context.Background(), "RUNTIME-LONG-STATUS", record.ProcessID))
		if status.State != StateRunning {
			t.Fatalf("long status=%#v", status)
		}
		stopped := decodeProcessRecord(t, runtime.Stop(context.Background(), "RUNTIME-LONG-STOP", record.ProcessID))
		if stopped.State != StateStopped || releases.Load() != 1 {
			t.Fatalf("long stop=%#v releases=%d", stopped, releases.Load())
		}
		before := fileModTime(t, heartbeat)
		time.Sleep(400 * time.Millisecond)
		if after := fileModTime(t, heartbeat); after.After(before) {
			t.Fatalf("heartbeat advanced after stop: before=%v after=%v", before, after)
		}
	})

	t.Run("full-denial-binding-and-one-time-system-fallback", func(t *testing.T) {
		if _, err := runtime.UpdatePermissionMode(config.PermissionModeFullAccess); err != nil {
			t.Fatal(err)
		}
		repositoryRoot := filepath.Join(root, "fallback-repository")
		outside := filepath.Join(root, "outside", "system-fallback.txt")
		if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(repositoryRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		source := `const fs=require("node:fs");try{fs.writeFileSync(process.argv[1],"system-ok");process.stdout.write("wrote");process.exit(0)}catch(e){process.exit(e&&(e.code==="EPERM"||e.code==="EACCES")?5:9)}`
		arguments := processcontract.StartArguments{Command: filepath.ToSlash(nodeExecutable), Argv: []string{"-e", source, outside}}
		var releases atomic.Int32
		denied, owned := runtime.Start(context.Background(), processRequest("RUNTIME-DENY-1", repositoryURL, commit, ""), arguments,
			executionContext(repositoryURL, commit, repositoryRoot), func() { releases.Add(1) })
		if !owned || denied.Status != protocol.MCPStatusBlocked || denied.Error == nil || denied.Error.Code != FailurePermission || len(denied.SystemToken) != 64 {
			t.Fatalf("denial response=%#v owned=%v", denied, owned)
		}
		if releases.Load() != 0 {
			t.Fatalf("denial tree released before fallback: %d", releases.Load())
		}
		wrong := arguments
		wrong.Argv = append([]string(nil), arguments.Argv...)
		wrong.Argv[len(wrong.Argv)-1] = filepath.Join(root, "outside", "wrong.txt")
		mismatch, _ := runtime.Start(context.Background(), processRequest("RUNTIME-DENY-WRONG", repositoryURL, commit, denied.SystemToken), wrong, gateway.MCPExecutionContext{}, nil)
		if mismatch.Error == nil || mismatch.Error.Code != ErrTokenBinding.Error() || releases.Load() != 0 {
			t.Fatalf("binding mismatch=%#v releases=%d", mismatch, releases.Load())
		}
		fallback, _ := runtime.Start(context.Background(), processRequest("RUNTIME-SYSTEM-1", repositoryURL, commit, denied.SystemToken), arguments, gateway.MCPExecutionContext{}, nil)
		record := decodeProcessRecord(t, fallback)
		if record.Backend != BackendSystem || record.State != StateCompleted || record.StdoutTail != "wrote" {
			t.Fatalf("System fallback record=%#v", record)
		}
		payload, err := os.ReadFile(outside)
		if err != nil || string(payload) != "system-ok" || releases.Load() != 1 {
			t.Fatalf("System fallback output=%q err=%v releases=%d", payload, err, releases.Load())
		}
		reused, _ := runtime.Start(context.Background(), processRequest("RUNTIME-SYSTEM-REUSE", repositoryURL, commit, denied.SystemToken), arguments, gateway.MCPExecutionContext{}, nil)
		if reused.Error == nil || reused.Error.Code != ErrTokenInvalid.Error() {
			t.Fatalf("consumed token reused: %#v", reused)
		}
	})
}

func processRequest(requestID, repositoryURL, commit, token string) protocol.MCPRequest {
	return protocol.MCPRequest{
		Schema: protocol.MCPRequestSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: requestID, RepositoryURL: repositoryURL, ExpectedCommit: commit,
		Method: protocol.MCPMethodToolCall, SystemToken: token,
	}
}

func executionContext(repositoryURL, commit, root string) gateway.MCPExecutionContext {
	return gateway.MCPExecutionContext{
		RepositoryURL: repositoryURL, Repository: "owner/repo",
		ExpectedCommit: commit, CWD: root,
	}
}

func decodeProcessRecord(t *testing.T, response protocol.MCPResponse) Record {
	t.Helper()
	if response.Status != protocol.MCPStatusCompleted || response.Error != nil {
		t.Fatalf("process response=%#v", response)
	}
	var record Record
	if err := json.Unmarshal(response.Result, &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func runtimeInstallRoot(executable string) string {
	root := filepath.Dir(executable)
	for index := 0; index < 4; index++ {
		root = filepath.Dir(root)
	}
	return root
}

func requireRuntimeExecutable(t *testing.T, key string) string {
	t.Helper()
	value := filepath.Clean(strings.TrimSpace(os.Getenv(key)))
	if !filepath.IsAbs(value) {
		t.Fatalf("%s must be absolute", key)
	}
	if info, err := os.Stat(value); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s unavailable: %v", key, err)
	}
	return value
}

func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if info, err := os.Stat(path); err == nil && info.Size() > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func fileModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}
