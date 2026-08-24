//go:build windows

package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	v161CapabilityGateEnv = "CWAPI_RUN_V161_CAPABILITY_RUNTIME"
	v161CapabilityNodeEnv = "CWAPI_TEST_NODE_EXE"
)

func TestV161CodexProcessCapabilities(t *testing.T) {
	if os.Getenv(v161CapabilityGateEnv) != "1" {
		t.Skip("v1.6.1 Codex process capability gate is not enabled")
	}
	codexExe := requireCapabilityExecutable(t, stockPermissionRuntimeExeEnv, true)
	nodeExe := requireCapabilityExecutable(t, v161CapabilityNodeEnv, false)
	for _, key := range []string{
		"CWAPI_SECRET_CANARY", "SLACK_BOT_TOKEN", "OPENAI_API_KEY",
		"CODEX_API_KEY", "GH_TOKEN", "GITHUB_TOKEN", "GIT_TRACE",
		"GIT_CURL_VERBOSE", "GH_DEBUG",
	} {
		t.Setenv(key, "must-not-reach-child")
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	validationRoot := filepath.Join(workingDirectory, "..", "..", "build", "validation")
	if err := os.MkdirAll(validationRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := os.MkdirTemp(validationRoot, "codex-capability-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	workA := filepath.Join(root, "work-a")
	workB := filepath.Join(root, "work-b")
	outside := filepath.Join(root, "outside")
	for _, directory := range []string{workA, workB, outside} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	clientA := startCapabilityClient(t, codexExe, filepath.Join(root, "home-a"), workA)
	clientB := startCapabilityClient(t, codexExe, filepath.Join(root, "home-b"), workB)
	defer closeCapabilityClient(clientA)
	defer closeCapabilityClient(clientB)

	t.Run("short-process-and-secret-environment", func(t *testing.T) {
		secretKeys := []string{
			"CWAPI_SECRET_CANARY", "SLACK_BOT_TOKEN", "OPENAI_API_KEY",
			"CODEX_API_KEY", "GH_TOKEN", "GITHUB_TOKEN", "GIT_TRACE",
			"GIT_CURL_VERBOSE", "GH_DEBUG", "CODEX_HOME",
		}
		source := `const keys=JSON.parse(process.argv[1]);const found={};for(const k of keys){if(process.env[k]!==undefined)found[k]=process.env[k]}process.stdout.write(JSON.stringify(found));process.stderr.write("short-stderr")`
		result := runCapabilityCommand(t, clientA, workA, []string{nodeExe, "-e", source, mustJSON(t, secretKeys)}, nil)
		if result.ExitCode != 0 || result.Stdout != "{}" || result.Stderr != "short-stderr" {
			t.Fatalf("unexpected short command result: %#v", result)
		}
	})

	t.Run("isolated-concurrent-roots", func(t *testing.T) {
		source := `const fs=require("node:fs");const own=process.argv[1],other=process.argv[2];let ownOK=false,crossOK=false;try{fs.writeFileSync(own,"own");ownOK=true}catch{}try{fs.writeFileSync(other,"cross");crossOK=true}catch{}process.stdout.write(JSON.stringify({ownOK,crossOK}))`
		type response struct {
			result capabilityCommandResult
			name   string
			err    error
		}
		responses := make(chan response, 2)
		var wg sync.WaitGroup
		for _, item := range []struct {
			name, cwd, own, other string
			client                *Client
		}{
			{"a", workA, filepath.Join(workA, "own.txt"), filepath.Join(workB, "cross-a.txt"), clientA},
			{"b", workB, filepath.Join(workB, "own.txt"), filepath.Join(workA, "cross-b.txt"), clientB},
		} {
			wg.Add(1)
			go func() {
				defer wg.Done()
				result, err := requestCapabilityCommand(item.client, item.cwd, []string{nodeExe, "-e", source, item.own, item.other}, nil)
				responses <- response{result: result, name: item.name, err: err}
			}()
		}
		wg.Wait()
		close(responses)
		for response := range responses {
			if response.err != nil {
				t.Fatalf("root %s command failed: %v", response.name, response.err)
			}
			var outcome struct{ OwnOK, CrossOK bool }
			if err := json.Unmarshal([]byte(response.result.Stdout), &outcome); err != nil {
				t.Fatalf("decode %s isolation result: %v (%q)", response.name, err, response.result.Stdout)
			}
			if response.result.ExitCode != 0 || !outcome.OwnOK || outcome.CrossOK {
				t.Fatalf("root %s was not isolated: result=%#v outcome=%#v", response.name, response.result, outcome)
			}
		}
	})

	t.Run("structured-denial-and-system-success", func(t *testing.T) {
		target := filepath.Join(outside, "system-fallback.txt")
		source := `const fs=require("node:fs");try{fs.writeFileSync(process.argv[1],"system-ok");process.exit(0)}catch(e){process.exit(e&&(e.code==="EPERM"||e.code==="EACCES")?5:9)}`
		command := []string{nodeExe, "-e", source, target}
		denied := runCapabilityCommand(t, clientA, workA, command, nil)
		if denied.ExitCode != 5 {
			t.Fatalf("Codex denial was not structured as exit code 5: %#v", denied)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatalf("Codex wrote outside its safe root: %v", err)
		}
		system := exec.Command(command[0], command[1:]...)
		system.Dir = workA
		system.Env = canonicalCapabilityEnvironment()
		if output, err := system.CombinedOutput(); err != nil {
			t.Fatalf("same invocation failed as System: %v (%s)", err, output)
		}
		if payload, err := os.ReadFile(target); err != nil || string(payload) != "system-ok" {
			t.Fatalf("System fallback did not write target: payload=%q err=%v", payload, err)
		}
	})

	t.Run("long-process-job-lifecycle", func(t *testing.T) {
		heartbeat := filepath.Join(workA, "heartbeat.txt")
		source := `const fs=require("node:fs");const p=process.argv[1];setInterval(()=>fs.writeFileSync(p,String(Date.now())),75)`
		requestDone := make(chan error, 1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			_, err := clientA.request(ctx, "command/exec", capabilityCommandParams(workA, []string{nodeExe, "-e", source, heartbeat}, map[string]any{"disableTimeout": true}), true)
			requestDone <- err
		}()
		waitForCapabilityFile(t, heartbeat, 15*time.Second)
		beforeOther := requireModTime(t, heartbeat)
		other := runCapabilityCommand(t, clientB, workB, []string{nodeExe, "-e", `process.stdout.write("parallel-ok")`}, nil)
		if other.ExitCode != 0 || other.Stdout != "parallel-ok" {
			t.Fatalf("parallel command failed: %#v", other)
		}
		waitForModTimeAfter(t, heartbeat, beforeOther, 3*time.Second)
		if err := clientA.releaseProcessTree(); err != nil {
			t.Fatalf("close process job: %v", err)
		}
		select {
		case err := <-requestDone:
			if err == nil {
				t.Fatal("long command unexpectedly completed cleanly after forced stop")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("long command request did not terminate with its process job")
		}
		stable := requireModTime(t, heartbeat)
		time.Sleep(600 * time.Millisecond)
		if after := requireModTime(t, heartbeat); after.After(stable) {
			t.Fatalf("descendant outlived process job: before=%s after=%s", stable, after)
		}
	})
}

type capabilityCommandResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func requireCapabilityExecutable(t *testing.T, key string, verifyPinned bool) string {
	t.Helper()
	value := filepath.Clean(strings.TrimSpace(os.Getenv(key)))
	if value == "." || !filepath.IsAbs(value) {
		t.Fatalf("%s must be an absolute executable path", key)
	}
	if info, err := os.Stat(value); err != nil || !info.Mode().IsRegular() {
		t.Fatalf("%s is unavailable: %v", key, err)
	}
	if verifyPinned {
		hash, err := hashFile(value)
		if err != nil || !strings.EqualFold(hash, PinnedExecutableSHA256) {
			t.Fatalf("pinned Codex hash mismatch: got=%s want=%s err=%v", hash, PinnedExecutableSHA256, err)
		}
	}
	return value
}

func startCapabilityClient(t *testing.T, executable, home, cwd string) *Client {
	t.Helper()
	if err := ensureCommandHome(home); err != nil {
		t.Fatal(err)
	}
	notifications := make(chan map[string]any, 8)
	newClient := func() *Client {
		return NewClient(executable, home, filepath.Join(home, "app-server.log"), appServerEnvironment(), 20*time.Second, func(message map[string]any) {
			select {
			case notifications <- message:
			default:
			}
		})
	}
	client := newClient()
	startAndOwnCapabilityClient(t, client)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	readiness, err := client.request(ctx, "windowsSandbox/readiness", nil, true)
	if err != nil {
		t.Fatalf("read Windows sandbox readiness: %v\nstderr=%s", err, client.StderrTail())
	}
	if fieldString(readiness, "status") != "ready" {
		if _, err := client.request(ctx, "windowsSandbox/setupStart", map[string]any{"mode": "unelevated", "cwd": cwd}, true); err != nil {
			t.Fatalf("start unelevated sandbox setup: %v\nstderr=%s", err, client.StderrTail())
		}
		for {
			select {
			case message := <-notifications:
				if fieldString(message, "method") != "windowsSandbox/setupCompleted" {
					continue
				}
				params, _ := message["params"].(map[string]any)
				if success, _ := params["success"].(bool); !success {
					t.Fatalf("unelevated sandbox setup failed: %#v", params)
				}
				goto setupComplete
			case <-ctx.Done():
				t.Fatalf("wait for unelevated sandbox setup: %v", ctx.Err())
			}
		}
	setupComplete:
		closeCapabilityClient(client)
		client = newClient()
		startAndOwnCapabilityClient(t, client)
		readiness, err = client.request(ctx, "windowsSandbox/readiness", nil, true)
		if err != nil || fieldString(readiness, "status") != "ready" {
			t.Fatalf("sandbox not ready after setup: readiness=%#v err=%v", readiness, err)
		}
	}
	return client
}

func startAndOwnCapabilityClient(t *testing.T, client *Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start Codex app-server: %v\nstderr=%s", err, client.StderrTail())
	}
	if err := client.ownProcessTree(); err != nil {
		client.Close()
		t.Fatalf("own Codex process tree: %v", err)
	}
}

func closeCapabilityClient(client *Client) {
	if client == nil {
		return
	}
	_ = client.releaseProcessTree()
	client.Close()
}

func runCapabilityCommand(t *testing.T, client *Client, cwd string, command []string, extra map[string]any) capabilityCommandResult {
	t.Helper()
	result, err := requestCapabilityCommand(client, cwd, command, extra)
	if err != nil {
		t.Fatalf("command/exec failed: %v\nstderr=%s", err, client.StderrTail())
	}
	return result
}

func requestCapabilityCommand(client *Client, cwd string, command []string, extra map[string]any) (capabilityCommandResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	value, err := client.request(ctx, "command/exec", capabilityCommandParams(cwd, command, extra), true)
	if err != nil {
		return capabilityCommandResult{}, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return capabilityCommandResult{}, fmt.Errorf("command/exec response type = %T", value)
	}
	exitCode, ok := object["exitCode"].(float64)
	if !ok {
		return capabilityCommandResult{}, fmt.Errorf("command/exec exitCode missing: %#v", object)
	}
	return capabilityCommandResult{int(exitCode), fieldString(object, "stdout"), fieldString(object, "stderr")}, nil
}

func capabilityCommandParams(cwd string, command []string, extra map[string]any) map[string]any {
	params := map[string]any{
		"command": command,
		"cwd":     cwd,
		"env": map[string]any{
			"CODEX_HOME": nil, "RUST_LOG": nil, "LOG_FORMAT": nil,
		},
		"sandboxPolicy": map[string]any{
			"type": "workspaceWrite", "writableRoots": []string{cwd},
			"networkAccess": false, "excludeSlashTmp": true, "excludeTmpdirEnvVar": true,
		},
	}
	if len(extra) == 0 {
		params["timeoutMs"] = 30000
	}
	for key, value := range extra {
		params[key] = value
	}
	return params
}

func canonicalCapabilityEnvironment() []string {
	return appServerEnvironment()
}

func fieldString(value any, key string) string {
	object, _ := value.(map[string]any)
	text, _ := object[key].(string)
	return text
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func waitForCapabilityFile(t *testing.T, path string, timeout time.Duration) {
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

func requireModTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}

func waitForModTimeAfter(t *testing.T, path string, previous time.Time, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if current := requireModTime(t, path); current.After(previous) {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s did not advance after %s", path, previous)
}
