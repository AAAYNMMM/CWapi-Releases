package main

import (
	"context"
	"errors"
	"fmt"
	"sync"

	coreapp "github.com/AAAYNMMM/CWapi/internal/app"
	"github.com/AAAYNMMM/CWapi/internal/tray"
	"github.com/wailsapp/wails/v2/pkg/options"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the thin Wails binding facade. Business state lives in the Go core.
type App struct {
	mu         sync.RWMutex
	ctx        context.Context
	service    *coreapp.Service
	startupErr error
	tray       *tray.Controller
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	service, err := coreapp.NewService()
	a.mu.Lock()
	a.ctx = ctx
	a.service, a.startupErr = service, err
	a.mu.Unlock()
	if err != nil {
		fmt.Println("CWapi core startup failed:", err.Error())
		wailsruntime.Quit(ctx)
		return
	}
	service.Start(ctx)
	a.tray = tray.New(a.showMainWindow, a.RequestShutdown)
	if err := a.tray.Start(); err != nil {
		service.DesktopLifecycleError("tray.start", err)
		return
	}
	service.DesktopLifecycleReady("single-instance window, tray and shutdown lifecycle ready")
}

func (a *App) shutdown(context.Context) {
	service, _ := a.core()
	if a.tray != nil {
		if err := a.tray.Close(); err != nil {
			if service != nil {
				service.DesktopLifecycleError("tray.close", err)
			}
		}
	}
	if service != nil {
		_ = service.Close()
	}
}

func (a *App) onSecondInstanceLaunch(_ options.SecondInstanceData) {
	a.showMainWindow()
}

func (a *App) showMainWindow() {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return
	}
	wailsruntime.WindowUnminimise(ctx)
	wailsruntime.Show(ctx)
}

// RequestShutdown quits the Wails runtime. Service.Close then stops Slack,
// closes the owned stock Codex app-server process tree and releases Go-owned state.
func (a *App) RequestShutdown() {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx == nil {
		return
	}
	wailsruntime.Quit(ctx)
}

func (a *App) RuntimeSnapshot() coreapp.RuntimeSnapshot {
	service, _ := a.core()
	if service == nil {
		return coreapp.RuntimeSnapshot{}
	}
	return service.RuntimeSnapshot()
}

func (a *App) DesktopSnapshot(limit int) (coreapp.DesktopSnapshot, error) {
	service, err := a.core()
	if err != nil {
		return coreapp.DesktopSnapshot{}, err
	}
	return service.DesktopSnapshot(a.runtimeContext(), limit)
}

func (a *App) StopProcess(processID string) (coreapp.ProcessSnapshot, error) {
	service, err := a.core()
	if err != nil {
		return coreapp.ProcessSnapshot{}, err
	}
	return service.StopProcess(processID)
}

func (a *App) ReadinessSnapshot(limit int) (coreapp.ReadinessSnapshot, error) {
	service, err := a.core()
	if err != nil {
		return coreapp.ReadinessSnapshot{}, err
	}
	return service.ReadinessSnapshot(a.runtimeContext(), limit)
}

func (a *App) CodexSnapshot() coreapp.CodexSnapshot {
	service, _ := a.core()
	if service == nil {
		return coreapp.CodexSnapshot{}
	}
	return service.CodexSnapshot()
}

func (a *App) ConfigSnapshot() coreapp.ConfigSnapshot {
	service, _ := a.core()
	if service == nil {
		return coreapp.ConfigSnapshot{}
	}
	return service.ConfigSnapshot()
}

func (a *App) UpdatePermissionMode(mode string) (coreapp.ConfigSnapshot, error) {
	service, err := a.core()
	if err != nil {
		return coreapp.ConfigSnapshot{}, err
	}
	return service.UpdatePermissionMode(mode)
}

func (a *App) RecentMCPRequests(limit int) ([]coreapp.MCPRequestSnapshot, error) {
	service, err := a.core()
	if err != nil {
		return nil, err
	}
	return service.RecentMCPRequests(a.runtimeContext(), limit)
}

func (a *App) RecentSlackProtocol(prefix string, limit int) []coreapp.SlackMessageSnapshot {
	service, _ := a.core()
	if service == nil {
		return nil
	}
	return service.RecentSlackProtocol(prefix, limit)
}

func (a *App) SlackSnapshot() coreapp.SlackSnapshot {
	service, _ := a.core()
	if service == nil {
		return coreapp.SlackSnapshot{}
	}
	return service.SlackSnapshot()
}

func (a *App) ConfigureSlack(command coreapp.ConfigureSlackCommand) (coreapp.SlackSnapshot, error) {
	service, err := a.core()
	if err != nil {
		return coreapp.SlackSnapshot{}, err
	}
	return service.ConfigureSlack(a.runtimeContext(), command)
}

func (a *App) PostSlackProtocol(subject, body, threadTS string) (coreapp.SlackMessageSnapshot, error) {
	service, err := a.core()
	if err != nil {
		return coreapp.SlackMessageSnapshot{}, err
	}
	return service.PostSlackProtocol(a.runtimeContext(), subject, body, threadTS)
}

func (a *App) runtimeContext() context.Context {
	a.mu.RLock()
	ctx := a.ctx
	a.mu.RUnlock()
	if ctx != nil {
		return ctx
	}
	return context.Background()
}

func (a *App) core() (*coreapp.Service, error) {
	if a == nil {
		return nil, errors.New("CORE_UNAVAILABLE")
	}
	a.mu.RLock()
	service, startupErr := a.service, a.startupErr
	a.mu.RUnlock()
	if service != nil {
		return service, nil
	}
	if startupErr != nil {
		return nil, errors.New("CORE_STARTUP_FAILED")
	}
	return nil, errors.New("CORE_NOT_STARTED")
}
