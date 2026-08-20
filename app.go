package main

import (
	"context"

	coreapp "github.com/AAAYNMMM/CWapi/internal/app"
	"github.com/AAAYNMMM/CWapi/internal/tray"
	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the thin Wails binding facade. Business state lives in the Go core.
type App struct {
	ctx     context.Context
	service *coreapp.Service
	tray    *tray.Controller
}

func NewApp() (*App, error) {
	service, err := coreapp.NewService()
	if err != nil {
		return nil, err
	}
	return &App{service: service}, nil
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.service.Start(ctx)
	a.tray = tray.New(a.showMainWindow, a.RequestShutdown)
	if err := a.tray.Start(); err != nil {
		a.service.DesktopLifecycleError("tray.start", err)
		return
	}
	a.service.DesktopLifecycleReady("single-instance window, tray and shutdown lifecycle ready")
}

func (a *App) shutdown(context.Context) {
	if a.tray != nil {
		if err := a.tray.Close(); err != nil {
			a.service.DesktopLifecycleError("tray.close", err)
		}
	}
	_ = a.service.Close()
}

func (a *App) onSecondInstanceLaunch(_ options.SecondInstanceData) {
	a.showMainWindow()
}

func (a *App) showMainWindow() {
	if a.ctx == nil {
		return
	}
	wailsruntime.WindowUnminimise(a.ctx)
	wailsruntime.Show(a.ctx)
}

// RequestShutdown quits the Wails runtime. Service.Close then stops Slack,
// closes the owned stock Codex app-server process tree and releases Go-owned state.
func (a *App) RequestShutdown() {
	if a.ctx == nil {
		return
	}
	wailsruntime.Quit(a.ctx)
}

func (a *App) RuntimeSnapshot() coreapp.RuntimeSnapshot {
	return a.service.RuntimeSnapshot()
}

func (a *App) DesktopSnapshot(limit int) (coreapp.DesktopSnapshot, error) {
	return a.service.DesktopSnapshot(a.runtimeContext(), limit)
}

func (a *App) DiagnosticsSnapshot() (coreapp.DiagnosticsSnapshot, error) {
	return a.service.DiagnosticsSnapshot(a.runtimeContext())
}

func (a *App) ReadinessSnapshot(limit int) (coreapp.ReadinessSnapshot, error) {
	return a.service.ReadinessSnapshot(a.runtimeContext(), limit)
}

func (a *App) CodexSnapshot() coreapp.CodexSnapshot {
	return a.service.CodexSnapshot()
}

func (a *App) ResolveDesktopError(fingerprint string) (coreapp.ObservabilitySnapshot, error) {
	return a.service.ResolveDesktopError(a.runtimeContext(), fingerprint)
}

func (a *App) ConfigSnapshot() coreapp.ConfigSnapshot {
	return a.service.ConfigSnapshot()
}

func (a *App) UpdatePermissionMode(mode string) (coreapp.ConfigSnapshot, error) {
	return a.service.UpdatePermissionMode(mode)
}

func (a *App) ObservabilitySnapshot() coreapp.ObservabilitySnapshot {
	return a.service.ObservabilitySnapshot()
}

func (a *App) RecentMCPRequests(limit int) ([]coreapp.MCPRequestSnapshot, error) {
	return a.service.RecentMCPRequests(a.runtimeContext(), limit)
}

func (a *App) RecentSlackProtocol(prefix string, limit int) []coreapp.SlackMessageSnapshot {
	return a.service.RecentSlackProtocol(prefix, limit)
}

func (a *App) SlackSnapshot() coreapp.SlackSnapshot {
	return a.service.SlackSnapshot()
}

func (a *App) ConfigureSlack(command coreapp.ConfigureSlackCommand) (coreapp.SlackSnapshot, error) {
	return a.service.ConfigureSlack(a.runtimeContext(), command)
}

func (a *App) PostSlackProtocol(subject, body, threadTS string) (coreapp.SlackMessageSnapshot, error) {
	return a.service.PostSlackProtocol(a.runtimeContext(), subject, body, threadTS)
}

func (a *App) AddProject(command coreapp.ProjectCommand) (coreapp.ConfigSnapshot, error) {
	return a.service.AddProject(command)
}

func (a *App) UpdateProject(id string, command coreapp.ProjectCommand) (coreapp.ConfigSnapshot, error) {
	return a.service.UpdateProject(id, command)
}

func (a *App) RemoveProject(id string) (coreapp.ConfigSnapshot, error) {
	return a.service.RemoveProject(id)
}

func (a *App) runtimeContext() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}
