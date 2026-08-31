package codextoolhost

import (
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/codex"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
)

type RuntimeSnapshot struct {
	State         string `json:"state"`
	AccessProfile string `json:"access_profile"`
	Executable    string `json:"executable,omitempty"`
	LastError     string `json:"last_error,omitempty"`
}

func Probe(dataRoot string, cfg v2config.CodexConfig) RuntimeSnapshot {
	snapshot := RuntimeSnapshot{State: "unavailable", AccessProfile: cfg.AccessProfile}
	dataRoot = strings.TrimSpace(dataRoot)
	service, err := codex.NewCommandService(filepath.Clean(dataRoot), cfg.Executable)
	if err != nil {
		snapshot.LastError = err.Error()
		return snapshot
	}
	runtime := service.Snapshot()
	if !runtime.Configured || !strings.EqualFold(runtime.ExecutableSHA, codex.PinnedExecutableSHA256) {
		snapshot.LastError = "CODEX_TOOLHOST_RUNTIME_UNAVAILABLE"
		return snapshot
	}
	snapshot.State = "ready"
	snapshot.Executable = runtime.ExecutablePath
	return snapshot
}
