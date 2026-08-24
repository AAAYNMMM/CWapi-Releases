package slack

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type slackFixture struct {
	server     *httptest.Server
	socketURL  string
	ackMu      sync.Mutex
	ackID      string
	postThread string
	postText   string
	postMrkdwn *bool
	deleteTS   string
}

func newSlackFixture(t *testing.T, sendAckEnvelope bool) *slackFixture {
	t.Helper()
	fixture := &slackFixture{}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth.test", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xoxb-test" {
			t.Errorf("auth header = %q", r.Header.Get("Authorization"))
		}
		writeJSON(w, map[string]any{"ok": true, "team": "CWapi Team", "team_id": "T123", "user": "cwapi", "user_id": "U-CWAPI", "bot_id": "B-CWAPI"})
	})
	mux.HandleFunc("/api/conversations.info", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("channel") != "C12345678" {
			t.Errorf("channel = %q", r.Form.Get("channel"))
		}
		writeJSON(w, map[string]any{"ok": true, "channel": map[string]any{"id": "C12345678", "name": "cwapi", "is_member": true}})
	})
	mux.HandleFunc("/api/apps.connections.open", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xapp-test" {
			t.Errorf("app auth header = %q", r.Header.Get("Authorization"))
		}
		writeJSON(w, map[string]any{"ok": true, "url": fixture.socketURL})
	})
	mux.HandleFunc("/api/conversations.history", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"ok": true, "messages": []map[string]any{
			{"ts": "5.000", "user": "U-OTHER", "text": "ordinary channel conversation"},
			{"ts": "4.000", "user": "U-OTHER", "text": "[CWapi/MCP/1][MCP_REQUEST][REQNOFRAME]\n{}"},
			{"ts": "3.000", "user": "U-OTHER", "text": framed("[CWapi/MCP/2][MCP_REQUEST][REQ3]", "third")},
			{"ts": "2.000", "user": "U-CWAPI", "text": framed("[CWapi/MCP/2][MCP_REQUEST][REQSELF]", "self")},
			{"ts": "1.000", "user": "U-OTHER", "text": framed("[CWapi/MCP/2][MCP_REQUEST][REQ1]", "first")},
		}})
	})
	mux.HandleFunc("/api/chat.postMessage", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode post: %v", err)
		}
		fixture.postThread, _ = payload["thread_ts"].(string)
		fixture.postText, _ = payload["text"].(string)
		if value, ok := payload["mrkdwn"].(bool); ok {
			fixture.postMrkdwn = &value
		}
		writeJSON(w, map[string]any{"ok": true, "channel": "C12345678", "ts": "9.000", "message": map[string]any{"ts": "9.000", "thread_ts": fixture.postThread, "bot_id": "B-CWAPI"}})
	})
	mux.HandleFunc("/api/chat.delete", func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode delete: %v", err)
		}
		fixture.deleteTS, _ = payload["ts"].(string)
		writeJSON(w, map[string]any{"ok": true, "channel": "C12345678", "ts": fixture.deleteTS})
	})
	mux.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer connection.Close()
		if err := connection.WriteJSON(map[string]any{"type": "hello"}); err != nil {
			return
		}
		if !sendAckEnvelope {
			return
		}
		_ = connection.WriteJSON(map[string]any{
			"type":        "events_api",
			"envelope_id": "E-ACK-FIRST",
			"payload":     "malformed-business-payload",
		})
		var ack socketAck
		_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := connection.ReadJSON(&ack); err == nil {
			fixture.ackMu.Lock()
			fixture.ackID = ack.EnvelopeID
			fixture.ackMu.Unlock()
		}
	})
	fixture.server = httptest.NewServer(mux)
	fixture.socketURL = "ws" + strings.TrimPrefix(fixture.server.URL, "http") + "/socket"
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *slackFixture) client(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient("xapp-test", "xoxb-test", "C12345678", f.server.Client())
	if err != nil {
		t.Fatal(err)
	}
	client.setBaseURLForTest(f.server.URL + "/api/")
	return client
}

func TestProtocolRoundTripAndNormalization(t *testing.T) {
	const wantSubject = "[CWapi/MCP/2][MCP_REQUEST][REQ1]"
	text, err := EncodeProtocol(wantSubject, "line1\r\nline2")
	if err != nil {
		t.Fatal(err)
	}
	subject, body, ok := DecodeProtocol(text)
	if !ok || subject != wantSubject || body != "line1\nline2" {
		t.Fatalf("decoded: subject=%q body=%q ok=%v", subject, body, ok)
	}
	if _, _, ok := DecodeProtocol("ordinary Slack text"); ok {
		t.Fatal("ordinary text must not become protocol traffic")
	}
	if _, _, ok := DecodeProtocol("+++\n[CWapi/MCP/2][BROKEN]\n{}\n+++"); ok {
		t.Fatal("malformed MCP subject must be routed to usage help")
	}
}

func TestReadinessChecksIdentityChannelAndRealWebSocket(t *testing.T) {
	fixture := newSlackFixture(t, false)
	readiness, err := ProbeCandidate(context.Background(), fixture.client(t))
	if err != nil {
		t.Fatal(err)
	}
	if !readiness.Ready || !readiness.SocketReady || readiness.UserID != "U-CWAPI" || readiness.ChannelID != "C12345678" {
		t.Fatalf("readiness = %#v", readiness)
	}
}

func TestHistoryIsBoundedChronologicalAndFiltersSelf(t *testing.T) {
	fixture := newSlackFixture(t, false)
	messages, err := fixture.client(t).History(context.Background(), 500, "U-CWAPI", "B-CWAPI")
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 3 || messages[0].MessageTS != "1.000" || messages[1].MessageTS != "3.000" ||
		messages[2].MessageTS != "4.000" || messages[2].ProtocolError != "invalid_format" {
		t.Fatalf("history = %#v", messages)
	}
}

func TestPostProtocolUsesRequestThread(t *testing.T) {
	fixture := newSlackFixture(t, false)
	message, err := fixture.client(t).PostProtocol(context.Background(), "[CWapi/MCP/2][MCP_RESPONSE][REQ1]", "done", "1.000")
	if err != nil {
		t.Fatal(err)
	}
	if fixture.postThread != "1.000" || fixture.postMrkdwn == nil || *fixture.postMrkdwn ||
		message.ThreadTS != "1.000" || message.MessageID != "slack:C12345678:9.000" {
		t.Fatalf("post result = %#v thread=%q", message, fixture.postThread)
	}
}

func TestPostTextSendsPlainUsageReplyInRequestThread(t *testing.T) {
	fixture := newSlackFixture(t, false)
	message, err := fixture.client(t).PostText(context.Background(), "CWapi v1.6.0 usage", "4.000")
	if err != nil {
		t.Fatal(err)
	}
	if fixture.postThread != "4.000" || fixture.postText != "CWapi v1.6.0 usage" || strings.HasPrefix(fixture.postText, "+++") || message.Subject != "" {
		t.Fatalf("message=%#v thread=%q text=%q", message, fixture.postThread, fixture.postText)
	}
}

func TestTemporaryPostCanBeDeleted(t *testing.T) {
	fixture := newSlackFixture(t, false)
	client := fixture.client(t)
	message, err := client.PostProtocol(context.Background(), "[CWapi/MCP/2][MCP_EVENT][REQSMOKE]", "temporary", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.DeleteMessage(context.Background(), message.MessageTS); err != nil {
		t.Fatal(err)
	}
	if fixture.deleteTS != "9.000" {
		t.Fatalf("deleted ts = %q", fixture.deleteTS)
	}
}

func TestEnvelopeAckPrecedesBusinessPayloadParsing(t *testing.T) {
	fixture := newSlackFixture(t, true)
	client := fixture.client(t)
	profile := Profile{UserID: "U-CWAPI", BotID: "B-CWAPI"}
	socket, err := NewSocket(client, profile, NewIndex(10))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url, err := client.OpenSocketURL(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = socket.runConnection(ctx, url, func(Message) {}, nil)
	fixture.ackMu.Lock()
	ackID := fixture.ackID
	fixture.ackMu.Unlock()
	if ackID != "E-ACK-FIRST" {
		t.Fatalf("ack id = %q", ackID)
	}
}

func TestEnvelopeFiltersChannelAndSelfAndDeduplicates(t *testing.T) {
	fixture := newSlackFixture(t, false)
	client := fixture.client(t)
	profile := Profile{UserID: "U-CWAPI", BotID: "B-CWAPI"}
	socket, _ := NewSocket(client, profile, NewIndex(2))

	makeEnvelope := func(channel, user, bot, ts, subject string) []byte {
		payload := map[string]any{"type": "event_callback", "event": map[string]any{
			"type": "message", "channel": channel, "user": user, "bot_id": bot, "ts": ts, "text": framed(subject, "body"),
		}}
		raw, _ := json.Marshal(map[string]any{"type": "events_api", "envelope_id": "E", "payload": payload})
		return raw
	}
	if _, _, ok, _ := socket.normalizeEnvelope(makeEnvelope("C-OTHER", "U-X", "", "1", "[CWapi/MCP/2][MCP_REQUEST][REQX]")); ok {
		t.Fatal("wrong channel passed filter")
	}
	if _, _, ok, _ := socket.normalizeEnvelope(makeEnvelope("C12345678", "U-CWAPI", "", "2", "[CWapi/MCP/2][MCP_REQUEST][REQX]")); ok {
		t.Fatal("self user passed filter")
	}
	_, message, ok, err := socket.normalizeEnvelope(makeEnvelope("C12345678", "U-X", "B-X", "3", "[CWapi/MCP/2][MCP_REQUEST][REQX]"))
	if err != nil || !ok || message.MessageID != "slack:C12345678:3" {
		t.Fatalf("normalized = %#v ok=%v err=%v", message, ok, err)
	}
	if !socket.index.Add(message) || socket.index.Add(message) {
		t.Fatal("message index did not deduplicate")
	}
}

func TestEnvelopeSurfacesMalformedCallerMessageForUsageReply(t *testing.T) {
	fixture := newSlackFixture(t, false)
	socket, err := NewSocket(fixture.client(t), Profile{UserID: "U-CWAPI", BotID: "B-CWAPI"}, NewIndex(2))
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"type": "event_callback", "event": map[string]any{
		"type": "message", "channel": "C12345678", "user": "U-WEB-GPT", "ts": "4.000",
		"text": "[CWapi/MCP/1][MCP_REQUEST][GPTCWAPIFIXNOFRAME06]\n{\"schema\":\"cwapi.mcp.request.v1\"}",
	}}
	raw, _ := json.Marshal(map[string]any{"type": "events_api", "envelope_id": "E-INVALID", "payload": payload})
	_, message, ok, err := socket.normalizeEnvelope(raw)
	if err != nil || !ok || message.ProtocolError != "invalid_format" || message.MessageTS != "4.000" {
		t.Fatalf("message=%#v ok=%v err=%v", message, ok, err)
	}
}

func TestEnvelopeIgnoresOrdinaryChannelConversation(t *testing.T) {
	fixture := newSlackFixture(t, false)
	socket, err := NewSocket(fixture.client(t), Profile{UserID: "U-CWAPI", BotID: "B-CWAPI"}, NewIndex(2))
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"type": "event_callback", "event": map[string]any{
		"type": "message", "channel": "C12345678", "user": "U-HUMAN", "ts": "5.000", "text": "ordinary channel conversation",
	}}
	raw, _ := json.Marshal(map[string]any{"type": "events_api", "envelope_id": "E-ORDINARY", "payload": payload})
	_, _, ok, err := socket.normalizeEnvelope(raw)
	if err != nil || ok {
		t.Fatalf("ordinary message surfaced: ok=%v err=%v", ok, err)
	}
}

func TestIndexEvictsOldest(t *testing.T) {
	index := NewIndex(2)
	for _, ts := range []string{"1", "2", "3"} {
		if !index.Add(Message{MessageID: MessageID("C", ts), Subject: "[CWapi/MCP/2]", MessageTS: ts}) {
			t.Fatalf("failed to add %s", ts)
		}
	}
	items := index.List("", 10)
	if index.Len() != 2 || len(items) != 2 || items[0].MessageTS != "3" || items[1].MessageTS != "2" {
		t.Fatalf("index = %#v", items)
	}
}

func framed(subject, body string) string {
	text, _ := EncodeProtocol(subject, body)
	return text
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}
