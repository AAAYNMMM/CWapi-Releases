package app

import (
	"context"
	"encoding/json"
	"strings"

	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

type SlackMessageSnapshot struct {
	MessageID string `json:"message_id"`
	ChannelID string `json:"channel_id"`
	MessageTS string `json:"message_ts"`
	ThreadTS  string `json:"thread_ts,omitempty"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
	BotID     string `json:"bot_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

func (s *Service) SlackSnapshot() SlackSnapshot {
	if s == nil || s.slack == nil {
		return SlackSnapshot{State: "unavailable", Detail: "Slack runtime unavailable"}
	}
	return s.slack.Snapshot()
}

func (s *Service) ConfigureSlack(ctx context.Context, command ConfigureSlackCommand) (SlackSnapshot, error) {
	if s == nil || s.slack == nil {
		return SlackSnapshot{}, context.Canceled
	}
	return s.slack.Configure(ctx, command)
}

func (s *Service) PostSlackProtocol(ctx context.Context, subject, body, threadTS string) (SlackMessageSnapshot, error) {
	if s == nil || s.slack == nil {
		return SlackMessageSnapshot{}, context.Canceled
	}
	message, err := s.slack.Post(ctx, subject, body, threadTS)
	if err != nil {
		return SlackMessageSnapshot{}, err
	}
	return slackMessageSnapshot(message), nil
}

// RecentSlackProtocol reads the in-memory bounded transport index. It includes
// both accepted inbound protocol messages and successfully posted outbound
// messages, which is enough for packaged real-Slack acceptance without adding
// a second durable message store.
func (s *Service) RecentSlackProtocol(prefix string, limit int) []SlackMessageSnapshot {
	if s == nil || s.slack == nil || s.slack.index == nil {
		return []SlackMessageSnapshot{}
	}
	prefix = strings.TrimSpace(prefix)
	messages := s.slack.index.List(prefix, limit)
	result := make([]SlackMessageSnapshot, len(messages))
	for index, message := range messages {
		result[index] = slackMessageSnapshot(message)
	}
	return result
}

func slackMessageSnapshot(message slackcore.Message) SlackMessageSnapshot {
	return SlackMessageSnapshot{
		MessageID: message.MessageID,
		ChannelID: message.ChannelID,
		MessageTS: message.MessageTS,
		ThreadTS:  message.ThreadTS,
		Subject:   message.Subject,
		Body:      publicSlackProtocolBody(message.Subject, message.Body),
		BotID:     message.BotID,
		UserID:    message.UserID,
	}
}

func publicSlackProtocolBody(subject, body string) string {
	if !strings.HasPrefix(subject, "[CWapi/MCP/2]") {
		return body
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		return body
	}
	raw, ok := envelope["system_token"]
	if !ok {
		return body
	}
	var token string
	if err := json.Unmarshal(raw, &token); err != nil || token == "" {
		return body
	}
	envelope["system_token"] = json.RawMessage(`"[REDACTED]"`)
	redacted, err := json.Marshal(envelope)
	if err != nil {
		return body
	}
	return string(redacted)
}
