//go:build windows

package tray

import (
	"os"
	"testing"
	"time"
)

func TestWindowsTrayStartsAndStops(t *testing.T) {
	controller := New(nil, nil)
	if err := controller.Start(); err != nil {
		t.Fatalf("tray start failed: %v", err)
	}
	if err := controller.Close(); err != nil {
		t.Fatalf("tray close failed: %v", err)
	}
}

func TestWindowsTrayLeftClickDispatchesOpen(t *testing.T) {
	opened := make(chan struct{}, 1)
	implementation := &windowsImplementation{onOpen: func() { opened <- struct{}{} }}
	if result := implementation.windowProc(0, wmTrayIcon, 0, wmLButtonUp); result != 0 {
		t.Fatalf("tray callback result=%d", result)
	}
	select {
	case <-opened:
	case <-time.After(time.Second):
		t.Fatal("tray open callback was not dispatched")
	}
}

func TestPackagedExecutableIconCanBeLoaded(t *testing.T) {
	executable := os.Getenv("CWAPI_TEST_TRAY_ICON_EXE")
	if executable == "" {
		t.Skip("packaged tray icon gate is not enabled")
	}
	icon, owned := loadExecutableIcon(executable)
	if icon == 0 || !owned {
		t.Fatalf("embedded icon missing from %s", executable)
	}
	procDestroyIcon.Call(icon)
}
