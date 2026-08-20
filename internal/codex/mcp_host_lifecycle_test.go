package codex

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeMCPHostClient struct {
	mu            sync.Mutex
	alive         bool
	threadCount   int
	internalCalls []string
	mcpThreadIDs  []string
	waitForCancel bool
	closeCalls    int
	releaseCalls  int
}

func newFakeMCPHostClient() *fakeMCPHostClient {
	return &fakeMCPHostClient{alive: true}
}

func (f *fakeMCPHostClient) RequestInternal(_ context.Context, method string, params map[string]any) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.internalCalls = append(f.internalCalls, method)
	switch method {
	case "thread/start":
		f.threadCount++
		return map[string]any{"thread": map[string]any{"id": "thread-" + strconv.Itoa(f.threadCount)}}, nil
	case "thread/unsubscribe":
		if params["threadId"] == "" {
			return nil, errors.New("missing threadId")
		}
		return map[string]any{"status": "unsubscribed"}, nil
	default:
		return nil, errors.New("unexpected internal method")
	}
}

func (f *fakeMCPHostClient) RequestMCP(ctx context.Context, _ string, params map[string]any) (any, error) {
	f.mu.Lock()
	threadID, _ := params["threadId"].(string)
	f.mcpThreadIDs = append(f.mcpThreadIDs, threadID)
	waitForCancel := f.waitForCancel
	f.mu.Unlock()
	if waitForCancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return map[string]any{"ok": true}, nil
}

func (f *fakeMCPHostClient) Alive() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive
}

func (f *fakeMCPHostClient) releaseProcessTree() error {
	f.mu.Lock()
	f.releaseCalls++
	f.mu.Unlock()
	return nil
}

func (f *fakeMCPHostClient) Close() {
	f.mu.Lock()
	f.alive = false
	f.closeCalls++
	f.mu.Unlock()
}

func seededMCPHost(t *testing.T, provider func() PermissionConfig, client *fakeMCPHostClient) *MCPHost {
	t.Helper()
	service := &Service{dataRoot: t.TempDir()}
	host := NewMCPHost(service, provider)
	permissionKey, err := provider().key(service.dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	host.generation = &mcpClientGeneration{client: client, permissionKey: permissionKey}
	return host
}

func TestMCPHostReusesExactWorktreeThreadAcrossCalls(t *testing.T) {
	permission := PermissionConfig{ProfileID: PermissionProfileSafe}
	client := newFakeMCPHostClient()
	host := seededMCPHost(t, func() PermissionConfig { return permission }, client)
	defer host.Close()

	worktree := filepath.Join(t.TempDir(), "worktree")
	for range 2 {
		_, err := host.CallMCP(context.Background(), MCPCall{
			Method: mcpServerStatusListMethod, Timeout: time.Second, CWD: worktree,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.internalCalls) != 1 || client.internalCalls[0] != "thread/start" {
		t.Fatalf("unexpected thread lifecycle: %#v", client.internalCalls)
	}
	if len(client.mcpThreadIDs) != 2 || client.mcpThreadIDs[0] != "thread-1" || client.mcpThreadIDs[1] != "thread-1" {
		t.Fatalf("unexpected MCP thread IDs: %#v", client.mcpThreadIDs)
	}
}

func TestMCPHostKeepsExactWorktreeThreadsIsolated(t *testing.T) {
	permission := PermissionConfig{ProfileID: PermissionProfileSafe}
	client := newFakeMCPHostClient()
	host := seededMCPHost(t, func() PermissionConfig { return permission }, client)
	defer host.Close()
	root := t.TempDir()
	for _, cwd := range []string{filepath.Join(root, "one"), filepath.Join(root, "two"), filepath.Join(root, "one")} {
		if _, err := host.CallMCP(context.Background(), MCPCall{
			Method: mcpServerStatusListMethod, Timeout: time.Second, CWD: cwd,
		}); err != nil {
			t.Fatal(err)
		}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if got := client.mcpThreadIDs; len(got) != 3 || got[0] != "thread-1" || got[1] != "thread-2" || got[2] != "thread-1" {
		t.Fatalf("worktree thread IDs = %#v", got)
	}
}

func TestMCPHostReusesDefaultContextThread(t *testing.T) {
	permission := PermissionConfig{ProfileID: PermissionProfileSafe}
	client := newFakeMCPHostClient()
	host := seededMCPHost(t, func() PermissionConfig { return permission }, client)
	defer host.Close()

	for range 2 {
		if _, err := host.CallMCP(context.Background(), MCPCall{Method: mcpServerStatusListMethod, Timeout: time.Second}); err != nil {
			t.Fatal(err)
		}
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.internalCalls) != 1 || client.internalCalls[0] != "thread/start" {
		t.Fatalf("default context was not reused: %#v", client.internalCalls)
	}
	if len(client.mcpThreadIDs) != 2 || client.mcpThreadIDs[0] != client.mcpThreadIDs[1] {
		t.Fatalf("default MCP thread changed: %#v", client.mcpThreadIDs)
	}
}

func TestMCPHostTimeoutStopsOwnedClientBeforeReturning(t *testing.T) {
	permission := PermissionConfig{ProfileID: PermissionProfileSafe}
	client := newFakeMCPHostClient()
	client.waitForCancel = true
	host := seededMCPHost(t, func() PermissionConfig { return permission }, client)

	_, err := host.CallMCP(context.Background(), MCPCall{
		Method: mcpToolCallMethod, Timeout: 20 * time.Millisecond, CWD: filepath.Join(t.TempDir(), "worktree"),
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v", err)
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closeCalls != 1 || client.releaseCalls != 1 || client.alive {
		t.Fatalf("owned client was not stopped: close=%d release=%d alive=%v", client.closeCalls, client.releaseCalls, client.alive)
	}
	if len(client.internalCalls) != 1 || client.internalCalls[0] != "thread/start" {
		t.Fatalf("timed-out thread must not issue cleanup over a stopped client: %#v", client.internalCalls)
	}
}

func TestPermissionChangeWaitsForActiveMCPLease(t *testing.T) {
	var permissionMu sync.RWMutex
	permission := PermissionConfig{ProfileID: PermissionProfileSafe}
	provider := func() PermissionConfig {
		permissionMu.RLock()
		defer permissionMu.RUnlock()
		return permission
	}
	client := newFakeMCPHostClient()
	host := seededMCPHost(t, provider, client)
	lease, err := host.clientAndThread(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	permissionMu.Lock()
	permission = PermissionConfig{ProfileID: PermissionProfileFullAccess}
	permissionMu.Unlock()

	waitCtx, cancel := context.WithCancel(context.Background())
	waitResult := make(chan error, 1)
	go func() {
		_, acquireErr := host.clientAndThread(waitCtx, "")
		waitResult <- acquireErr
	}()
	deadline := time.Now().Add(time.Second)
	for {
		host.mu.Lock()
		retired := host.generation != nil && host.generation.retired
		host.mu.Unlock()
		if retired {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("permission change did not wait on the active generation")
		}
		time.Sleep(time.Millisecond)
	}
	client.mu.Lock()
	closedWhileActive := client.closeCalls
	client.mu.Unlock()
	if closedWhileActive != 0 {
		t.Fatal("permission change closed an active MCP client")
	}
	cancel()
	if err := <-waitResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting acquire error = %v", err)
	}
	lease.release()
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closeCalls != 1 {
		t.Fatalf("retired client close calls = %d", client.closeCalls)
	}
}
