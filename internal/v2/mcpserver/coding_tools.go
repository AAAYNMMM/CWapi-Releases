package mcpserver

import (
	"context"
	"errors"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolCodingOpen   = "coding_open"
	ToolCodingExec   = "coding_exec"
	ToolCodingStatus = "coding_status"
	ToolCodingClose  = "coding_close"
)

type CodingOpenInput struct {
	RepositoryURL  string `json:"repository_url" jsonschema:"Git repository URL"`
	TargetRef      string `json:"target_ref" jsonschema:"branch or ref used as the task target"`
	ExpectedCommit string `json:"expected_commit,omitempty" jsonschema:"optional exact commit guard"`
	Resume         bool   `json:"resume,omitempty" jsonschema:"resume the repository's existing active/durable workspace when compatible"`
}

type CodingOpenOutput struct {
	Repository     string `json:"repository"`
	TargetRef      string `json:"target_ref"`
	ResolvedCommit string `json:"resolved_commit"`
	CurrentHead    string `json:"current_head"`
	TrackedDirty   bool   `json:"tracked_dirty"`
	Resumed        bool   `json:"resumed"`
	State          string `json:"state"`
}

type CodingExecInput struct {
	RepositoryURL  string   `json:"repository_url" jsonschema:"Git repository URL used to locate the repository's active Coding session"`
	Command        string   `json:"command" jsonschema:"executable name or forward-slash path; do not include shell quoting"`
	Argv           []string `json:"argv,omitempty" jsonschema:"exact argument vector"`
	CWD            string   `json:"cwd,omitempty" jsonschema:"optional forward-slash relative directory inside the prepared repository"`
	TimeoutSeconds int      `json:"timeout_seconds,omitempty" jsonschema:"optional command timeout from 1 to 600 seconds"`
}

type CodingExecOutput struct {
	State     string `json:"state"`
	ExitCode  int    `json:"exit_code"`
	Stdout    string `json:"stdout,omitempty"`
	Stderr    string `json:"stderr,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

type CodingStatusInput struct {
	RepositoryURL string `json:"repository_url" jsonschema:"Git repository URL used to locate the repository's active Coding session"`
}

type CodingStatusOutput struct {
	State          string `json:"state"`
	Repository     string `json:"repository,omitempty"`
	TargetRef      string `json:"target_ref,omitempty"`
	ResolvedCommit string `json:"resolved_commit,omitempty"`
	CurrentHead    string `json:"current_head,omitempty"`
	TrackingHead   string `json:"tracking_head,omitempty"`
	TrackedDirty   bool   `json:"tracked_dirty,omitempty"`
	Divergence     string `json:"divergence,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

type CodingCloseInput struct {
	RepositoryURL string `json:"repository_url" jsonschema:"Git repository URL whose current active Coding session should be closed"`
}

type CodingCloseOutput struct {
	Repository string `json:"repository,omitempty"`
	State      string `json:"state"`
}

type CodingService interface {
	Open(context.Context, CodingOpenInput) (CodingOpenOutput, error)
	Exec(context.Context, CodingExecInput) (CodingExecOutput, error)
	Status(context.Context, CodingStatusInput) (CodingStatusOutput, error)
	Close(context.Context, CodingCloseInput) (CodingCloseOutput, error)
}

func RegisterCoding(server *mcp.Server, service CodingService) error {
	if server == nil || service == nil {
		return errors.New("CODING_SERVICE_REQUIRED")
	}
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolCodingOpen,
		Description: "Open or resume one repository-scoped coding workspace. Web GPT does not receive or retain a session ID; CWapi maps the canonical repository to its internal active session and durable workspace without starting a Codex agent or model turn.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodingOpenInput) (*mcp.CallToolResult, CodingOpenOutput, error) {
		input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
		input.TargetRef = strings.TrimSpace(input.TargetRef)
		input.ExpectedCommit = strings.TrimSpace(input.ExpectedCommit)
		if input.RepositoryURL == "" || input.TargetRef == "" {
			return nil, CodingOpenOutput{}, errors.New("CODING_OPEN_INPUT_INVALID")
		}
		output, err := service.Open(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolCodingExec,
		Description: "Run one exact development command in the active workspace selected by repository_url. Pass the executable separately from an exact argv array; CWapi never starts a Codex thread/turn or uses a Codex account.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodingExecInput) (*mcp.CallToolResult, CodingExecOutput, error) {
		input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
		input.Command = strings.TrimSpace(input.Command)
		input.CWD = strings.TrimSpace(input.CWD)
		if input.RepositoryURL == "" || input.Command == "" {
			return nil, CodingExecOutput{}, errors.New("CODING_EXEC_INPUT_INVALID")
		}
		output, err := service.Exec(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolCodingStatus,
		Description: "Return current local Git truth for repository_url's active durable workspace without fetching or invoking a Codex model.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodingStatusInput) (*mcp.CallToolResult, CodingStatusOutput, error) {
		input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
		if input.RepositoryURL == "" {
			return nil, CodingStatusOutput{}, errors.New("CODING_REPOSITORY_REQUIRED")
		}
		output, err := service.Status(ctx, input)
		return nil, output, err
	})
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolCodingClose,
		Description: "Close repository_url's current active Coding session handle without resetting, cleaning or deleting the durable workspace.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodingCloseInput) (*mcp.CallToolResult, CodingCloseOutput, error) {
		input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
		if input.RepositoryURL == "" {
			return nil, CodingCloseOutput{}, errors.New("CODING_REPOSITORY_REQUIRED")
		}
		output, err := service.Close(ctx, input)
		return nil, output, err
	})
	return nil
}
