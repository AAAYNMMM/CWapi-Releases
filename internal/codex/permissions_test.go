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
	project := filepath.Join(dataRoot, "project")
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	s := newService(dataRoot, installRoot)
	if err := s.ensureHome(PermissionConfig{
		ProfileID:    PermissionProfileSafe,
		ProjectPaths: []string{project},
	}); err != nil {
		t.Fatal(err)
	}

	configBytes, err := os.ReadFile(filepath.Join(s.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(configBytes)
	for _, required := range []string{
		`default_permissions = "cwapi-safe"`,
		`[permissions.cwapi-safe]`,
		`extends = ":workspace"`,
		`[permissions.cwapi-full-access.filesystem]`,
		`":root" = "write"`,
		tomlString(project) + ` = "write"`,
		tomlString(filepath.Join(project, ".git")) + ` = "deny"`,
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

func TestEnsureHomeRegistersPackagedCWapiProcessServer(t *testing.T) {
	dataRoot := t.TempDir()
	installRoot := t.TempDir()
	project := filepath.Join(dataRoot, "project")
	for _, directory := range []string{project, filepath.Join(installRoot, "runtime", "node"), filepath.Join(installRoot, "runtime", "mcp", "cwapi")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{
		filepath.Join(installRoot, "runtime", "node", "node.exe"),
		filepath.Join(installRoot, "runtime", "mcp", "cwapi", "process-server.cjs"),
		filepath.Join(installRoot, "runtime", "mcp", "cwapi", "process-invocation.cjs"),
		filepath.Join(installRoot, "runtime", "mcp", "cwapi", "process-output.cjs"),
	} {
		if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	s := newService(dataRoot, installRoot)
	if err := s.ensureHome(PermissionConfig{ProfileID: PermissionProfileSafe, ProjectPaths: []string{project}}); err != nil {
		t.Fatal(err)
	}
	configBytes, err := os.ReadFile(filepath.Join(s.home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	config := string(configBytes)
	for _, required := range []string{"[mcp_servers.cwapi]", tomlString(s.processMCPPath()), "CWAPI_PROCESS_LOG_ROOT", "tool_timeout_sec = 30"} {
		if !strings.Contains(config, required) {
			t.Fatalf("missing %q in config:\n%s", required, config)
		}
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
	threadID, err := startMCPContextThread(context.Background(), client, PermissionProfileFullAccess, `C:\Projects\CWapiExample`)
	if err != nil {
		t.Fatal(err)
	}
	if threadID != "thread-cwapi" || client.method != "thread/start" {
		t.Fatalf("unexpected thread start: id=%q method=%q", threadID, client.method)
	}
	if client.params["ephemeral"] != true || client.params["permissions"] != PermissionProfileFullAccess || client.params["cwd"] != `C:\Projects\CWapiExample` {
		t.Fatalf("unexpected thread params: %#v", client.params)
	}
}

func TestPermissionKeyChangesWithProfileAndProjects(t *testing.T) {
	root := t.TempDir()
	base := PermissionConfig{ProfileID: PermissionProfileSafe, ProjectPaths: []string{filepath.Join(root, "a")}}
	first, err := base.key(root)
	if err != nil {
		t.Fatal(err)
	}
	second, err := (PermissionConfig{ProfileID: PermissionProfileFullAccess, ProjectPaths: base.ProjectPaths}).key(root)
	if err != nil {
		t.Fatal(err)
	}
	third, err := (PermissionConfig{ProfileID: PermissionProfileSafe, ProjectPaths: []string{filepath.Join(root, "b")}}).key(root)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first == third {
		t.Fatalf("permission key did not change: first=%s second=%s third=%s", first, second, third)
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
