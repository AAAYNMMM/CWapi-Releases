package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureHomeWritesCodexNativePermissionProfiles(t *testing.T) {
	dataRoot := t.TempDir()
	installRoot := t.TempDir()
	s := newService(dataRoot, installRoot)
	if err := s.ensureHome(PermissionConfig{ProfileID: PermissionProfileSafe}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(filepath.Join(s.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(configBytes)
	for _, required := range []string{
		`default_permissions = "cwapi-safe"`,
		`[windows]`,
		`sandbox = "unelevated"`,
		`[permissions.cwapi-safe]`,
		`extends = ":workspace"`,
		`[permissions.cwapi-full-access.filesystem]`,
		`":root" = "write"`,
		tomlString(filepath.Join(dataRoot, "temp", "mcp-global")) + ` = "write"`,
	} {
		if !strings.Contains(config, required) {
			t.Fatalf("missing %q in config:\n%s", required, config)
		}
	}
	if strings.Contains(config, `default_permissions = ":danger-full-access"`) {
		t.Fatalf("full access must not use unsandboxed built-in profile:\n%s", config)
	}

	rulesBytes, err := os.ReadFile(filepath.Join(s.home, "rules", "default.rules"))
	if err != nil {
		t.Fatal(err)
	}
	rules := string(rulesBytes)
	for _, required := range []string{
		`pattern=["diskpart"]`,
		`pattern=["git", "reset"]`,
		`pattern=["git", "push"]`,
		`pattern=["git", "checkout"]`,
		`pattern=["git", "fetch"]`,
		`decision="forbidden"`,
	} {
		if !strings.Contains(rules, required) {
			t.Fatalf("missing %q in rules:\n%s", required, rules)
		}
	}
}

func TestEnsureHomeDoesNotRegisterRetiredCWapiProcessServer(t *testing.T) {
	s := newService(t.TempDir(), t.TempDir())
	if err := s.ensureHome(PermissionConfig{ProfileID: PermissionProfileSafe}); err != nil {
		t.Fatal(err)
	}
	configBytes, err := os.ReadFile(filepath.Join(s.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if config := string(configBytes); strings.Contains(config, "mcp_servers.cwapi") || strings.Contains(config, "CWAPI_PROCESS_LOG_ROOT") {
		t.Fatalf("retired process MCP registration remained:\n%s", config)
	}
}

type fakeInternalRequester struct {
	method string
	params map[string]any
}

func (f *fakeInternalRequester) RequestInternal(_ context.Context, method string, params map[string]any) (any, error) {
	f.method = method
	f.params = params
	return map[string]any{"thread": map[string]any{"id": "thread-cwapi"}}, nil
}

func TestStartMCPContextThreadPassesNativePermissionProfile(t *testing.T) {
	client := &fakeInternalRequester{}
	threadID, err := startMCPContextThread(context.Background(), client, PermissionProfileFullAccess, `C:/workspace/cwapi-test`)
	if err != nil {
		t.Fatal(err)
	}
	if threadID != "thread-cwapi" || client.method != "thread/start" {
		t.Fatalf("unexpected thread start: id=%q method=%q", threadID, client.method)
	}
	if client.params["ephemeral"] != true || client.params["permissions"] != PermissionProfileFullAccess || client.params["cwd"] != `C:/workspace/cwapi-test` {
		t.Fatalf("unexpected thread params: %#v", client.params)
	}
}

func TestPermissionKeyChangesOnlyWithProfile(t *testing.T) {
	root := t.TempDir()
	base := PermissionConfig{ProfileID: PermissionProfileSafe}
	first, err := base.key(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (PermissionConfig{ProfileID: PermissionProfileFullAccess}).key(root)
	if err != nil {
		t.Fatal(err)
	}
	third, err := (PermissionConfig{ProfileID: PermissionProfileSafe}).key(filepath.Join(root, "other"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first != third {
		t.Fatalf("permission key contract failed: first=%s second=%s third=%s", first, second, third)
	}
}

func TestPathWithin(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	if !pathWithin(filepath.Join(root, "project"), root) {
		t.Fatal("child path must be inside root")
	}
	if pathWithin(filepath.Join(filepath.Dir(root), "other"), root) {
		t.Fatal("sibling path must not be inside root")
	}
}
