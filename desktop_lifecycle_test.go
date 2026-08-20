package main

import "testing"

func TestS16DesktopLifecycleOptions(t *testing.T) {
	app := &App{}
	options := applicationOptions(app)
	if !options.HideWindowOnClose {
		t.Fatal("window close must hide CWapi so the Go Core can remain resident")
	}
	if options.SingleInstanceLock == nil {
		t.Fatal("single-instance lock missing")
	}
	if options.SingleInstanceLock.UniqueId != cwapiSingleInstanceID || cwapiSingleInstanceID == "" {
		t.Fatalf("single-instance id=%q", options.SingleInstanceLock.UniqueId)
	}
	if options.SingleInstanceLock.OnSecondInstanceLaunch == nil {
		t.Fatal("second-instance focus callback missing")
	}
	if options.OnStartup == nil || options.OnShutdown == nil {
		t.Fatal("desktop lifecycle callbacks missing")
	}
	if len(options.Bind) != 1 || options.Bind[0] != app {
		t.Fatalf("Wails binding facade=%#v", options.Bind)
	}
}
