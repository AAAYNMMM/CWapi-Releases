package app

import "github.com/AAAYNMMM/CWapi/internal/config"

type SlackConfigSnapshot struct {
	ChannelID string `json:"channel_id"`
}

type ConfigSnapshot struct {
	Schema         string              `json:"schema"`
	Version        string              `json:"version"`
	ConfigPath     string              `json:"config_path"`
	PermissionMode string              `json:"permission_mode"`
	Slack          SlackConfigSnapshot `json:"slack"`
}

func (s *Service) ConfigSnapshot() ConfigSnapshot {
	return snapshotFromConfig(s.config.Path(), s.config.Snapshot())
}

func snapshotFromConfig(path string, cfg config.Config) ConfigSnapshot {
	return ConfigSnapshot{
		Schema: cfg.Schema, Version: cfg.Version, ConfigPath: path,
		PermissionMode: config.EffectivePermissionMode(cfg.PermissionMode),
		Slack:          SlackConfigSnapshot{ChannelID: cfg.Slack.ChannelID},
	}
}
