package gateway

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/buildinfo"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
)

type projectDiscoveryItem struct {
	ProjectID   string `json:"project_id"`
	DisplayName string `json:"display_name"`
	Repository  string `json:"repository"`
}

const projectDiscoveryUsage = "Use project_id together with the exact 40-character expected_commit for project-bound calls."

func (g *Gateway) projectDiscoveryItems() []projectDiscoveryItem {
	cfg := g.config.Snapshot()
	projects := make([]projectDiscoveryItem, len(cfg.Projects))
	for index, project := range cfg.Projects {
		projects[index] = projectDiscoveryItem{
			ProjectID: project.ID, DisplayName: project.DisplayName, Repository: project.Repository,
		}
	}
	sort.Slice(projects, func(i, j int) bool {
		if projects[i].DisplayName == projects[j].DisplayName {
			return projects[i].Repository < projects[j].Repository
		}
		return projects[i].DisplayName < projects[j].DisplayName
	})
	return projects
}

func (g *Gateway) withCWapiDiscovery(value any) any {
	discovery := map[string]any{
		"schema":        "cwapi.discovery.v1",
		"source_commit": buildinfo.Commit(),
		"request_methods": []string{
			"projects/list", "mcpServerStatus/list", "mcpServer/resource/read", "mcpServer/tool/call",
		},
		"projects": g.projectDiscoveryItems(),
		"project_context": map[string]any{
			"required_fields": []string{"project_id", "expected_commit"},
			"usage":           projectDiscoveryUsage,
		},
		"process_tools":       []string{"cwapi/process_start", "cwapi/process_status", "cwapi/process_stop"},
		"process_start_modes": []string{"command_argv", "runtime_entrypoint"},
		"command_path_forms":  []string{"PATH executable name", "absolute executable path", "working-directory-relative executable path"},
		"projects_list_request": map[string]any{
			"method": "projects/list",
			"params": map[string]any{},
		},
	}
	if result, ok := value.(map[string]any); ok {
		augmented := make(map[string]any, len(result)+1)
		for key, item := range result {
			augmented[key] = item
		}
		augmented["cwapi"] = discovery
		return augmented
	}
	return map[string]any{"toolhost": value, "cwapi": discovery}
}

func (g *Gateway) projectDiscoverySummary() string {
	projects := g.projectDiscoveryItems()
	if len(projects) == 0 {
		return "No projects are configured; add one in the CWapi Projects page, then call method=projects/list with params={}."
	}
	const maxSummaryProjects = 10
	parts := make([]string, 0, min(len(projects), maxSummaryProjects))
	for index, project := range projects {
		if index >= maxSummaryProjects {
			break
		}
		parts = append(parts, fmt.Sprintf(
			"project_id=%s | name=%s | repository=%s",
			project.ProjectID,
			discoverySummaryField(project.DisplayName, 80),
			discoverySummaryField(project.Repository, 160),
		))
	}
	summary := "Configured projects: " + strings.Join(parts, "; ") + "."
	if len(projects) > maxSummaryProjects {
		summary += fmt.Sprintf(" %d more project(s) are available through method=projects/list with params={}.", len(projects)-maxSummaryProjects)
	} else {
		summary += " Call method=projects/list with params={} for structured discovery."
	}
	return summary
}

func discoverySummaryField(value string, maxRunes int) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value)
	characters := []rune(value)
	if len(characters) > maxRunes {
		characters = characters[:maxRunes]
	}
	return string(characters)
}

func (g *Gateway) projectsListResponse(request protocol.MCPRequest) protocol.MCPResponse {
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil || len(params) != 0 {
		return mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_PROJECTS_LIST_PARAMS_INVALID", "discovery", "projects/list requires empty params")
	}
	payload, err := json.Marshal(map[string]any{
		"schema":        "cwapi.projects.list.v1",
		"source_commit": buildinfo.Commit(),
		"projects":      g.projectDiscoveryItems(),
		"usage":         projectDiscoveryUsage,
	})
	if err != nil {
		return mcpErrorResponse(request.RequestID, protocol.MCPStatusFailed,
			"MCP_PROJECTS_LIST_ENCODE_FAILED", "discovery", "project discovery result could not be encoded")
	}
	return protocol.MCPResponse{
		Schema: protocol.MCPResponseSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: request.RequestID, Status: protocol.MCPStatusCompleted, Result: payload,
	}
}
