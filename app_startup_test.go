package main

import (
	"strings"
	"testing"
)

func TestNewAppIsLightweightUntilWailsStartup(t *testing.T) {
	app := NewApp()
	if app == nil || app.service != nil || app.ctx != nil {
		t.Fatalf("pre-run facade initialized Core state: %#v", app)
	}
	if _, err := app.core(); err == nil || !strings.Contains(err.Error(), "CORE_NOT_STARTED") {
		t.Fatalf("pre-run Core state error = %v", err)
	}
}
