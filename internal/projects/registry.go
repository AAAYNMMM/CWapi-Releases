package projects

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/config"
)

// Input is the complete project configuration needed by the MCP runtime.
type Input struct {
	DisplayName string `json:"display_name"`
	Repository  string `json:"repository"`
	LocalPath   string `json:"local_path"`
	RemoteURL   string `json:"remote_url"`
}

type Registry struct {
	manager *config.Manager
	newID   func() (string, error)
}

func NewRegistry(manager *config.Manager) *Registry {
	return &Registry{manager: manager, newID: randomProjectID}
}

func (r *Registry) Snapshot() config.Config {
	return r.manager.Snapshot()
}

func (r *Registry) Add(input Input) (config.Config, error) {
	project, err := r.projectFromInput("", input)
	if err != nil {
		return r.manager.Snapshot(), err
	}
	project.ID, err = r.newID()
	if err != nil {
		return r.manager.Snapshot(), fmt.Errorf("PROJECT_ID_CREATE_FAILED: %w", err)
	}
	if err := config.ValidateProject(project); err != nil {
		return r.manager.Snapshot(), err
	}
	return r.manager.Update(func(candidate *config.Config) error {
		candidate.Projects = append(candidate.Projects, project)
		return nil
	})
}

func (r *Registry) Update(id string, input Input) (config.Config, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return r.manager.Snapshot(), fmt.Errorf("PROJECT_ID_REQUIRED")
	}
	project, err := r.projectFromInput(id, input)
	if err != nil {
		return r.manager.Snapshot(), err
	}
	if err := config.ValidateProject(project); err != nil {
		return r.manager.Snapshot(), err
	}
	return r.manager.Update(func(candidate *config.Config) error {
		index := findProject(candidate.Projects, id)
		if index < 0 {
			return fmt.Errorf("PROJECT_NOT_FOUND: %s", id)
		}
		candidate.Projects[index] = project
		return nil
	})
}

func (r *Registry) Remove(id string) (config.Config, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return r.manager.Snapshot(), fmt.Errorf("PROJECT_ID_REQUIRED")
	}
	return r.manager.Update(func(candidate *config.Config) error {
		index := findProject(candidate.Projects, id)
		if index < 0 {
			return fmt.Errorf("PROJECT_NOT_FOUND: %s", id)
		}
		candidate.Projects = append(candidate.Projects[:index], candidate.Projects[index+1:]...)
		return nil
	})
}

func (r *Registry) projectFromInput(id string, input Input) (config.Project, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return config.Project{}, fmt.Errorf("PROJECT_DISPLAY_NAME_REQUIRED")
	}
	repository, err := config.CanonicalRepository(input.Repository)
	if err != nil {
		return config.Project{}, err
	}
	localPath, err := config.CanonicalProjectPath(input.LocalPath)
	if err != nil {
		return config.Project{}, err
	}
	remoteURL, err := config.ValidateRemoteRepository(input.RemoteURL, repository)
	if err != nil {
		return config.Project{}, err
	}
	return config.Project{
		ID:          id,
		DisplayName: displayName,
		Repository:  repository,
		LocalPath:   localPath,
		RemoteURL:   remoteURL,
	}, nil
}

func findProject(projects []config.Project, id string) int {
	for index := range projects {
		if projects[index].ID == id {
			return index
		}
	}
	return -1
}

func randomProjectID() (string, error) {
	var bytes [12]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return "prj-" + hex.EncodeToString(bytes[:]), nil
}
