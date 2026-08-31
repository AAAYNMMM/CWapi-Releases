package mcpserver

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/repository"
	"github.com/AAAYNMMM/CWapi/internal/v2/attachments"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	ToolCodingOpen       = "coding_open"
	ToolCodingExec       = "coding_exec"
	ToolCodingStatus     = "coding_status"
	ToolCodingAttachment = "coding_attachment"
	ToolCodingClose      = "coding_close"
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
	TimeoutSeconds int      `json:"timeout_seconds,omitempty" jsonschema:"optional command timeout from 1 to 180 seconds"`
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

type CodingAttachmentInput struct {
	RepositoryURL string   `json:"repository_url" jsonschema:"Git repository URL used to locate the repository's active Coding session"`
	Paths         []string `json:"paths" jsonschema:"one or more forward-slash relative image paths inside the prepared repository; ordinary files are not supported"`
}

type CodingAttachmentOutput struct {
	Repository   string                 `json:"repository"`
	State        string                 `json:"state"`
	TotalBytes   int64                  `json:"total_bytes"`
	Attachments  []attachments.Metadata `json:"attachments"`
	ContentItems []attachments.Item     `json:"-"`
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
	Attachment(context.Context, CodingAttachmentInput) (CodingAttachmentOutput, error)
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
		Name:        ToolCodingAttachment,
		Description: "Return one or more raster images from repository_url's active workspace as native MCP ImageContent. Ordinary text, PDF, archive, document and other file attachments are not supported; read text through coding_exec instead.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, input CodingAttachmentInput) (*mcp.CallToolResult, CodingAttachmentOutput, error) {
		input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
		if input.RepositoryURL == "" || len(input.Paths) == 0 {
			return nil, CodingAttachmentOutput{}, errors.New("CODING_ATTACHMENT_INPUT_INVALID")
		}
		output, err := service.Attachment(ctx, input)
		if err != nil {
			return nil, output, err
		}
		for _, item := range output.ContentItems {
			if item.Metadata.Kind != "image" {
				return nil, CodingAttachmentOutput{}, errors.New("CODING_ATTACHMENT_IMAGE_ONLY")
			}
		}
		repositoryKey := output.Repository
		if repositoryKey == "" {
			if identity, parseErr := repository.Parse(input.RepositoryURL); parseErr == nil {
				repositoryKey = identity.Repository
			}
		}
		result := &mcp.CallToolResult{}
		for _, item := range output.ContentItems {
			ref := strings.TrimSpace(item.Metadata.Ref)
			if ref == "" {
				ref = item.Metadata.Name
			}
			uri := "cwapi://coding/" + url.PathEscape(repositoryKey) + "/" + url.PathEscape(ref) + "/" + url.PathEscape(item.Metadata.Name)
			result.Content = append(result.Content, attachments.MCPContent(item, "repository="+repositoryKey, uri)...)
		}
		return result, output, nil
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
