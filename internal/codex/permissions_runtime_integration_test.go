package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const stockPermissionRuntimeTestEnv = "CWAPI_RUN_STOCK_CODEX_PERMISSION_RUNTIME"
const stockPermissionRuntimeExeEnv = "CWAPI_TEST_CODEX_EXE"

func TestStockCodexPermissionProfilesRuntime(t *testing.T) {
	if os.Getenv(stockPermissionRuntimeTestEnv) != "1" {
		t.Skip("stock Codex permission runtime gate is not enabled")
	}

	executable := strings.TrimSpace(os.Getenv(stockPermissionRuntimeExeEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must be an absolute path", stockPermissionRuntimeExeEnv)
	}
	executable = filepath.Clean(executable)
	actualHash, err := hashFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(actualHash, PinnedExecutableSHA256) {
		t.Fatalf("stock Codex hash mismatch: got=%s want=%s", actualHash, PinnedExecutableSHA256)
	}

	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	projectRoot := filepath.Join(root, "project")
	if err := os.MkdirAll(projectRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	service := &Service{
		dataRoot:    dataRoot,
		installRoot: filepath.Join(root, "install"),
		codexExe:    executable,
		home:        filepath.Join(dataRoot, "state", "codex-home"),
		stderrLog:   filepath.Join(dataRoot, "logs", "codex-app-server.log"),
	}
	permission := PermissionConfig{ProfileID: PermissionProfileSafe}
	if err := service.ensureHome(permission); err != nil {
		t.Fatalf("generate CWapi Codex home: %v", err)
	}

	client := NewClient(
		executable,
		service.home,
		service.stderrLog,
		appServerEnvironment(),
		30*time.Second,
		nil,
	)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := client.Start(ctx); err != nil {
		t.Fatalf("start stock Codex app-server: %v\nstderr=%s", err, client.StderrTail())
	}
	defer client.Close()

	profiles, err := client.request(ctx, "permissionProfile/list", map[string]any{"cwd": projectRoot}, true)
	if err != nil {
		t.Fatalf("permissionProfile/list: %v\nstderr=%s", err, client.StderrTail())
	}
	assertPermissionProfileListed(t, profiles, PermissionProfileSafe)
	assertPermissionProfileListed(t, profiles, PermissionProfileFullAccess)

	safeResponse, err := client.RequestInternal(ctx, "thread/start", map[string]any{
		"ephemeral":   true,
		"cwd":         projectRoot,
		"permissions": PermissionProfileSafe,
	})
	if err != nil {
		t.Fatalf("thread/start safe profile: %v\nstderr=%s", err, client.StderrTail())
	}
	safeThreadID := assertActivePermissionProfile(t, safeResponse, PermissionProfileSafe, ":workspace")

	fullResponse, err := client.RequestInternal(ctx, "thread/start", map[string]any{
		"ephemeral":   true,
		"cwd":         projectRoot,
		"permissions": PermissionProfileFullAccess,
	})
	if err != nil {
		t.Fatalf("thread/start full-access profile: %v\nstderr=%s", err, client.StderrTail())
	}
	fullThreadID := assertActivePermissionProfile(t, fullResponse, PermissionProfileFullAccess, "")
	if err := unsubscribeMCPContextThread(ctx, client, safeThreadID); err != nil {
		t.Fatalf("thread/unsubscribe safe profile: %v\nstderr=%s", err, client.StderrTail())
	}
	if err := unsubscribeMCPContextThread(ctx, client, fullThreadID); err != nil {
		t.Fatalf("thread/unsubscribe full-access profile: %v\nstderr=%s", err, client.StderrTail())
	}

	if _, err := client.RequestInternal(ctx, "thread/start", map[string]any{
		"ephemeral":   true,
		"cwd":         projectRoot,
		"permissions": "cwapi-profile-does-not-exist",
	}); err == nil {
		t.Fatal("stock Codex accepted an unknown permission profile")
	}
	client.Close()

	host := NewMCPHost(service, func() PermissionConfig { return permission })
	defer host.Close()
	status, err := host.CallMCP(ctx, MCPCall{
		Method: mcpServerStatusListMethod, Timeout: 30 * time.Second, CWD: projectRoot,
	})
	if err != nil {
		t.Fatalf("MCPHost stock status call: %v", err)
	}
	if _, ok := status.(map[string]any); !ok {
		t.Fatalf("MCPHost status response type = %T", status)
	}
}

func assertPermissionProfileListed(t *testing.T, value any, profileID string) {
	t.Helper()
	response, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("permissionProfile/list response type = %T", value)
	}
	rows, ok := response["data"].([]any)
	if !ok {
		t.Fatalf("permissionProfile/list data = %#v", response["data"])
	}
	for _, row := range rows {
		entry, ok := row.(map[string]any)
		if !ok || entry["id"] != profileID {
			continue
		}
		allowed, ok := entry["allowed"].(bool)
		if !ok || !allowed {
			t.Fatalf("permission profile %q is not allowed: %#v", profileID, entry)
		}
		return
	}
	t.Fatalf("permission profile %q was not loaded: %#v", profileID, rows)
}

func assertActivePermissionProfile(t *testing.T, value any, profileID, expectedExtends string) string {
	t.Helper()
	response, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("thread/start response type = %T", value)
	}
	thread, ok := response["thread"].(map[string]any)
	if !ok || strings.TrimSpace(stringValue(thread["id"])) == "" {
		t.Fatalf("thread/start thread missing: %#v", response)
	}
	threadID := strings.TrimSpace(stringValue(thread["id"]))
	active, ok := response["activePermissionProfile"].(map[string]any)
	if !ok {
		t.Fatalf("activePermissionProfile missing: %#v", response)
	}
	if got := stringValue(active["id"]); got != profileID {
		t.Fatalf("active profile id = %q, want %q", got, profileID)
	}
	if expectedExtends != "" {
		if got := stringValue(active["extends"]); got != expectedExtends {
			t.Fatalf("active profile extends = %q, want %q", got, expectedExtends)
		}
	}
	return threadID
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
