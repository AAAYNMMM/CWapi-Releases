package mcpserver

import (
	"context"
	"errors"

	"github.com/AAAYNMMM/CWapi/internal/v2/promptstore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const ToolLoadSkill = "load_skill"

type LoadSkillInput struct {
	Name string `json:"name"`
}

type LoadSkillOutput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

func registerSkillTool(server *mcp.Server, store *promptstore.Store) error {
	if server == nil || store == nil {
		return errors.New("SKILL_STORE_REQUIRED")
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolLoadSkill,
		Description: "Load one global CWapi task Skill by ID. Skills are cached at CWapi startup; editing Skill files requires restarting CWapi.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input LoadSkillInput) (*mcp.CallToolResult, LoadSkillOutput, error) {
		_ = ctx
		skill, err := store.LoadSkill(input.Name)
		if err != nil {
			return nil, LoadSkillOutput{}, err
		}
		return nil, LoadSkillOutput{ID: skill.ID, Name: skill.Name, Description: skill.Description, Content: skill.Content}, nil
	})
	return nil
}
