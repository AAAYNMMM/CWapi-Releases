package slack

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type countingHTTPDoer struct {
	calls atomic.Int32
}

func (d *countingHTTPDoer) Do(*http.Request) (*http.Response, error) {
	d.calls.Add(1)
	return nil, errors.New("unexpected Slack Web API call from Socket connection")
}

func TestSocketConnectionDoesNotReplayHistory(t *testing.T) {
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close()
		_ = connection.WriteJSON(map[string]any{"type": "hello"})
		for {
			if _, _, err := connection.ReadMessage(); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	doer := &countingHTTPDoer{}
	client, err := NewClient("xapp-test", "xoxb-test", "C12345678", doer)
	if err != nil {
		t.Fatal(err)
	}
	socket, err := NewSocket(client, Profile{UserID: "U-CWAPI", BotID: "B-CWAPI"}, NewIndex(10))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	websocketURL := "ws" + strings.TrimPrefix(server.URL, "http")
	connected, err := socket.runConnection(ctx, websocketURL, func(Message) {}, nil)
	if err != nil {
		t.Fatalf("runConnection returned error: %v", err)
	}
	if !connected {
		t.Fatal("expected real WebSocket connection")
	}
	if calls := doer.calls.Load(); calls != 0 {
		t.Fatalf("Socket made %d Slack Web API calls; durable history recovery belongs to app/runtime", calls)
	}
}

func TestSocketRunInvokesAppRecoveryBeforeEveryReconnect(t *testing.T) {
	fixture := newSlackFixture(t, false)
	client := fixture.client(t)
	socket, err := NewSocket(client, Profile{UserID: "U-CWAPI", BotID: "B-CWAPI"}, NewIndex(10))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	var recoveries atomic.Int32
	var connections atomic.Int32
	err = socket.Run(ctx, func(Message) {}, func(state ConnectionState) {
		if state.State == "healthy" && connections.Add(1) == 2 {
			cancel()
		}
	}, func(context.Context) error {
		recoveries.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if recoveries.Load() < 2 || connections.Load() < 2 {
		t.Fatalf("reconnect skipped durable recovery: recoveries=%d connections=%d", recoveries.Load(), connections.Load())
	}
}
