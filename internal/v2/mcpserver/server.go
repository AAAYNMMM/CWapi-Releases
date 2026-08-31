package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	SurfaceCoding = "coding"
	SurfaceAgent  = "agent"

	codingInstructions = `CWapi 2.0 Coding operating manual for the current conversation/task. Apply these rules after first contact with this MCP without requiring repeated reminders on every tool call.

- Web GPT is the sole coding agent. CWapi does not use Codex accounts, models, threads, turns, or agent behavior.
- Start each Coding conversation/task with coding_open. Web GPT never receives or stores a coding session ID. CWapi canonicalizes repository_url and maps that stable repository identity to its internal active session. Reuse the same repository_url throughout the task; compatible coding_open(..., resume=true) resumes the existing internal session, including from a new conversation. A browser/chat conversation ending does not automatically close the CWapi session.
- Use coding_exec(repository_url, command, argv, ...) for exact development commands. Pass the executable separately from an exact argv array; do not send one shell-quoted command string.
- Inspect before editing. Avoid large numbers of fragmented coding_exec round trips. When several reads or searches are independent, prefer one information-rich command or a small number of well-grouped commands instead of many tiny round trips. Do not re-read unchanged files without a reason.
- coding_attachment is image-only. Use it only for raster images that should be shown to Web GPT as native MCP ImageContent. Ordinary text, source, JSON, logs, PDF, archive, document and other files are not transferable through Coding; read inspectable text through coding_exec instead.
- After edits, run the narrowest useful verification first, then broaden tests when needed. Use coding_status when Git/workspace truth is actually needed and before final handoff rather than after every small step.
- SAFE is for normal read/edit/test work. FULL is only for explicitly authorized Git metadata operations such as commit/push. Never delete remote refs or force-push unless the user explicitly requests it.
- Close the Coding session with coding_close(repository_url) when the task is genuinely finished. Closing releases only the active session owner; it does not reset Git, clean files, delete uncommitted work or delete the durable workspace. If a conversation disappears before close, a later conversation should use coding_open(..., resume=true) for that repository.`

	agentInstructions = `CWapi 2.0 Agent operating manual for the current conversation/task. Apply these rules after first contact with this MCP without requiring repeated reminders on every exchange.

- Call agent_open() to open or resume CWapi's single active Agent bridge. Web GPT never receives or stores bridge_id; CWapi retains the internal bridge generation and automatically uses it for agent_exchange and agent_close. Do not close and reopen merely because an exchange returns no_request.
- Default agent_exchange(capacity=4); if agent_open reports max_inflight below 4, use that smaller capacity. agent_exchange automatically uses the current bridge. Process the full returned batch and correlate every response by its exact request_id; request_id remains mandatory because multiple requests may be in flight at once.
- Submit all completed responses together in the next agent_exchange; that same exchange also waits for and returns the next request batch.
- Each returned request is one OpenAI-compatible model request. When one request needs multiple independent third-party function calls and their arguments are already known, return those calls together in the same assistant tool_calls array. Do not split independent calls across extra model turns just to inspect intermediate results. Keep calls sequential only when a later call genuinely depends on an earlier tool result.
- Tool calls are executed by the third-party software, not by CWapi or this MCP. Use unique tool-call IDs, valid function names, and valid JSON argument strings.
- Local software may include bounded inline raster images using the standard Chat Completions image_url data-URI form. agent_exchange returns their metadata plus native MCP ImageContent after removing inline bytes from the request JSON. Generic file attachments are not supported: no text/PDF/archive/document file is turned into an MCP resource. Treat images as temporary request data; on delivery > 1, correlate them with the same request_id. CWapi does not import ChatGPT conversation uploads into local software.
- Independent requests in the same batch may be reasoned about together when practical, but correctness must not depend on parallel scheduling.
- delivery > 1 is redelivery of the same request_id, not new work. Re-send the same completed response safely if delivery was lost; never invent a different response for an already completed request ID.
- A rejected response remains available for correction without rolling back successful siblings. Continue the exchange loop until the continuous task is complete, then use agent_close() with no ID.`
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

	codingServer := mcp.NewServer(
		&mcp.Implementation{Name: "cwapi-coding", Version: v2config.Version},
		&mcp.ServerOptions{Instructions: codingInstructions},
	)
	agentServer := mcp.NewServer(
		&mcp.Implementation{Name: "cwapi-agent", Version: v2config.Version},
		&mcp.ServerOptions{Instructions: agentInstructions},
	)
	codingEnabled := coding != nil
	agentEnabled := agent != nil
	if codingEnabled {
		if err := coding(codingServer); err != nil {
			return nil, fmt.Errorf("MCP_CODING_REGISTER_FAILED: %w", err)
		}
	}
	if agentEnabled {
		if err := agent(agentServer); err != nil {
			return nil, fmt.Errorf("MCP_AGENT_REGISTER_FAILED: %w", err)
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
