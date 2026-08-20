package app

import (
	"sort"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/projects"
)

// ProjectCommand contains only facts the MCP runtime actually needs.
type ProjectCommand struct {
	DisplayName string `json:"display_name"`
	Repository  string `json:"repository"`
	LocalPath   string `json:"local_path"`
	RemoteURL   string `json:"remote_url"`
}

type ProjectSnapshot struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Repository  string `json:"repository"`
	LocalPath   string `json:"local_path"`
	RemoteURL   string `json:"remote_url"`
}

type SlackConfigSnapshot struct {
	ChannelID string `json:"channel_id"`
}

type ConfigSnapshot struct {
	Schema         string              `json:"schema"`
	Version        string              `json:"version"`
	ConfigPath     string              `json:"config_path"`
	PermissionMode string              `json:"permission_mode"`
	Slack          SlackConfigSnapshot `json:"slack"`
	Projects       []ProjectSnapshot   `json:"projects"`
}

func (s *Service) ConfigSnapshot() ConfigSnapshot {
	return snapshotFromConfig(s.config.Path(), s.config.Snapshot())
}

func (s *Service) AddProject(command ProjectCommand) (ConfigSnapshot, error) {
	cfg, err := s.projects.Add(projectInput(command))
	if err != nil {
		s.recordOperationalError("projects", "projects.add", err)
		return s.ConfigSnapshot(), err
	}
	s.runtimeInfo("projects", "project added", map[string]any{"repository": command.Repository})
	return snapshotFromConfig(s.config.Path(), cfg), nil
}

func (s *Service) UpdateProject(id string, command ProjectCommand) (ConfigSnapshot, error) {
	cfg, err := s.projects.Update(id, projectInput(command))
	if err != nil {
		s.recordOperationalError("projects", "projects.update", err)
		return s.ConfigSnapshot(), err
	}
	s.runtimeInfo("projects", "project updated", map[string]any{"project_id": id, "repository": command.Repository})
	return snapshotFromConfig(s.config.Path(), cfg), nil
}

func (s *Service) RemoveProject(id string) (ConfigSnapshot, error) {
	cfg, err := s.projects.Remove(id)
	if err != nil {
		s.recordOperationalError("projects", "projects.remove", err)
		return s.ConfigSnapshot(), err
	}
	s.runtimeInfo("projects", "project removed", map[string]any{"project_id": id})
	return snapshotFromConfig(s.config.Path(), cfg), nil
}

func projectInput(command ProjectCommand) projects.Input {
	return projects.Input{
		DisplayName: command.DisplayName,
		Repository:  command.Repository,
		LocalPath:   command.LocalPath,
		RemoteURL:   command.RemoteURL,
	}
}

func snapshotFromConfig(path string, cfg config.Config) ConfigSnapshot {
	projectSnapshots := make([]ProjectSnapshot, len(cfg.Projects))
	for index, project := range cfg.Projects {
		projectSnapshots[index] = ProjectSnapshot{
			ID:          project.ID,
			DisplayName: project.DisplayName,
			Repository:  project.Repository,
			LocalPath:   project.LocalPath,
			RemoteURL:   project.RemoteURL,
		}
	}
	sort.Slice(projectSnapshots, func(i, j int) bool {
		if projectSnapshots[i].DisplayName == projectSnapshots[j].DisplayName {
			return projectSnapshots[i].Repository < projectSnapshots[j].Repository
		}
		return projectSnapshots[i].DisplayName < projectSnapshots[j].DisplayName
	})
	return ConfigSnapshot{
		Schema:         cfg.Schema,
		Version:        cfg.Version,
		ConfigPath:     path,
		PermissionMode: config.EffectivePermissionMode(cfg.PermissionMode),
		Slack:          SlackConfigSnapshot{ChannelID: cfg.Slack.ChannelID},
		Projects:       projectSnapshots,
	}
}
