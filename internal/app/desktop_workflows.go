package app

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type DesktopSnapshot struct {
	GeneratedAt   int64                 `json:"generated_at"`
	Runtime       RuntimeSnapshot       `json:"runtime"`
	Config        ConfigSnapshot        `json:"config"`
	Slack         SlackSnapshot         `json:"slack"`
	Codex         CodexSnapshot         `json:"codex"`
	MCPRequests   []MCPRequestSnapshot  `json:"mcp_requests"`
	Observability ObservabilitySnapshot `json:"observability"`
}

type DiagnosticsSnapshot struct {
	GeneratedAt  int64                `json:"generated_at"`
	Version      string               `json:"version"`
	SourceCommit string               `json:"source_commit"`
	Architecture string               `json:"architecture"`
	Platform     string               `json:"platform"`
	Stage        string               `json:"stage"`
	ConfigPath   string               `json:"config_path"`
	StatePath    string               `json:"state_path"`
	StateSchema  string               `json:"state_schema"`
	Slack        SlackSnapshot        `json:"slack"`
	Codex        CodexSnapshot        `json:"codex"`
	MCPRequests  []MCPRequestSnapshot `json:"mcp_requests"`
	Components   []ComponentSnapshot  `json:"components"`
}

func (s *Service) DesktopSnapshot(ctx context.Context, limit int) (DesktopSnapshot, error) {
	if s == nil || s.state == nil {
		return DesktopSnapshot{}, fmt.Errorf("DESKTOP_STATE_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.state.Ping(ctx); err != nil {
		s.recordOperationalError("desktop", "state.ping", err)
		return DesktopSnapshot{}, err
	}
	requests, err := s.RecentMCPRequests(ctx, limit)
	if err != nil {
		return DesktopSnapshot{}, err
	}
	return DesktopSnapshot{
		GeneratedAt:   time.Now().UnixMilli(),
		Runtime:       s.RuntimeSnapshot(),
		Config:        s.ConfigSnapshot(),
		Slack:         s.SlackSnapshot(),
		Codex:         s.CodexSnapshot(),
		MCPRequests:   requests,
		Observability: s.ObservabilitySnapshot(),
	}, nil
}

func (s *Service) ResolveDesktopError(ctx context.Context, fingerprint string) (ObservabilitySnapshot, error) {
	if s == nil || s.observability == nil {
		return ObservabilitySnapshot{}, fmt.Errorf("DESKTOP_OBSERVABILITY_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	fingerprint = strings.TrimSpace(fingerprint)
	if fingerprint == "" {
		return ObservabilitySnapshot{}, fmt.Errorf("DESKTOP_ERROR_FINGERPRINT_REQUIRED")
	}
	if err := s.observability.ResolveError(ctx, fingerprint); err != nil {
		return ObservabilitySnapshot{}, err
	}
	s.runtimeInfo("desktop", "persistent error resolved", map[string]any{"fingerprint": fingerprint})
	return s.ObservabilitySnapshot(), nil
}

func (s *Service) DiagnosticsSnapshot(ctx context.Context) (DiagnosticsSnapshot, error) {
	if s == nil || s.state == nil {
		return DiagnosticsSnapshot{}, fmt.Errorf("DESKTOP_STATE_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.state.Ping(ctx); err != nil {
		return DiagnosticsSnapshot{}, err
	}
	schema, err := s.state.SchemaVersion(ctx)
	if err != nil {
		return DiagnosticsSnapshot{}, err
	}
	requests, err := s.RecentMCPRequests(ctx, 50)
	if err != nil {
		return DiagnosticsSnapshot{}, err
	}
	runtimeSnapshot := s.RuntimeSnapshot()
	observabilitySnapshot := s.ObservabilitySnapshot()
	return DiagnosticsSnapshot{
		GeneratedAt:  time.Now().UnixMilli(),
		Version:      runtimeSnapshot.Version,
		SourceCommit: runtimeSnapshot.SourceCommit,
		Architecture: runtimeSnapshot.Architecture,
		Platform:     runtimeSnapshot.Platform,
		Stage:        runtimeSnapshot.Stage,
		ConfigPath:   s.config.Path(),
		StatePath:    s.state.Path(),
		StateSchema:  schema,
		Slack:        s.SlackSnapshot(),
		Codex:        s.CodexSnapshot(),
		MCPRequests:  requests,
		Components:   observabilitySnapshot.Components,
	}, nil
}
