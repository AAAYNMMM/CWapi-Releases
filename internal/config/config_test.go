package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultIsExactV2Shape(t *testing.T) {
	payload, err := json.Marshal(Default())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema":"cwapi.config.v2","version":"1.6.1","permission_mode":"safe","slack":{"channel_id":""}}`
	if string(payload) != want {
		t.Fatalf("default config = %s", payload)
	}
}

func TestOpenRejectsOldMismatchAndUnknownWithoutMigration(t *testing.T) {
	for name, payload := range map[string]string{
		"old":        `{"schema":"cwapi.config.v1","version":"1.6.0","permission_mode":"safe","slack":{"channel_id":""},"projects":[]}`,
		"mismatch":   `{"schema":"cwapi.config.v2","version":"1.6.0","permission_mode":"safe","slack":{"channel_id":""}}`,
		"unknown":    `{"schema":"cwapi.config.v2","version":"1.6.1","permission_mode":"safe","slack":{"channel_id":""},"projects":[]}`,
		"empty-mode": `{"schema":"cwapi.config.v2","version":"1.6.1","permission_mode":"","slack":{"channel_id":""}}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cwapi.json")
			if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Open(path); err == nil {
				t.Fatal("invalid config unexpectedly opened")
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != payload {
				t.Fatalf("invalid config was modified: payload=%q err=%v", after, err)
			}
		})
	}
}

func TestStrictConfigRejectsSlackTokenFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cwapi.json")
	payload := `{"schema":"cwapi.config.v2","version":"1.6.1","permission_mode":"safe","slack":{"channel_id":"C12345678","bot_token":"secret"}}`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "unknown field") || !strings.Contains(err.Error(), "bot_token") {
		t.Fatalf("expected Slack token rejection, got %v", err)
	}
}

func TestPermissionModeAndSlackValidation(t *testing.T) {
	for input, want := range map[string]string{
		PermissionModeSafe:       PermissionModeSafe,
		PermissionModeFullAccess: PermissionModeFullAccess,
	} {
		actual, err := CanonicalPermissionMode(input)
		if err != nil || actual != want {
			t.Fatalf("mode %q: actual=%q err=%v", input, actual, err)
		}
	}
	for _, value := range []string{"", "full", "admin", " full_access"} {
		if _, err := CanonicalPermissionMode(value); err == nil {
			t.Fatalf("mode %q should fail", value)
		}
	}
	for _, value := range []string{"", "C12345678", "G1234567890"} {
		if _, err := CanonicalSlackChannelID(value); err != nil {
			t.Fatalf("channel %q should pass: %v", value, err)
		}
	}
}

func TestManagerPersistsBeforeAdvancingMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "cwapi.json")
	manager, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	updated, err := manager.Update(func(candidate *Config) error {
		candidate.Slack.ChannelID = "C12345678"
		candidate.PermissionMode = PermissionModeFullAccess
		return nil
	})
	if err != nil || updated.PermissionMode != PermissionModeFullAccess {
		t.Fatalf("update=%#v err=%v", updated, err)
	}
	loaded, err := Load(path)
	if err != nil || loaded != updated {
		t.Fatalf("disk=%#v memory=%#v err=%v", loaded, updated, err)
	}
	before := manager.Snapshot()
	returned, err := manager.Update(func(candidate *Config) error {
		candidate.Schema = "bad"
		return nil
	})
	if err == nil || returned != before || manager.Snapshot() != before {
		t.Fatalf("invalid update advanced state: returned=%#v err=%v", returned, err)
	}
}
