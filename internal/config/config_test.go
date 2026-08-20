package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStrictConfigRejectsRetiredSecurityFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cwapi.json")
	payload := `{
  "schema":"cwapi.config.v1",
  "version":"1.6.0",
  "slack":{"channel_id":""},
  "security":{"allowed_repositories":[],"allowed_actions":[]},
  "projects":[]
}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") || !strings.Contains(err.Error(), "security") {
		t.Fatalf("expected retired security field rejection, got %v", err)
	}
}

func TestStrictConfigRejectsUnknownLegacyField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cwapi.json")
	legacyField := "poll" + "_interval_seconds"
	payload := fmt.Sprintf(`{
  "schema":"cwapi.config.v1",
  "version":"1.6.0",
  "slack":{"channel_id":""},
  "projects":[],
  %q:5
}`, legacyField)
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown-field rejection, got %v", err)
	}
}

func TestStrictConfigRejectsSlackTokenFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cwapi.json")
	payload := `{
  "schema":"cwapi.config.v1",
  "version":"1.6.0",
  "slack":{"channel_id":"C12345678","bot_token":"xoxb-must-not-persist"},
  "projects":[]
}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") || !strings.Contains(err.Error(), "bot_token") {
		t.Fatalf("expected Slack token field rejection, got %v", err)
	}
}

func TestPermissionModeValidationAndLegacyDefault(t *testing.T) {
	if Default().PermissionMode != PermissionModeSafe {
		t.Fatalf("default permission mode should be safe")
	}
	for input, want := range map[string]string{
		"":                       PermissionModeSafe,
		PermissionModeSafe:       PermissionModeSafe,
		PermissionModeFullAccess: PermissionModeFullAccess,
	} {
		actual, err := CanonicalPermissionMode(input)
		if err != nil || actual != want {
			t.Fatalf("permission mode %q: actual=%q err=%v", input, actual, err)
		}
	}
	for _, value := range []string{"full", "admin", " full_access"} {
		if _, err := CanonicalPermissionMode(value); err == nil {
			t.Fatalf("permission mode %q should be rejected", value)
		}
	}

	path := filepath.Join(t.TempDir(), "legacy.json")
	payload := `{
  "schema":"cwapi.config.v1",
  "version":"1.6.0",
  "slack":{"channel_id":""},
  "projects":[]
}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("legacy config load failed: %v", err)
	}
	if loaded.PermissionMode != PermissionModeSafe {
		t.Fatalf("legacy config did not normalize to safe: %q", loaded.PermissionMode)
	}
}

func TestSlackChannelIDValidation(t *testing.T) {
	for _, value := range []string{"", "C12345678", "G1234567890"} {
		if _, err := CanonicalSlackChannelID(value); err != nil {
			t.Fatalf("channel %q should be valid: %v", value, err)
		}
	}
	for _, value := range []string{"c12345678", " C12345678", "C123"} {
		if _, err := CanonicalSlackChannelID(value); err == nil {
			t.Fatalf("channel %q should be rejected", value)
		}
	}
}

func TestSaveAtomicReplacesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "cwapi.json")
	first := Default()
	if err := SaveAtomic(path, first); err != nil {
		t.Fatalf("first save: %v", err)
	}
	second := Default()
	second.Slack.ChannelID = "C12345678"
	second.PermissionMode = PermissionModeFullAccess
	if err := SaveAtomic(path, second); err != nil {
		t.Fatalf("replacement save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load replacement: %v", err)
	}
	if loaded.Slack.ChannelID != "C12345678" {
		t.Fatalf("Slack channel was not persisted: %#v", loaded.Slack)
	}
	if loaded.PermissionMode != PermissionModeFullAccess {
		t.Fatalf("permission mode was not persisted: %q", loaded.PermissionMode)
	}
}

func TestManagerRejectsInvalidCandidateWithoutAdvancingState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cwapi.json")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	before := manager.Snapshot()
	returned, err := manager.Update(func(candidate *Config) error {
		candidate.Schema = "cwapi.config.legacy"
		return nil
	})
	if err == nil {
		t.Fatal("expected invalid candidate to fail")
	}
	if returned.Schema != before.Schema || manager.Snapshot().Schema != before.Schema {
		t.Fatalf("in-memory state advanced after failure")
	}
	loaded, loadErr := Load(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	if loaded.Schema != before.Schema {
		t.Fatalf("disk state advanced after failure: %s", loaded.Schema)
	}
}

func TestProjectRemoteMustMatchRepository(t *testing.T) {
	projectPath := t.TempDir()
	project := Project{
		ID:          "prj-0123456789abcdef01234567",
		DisplayName: "CWapi",
		Repository:  "AAAYNMMM/CWapi",
		LocalPath:   filepath.Clean(projectPath),
		RemoteURL:   "https://github.com/AAAYNMMM/other.git",
	}
	if err := ValidateProject(project); err == nil || !strings.Contains(err.Error(), "MISMATCH") {
		t.Fatalf("expected remote mismatch, got %v", err)
	}
}
