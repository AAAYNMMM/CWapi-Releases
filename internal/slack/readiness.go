package slack

import (
	"context"
	"fmt"
)

type Readiness struct {
	Ready       bool   `json:"ready"`
	Team        string `json:"team"`
	TeamID      string `json:"team_id"`
	User        string `json:"user"`
	UserID      string `json:"user_id"`
	BotID       string `json:"bot_id"`
	ChannelID   string `json:"channel_id"`
	ChannelName string `json:"channel_name"`
	SocketReady bool   `json:"socket_ready"`
}

// ProbeCandidate performs network readiness against candidate credentials
// before they are allowed to replace the stored credential pair.
func ProbeCandidate(ctx context.Context, client *Client) (Readiness, error) {
	profile, err := client.Profile(ctx)
	if err != nil {
		return Readiness{}, fmt.Errorf("SLACK_READINESS_BOT_IDENTITY_FAILED: %w", err)
	}
	channel, err := client.Channel(ctx)
	if err != nil {
		return Readiness{}, fmt.Errorf("SLACK_READINESS_CHANNEL_FAILED: %w", err)
	}
	socket, err := NewSocket(client, profile, NewIndex(defaultIndexCapacity))
	if err != nil {
		return Readiness{}, err
	}
	if err := socket.Probe(ctx); err != nil {
		return Readiness{}, fmt.Errorf("SLACK_READINESS_SOCKET_FAILED: %w", err)
	}
	return Readiness{
		Ready:       true,
		Team:        profile.Team,
		TeamID:      profile.TeamID,
		User:        profile.User,
		UserID:      profile.UserID,
		BotID:       profile.BotID,
		ChannelID:   channel.ID,
		ChannelName: channel.Name,
		SocketReady: true,
	}, nil
}
