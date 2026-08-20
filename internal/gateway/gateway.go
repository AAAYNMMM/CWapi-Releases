package gateway

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

type ConfigProvider interface {
	Snapshot() config.Config
}

type SlackPoster interface {
	Post(context.Context, string, string, string) (PostedMessage, error)
}

type SlackFilePoster interface {
	UploadFile(context.Context, string, string, []byte, string) (UploadedFile, error)
}

type PostedMessage struct {
	MessageID string
	MessageTS string
	ThreadTS  string
}

type UploadedFile struct {
	FileID    string
	Name      string
	Size      int64
	Permalink string
}

type MCPExecutionContext struct {
	ProjectID      string
	ExpectedCommit string
	Repository     string
	CWD            string
}

type MCPContextResolver interface {
	PrepareMCPContext(context.Context, string, string, string) (MCPExecutionContext, func(), error)
}

type MCPToolhost interface {
	CallMCP(context.Context, string, map[string]any, time.Duration, MCPExecutionContext) (any, error)
}

type MCPRuntime struct {
	Toolhost        MCPToolhost
	ContextResolver MCPContextResolver
}

type Gateway struct {
	config ConfigProvider
	store  *state.Store
	poster SlackPoster
	logs   *observability.Hub

	mcpMu      sync.RWMutex
	mcpRuntime MCPRuntime
}

func NewMCP(provider ConfigProvider, store *state.Store, poster SlackPoster, logs *observability.Hub) (*Gateway, error) {
	if provider == nil || store == nil || poster == nil {
		return nil, errors.New("GATEWAY_DEPENDENCY_MISSING")
	}
	return &Gateway{config: provider, store: store, poster: poster, logs: logs}, nil
}

func (g *Gateway) AttachMCPRuntime(runtime MCPRuntime) error {
	if runtime.Toolhost == nil {
		return errors.New("MCP_RUNTIME_INVALID")
	}
	g.mcpMu.Lock()
	g.mcpRuntime = runtime
	g.mcpMu.Unlock()
	return nil
}

func (g *Gateway) MCPRuntimeReady() bool {
	g.mcpMu.RLock()
	ready := g.mcpRuntime.Toolhost != nil
	g.mcpMu.RUnlock()
	return ready
}

func (g *Gateway) mcpRuntimeSnapshot() MCPRuntime {
	g.mcpMu.RLock()
	defer g.mcpMu.RUnlock()
	return g.mcpRuntime
}

func (g *Gateway) HandleMessage(ctx context.Context, message slackcore.Message) error {
	return g.HandleMCPMessage(ctx, message)
}

func (g *Gateway) HandleMCPMessage(ctx context.Context, message slackcore.Message) error {
	subject, err := protocol.ParseMCPSubject(message.Subject)
	if err != nil {
		return nil
	}
	return g.handleMCPMessage(ctx, message, subject)
}
