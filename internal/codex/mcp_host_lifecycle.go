package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

type mcpHostClient interface {
	internalRequester
	RequestMCP(context.Context, string, map[string]any) (any, error)
	Alive() bool
	releaseProcessTree() error
	Close()
}

type mcpClientGeneration struct {
	client        mcpHostClient
	permissionKey string
	threads       map[string]string
	active        int
	idle          chan struct{}
	retired       bool
	closeOnce     sync.Once
}

func (g *mcpClientGeneration) close() {
	if g == nil {
		return
	}
	g.closeOnce.Do(func() { closeOwnedClient(g.client) })
}

type mcpClientLease struct {
	host        *MCPHost
	generation  *mcpClientGeneration
	threadID    string
	releaseOnce sync.Once
}

func (l *mcpClientLease) abort() {
	if l == nil || l.host == nil || l.generation == nil {
		return
	}
	l.host.mu.Lock()
	l.generation.retired = true
	if l.host.generation == l.generation {
		l.host.generation = nil
	}
	l.host.mu.Unlock()
	l.generation.close()
}

func (l *mcpClientLease) release() {
	if l == nil {
		return
	}
	l.releaseOnce.Do(func() {
		l.host.mu.Lock()
		generation := l.generation
		if !generation.client.Alive() {
			generation.retired = true
		}
		if generation.active > 0 {
			generation.active--
			if generation.active == 0 && generation.idle != nil {
				close(generation.idle)
				generation.idle = nil
			}
		}
		closeGeneration := generation.active == 0 && generation.retired && l.host.generation == generation
		if closeGeneration {
			l.host.generation = nil
		}
		l.host.mu.Unlock()
		if closeGeneration {
			generation.close()
		}
	})
}

func unsubscribeMCPContextThread(ctx context.Context, client internalRequester, threadID string) error {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return errors.New("CODEX_MCP_THREAD_UNSUBSCRIBE_ID_REQUIRED")
	}
	value, err := client.RequestInternal(ctx, "thread/unsubscribe", map[string]any{"threadId": threadID})
	if err != nil {
		return fmt.Errorf("CODEX_MCP_THREAD_UNSUBSCRIBE_FAILED: %w", err)
	}
	response, ok := value.(map[string]any)
	if !ok {
		return errors.New("CODEX_MCP_THREAD_UNSUBSCRIBE_RESPONSE_INVALID")
	}
	status, _ := response["status"].(string)
	switch strings.TrimSpace(status) {
	case "notLoaded", "notSubscribed", "unsubscribed":
		return nil
	default:
		return errors.New("CODEX_MCP_THREAD_UNSUBSCRIBE_STATUS_INVALID")
	}
}

func closeOwnedClient(client mcpHostClient) {
	if client == nil {
		return
	}
	_ = client.releaseProcessTree()
	client.Close()
}
