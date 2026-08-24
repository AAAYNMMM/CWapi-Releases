package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/executionpolicy"
)

func TestEnsureCommandHomeWritesOnlyExecutionRuntimeConfig(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	if err := ensureCommandHome(home); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(config)
	if !strings.Contains(text, "sandbox = \"unelevated\"") || strings.Contains(text, "mcp_servers") {
		t.Fatalf("unexpected command home config: %s", text)
	}
	rules, err := os.ReadFile(filepath.Join(home, "rules", "default.rules"))
	if err != nil {
		t.Fatal(err)
	}
	if string(rules) != executionpolicy.CodexRules() {
		t.Fatal("command home rules diverged from the permanent policy")
	}
}
