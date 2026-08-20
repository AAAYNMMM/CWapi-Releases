package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	Schema                   = "cwapi.config.v1"
	Version                  = "1.6.0"
	PermissionModeSafe       = "safe"
	PermissionModeFullAccess = "full_access"
)

var (
	repositoryPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	projectIDPattern    = regexp.MustCompile(`^prj-[a-f0-9]{24}$`)
	slackChannelPattern = regexp.MustCompile(`^[A-Z0-9]{9,32}$`)
)

// SlackConfig contains only non-secret Slack routing configuration.
type SlackConfig struct {
	ChannelID string `json:"channel_id"`
}

// Project is the single authoritative project registry entry used by MCP.
type Project struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	Repository  string `json:"repository"`
	LocalPath   string `json:"local_path"`
	RemoteURL   string `json:"remote_url"`
}

type Config struct {
	Schema         string      `json:"schema"`
	Version        string      `json:"version"`
	PermissionMode string      `json:"permission_mode"`
	Slack          SlackConfig `json:"slack"`
	Projects       []Project   `json:"projects"`
}

func Default() Config {
	return Config{
		Schema: Schema, Version: Version, PermissionMode: PermissionModeSafe,
		Slack: SlackConfig{}, Projects: []Project{},
	}
}

func (c Config) Clone() Config {
	clone := c
	clone.Projects = append([]Project(nil), c.Projects...)
	return clone
}

func Validate(c Config) error {
	if c.Schema != Schema {
		return fmt.Errorf("CONFIG_SCHEMA_UNSUPPORTED: expected %q, got %q", Schema, c.Schema)
	}
	if c.Version != Version {
		return fmt.Errorf("CONFIG_VERSION_UNSUPPORTED: expected %q, got %q", Version, c.Version)
	}
	if _, err := CanonicalPermissionMode(c.PermissionMode); err != nil {
		return err
	}
	if _, err := CanonicalSlackChannelID(c.Slack.ChannelID); err != nil {
		return err
	}
	if len(c.Projects) > 128 {
		return fmt.Errorf("CONFIG_PROJECT_LIMIT: got %d projects", len(c.Projects))
	}

	seenIDs := map[string]struct{}{}
	seenRepositories := map[string]struct{}{}
	seenPaths := map[string]struct{}{}
	for index, project := range c.Projects {
		if err := ValidateProject(project); err != nil {
			return fmt.Errorf("projects[%d]: %w", index, err)
		}
		if _, exists := seenIDs[project.ID]; exists {
			return fmt.Errorf("CONFIG_PROJECT_ID_DUPLICATE: %s", project.ID)
		}
		seenIDs[project.ID] = struct{}{}

		repositoryKey := strings.ToLower(project.Repository)
		if _, exists := seenRepositories[repositoryKey]; exists {
			return fmt.Errorf("CONFIG_PROJECT_REPOSITORY_DUPLICATE: %s", project.Repository)
		}
		seenRepositories[repositoryKey] = struct{}{}

		pathKey := strings.ToLower(filepath.Clean(project.LocalPath))
		if _, exists := seenPaths[pathKey]; exists {
			return fmt.Errorf("CONFIG_PROJECT_PATH_DUPLICATE: %s", project.LocalPath)
		}
		seenPaths[pathKey] = struct{}{}
	}
	return nil
}

func EffectivePermissionMode(value string) string {
	if value == "" {
		return PermissionModeSafe
	}
	return value
}

func CanonicalPermissionMode(value string) (string, error) {
	if value == "" {
		return PermissionModeSafe, nil
	}
	mode := strings.TrimSpace(value)
	if mode != value {
		return "", fmt.Errorf("CONFIG_PERMISSION_MODE_INVALID: %q", value)
	}
	switch mode {
	case PermissionModeSafe, PermissionModeFullAccess:
		return mode, nil
	default:
		return "", fmt.Errorf("CONFIG_PERMISSION_MODE_INVALID: %q", value)
	}
}

func CanonicalSlackChannelID(value string) (string, error) {
	channelID := strings.TrimSpace(value)
	if channelID == "" {
		return "", nil
	}
	if channelID != value || !slackChannelPattern.MatchString(channelID) {
		return "", fmt.Errorf("SLACK_CHANNEL_ID_INVALID: %q", value)
	}
	return channelID, nil
}

func ValidateProject(project Project) error {
	if !projectIDPattern.MatchString(project.ID) {
		return fmt.Errorf("PROJECT_ID_INVALID: %q", project.ID)
	}
	if project.DisplayName == "" || project.DisplayName != strings.TrimSpace(project.DisplayName) || len(project.DisplayName) > 120 {
		return fmt.Errorf("PROJECT_DISPLAY_NAME_INVALID")
	}
	if project.Repository != strings.TrimSpace(project.Repository) || !repositoryPattern.MatchString(project.Repository) {
		return fmt.Errorf("PROJECT_REPOSITORY_INVALID: %q", project.Repository)
	}
	if project.LocalPath == "" || project.LocalPath != filepath.Clean(project.LocalPath) || !isAbsolutePath(project.LocalPath) {
		return fmt.Errorf("PROJECT_PATH_INVALID: %q", project.LocalPath)
	}
	info, err := os.Stat(project.LocalPath)
	if err != nil {
		return fmt.Errorf("PROJECT_PATH_UNAVAILABLE: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("PROJECT_PATH_NOT_DIRECTORY: %s", project.LocalPath)
	}
	if len(project.RemoteURL) > 2048 || project.RemoteURL != strings.TrimSpace(project.RemoteURL) {
		return fmt.Errorf("PROJECT_REMOTE_INVALID")
	}
	remoteRepository, err := repositoryFromRemote(project.RemoteURL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(remoteRepository, project.Repository) {
		return fmt.Errorf("PROJECT_REMOTE_REPOSITORY_MISMATCH: remote=%s repository=%s", remoteRepository, project.Repository)
	}
	return nil
}

func CanonicalProjectPath(path string) (string, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", fmt.Errorf("PROJECT_PATH_REQUIRED")
	}
	cleaned := filepath.Clean(trimmed)
	if !isAbsolutePath(cleaned) {
		return "", fmt.Errorf("PROJECT_PATH_NOT_ABSOLUTE: %s", trimmed)
	}
	info, err := os.Stat(cleaned)
	if err != nil {
		return "", fmt.Errorf("PROJECT_PATH_UNAVAILABLE: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("PROJECT_PATH_NOT_DIRECTORY: %s", cleaned)
	}
	absolute, err := filepath.Abs(cleaned)
	if err != nil {
		return "", fmt.Errorf("PROJECT_PATH_RESOLVE_FAILED: %w", err)
	}
	return filepath.Clean(absolute), nil
}

func CanonicalRepository(value string) (string, error) {
	repository := strings.TrimSpace(value)
	if !repositoryPattern.MatchString(repository) {
		return "", fmt.Errorf("PROJECT_REPOSITORY_INVALID: %q", value)
	}
	return repository, nil
}

func ValidateRemoteRepository(remoteURL, repository string) (string, error) {
	remote := strings.TrimSpace(remoteURL)
	resolved, err := repositoryFromRemote(remote)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(resolved, repository) {
		return "", fmt.Errorf("PROJECT_REMOTE_REPOSITORY_MISMATCH: remote=%s repository=%s", resolved, repository)
	}
	return remote, nil
}

func repositoryFromRemote(raw string) (string, error) {
	remote := strings.TrimSpace(raw)
	if remote == "" {
		return "", fmt.Errorf("PROJECT_REMOTE_REQUIRED")
	}

	var remotePath string
	if strings.HasPrefix(remote, "git@") && strings.Contains(remote, ":") {
		parts := strings.SplitN(remote, ":", 2)
		if len(parts) != 2 || parts[1] == "" {
			return "", fmt.Errorf("PROJECT_REMOTE_INVALID: %q", raw)
		}
		remotePath = parts[1]
	} else {
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Host == "" {
			return "", fmt.Errorf("PROJECT_REMOTE_INVALID: %q", raw)
		}
		switch strings.ToLower(parsed.Scheme) {
		case "https", "http", "ssh", "git":
		default:
			return "", fmt.Errorf("PROJECT_REMOTE_SCHEME_UNSUPPORTED: %q", parsed.Scheme)
		}
		if parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("PROJECT_REMOTE_INVALID: query/fragment not allowed")
		}
		remotePath = parsed.Path
	}

	remotePath = strings.Trim(strings.TrimSuffix(strings.TrimSpace(remotePath), ".git"), "/")
	segments := strings.Split(remotePath, "/")
	if len(segments) != 2 {
		return "", fmt.Errorf("PROJECT_REMOTE_PATH_INVALID: %q", remotePath)
	}
	repository := segments[0] + "/" + segments[1]
	if !repositoryPattern.MatchString(repository) {
		return "", fmt.Errorf("PROJECT_REMOTE_REPOSITORY_INVALID: %q", repository)
	}
	return repository, nil
}

func isAbsolutePath(path string) bool {
	if filepath.IsAbs(path) {
		return true
	}
	if len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		return true
	}
	return strings.HasPrefix(path, `\\`)
}
