package app

import (
	"context"
	"errors"
	"time"

	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

func (r *slackRuntime) supervise(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		pair, err := r.credentials.RequirePair()
		channelID := r.config.Snapshot().Slack.ChannelID
		if err != nil || channelID == "" {
			r.setState("setup_required", "Slack App Token, Bot Token and Channel ID are required")
			return
		}
		client, err := slackcore.NewClient(pair.AppToken, pair.BotToken, channelID, nil)
		if err != nil {
			r.setState("degraded", err.Error())
			r.recordError("slack.client", err)
			return
		}
		profile, err := client.Profile(ctx)
		if err != nil {
			r.setState("degraded", "Slack bot identity check failed; retrying")
			r.recordError("slack.auth.test", err)
			if !waitRetry(ctx, backoff) {
				return
			}
			backoff = growBackoff(backoff)
			continue
		}
		channel, err := client.Channel(ctx)
		if err != nil {
			r.setState("degraded", "Slack channel readiness failed; retrying")
			r.recordError("slack.conversations.info", err)
			if !waitRetry(ctx, backoff) {
				return
			}
			backoff = growBackoff(backoff)
			continue
		}
		socket, err := slackcore.NewSocket(client, profile, r.index)
		if err != nil {
			r.setState("degraded", err.Error())
			r.recordError("slack.socket.create", err)
			return
		}
		r.setReadiness(slackcore.Readiness{
			Team:        profile.Team,
			TeamID:      profile.TeamID,
			User:        profile.User,
			UserID:      profile.UserID,
			BotID:       profile.BotID,
			ChannelID:   channel.ID,
			ChannelName: channel.Name,
		})
		backoff = time.Second
		err = socket.Run(ctx, r.onMessage, func(connection slackcore.ConnectionState) {
			ready := connection.State == "healthy"
			r.mu.Lock()
			r.readiness.SocketReady = ready
			r.readiness.Ready = ready
			r.mu.Unlock()
			r.setState(connection.State, connection.Detail)
		}, func(recoveryCtx context.Context) error {
			recoveryErr := r.recoverHistory(recoveryCtx, client, profile)
			if recoveryErr != nil {
				r.recordError("slack.recovery", recoveryErr)
			}
			return recoveryErr
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			r.recordError("slack.socket.run", err)
		}
	}
}

func (r *slackRuntime) recoverHistory(ctx context.Context, client *slackcore.Client, profile slackcore.Profile) error {
	r.mu.RLock()
	handler := r.handler
	store := r.stateStore
	r.mu.RUnlock()
	if handler == nil {
		return nil
	}
	if store == nil {
		return errors.New("SLACK_RECOVERY_STATE_REQUIRED")
	}
	baseline := formatSlackTimestamp(time.Now().UTC())
	cursor, found, err := store.Metadata(ctx, slackCursorKey)
	if err != nil {
		return err
	}
	history, err := client.History(ctx, 100, profile.UserID, profile.BotID)
	if err != nil {
		return err
	}
	initialSync := !found
	if initialSync {
		cursor = baseline
		if err := store.SetMetadata(ctx, slackCursorKey, cursor); err != nil {
			return err
		}
	}
	if len(history) == 100 && compareSlackTimestamp(history[0].MessageTS, cursor) > 0 {
		return errors.New("SLACK_RECOVERY_WINDOW_EXCEEDED")
	}
	for _, message := range history {
		if !shouldDispatchRecoveredMessage(message.MessageTS, cursor) {
			continue
		}
		if err := r.dispatchMessage(ctx, message); err != nil {
			return err
		}
		cursor = message.MessageTS
	}
	return nil
}

func shouldDispatchRecoveredMessage(messageTS, cursor string) bool {
	// Startup establishes a fresh cursor and reconnect only fills the gap after
	// the last successfully handled message in this CWapi process.
	return compareSlackTimestamp(messageTS, cursor) > 0
}
