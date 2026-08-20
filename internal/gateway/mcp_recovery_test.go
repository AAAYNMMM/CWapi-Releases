package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

type recoveryConfigProvider struct{}

func (recoveryConfigProvider) Snapshot() config.Config { return config.Default() }

type recoveryPoster struct {
	calls    int
	failures int
}

func (p *recoveryPoster) Post(context.Context, string, string, string) (PostedMessage, error) {
	p.calls++
	if p.calls <= p.failures {
		return PostedMessage{}, errors.New("temporary Slack delivery failure")
	}
	return PostedMessage{MessageID: "posted", MessageTS: "2.000"}, nil
}

type recoveryToolhost struct {
	calls int
}

func (h *recoveryToolhost) CallMCP(context.Context, string, map[string]any, time.Duration, MCPExecutionContext) (any, error) {
	h.calls++
	return map[string]any{"servers": []any{}}, nil
}

func TestStoredTerminalResponseRedeliversForDuplicateSourceMessageWithinSession(t *testing.T) {
	store, err := state.Open(filepath.Join(t.TempDir(), "state", "cwapi.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	poster := &recoveryPoster{failures: 1}
	host := &recoveryToolhost{}
	gateway, err := NewMCP(recoveryConfigProvider{}, store, poster, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := gateway.AttachMCPRuntime(MCPRuntime{Toolhost: host}); err != nil {
		t.Fatal(err)
	}

	const requestID = "REQ-RECOVERY"
	request := protocol.MCPRequest{
		Schema:          protocol.MCPRequestSchema,
		ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID:       requestID,
		Method:          "mcpServerStatus/list",
		Params:          json.RawMessage(`{}`),
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := protocol.BuildMCPSubject(protocol.MCPFamilyRequest, requestID)
	if err != nil {
		t.Fatal(err)
	}
	message := slackcore.Message{
		MessageID: "slack:C12345678:1.000",
		ChannelID: "C12345678",
		MessageTS: "1.000",
		Subject:   subject,
		Body:      string(body),
	}

	if err := gateway.HandleMCPMessage(context.Background(), message); err == nil {
		t.Fatal("first delivery should fail after the MCP result is persisted")
	}
	stored, err := store.MCPRequestByID(context.Background(), requestID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.ResponseJSON == "" || stored.DeliveryState != state.MCPDeliveryAttention {
		t.Fatalf("terminal result was not retained for redelivery: %#v", stored)
	}

	if err := gateway.HandleMCPMessage(context.Background(), message); err != nil {
		t.Fatalf("duplicate source message did not redeliver: %v", err)
	}
	stored, err = store.MCPRequestByID(context.Background(), requestID)
	if err != nil {
		t.Fatal(err)
	}
	if poster.calls != 2 || host.calls != 1 || stored.DeliveryState != state.MCPDeliveryDelivered {
		t.Fatalf("same-session redelivery replayed execution or missed delivery: posts=%d calls=%d state=%s", poster.calls, host.calls, stored.DeliveryState)
	}
}
