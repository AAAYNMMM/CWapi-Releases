package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
	"github.com/AAAYNMMM/CWapi/internal/v2/promptstore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	SurfaceCoding = "coding"
	SurfaceAgent  = "agent"
)

type Config = v2config.MCPConfig

type Registrar func(*mcp.Server) error

type Snapshot struct {
	State   string `json:"state"`
	Address string `json:"address,omitempty"`
	Error   string `json:"error,omitempty"`
}

type Runtime struct {
	mu sync.RWMutex

	cfg Config

	codingEnabled bool
	agentEnabled  bool
	codingServer  *mcp.Server
	agentServer   *mcp.Server
	codingHandler http.Handler
	agentHandler  http.Handler
	handler       http.Handler

	server   *http.Server
	listener net.Listener
	snapshot Snapshot
}

func New(cfg Config, coding Registrar, agent Registrar) (*Runtime, error) {
	if err := v2config.ValidateMCP(cfg); err != nil {
		return nil, err
	}
	codingEnabled := coding != nil
	agentEnabled := agent != nil
	promptRoot, err := promptstore.DiscoverRoot()
	if err != nil {
		return nil, err
	}
	prompts, warnings, err := promptstore.Load(promptRoot, codingEnabled, agentEnabled)
	if err != nil {
		return nil, err
	}
	for _, warning := range warnings {
		log.Printf("[WARN] %s", warning)
	}
	codingInstructions, agentInstructions := "", ""
	if codingEnabled {
		codingInstructions, err = prompts.Instructions(SurfaceCoding)
		if err != nil {
			return nil, err
		}
	}
	if agentEnabled {
		agentInstructions, err = prompts.Instructions(SurfaceAgent)
		if err != nil {
			return nil, err
		}
	}

	codingServer := mcp.NewServer(
		&mcp.Implementation{Name: "cwapi-coding", Version: v2config.Version},
		&mcp.ServerOptions{Instructions: codingInstructions},
	)
	agentServer := mcp.NewServer(
		&mcp.Implementation{Name: "cwapi-agent", Version: v2config.Version},
		&mcp.ServerOptions{Instructions: agentInstructions},
	)
	if codingEnabled {
		if err := coding(codingServer); err != nil {
			return nil, fmt.Errorf("MCP_CODING_REGISTER_FAILED: %w", err)
		}
		if err := registerSkillTool(codingServer, prompts); err != nil {
			return nil, fmt.Errorf("MCP_CODING_SKILL_REGISTER_FAILED: %w", err)
		}
	}
	if agentEnabled {
		if err := agent(agentServer); err != nil {
			return nil, fmt.Errorf("MCP_AGENT_REGISTER_FAILED: %w", err)
		}
		if err := registerSkillTool(agentServer, prompts); err != nil {
			return nil, fmt.Errorf("MCP_AGENT_SKILL_REGISTER_FAILED: %w", err)
		}
	}

	r := &Runtime{
		cfg:           cfg,
		codingEnabled: codingEnabled,
		agentEnabled:  agentEnabled,
		codingServer:  codingServer,
		agentServer:   agentServer,
	}
	streamableOptions := &mcp.StreamableHTTPOptions{Stateless: true}
	r.codingHandler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return codingServer }, streamableOptions)
	r.agentHandler = mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return agentServer }, streamableOptions)
	r.handler = r.route()
	r.snapshot = Snapshot{State: "stopped"}
	return r, nil
}

// CodingEndpoint intentionally exposes the secret-bearing URL only through an
// explicit getter. Disabled surfaces return an empty endpoint.
func (r *Runtime) CodingEndpoint() string {
	if r == nil || !r.codingEnabled {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(r.cfg.Port) + r.codingPath()
}

// AgentEndpoint intentionally exposes the secret-bearing URL only through an
// explicit getter. Disabled surfaces return an empty endpoint.
func (r *Runtime) AgentEndpoint() string {
	if r == nil || !r.agentEnabled {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(r.cfg.Port) + r.agentPath()
}

func (r *Runtime) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshot
}

func (r *Runtime) Start(ctx context.Context) error {
	r.mu.Lock()
	if r.listener != nil {
		r.mu.Unlock()
		return nil
	}

	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(r.cfg.Port)))
	if err != nil {
		r.snapshot.State = "failed"
		r.snapshot.Error = "MCP_LISTEN_FAILED"
		r.mu.Unlock()
		return fmt.Errorf("MCP_LISTEN_FAILED: %w", err)
	}

	httpServer := &http.Server{
		Handler:           r.handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	r.listener = listener
	r.server = httpServer
	r.snapshot.State = "running"
	r.snapshot.Address = listener.Addr().String()
	r.snapshot.Error = ""
	r.mu.Unlock()

	go func() {
		err := httpServer.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			r.mu.Lock()
			r.snapshot.State = "failed"
			r.snapshot.Error = "MCP_SERVE_FAILED"
			r.mu.Unlock()
		}
	}()

	if ctx != nil && ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = r.Close(closeCtx)
		}()
	}
	return nil
}

func (r *Runtime) Close(ctx context.Context) error {
	r.mu.Lock()
	server := r.server
	if server == nil {
		r.snapshot.State = "stopped"
		r.snapshot.Address = ""
		r.mu.Unlock()
		return nil
	}
	r.server = nil
	r.listener = nil
	r.mu.Unlock()

	err := server.Shutdown(ctx)
	r.mu.Lock()
	r.snapshot.State = "stopped"
	r.snapshot.Address = ""
	if err != nil {
		r.snapshot.Error = "MCP_SHUTDOWN_FAILED"
	} else {
		r.snapshot.Error = ""
	}
	r.mu.Unlock()
	if err != nil {
		return fmt.Errorf("MCP_SHUTDOWN_FAILED: %w", err)
	}
	return nil
}

func (r *Runtime) codingPath() string { return "/mcp/coding/" + r.cfg.CodingToken }
func (r *Runtime) agentPath() string  { return "/mcp/agent/" + r.cfg.AgentToken }

func (r *Runtime) route() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var next http.Handler
		switch {
		case r.codingEnabled && req.URL.Path == r.codingPath():
			next = r.codingHandler
		case r.agentEnabled && req.URL.Path == r.agentPath():
			next = r.agentHandler
		default:
			http.NotFound(w, req)
			return
		}
		if !validOrigin(req.Header.Get("Origin")) {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, req)
	})
}

func validOrigin(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "127.0.0.1" || host == "localhost" || host == "::1" {
		return parsed.Scheme == "http" || parsed.Scheme == "https"
	}
	if parsed.Scheme != "https" {
		return false
	}
	return host == "chatgpt.com" || strings.HasSuffix(host, ".chatgpt.com") ||
		host == "openai.com" || strings.HasSuffix(host, ".openai.com")
}
