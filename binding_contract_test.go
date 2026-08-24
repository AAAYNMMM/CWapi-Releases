package main

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMCPDesktopBindingContract(t *testing.T) {
	typeOfApp := reflect.TypeOf(&App{})
	expected := map[string]int{
		"RuntimeSnapshot":      0,
		"DesktopSnapshot":      1,
		"StopProcess":          1,
		"RequestShutdown":      0,
		"ConfigSnapshot":       0,
		"UpdatePermissionMode": 1,
		"SlackSnapshot":        0,
		"ConfigureSlack":       1,
		"PostSlackProtocol":    3,
	}
	for name, argumentCount := range expected {
		method, ok := typeOfApp.MethodByName(name)
		if !ok {
			t.Fatalf("missing Wails binding method %s", name)
		}
		if method.Type.NumIn()-1 != argumentCount {
			t.Fatalf("binding %s argument count = %d, want %d", name, method.Type.NumIn()-1, argumentCount)
		}
	}

	retiredBindings := []string{
		"TaskDetail", "CancelTask", "SetSecurityPolicy", "AddProject", "UpdateProject", "RemoveProject",
		"DiagnosticsSnapshot", "ResolveDesktopError", "GitHubSnapshot", "RefreshGitHubStatus", "ObservabilitySnapshot",
	}
	for _, retired := range retiredBindings {
		if _, ok := typeOfApp.MethodByName(retired); ok {
			t.Fatalf("retired Runner binding still exposed: %s", retired)
		}
	}

	declarations, err := os.ReadFile("frontend/wailsjs/go/main/App.d.ts")
	if err != nil {
		t.Fatal(err)
	}
	javascript, err := os.ReadFile("frontend/wailsjs/go/main/App.js")
	if err != nil {
		t.Fatal(err)
	}
	for name := range expected {
		if !strings.Contains(string(declarations), "function "+name+"(") {
			t.Fatalf("TypeScript binding declaration missing %s", name)
		}
		if !strings.Contains(string(javascript), "function "+name+"(") {
			t.Fatalf("JavaScript binding implementation missing %s", name)
		}
	}
	for _, retired := range retiredBindings {
		if strings.Contains(string(declarations), "function "+retired+"(") || strings.Contains(string(javascript), "function "+retired+"(") {
			t.Fatalf("retired Runner binding generated: %s", retired)
		}
	}
}
