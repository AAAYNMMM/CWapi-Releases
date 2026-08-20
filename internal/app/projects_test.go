package app

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestConfigSnapshotSerializesProjectsAsArray(t *testing.T) {
	service, err := NewServiceWithConfigPath(filepath.Join(t.TempDir(), "cwapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()

	snapshot := service.ConfigSnapshot()
	if snapshot.Projects == nil {
		t.Fatalf("projects must serialize as an array: %#v", snapshot)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if !containsText(text, `"projects":[]`) {
		t.Fatalf("snapshot JSON missing empty projects array: %s", text)
	}
	if containsText(text, `"security"`) || containsText(text, `"allowed_actions"`) {
		t.Fatalf("MCP UI snapshot exposes retired policy fields: %s", text)
	}
}

func TestServiceProjectWriteReturnsCurrentSnapshot(t *testing.T) {
	service, err := NewServiceWithConfigPath(filepath.Join(t.TempDir(), "cwapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	projectPath := t.TempDir()

	afterAdd, err := service.AddProject(ProjectCommand{
		DisplayName: "CWapi test",
		Repository:  "example/example-project",
		LocalPath:   projectPath,
		RemoteURL:   "https://github.com/example/example-project.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(afterAdd.Projects) != 1 || len(service.ConfigSnapshot().Projects) != 1 {
		t.Fatalf("add snapshot is stale: %#v", afterAdd)
	}
	id := afterAdd.Projects[0].ID

	afterUpdate, err := service.UpdateProject(id, ProjectCommand{
		DisplayName: "CWapi renamed",
		Repository:  "example/example-project",
		LocalPath:   projectPath,
		RemoteURL:   "https://github.com/example/example-project.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	if afterUpdate.Projects[0].DisplayName != "CWapi renamed" {
		t.Fatalf("update returned stale snapshot: %#v", afterUpdate.Projects)
	}

	afterRemove, err := service.RemoveProject(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(afterRemove.Projects) != 0 || len(service.ConfigSnapshot().Projects) != 0 {
		t.Fatalf("remove returned stale snapshot: %#v", afterRemove.Projects)
	}
}

func containsText(value, wanted string) bool {
	for i := 0; i+len(wanted) <= len(value); i++ {
		if value[i:i+len(wanted)] == wanted {
			return true
		}
	}
	return false
}
