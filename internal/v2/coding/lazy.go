package coding

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AAAYNMMM/CWapi/internal/v2/codextoolhost"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
	"github.com/AAAYNMMM/CWapi/internal/v2/workspace"
)

// NewLazy verifies the private Codex command toolhost on first coding_open so
// an unavailable Codex runtime does not block the independent AGENT surface.
// Verification is model-free and does not touch the user's native CODEX_HOME.
func NewLazy(manager *workspace.Manager, dataRoot string, cfg v2config.CodexConfig) (*Service, error) {
	if manager == nil {
		return nil, errors.New("CODING_WORKSPACE_MANAGER_REQUIRED")
	}
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("CODING_DATA_ROOT_INVALID")
	}
	if err := v2config.ValidateCodex(cfg); err != nil {
		return nil, err
	}
	var hostMu sync.Mutex
	var host *codextoolhost.Host
	currentConfig := cfg
	resolve := func() (*codextoolhost.Host, error) {
		hostMu.Lock()
		defer hostMu.Unlock()
		if host != nil {
			return host, nil
		}
		resolved, err := codextoolhost.New(filepath.Clean(dataRoot), currentConfig)
		if err != nil {
			return nil, err
		}
		host = resolved
		return host, nil
	}
	setAccessProfile := func(profile string) error {
		hostMu.Lock()
		defer hostMu.Unlock()
		candidate := currentConfig
		candidate.AccessProfile = strings.ToLower(strings.TrimSpace(profile))
		if err := v2config.ValidateCodex(candidate); err != nil {
			return err
		}
		if host != nil {
			if err := host.SetAccessProfile(candidate.AccessProfile); err != nil {
				return err
			}
		}
		currentConfig = candidate
		return nil
	}
	execute := func(ctx context.Context, path string, input codextoolhost.ExecInput) (codextoolhost.ExecResult, error) {
		current, err := resolve()
		if err != nil {
			return codextoolhost.ExecResult{}, err
		}
		return current.Exec(ctx, path, input)
	}
	service, err := newService(manager.Prepare, execute, manager.Inspect, func() error {
		_, err := resolve()
		return err
	})
	if err != nil {
		return nil, err
	}
	service.setAccessProfile = setAccessProfile
	return service, nil
}
