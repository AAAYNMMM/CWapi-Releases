package projects

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AAAYNMMM/CWapi/internal/config"
)

func TestProjectAddUpdateRemoveReturnAuthoritativeSnapshot(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "cwapi.json")
	manager, err := config.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(manager)

	projectPath := t.TempDir()
	added, err := registry.Add(Input{
		DisplayName: "CWapi test",
		Repository:  "AAAYNMMM/CWapi-test",
		LocalPath:   projectPath,
		RemoteURL:   "https://github.com/AAAYNMMM/CWapi-test.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(added.Projects) != 1 || added.Projects[0].DisplayName != "CWapi test" {
		t.Fatalf("add did not return new authoritative state: %#v", added.Projects)
	}
	id := added.Projects[0].ID

	updated, err := registry.Update(id, Input{
		DisplayName: "CWapi renamed",
		Repository:  "AAAYNMMM/CWapi-test",
		LocalPath:   projectPath,
		RemoteURL:   "git@github.com:AAAYNMMM/CWapi-test.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Projects) != 1 || updated.Projects[0].ID != id || updated.Projects[0].DisplayName != "CWapi renamed" {
		t.Fatalf("update did not return new authoritative state: %#v", updated.Projects)
	}

	removed, err := registry.Remove(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.Projects) != 0 {
		t.Fatalf("remove did not return new authoritative state: %#v", removed.Projects)
	}

	reloaded, err := config.Load(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.Projects) != 0 {
		t.Fatalf("disk state diverged from runtime state: %#v", reloaded.Projects)
	}
}

func TestRegisteredProjectIsTheOnlyRepositoryAuthority(t *testing.T) {
	manager, err := config.Open(filepath.Join(t.TempDir(), "cwapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry(manager)
	added, err := registry.Add(Input{
		DisplayName: "Simple",
		Repository:  "AAAYNMMM/CWapi-test",
		LocalPath:   t.TempDir(),
		RemoteURL:   "https://github.com/AAAYNMMM/CWapi-test.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(added.Projects) != 1 || added.Projects[0].Repository != "AAAYNMMM/CWapi-test" {
		t.Fatalf("project was not registered: %#v", added.Projects)
	}
	encoded, err := json.Marshal(added)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "security") || strings.Contains(string(encoded), "allowed_repositories") {
		t.Fatalf("config must not persist a second repository authority: %s", encoded)
	}
}
