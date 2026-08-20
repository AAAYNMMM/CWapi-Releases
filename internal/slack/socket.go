package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	maxSocketEnvelopeBytes = 64 * 1024
	maxReconnectBackoff    = 30 * time.Second
)

type ConnectionState struct {
	State  string
	Detail string
}

type Socket struct {
	client  *Client
	profile Profile
	index   *Index
	dialer  *websocket.Dialer
}

type socketEnvelope struct {
	Type       string          `json:"type"`
	EnvelopeID string          `json:"envelope_id"`
	Payload    json.RawMessage `json:"payload"`
	Reason     string          `json:"reason"`
}

type socketAck struct {
	EnvelopeID string `json:"envelope_id"`
}

func NewSocket(client *Client, profile Profile, index *Index) (*Socket, error) {
	if client == nil {
		return nil, errors.New("SLACK_CLIENT_REQUIRED")
	}
	if index == nil {
		index = NewIndex(defaultIndexCapacity)
	}
	return &Socket{
		client:  client,
		profile: profile,
		index:   index,
		dialer:  websocket.DefaultDialer,
	}, nil
}

// Probe proves that apps.connections.open returns a usable WebSocket URL and
// that Slack accepts the WebSocket connection. The normal Run loop owns ACK
// handling; tests exercise ACK-before-payload parsing directly.
func (s *Socket) Probe(ctx context.Context) error {
	websocketURL, err := s.client.OpenSocketURL(ctx)
	if err != nil {
		return err
	}
	connection, response, err := s.dialer.DialContext(ctx, websocketURL, nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return errors.New("SLACK_SOCKET_CONNECT_FAILED")
	}
	defer connection.Close()
	connection.SetReadLimit(maxSocketEnvelopeBytes)
	deadline := time.Now().Add(8 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	_ = connection.SetReadDeadline(deadline)
	messageType, raw, err := connection.ReadMessage()
	if err != nil {
		return errors.New("SLACK_SOCKET_HELLO_FAILED")
	}
	if messageType != websocket.TextMessage || len(raw) > maxSocketEnvelopeBytes {
		return errors.New("SLACK_SOCKET_HELLO_INVALID")
	}
	var hello struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &hello) != nil || hello.Type != "hello" {
		return errors.New("SLACK_SOCKET_HELLO_INVALID")
	}
	return nil
}

func (s *Socket) Run(
	ctx context.Context,
	onMessage func(Message),
	onState func(ConnectionState),
	beforeConnect func(context.Context) error,
) error {
	if onMessage == nil {
		return errors.New("SLACK_MESSAGE_HANDLER_REQUIRED")
	}
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return nil
		}
		// Durable history recovery belongs to the app runtime, but it must run
		// before every WebSocket attempt, including Socket's internal reconnects.
		if beforeConnect != nil {
			if err := beforeConnect(ctx); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				if onState != nil {
					onState(ConnectionState{State: "degraded", Detail: boundedCode(err.Error())})
				}
				if err := sleepContext(ctx, backoff); err != nil {
					return nil
				}
				backoff *= 2
				if backoff > maxReconnectBackoff {
					backoff = maxReconnectBackoff
				}
				continue
			}
		}
		if onState != nil {
			onState(ConnectionState{State: "connecting", Detail: "opening Socket Mode connection"})
		}
		connected := false
		websocketURL, err := s.client.OpenSocketURL(ctx)
		if err == nil {
			connected, err = s.runConnection(ctx, websocketURL, onMessage, onState)
		}
		if ctx.Err() != nil {
			return nil
		}
		if connected {
			// Reset only after a real WebSocket connection was established.
			backoff = time.Second
		}
		if onState != nil {
			detail := "retrying after Slack disconnect"
			if err != nil {
				detail = boundedCode(err.Error())
			}
			onState(ConnectionState{State: "degraded", Detail: detail})
		}
		if err := sleepContext(ctx, backoff); err != nil {
			return nil
		}
		backoff *= 2
		if backoff > maxReconnectBackoff {
			backoff = maxReconnectBackoff
		}
	}
}

func (s *Socket) runConnection(ctx context.Context, websocketURL string, onMessage func(Message), onState func(ConnectionState)) (bool, error) {
	connection, response, err := s.dialer.DialContext(ctx, websocketURL, nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return false, errors.New("SLACK_SOCKET_CONNECT_FAILED")
	}
	defer connection.Close()
	connection.SetReadLimit(maxSocketEnvelopeBytes)

	closed := make(chan struct{})
	defer close(closed)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-closed:
		}
	}()

	if onState != nil {
		onState(ConnectionState{State: "healthy", Detail: "Socket Mode connected"})
	}

	// History recovery deliberately does not live in runConnection. Socket.Run
	// invokes the app-owned durable recovery callback before each connection.
	// Replaying history here would bypass that cursor and could execute old
	// protocol messages again on first start or reconnect.
	for {
		messageType, raw, err := connection.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return true, nil
			}
			return true, errors.New("SLACK_SOCKET_READ_FAILED")
		}
		if messageType != websocket.TextMessage {
			continue
		}
		if len(raw) > maxSocketEnvelopeBytes {
			return true, errors.New("SLACK_SOCKET_ENVELOPE_TOO_LARGE")
		}

		// ACK is intentionally based only on envelope_id and happens before any
		// business payload parsing. A malformed payload must not delay Slack ACK.
		var ackEnvelope struct {
			EnvelopeID string `json:"envelope_id"`
		}
		if json.Unmarshal(raw, &ackEnvelope) == nil && strings.TrimSpace(ackEnvelope.EnvelopeID) != "" {
			if err := connection.WriteJSON(socketAck{EnvelopeID: strings.TrimSpace(ackEnvelope.EnvelopeID)}); err != nil {
				return true, errors.New("SLACK_SOCKET_ACK_FAILED")
			}
		}

		envelope, message, hasMessage, err := s.normalizeEnvelope(raw)
		if err != nil {
			continue
		}
		if envelope.Type == "disconnect" {
			return true, fmt.Errorf("SLACK_SOCKET_DISCONNECT_%s", boundedCode(envelope.Reason))
		}
		if hasMessage && s.index.Add(message) {
			onMessage(message)
		}
	}
}

func (s *Socket) normalizeEnvelope(raw []byte) (socketEnvelope, Message, bool, error) {
	if len(raw) > maxSocketEnvelopeBytes {
		return socketEnvelope{}, Message{}, false, errors.New("SLACK_SOCKET_ENVELOPE_TOO_LARGE")
	}
	var envelope socketEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return socketEnvelope{}, Message{}, false, errors.New("SLACK_SOCKET_ENVELOPE_INVALID")
	}
	if envelope.Type != "events_api" || len(envelope.Payload) == 0 {
		return envelope, Message{}, false, nil
	}
	var payload struct {
		Type  string `json:"type"`
		Event struct {
			Type     string `json:"type"`
			Subtype  string `json:"subtype"`
			Channel  string `json:"channel"`
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
			Text     string `json:"text"`
			BotID    string `json:"bot_id"`
			User     string `json:"user"`
		} `json:"event"`
	}
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		return envelope, Message{}, false, errors.New("SLACK_EVENT_PAYLOAD_INVALID")
	}
	if payload.Type != "event_callback" || payload.Event.Type != "message" {
		return envelope, Message{}, false, nil
	}
	if payload.Event.Subtype != "" && payload.Event.Subtype != "bot_message" {
		return envelope, Message{}, false, nil
	}
	if strings.TrimSpace(payload.Event.Channel) != s.client.channelID {
		return envelope, Message{}, false, nil
	}
	if s.profile.UserID != "" && strings.TrimSpace(payload.Event.User) == s.profile.UserID {
		return envelope, Message{}, false, nil
	}
	if s.profile.BotID != "" && strings.TrimSpace(payload.Event.BotID) == s.profile.BotID {
		return envelope, Message{}, false, nil
	}
	messageTS := strings.TrimSpace(payload.Event.TS)
	if messageTS == "" {
		return envelope, Message{}, false, nil
	}
	subject, body, ok := DecodeProtocol(payload.Event.Text)
	if !ok {
		if !IsProtocolCandidate(payload.Event.Text) {
			return envelope, Message{}, false, nil
		}
		return envelope, Message{
			MessageID:     MessageID(s.client.channelID, messageTS),
			ChannelID:     s.client.channelID,
			MessageTS:     messageTS,
			ThreadTS:      strings.TrimSpace(payload.Event.ThreadTS),
			BotID:         strings.TrimSpace(payload.Event.BotID),
			UserID:        strings.TrimSpace(payload.Event.User),
			ProtocolError: "invalid_format",
		}, true, nil
	}
	return envelope, Message{
		MessageID: MessageID(s.client.channelID, messageTS),
		ChannelID: s.client.channelID,
		MessageTS: messageTS,
		ThreadTS:  strings.TrimSpace(payload.Event.ThreadTS),
		Subject:   subject,
		Body:      body,
		BotID:     strings.TrimSpace(payload.Event.BotID),
		UserID:    strings.TrimSpace(payload.Event.User),
	}, true, nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
