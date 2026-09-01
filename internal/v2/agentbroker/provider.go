package agentbroker

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/v2/agentprotocol"
	v2config "github.com/AAAYNMMM/CWapi/internal/v2/config"
)

const (
	maxRequestBody              = DefaultMaxBatchBytes
	defaultSSEHeartbeatInterval = 15 * time.Second
	DefaultModel                = agentprotocol.DefaultModel
)

type ProviderSnapshot struct {
	State     string `json:"state"`
	Address   string `json:"address,omitempty"`
	LastError string `json:"last_error,omitempty"`
}

type Provider struct {
	mu        sync.RWMutex
	cfg       v2config.AgentConfig
	broker    *Broker
	adapter   agentprotocol.Adapter
	optimizer agentprotocol.ContextOptimizer
	server    *http.Server
	listen    net.Listener
	heartbeat time.Duration
	state     ProviderSnapshot
}

func NewProvider(cfg v2config.AgentConfig, broker *Broker) (*Provider, error) {
	return NewProviderWithAdapter(cfg, broker, agentprotocol.NewOpenAICompatibleAdapter())
}

func NewProviderWithAdapter(cfg v2config.AgentConfig, broker *Broker, adapter agentprotocol.Adapter) (*Provider, error) {
	if broker == nil {
		return nil, errors.New("AGENT_BROKER_REQUIRED")
	}
	if adapter == nil {
		return nil, errors.New("AGENT_PROTOCOL_ADAPTER_REQUIRED")
	}
	if err := v2config.ValidateAgent(cfg, 0); err != nil {
		return nil, err
	}
	return &Provider{
		cfg: cfg, broker: broker, adapter: adapter, optimizer: agentprotocol.NewContextOptimizer(),
		heartbeat: defaultSSEHeartbeatInterval, state: ProviderSnapshot{State: "stopped"},
	}, nil
}

func (p *Provider) Start(ctx context.Context) error {
	if p == nil {
		return errors.New("AGENT_PROVIDER_UNAVAILABLE")
	}
	p.mu.Lock()
	if p.listen != nil {
		p.mu.Unlock()
		return nil
	}
	listener, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(p.cfg.Port)))
	if err != nil {
		p.state = ProviderSnapshot{State: "failed", LastError: "AGENT_PROVIDER_LISTEN_FAILED"}
		p.mu.Unlock()
		return fmt.Errorf("AGENT_PROVIDER_LISTEN_FAILED: %w", err)
	}
	server := &http.Server{Handler: p.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 2 * time.Minute}
	p.listen, p.server = listener, server
	p.state = ProviderSnapshot{State: "running", Address: listener.Addr().String()}
	p.mu.Unlock()
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.mu.Lock()
			p.state.State = "failed"
			p.state.LastError = "AGENT_PROVIDER_SERVE_FAILED"
			p.mu.Unlock()
		}
	}()
	if ctx != nil && ctx.Done() != nil {
		go func() {
			<-ctx.Done()
			closeCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = p.Close(closeCtx)
		}()
	}
	return nil
}

func (p *Provider) Close(ctx context.Context) error {
	if p == nil {
		return nil
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
	}
	p.mu.Lock()
	server := p.server
	p.server = nil
	p.listen = nil
	p.mu.Unlock()
	p.broker.Shutdown()
	if server == nil {
		p.mu.Lock()
		p.state = ProviderSnapshot{State: "stopped"}
		p.mu.Unlock()
		return nil
	}
	err := server.Shutdown(ctx)
	p.mu.Lock()
	p.state = ProviderSnapshot{State: "stopped"}
	if err != nil {
		p.state.LastError = "AGENT_PROVIDER_SHUTDOWN_FAILED"
	}
	p.mu.Unlock()
	if err != nil {
		return fmt.Errorf("AGENT_PROVIDER_SHUTDOWN_FAILED: %w", err)
	}
	return nil
}

func (p *Provider) Snapshot() ProviderSnapshot {
	if p == nil {
		return ProviderSnapshot{State: "unavailable"}
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}
func (p *Provider) Endpoint() string {
	if p == nil {
		return ""
	}
	return "http://127.0.0.1:" + strconv.Itoa(p.cfg.Port) + "/v1"
}

func (p *Provider) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/models", p.authorized(p.handleModels))
	mux.HandleFunc("POST /v1/chat/completions", p.authorized(p.handleChatCompletions))
	return mux
}

func (p *Provider) authorized(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) {
			writeError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		value := strings.TrimSpace(strings.TrimPrefix(header, prefix))
		if len(value) != len(p.cfg.APIKey) || subtle.ConstantTimeCompare([]byte(value), []byte(p.cfg.APIKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "invalid_api_key")
			return
		}
		next(w, r)
	}
}

func (p *Provider) handleModels(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": []any{map[string]any{
		"id": DefaultModel, "object": "model", "owned_by": "cwapi", "protocol": p.adapter.Name(), "capabilities": p.adapter.Capabilities(),
	}}})
}

func (p *Provider) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "request_read_failed")
		return
	}
	if len(body) > maxRequestBody {
		writeError(w, http.StatusRequestEntityTooLarge, "request_too_large")
		return
	}
	conversation, err := p.adapter.DecodeRequest(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCode(err))
		return
	}
	conversation, _, err = p.optimizer.Optimize(conversation)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCode(err))
		return
	}
	handle, err := p.broker.Enqueue(conversation)
	if err != nil {
		switch errorCode(err) {
		case "AGENT_BRIDGE_UNAVAILABLE":
			writeError(w, http.StatusServiceUnavailable, "AGENT_BRIDGE_UNAVAILABLE")
		case "AGENT_BUSY":
			writeError(w, http.StatusTooManyRequests, "AGENT_BUSY")
		default:
			writeError(w, http.StatusServiceUnavailable, "AGENT_UNAVAILABLE")
		}
		return
	}
	if conversation.Stream {
		p.handleStreamingCompletion(w, r, handle)
		return
	}
	result, err := handle.Wait(r.Context())
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		code := errorCode(err)
		status := http.StatusBadGateway
		switch code {
		case "AGENT_REQUEST_TIMEOUT":
			status = http.StatusGatewayTimeout
		case "AGENT_BRIDGE_CLOSED", "AGENT_BROKER_CLOSED", "AGENT_CLIENT_DISCONNECTED":
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, code)
		return
	}
	completion, err := p.adapter.EncodeCompletion(result.Completion, completionMetadata(result))
	if err != nil {
		writeError(w, http.StatusBadGateway, errorCode(err))
		return
	}
	writeJSON(w, http.StatusOK, completion)
}

type requestWaitResult struct {
	result RequestResult
	err    error
}

func (p *Provider) handleStreamingCompletion(w http.ResponseWriter, r *http.Request, handle *RequestHandle) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, ": cwapi stream open\n\n")
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	waited := make(chan requestWaitResult, 1)
	go func() { result, err := handle.Wait(r.Context()); waited <- requestWaitResult{result: result, err: err} }()
	interval := p.heartbeat
	if interval <= 0 {
		interval = defaultSSEHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = io.WriteString(w, ": cwapi keepalive\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		case waitedResult := <-waited:
			if waitedResult.err != nil {
				code := errorCode(waitedResult.err)
				payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": code, "type": "cwapi_error", "code": code}})
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\ndata: [DONE]\n\n", payload)
				if flusher != nil {
					flusher.Flush()
				}
				return
			}
			if err := p.writeSSECompletion(w, waitedResult.result); err != nil {
				code := errorCode(err)
				payload, _ := json.Marshal(map[string]any{"error": map[string]any{"message": code, "type": "cwapi_error", "code": code}})
				_, _ = fmt.Fprintf(w, "event: error\ndata: %s\n\ndata: [DONE]\n\n", payload)
			}
			if flusher != nil {
				flusher.Flush()
			}
			return
		}
	}
}

func (p *Provider) writeSSECompletion(w io.Writer, result RequestResult) error {
	metadata := completionMetadata(result)
	for _, chunk := range agentprotocol.BufferedCompletionChunks(result.Completion) {
		if chunk.Done {
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			continue
		}
		encoded, err := p.adapter.EncodeStreamChunk(chunk, metadata)
		if err != nil {
			return err
		}
		payload, err := json.Marshal(encoded)
		if err != nil {
			return errors.New("AGENT_STREAM_CONVERSION_FAILED")
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	}
	return nil
}

func completionMetadata(result RequestResult) agentprotocol.CompletionMetadata {
	return agentprotocol.CompletionMetadata{ID: result.ID, Model: result.Model, Created: result.Created}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": code, "type": "cwapi_error", "code": code}})
}
func errorCode(err error) string {
	if err == nil {
		return ""
	}
	text := err.Error()
	if index := strings.IndexByte(text, ':'); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}
