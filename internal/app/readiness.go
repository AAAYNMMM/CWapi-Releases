package app

import (
	"context"
	"fmt"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/codex"
)

type CodexSnapshot struct {
	Configured       bool   `json:"configured"`
	Ready            bool   `json:"ready"`
	Running          bool   `json:"running"`
	Version          string `json:"version"`
	ExecutablePath   string `json:"executable_path"`
	ExecutableSHA256 string `json:"executable_sha256"`
	BrowserMCPReady  bool   `json:"browser_mcp_ready"`
	ProcessMCPReady  bool   `json:"process_mcp_ready"`
	NodePath         string `json:"node_path"`
	BrowserPath      string `json:"browser_path"`
}

type ReadinessSnapshot struct {
	GeneratedAt     int64                `json:"generated_at"`
	Runtime         RuntimeSnapshot      `json:"runtime"`
	Slack           SlackSnapshot        `json:"slack"`
	Codex           CodexSnapshot        `json:"codex"`
	MCPRuntimeReady bool                 `json:"mcp_runtime_ready"`
	LocalReady      bool                 `json:"local_ready"`
	Ready           bool                 `json:"ready"`
	Detail          string               `json:"detail"`
	RecentRequests  []MCPRequestSnapshot `json:"recent_requests"`
}

func (s *Service) CodexSnapshot() CodexSnapshot {
	if s == nil || s.codexHost == nil {
		return CodexSnapshot{}
	}
	return codexSnapshotFromHost(s.codexHost.HostSnapshot())
}

func (s *Service) ReadinessSnapshot(ctx context.Context, limit int) (ReadinessSnapshot, error) {
	if s == nil || s.state == nil {
		return ReadinessSnapshot{}, fmt.Errorf("READINESS_STATE_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.state.Ping(ctx); err != nil {
		return ReadinessSnapshot{}, err
	}
	recent, err := s.RecentMCPRequests(ctx, limit)
	if err != nil {
		return ReadinessSnapshot{}, err
	}
	slack := s.SlackSnapshot()
	codexState := s.CodexSnapshot()
	mcpRuntimeReady := s.gateway != nil && s.gateway.MCPRuntimeReady()
	localReady := codexState.Ready && mcpRuntimeReady
	ready := localReady && slack.Ready && slack.SocketReady
	detail := readinessDetail(localReady, mcpRuntimeReady, codexState, slack)
	return ReadinessSnapshot{
		GeneratedAt:     time.Now().UnixMilli(),
		Runtime:         s.RuntimeSnapshot(),
		Slack:           slack,
		Codex:           codexState,
		MCPRuntimeReady: mcpRuntimeReady,
		LocalReady:      localReady,
		Ready:           ready,
		Detail:          detail,
		RecentRequests:  recent,
	}, nil
}

func codexSnapshotFromHost(value codex.MCPHostSnapshot) CodexSnapshot {
	return CodexSnapshot{
		Configured:       value.Runtime.Configured,
		Ready:            value.ExecutableVerified,
		Running:          value.Running,
		Version:          value.Runtime.Version,
		ExecutablePath:   value.Runtime.ExecutablePath,
		ExecutableSHA256: value.Runtime.ExecutableSHA,
		BrowserMCPReady:  value.Runtime.MCPReady,
		ProcessMCPReady:  value.Runtime.ProcessMCPReady,
		NodePath:         value.Runtime.NodePath,
		BrowserPath:      value.Runtime.BrowserPath,
	}
}

func readinessDetail(localReady, mcpRuntimeReady bool, codexState CodexSnapshot, slack SlackSnapshot) string {
	switch {
	case !codexState.Configured:
		return "packaged stock Codex runtime is missing"
	case !codexState.Ready:
		return "packaged stock Codex executable is not verified"
	case !mcpRuntimeReady:
		return "stock Codex MCP relay is not attached"
	case !slack.Configured:
		return "Slack configuration is required"
	case !slack.Ready || !slack.SocketReady:
		return "Slack transport is not ready"
	case !localReady:
		return "local MCP relay is not ready"
	default:
		return "Slack, stock Codex app-server and MCP relay are ready"
	}
}
