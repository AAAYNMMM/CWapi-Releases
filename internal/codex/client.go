package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	mcpServerStatusListMethod = "mcpServerStatus/list"
	mcpResourceReadMethod     = "mcpServer/resource/read"
	mcpToolCallMethod         = "mcpServer/tool/call"
)

type notificationHandler func(map[string]any)

type rpcResponse struct {
	result any
	err    error
}

type Client struct {
	executable     string
	home           string
	stderrLog      string
	environment    []string
	startupTimeout time.Duration
	onNotification notificationHandler

	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser

	writeMu    sync.Mutex
	pendingMu  sync.Mutex
	pending    map[int64]chan rpcResponse
	nextID     atomic.Int64
	closed     atomic.Bool
	stderrMu   sync.Mutex
	stderrTail strings.Builder
	done       chan struct{}
}

func NewClient(executable, home, stderrLog string, environment []string, startupTimeout time.Duration, onNotification notificationHandler) *Client {
	cleanLog := ""
	if strings.TrimSpace(stderrLog) != "" {
		cleanLog = filepath.Clean(stderrLog)
	}
	return &Client{
		executable: filepath.Clean(executable), home: filepath.Clean(home), stderrLog: cleanLog,
		environment: append([]string(nil), environment...), startupTimeout: startupTimeout,
		onNotification: onNotification, pending: map[int64]chan rpcResponse{}, done: make(chan struct{}),
	}
}

func (c *Client) Start(ctx context.Context) error {
	if c.cmd != nil {
		return nil
	}
	if c.closed.Load() {
		return errors.New("CODEX_CLIENT_CLOSED")
	}
	if !filepath.IsAbs(c.executable) {
		return errors.New("CODEX_EXECUTABLE_NOT_ABSOLUTE")
	}
	if info, err := os.Stat(c.executable); err != nil || !info.Mode().IsRegular() {
		if err != nil {
			return fmt.Errorf("CODEX_EXECUTABLE_UNAVAILABLE: %w", err)
		}
		return errors.New("CODEX_EXECUTABLE_NOT_FILE")
	}
	if err := os.MkdirAll(c.home, 0o700); err != nil {
		return fmt.Errorf("CODEX_HOME_CREATE_FAILED: %w", err)
	}
	if c.stderrLog != "" {
		if err := os.MkdirAll(filepath.Dir(c.stderrLog), 0o700); err != nil {
			return fmt.Errorf("CODEX_LOG_DIR_CREATE_FAILED: %w", err)
		}
	}

	cmd := exec.Command(c.executable, "app-server", "--stdio")
	cmd.Dir = filepath.Dir(c.executable)
	cmd.Env = c.launchEnvironment()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("CODEX_STDIN_PIPE_FAILED: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("CODEX_STDOUT_PIPE_FAILED: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("CODEX_STDERR_PIPE_FAILED: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("CODEX_START_FAILED: %w", err)
	}
	c.cmd, c.stdin, c.stdout, c.stderr = cmd, stdin, stdout, stderr
	go c.readStdout()
	go c.readStderr()
	go func() {
		_ = cmd.Wait()
		c.failPending(errors.New("CODEX_APP_SERVER_EXITED"))
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}()

	initCtx := ctx
	if initCtx == nil {
		initCtx = context.Background()
	}
	var cancel context.CancelFunc
	if c.startupTimeout > 0 {
		initCtx, cancel = context.WithTimeout(initCtx, c.startupTimeout)
		defer cancel()
	}
	result, err := c.request(initCtx, "initialize", map[string]any{
		"clientInfo": map[string]any{"name": "cwapi", "title": "CWapi Codex MCP Relay", "version": "1.6.1"},
		"capabilities": map[string]any{
			"experimentalApi":           true,
			"optOutNotificationMethods": []string{"item/agentMessage/delta", "turn/started", "turn/completed"},
		},
	}, false)
	if err != nil {
		c.Close()
		return err
	}
	if object, ok := result.(map[string]any); ok {
		if reported, ok := object["codexHome"].(string); ok && strings.TrimSpace(reported) != "" {
			expected, _ := filepath.Abs(c.home)
			actual, _ := filepath.Abs(reported)
			if !strings.EqualFold(filepath.Clean(expected), filepath.Clean(actual)) {
				c.Close()
				return fmt.Errorf("CODEX_HOME_MISMATCH: expected=%s actual=%s", expected, actual)
			}
		}
	}
	return c.notify("initialized", nil)
}

func (c *Client) RequestMCP(ctx context.Context, method string, params map[string]any) (any, error) {
	switch strings.TrimSpace(method) {
	case mcpServerStatusListMethod, mcpResourceReadMethod, mcpToolCallMethod:
	default:
		return nil, fmt.Errorf("CODEX_MCP_METHOD_NOT_ALLOWED: %s", method)
	}
	return c.request(ctx, method, params, true)
}

func (c *Client) RequestInternal(ctx context.Context, method string, params map[string]any) (any, error) {
	switch strings.TrimSpace(method) {
	case "thread/start", "thread/unsubscribe":
	default:
		return nil, fmt.Errorf("CODEX_INTERNAL_METHOD_NOT_ALLOWED: %s", method)
	}
	return c.request(ctx, method, params, true)
}

func (c *Client) request(ctx context.Context, method string, params map[string]any, requireStarted bool) (any, error) {
	if requireStarted && c.cmd == nil {
		if err := c.Start(ctx); err != nil {
			return nil, err
		}
	}
	id := c.nextID.Add(1)
	response := make(chan rpcResponse, 1)
	c.pendingMu.Lock()
	c.pending[id] = response
	c.pendingMu.Unlock()
	payload := map[string]any{"method": method, "id": id}
	if params != nil {
		payload["params"] = params
	}
	if err := c.send(payload); err != nil {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case value := <-response:
		return value.result, value.err
	case <-ctx.Done():
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
		return nil, ctx.Err()
	case <-c.done:
		return nil, errors.New("CODEX_APP_SERVER_EXITED")
	}
}

func (c *Client) notify(method string, params map[string]any) error {
	payload := map[string]any{"method": method}
	if params != nil {
		payload["params"] = params
	}
	return c.send(payload)
}

func (c *Client) send(payload map[string]any) error {
	if c.stdin == nil {
		return errors.New("CODEX_APP_SERVER_NOT_RUNNING")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("CODEX_PIPE_WRITE_FAILED: %w", err)
	}
	return nil
}

func (c *Client) launchEnvironment() []string {
	values := map[string]string{}
	for _, entry := range c.environment {
		key, value, ok := strings.Cut(entry, "=")
		if ok && strings.TrimSpace(key) != "" {
			values[key] = value
		}
	}
	for _, key := range []string{"OPENAI_API_KEY", "OPENAI_ORG_ID", "OPENAI_PROJECT_ID", "CODEX_API_KEY", "AZURE_OPENAI_API_KEY"} {
		for existing := range values {
			if strings.EqualFold(existing, key) {
				delete(values, existing)
			}
		}
	}
	releaseRoot := filepath.Dir(filepath.Dir(c.executable))
	privatePaths := []string{filepath.Dir(c.executable), filepath.Join(releaseRoot, "codex-resources"), filepath.Join(releaseRoot, "codex-path")}
	inherited := ""
	for key, value := range values {
		if strings.EqualFold(key, "PATH") {
			inherited = value
			delete(values, key)
			break
		}
	}
	parts := make([]string, 0, 4)
	for _, path := range privatePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			parts = append(parts, path)
		}
	}
	if inherited != "" {
		parts = append(parts, inherited)
	}
	values["PATH"] = strings.Join(parts, string(os.PathListSeparator))
	values["CODEX_HOME"] = c.home
	values["RUST_LOG"] = "warn"
	values["LOG_FORMAT"] = "json"
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func (c *Client) readStdout() {
	scanner := bufio.NewScanner(c.stdout)
	buffer := make([]byte, 64*1024)
	scanner.Buffer(buffer, 8*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var message map[string]any
		if err := json.Unmarshal([]byte(line), &message); err != nil {
			c.failPending(fmt.Errorf("CODEX_PROTOCOL_JSON_INVALID: %w", err))
			continue
		}
		c.dispatch(message)
	}
	if err := scanner.Err(); err != nil {
		c.failPending(fmt.Errorf("CODEX_STDOUT_READ_FAILED: %w", err))
	}
}

func (c *Client) dispatch(message map[string]any) {
	_, hasMethod := message["method"]
	idValue, hasID := message["id"]
	if hasID && !hasMethod {
		id, ok := numberToInt64(idValue)
		if !ok {
			return
		}
		c.pendingMu.Lock()
		response := c.pending[id]
		delete(c.pending, id)
		c.pendingMu.Unlock()
		if response == nil {
			return
		}
		if rpcErr, exists := message["error"]; exists {
			response <- rpcResponse{err: fmt.Errorf("CODEX_RPC_FAILED: %v", rpcErr)}
		} else {
			response <- rpcResponse{result: message["result"]}
		}
		return
	}
	if hasID && hasMethod {
		_ = c.send(map[string]any{"id": idValue, "error": map[string]any{"code": -32601, "message": "CWapi MCP relay does not handle server requests."}})
		return
	}
	if c.onNotification != nil {
		c.onNotification(message)
	}
}

func (c *Client) readStderr() {
	var file *os.File
	if c.stderrLog != "" {
		file, _ = os.OpenFile(c.stderrLog, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	if file != nil {
		defer file.Close()
	}
	scanner := bufio.NewScanner(c.stderr)
	for scanner.Scan() {
		line := scanner.Text() + "\n"
		c.stderrMu.Lock()
		if c.stderrTail.Len() > 32*1024 {
			text := c.stderrTail.String()
			c.stderrTail.Reset()
			c.stderrTail.WriteString(text[len(text)/2:])
		}
		c.stderrTail.WriteString(line)
		c.stderrMu.Unlock()
		if file != nil {
			_, _ = file.WriteString(line)
		}
	}
}

func (c *Client) failPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = map[int64]chan rpcResponse{}
	c.pendingMu.Unlock()
	for _, response := range pending {
		select {
		case response <- rpcResponse{err: err}:
		default:
		}
	}
}

func (c *Client) StderrTail() string {
	c.stderrMu.Lock()
	defer c.stderrMu.Unlock()
	value := c.stderrTail.String()
	if len(value) > 8000 {
		value = value[len(value)-8000:]
	}
	return value
}

func (c *Client) Close() {
	if c.closed.Swap(true) {
		return
	}
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.releaseProcessTree()
		_ = c.cmd.Process.Kill()
	}
	c.failPending(errors.New("CODEX_CLIENT_CLOSED"))
}
