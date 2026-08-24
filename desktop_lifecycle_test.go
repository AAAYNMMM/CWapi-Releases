package main

import "testing"

func TestS16DesktopLifecycleOptions(t *testing.T) {
	app := &App{}
	options := applicationOptions(app)
	if options.Width != cwapiWindowWidth || options.Height != cwapiWindowHeight ||
		options.MinWidth != cwapiWindowWidth || options.MaxWidth != cwapiWindowWidth ||
		options.MinHeight != cwapiWindowHeight || options.MaxHeight != cwapiWindowHeight {
		t.Fatalf("fixed window bounds=%dx%d min=%dx%d max=%dx%d", options.Width, options.Height,
			options.MinWidth, options.MinHeight, options.MaxWidth, options.MaxHeight)
	}
	if !options.DisableResize || !options.Frameless {
		t.Fatalf("compact window resize=%v frameless=%v", options.DisableResize, options.Frameless)
	}
	if options.BackgroundColour == nil || options.BackgroundColour.R != 8 ||
		options.BackgroundColour.G != 10 || options.BackgroundColour.B != 32 ||
		options.BackgroundColour.A != 255 || options.Windows != nil {
		t.Fatalf("opaque cosmic window options=%#v windows=%#v", options.BackgroundColour, options.Windows)
	}
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
	if options.OnStartup == nil || options.OnDomReady != nil || options.OnShutdown == nil {
		t.Fatal("desktop lifecycle callbacks missing")
	}
	if len(options.Bind) != 1 || options.Bind[0] != app {
		t.Fatalf("Wails binding facade=%#v", options.Bind)
	}
}
