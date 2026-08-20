package architecture_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestProductRootsStayGoWailsReactOnly(t *testing.T) {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve architecture test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", ".."))

	roots := []string{
		filepath.Join(root, "cmd"),
		filepath.Join(root, "internal"),
		filepath.Join(root, "frontend"),
	}
	entryFiles := []string{
		filepath.Join(root, "main.go"),
		filepath.Join(root, "app.go"),
		filepath.Join(root, "wails.json"),
	}

	forbiddenExtensions := map[string]struct{}{
		".py": {},
		".rs": {},
	}
	forbiddenText := []string{
		"@tauri-apps",
		"Gmail" + "Transport",
		"gmail" + "_gateway",
		"poll" + "_interval_seconds",
		"cancel" + "_poll_seconds",
		"max" + "_tasks_per_poll",
	}

	checkFile := func(path string) {
		if filepath.Clean(path) == filepath.Clean(current) {
			return
		}
		if _, blocked := forbiddenExtensions[strings.ToLower(filepath.Ext(path))]; blocked {
			t.Errorf("forbidden product source language: %s", path)
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		text := string(data)
		for _, token := range forbiddenText {
			if strings.Contains(text, token) {
				t.Errorf("forbidden legacy/runtime token %q in %s", token, path)
			}
		}
	}

	for _, path := range entryFiles {
		checkFile(path)
	}
	for _, base := range roots {
		if _, err := os.Stat(base); os.IsNotExist(err) {
			continue
		} else if err != nil {
			t.Fatalf("stat %s: %v", base, err)
		}
		err := filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				name := entry.Name()
				if name == "node_modules" || name == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			checkFile(path)
			return nil
		})
		if err != nil {
			t.Fatalf("scan %s: %v", base, err)
		}
	}
}
