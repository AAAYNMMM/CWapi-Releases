package app

import (
	"context"
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
		Body:      message.Body,
		BotID:     message.BotID,
		UserID:    message.UserID,
	}
}
