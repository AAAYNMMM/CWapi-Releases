package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	v2service "github.com/AAAYNMMM/CWapi/internal/v2/service"
	"github.com/AAAYNMMM/CWapi/internal/v2/workspace"
)

// DeleteWorkspace is a desktop-only maintenance action. It is deliberately not
// part of either MCP app. The next coding_open for the repository recreates it.
func (a *App) DeleteWorkspace(repositoryName string) (v2service.Snapshot, error) {
	a.reconfigureMu.Lock()
	defer a.reconfigureMu.Unlock()

	current, err := a.core()
	if err != nil {
		return a.RuntimeSnapshot(), err
	}
	before := current.Snapshot()
	if before.Coding.Active > 0 {
		return before, errors.New("WORKSPACE_MAINTENANCE_BUSY_CODING")
	}
	if before.Agent.Pending > 0 || before.Agent.Claimed > 0 {
		return before, errors.New("WORKSPACE_MAINTENANCE_BUSY_AGENT")
	}
	repositoryName = strings.ToLower(strings.TrimSpace(repositoryName))
	if repositoryName == "" {
		return before, errors.New("WORKSPACE_REPOSITORY_REQUIRED")
	}

	a.mu.RLock()
	configPath, runtimeCtx := a.configPath, a.ctx
	a.mu.RUnlock()
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr := current.Close(closeCtx)
	cancel()
	if closeErr != nil {
		restartErr := a.restartV2Service(configPath, runtimeCtx)
		return a.RuntimeSnapshot(), errors.Join(closeErr, restartErr)
	}

	dataRoot := filepath.Dir(configPath)
	if strings.EqualFold(filepath.Base(dataRoot), "config") {
		dataRoot = filepath.Dir(dataRoot)
	}
	deleteErr := workspace.DeleteAt(filepath.Clean(dataRoot), repositoryName)
	restartErr := a.restartV2Service(configPath, runtimeCtx)
	return a.RuntimeSnapshot(), errors.Join(deleteErr, restartErr)
}

func (a *App) restartV2Service(configPath string, runtimeCtx context.Context) error {
	next, err := v2service.NewDefault(configPath)
	if err == nil {
		err = next.Start(runtimeCtx)
	}
	a.mu.Lock()
	if err == nil {
		a.service = next
		a.startupErr = nil
	} else {
		a.service = nil
		a.startupErr = err
	}
	a.mu.Unlock()
	return err
}
