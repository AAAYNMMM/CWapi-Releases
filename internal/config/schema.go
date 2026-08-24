package config

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	Schema                   = "cwapi.config.v2"
	Version                  = "1.6.1"
	PermissionModeSafe       = "safe"
	PermissionModeFullAccess = "full_access"
)

var slackChannelPattern = regexp.MustCompile(`^[A-Z0-9]{9,32}$`)

type SlackConfig struct {
	ChannelID string `json:"channel_id"`
}

// Config is intentionally small. Repository identity is supplied by each v2
// request and credentials remain in Windows Credential Manager.
type Config struct {
	Schema         string      `json:"schema"`
	Version        string      `json:"version"`
	PermissionMode string      `json:"permission_mode"`
	Slack          SlackConfig `json:"slack"`
}

func Default() Config {
	return Config{
		Schema: Schema, Version: Version, PermissionMode: PermissionModeSafe,
		Slack: SlackConfig{},
	}
}

func (c Config) Clone() Config { return c }

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
	_, err := CanonicalSlackChannelID(c.Slack.ChannelID)
	return err
}

func EffectivePermissionMode(value string) string {
	if value == "" {
		return PermissionModeSafe
	}
	return value
}

func CanonicalPermissionMode(value string) (string, error) {
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
