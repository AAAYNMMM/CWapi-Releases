package codex

import (
	"fmt"
	"os"
	"path/filepath"
)

const commandHomeConfig = "approval_policy = \"never\"\n\n[windows]\nsandbox = \"unelevated\"\n"

func ensureCommandHome(home string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return fmt.Errorf("CODEX_EXECUTION_HOME_CREATE_FAILED: %w", err)
	}
	if err := writeBaseExecPolicyAt(home); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(home, "config.toml"), []byte(commandHomeConfig), 0o600); err != nil {
		return fmt.Errorf("CODEX_EXECUTION_CONFIG_WRITE_FAILED: %w", err)
	}
	return nil
}
