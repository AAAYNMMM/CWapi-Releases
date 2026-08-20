package codex

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MCPHost owns one long-lived stock Codex app-server connection and ephemeral
// threads used only as MCP context. It never starts a Codex turn. Threads are
// reused by exact CWD while the permission generation is stable so stateful MCP
// servers can support multi-step workflows without crossing workspace bounds.
type MCPHost struct {
	service     *Service
	permissions func() PermissionConfig
	mu          sync.Mutex
	generation  *mcpClientGeneration
	closed      bool
}

func NewMCPHost(service *Service, providers ...func() PermissionConfig) *MCPHost {
	permissions := func() PermissionConfig { return PermissionConfig{ProfileID: PermissionProfileSafe} }
	if len(providers) > 0 && providers[0] != nil {
		permissions = providers[0]
	}
	return &MCPHost{service: service, permissions: permissions}
}

func (h *MCPHost) Snapshot() RuntimeSnapshot {
	if h == nil || h.service == nil {
		return RuntimeSnapshot{}
	}
	return h.service.Snapshot()
}

func (h *MCPHost) CallMCP(ctx context.Context, call MCPCall) (any, error) {
	if h == nil || h.service == nil {
		return nil, errors.New("CODEX_MCP_HOST_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if call.Timeout <= 0 {
		return nil, errors.New("CODEX_MCP_TIMEOUT_INVALID")
	}
	method := strings.TrimSpace(call.Method)
	switch method {
	case mcpServerStatusListMethod, mcpResourceReadMethod, mcpToolCallMethod:
	default:
		return nil, fmt.Errorf("CODEX_MCP_METHOD_NOT_ALLOWED: %s", method)
	}
	params := cloneMCPParams(call.Params)
	if _, supplied := params["threadId"]; supplied {
		return nil, errors.New("CODEX_MCP_THREAD_ID_MANAGED_BY_CWAPI")
	}

	callCtx, cancel := context.WithTimeout(ctx, call.Timeout)
	defer cancel()
	lease, err := h.clientAndThread(callCtx, call.CWD)
	if err != nil {
		return nil, err
	}
	defer lease.release()
	params["threadId"] = lease.threadID
	value, err := lease.generation.client.RequestMCP(callCtx, method, params)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !lease.generation.client.Alive() {
			lease.abort()
		}
		// Never replay an MCP tool after an ambiguous transport/process failure.
		return nil, err
	}
	return value, nil
}

func (h *MCPHost) clientAndThread(ctx context.Context, requestedCWD string) (*mcpClientLease, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	permission := h.permissions()
	canonicalPermission, err := permission.canonical(h.service.dataRoot)
	if err != nil {
		return nil, err
	}
	permissionKey, err := canonicalPermission.key(h.service.dataRoot)
	if err != nil {
		return nil, err
	}
	targetCWD, err := h.resolveThreadCWD(canonicalPermission, requestedCWD)
	if err != nil {
		return nil, err
	}
	threadKey := strings.ToLower(targetCWD)

	for {
		h.mu.Lock()
		if h.closed {
			h.mu.Unlock()
			return nil, errors.New("CODEX_MCP_HOST_CLOSED")
		}
		generation := h.generation
		if generation != nil && (generation.retired || !generation.client.Alive() || generation.permissionKey != permissionKey) {
			if generation.active > 0 {
				generation.retired = true
				idle := generation.idle
				h.mu.Unlock()
				select {
				case <-idle:
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			h.generation = nil
			h.mu.Unlock()
			generation.close()
			continue
		}
		if generation == nil {
			generation, err = h.startClient(ctx, canonicalPermission, permissionKey)
			if err != nil {
				h.mu.Unlock()
				return nil, err
			}
			h.generation = generation
		}

		threadID := generation.threads[threadKey]
		if threadID == "" {
			startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			threadID, err = startMCPContextThread(startCtx, generation.client, canonicalPermission.ProfileID, targetCWD)
			cancel()
			if err != nil {
				abortGeneration := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || !generation.client.Alive()
				if abortGeneration {
					generation.retired = true
					if h.generation == generation {
						h.generation = nil
					}
				}
				h.mu.Unlock()
				if abortGeneration {
					generation.close()
				}
				return nil, err
			}
			if generation.threads == nil {
				generation.threads = make(map[string]string)
			}
			generation.threads[threadKey] = threadID
		}
		if generation.active == 0 {
			generation.idle = make(chan struct{})
		}
		generation.active++
		h.mu.Unlock()
		return &mcpClientLease{host: h, generation: generation, threadID: threadID}, nil
	}
}

func (h *MCPHost) startClient(ctx context.Context, permission PermissionConfig, permissionKey string) (*mcpClientGeneration, error) {
	actualHash, err := hashFile(h.service.codexExe)
	if err != nil {
		return nil, fmt.Errorf("CODEX_RUNTIME_UNAVAILABLE: %w", err)
	}
	if !strings.EqualFold(actualHash, PinnedExecutableSHA256) {
		return nil, fmt.Errorf(
			"CODEX_EXECUTABLE_SHA256_MISMATCH: expected=%s actual=%s",
			PinnedExecutableSHA256,
			actualHash,
		)
	}
	if err := h.service.ensureHome(permission); err != nil {
		return nil, err
	}

	client := NewClient(h.service.codexExe, h.service.home, h.service.stderrLog, appServerEnvironment(), 30*time.Second, nil)
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := client.Start(startCtx); err != nil {
		client.Close()
		return nil, err
	}
	if err := client.ownProcessTree(); err != nil {
		client.Close()
		return nil, fmt.Errorf("CODEX_MCP_HOST_PROCESS_SCOPE_FAILED: %w", err)
	}
	return &mcpClientGeneration{client: client, permissionKey: permissionKey}, nil
}

func (h *MCPHost) resolveThreadCWD(permission PermissionConfig, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return permission.threadCWD(h.service.dataRoot), nil
	}
	if !filepath.IsAbs(requested) {
		return "", errors.New("CODEX_MCP_CWD_NOT_ABSOLUTE")
	}
	return filepath.Clean(requested), nil
}

type internalRequester interface {
	RequestInternal(context.Context, string, map[string]any) (any, error)
}

func startMCPContextThread(ctx context.Context, client internalRequester, profileID, cwd string) (string, error) {
	profileID = strings.TrimSpace(profileID)
	cwd = strings.TrimSpace(cwd)
	if profileID == "" || cwd == "" {
		return "", errors.New("CODEX_MCP_THREAD_PERMISSION_CONTEXT_INVALID")
	}
	value, err := client.RequestInternal(ctx, "thread/start", map[string]any{
		"ephemeral":   true,
		"cwd":         cwd,
		"permissions": profileID,
	})
	if err != nil {
		return "", fmt.Errorf("CODEX_MCP_THREAD_START_FAILED: %w", err)
	}
	response, ok := value.(map[string]any)
	if !ok {
		return "", errors.New("CODEX_MCP_THREAD_START_RESPONSE_INVALID")
	}
	thread, ok := response["thread"].(map[string]any)
	if !ok {
		return "", errors.New("CODEX_MCP_THREAD_START_THREAD_MISSING")
	}
	threadID, _ := thread["id"].(string)
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return "", errors.New("CODEX_MCP_THREAD_START_ID_MISSING")
	}
	return threadID, nil
}

func cloneMCPParams(params map[string]any) map[string]any {
	result := make(map[string]any, len(params)+1)
	for key, value := range params {
		result[key] = value
	}
	return result
}

func (h *MCPHost) Close() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.closed = true
	generation := h.generation
	h.generation = nil
	if generation != nil {
		generation.retired = true
	}
	h.mu.Unlock()
	if generation != nil {
		generation.close()
	}
}
