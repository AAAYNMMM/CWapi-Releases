package app

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/credentials"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
	"github.com/AAAYNMMM/CWapi/internal/state"
)

const slackCursorKey = "slack.last_successful_message_ts"

type ConfigureSlackCommand struct {
	AppToken  string `json:"app_token"`
	BotToken  string `json:"bot_token"`
	ChannelID string `json:"channel_id"`
}

type SlackSnapshot struct {
	Configured      bool   `json:"configured"`
	Ready           bool   `json:"ready"`
	State           string `json:"state"`
	Detail          string `json:"detail"`
	CredentialStore string `json:"credential_store"`
	AppTokenPresent bool   `json:"app_token_present"`
	BotTokenPresent bool   `json:"bot_token_present"`
	ChannelID       string `json:"channel_id"`
	ChannelName     string `json:"channel_name"`
	Team            string `json:"team"`
	TeamID          string `json:"team_id"`
	User            string `json:"user"`
	UserID          string `json:"user_id"`
	BotID           string `json:"bot_id"`
	SocketReady     bool   `json:"socket_ready"`
	RecentIndexSize int    `json:"recent_index_size"`
}

type slackRuntime struct {
	mu            sync.RWMutex
	config        *config.Manager
	credentials   *credentials.Manager
	observability *observability.Hub
	index         *slackcore.Index
	stateStore    *state.Store
	handler       func(context.Context, slackcore.Message) error
	parent        context.Context
	cancel        context.CancelFunc
	state         string
	detail        string
	readiness     slackcore.Readiness
}

func newSlackRuntime(manager *config.Manager, hub *observability.Hub) *slackRuntime {
	return &slackRuntime{
		config:        manager,
		credentials:   credentials.New(),
		observability: hub,
		index:         slackcore.NewIndex(500),
		state:         "setup_required",
		detail:        "Slack credentials and channel are not ready",
	}
}

func (r *slackRuntime) SetProtocolHandler(store *state.Store, handler func(context.Context, slackcore.Message) error) {
	r.mu.Lock()
	r.stateStore = store
	r.handler = handler
	r.mu.Unlock()
}

func (r *slackRuntime) Start(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	r.parent = ctx
	if r.cancel != nil {
		r.cancel()
	}
	runContext, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.mu.Unlock()
	go r.supervise(runContext)
}

func (r *slackRuntime) Close() {
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	r.mu.Unlock()
}

func (r *slackRuntime) restart() {
	r.mu.RLock()
	parent := r.parent
	r.mu.RUnlock()
	if parent != nil {
		r.Start(parent)
	}
}

func (r *slackRuntime) onMessage(message slackcore.Message) {
	r.mu.RLock()
	parent := r.parent
	r.mu.RUnlock()
	if parent == nil {
		parent = context.Background()
	}

	// Socket Mode ACK/read handling must never wait for a local test/build/tool.
	// Tool-specific timeouts already live in the MCP Tool layer, so there is no
	// transport-level 30 second deadline here.
	go func(ctx context.Context, current slackcore.Message) {
		if err := r.dispatchMessage(ctx, current); err != nil {
			r.recordError("slack.protocol.dispatch", err)
		}
	}(parent, message)
}

func (r *slackRuntime) dispatchMessage(ctx context.Context, message slackcore.Message) error {
	if message.ProtocolError != "" {
		if err := r.replyProtocolHelp(ctx, message); err != nil {
			return err
		}
		r.mu.RLock()
		store := r.stateStore
		r.mu.RUnlock()
		if store != nil && message.MessageTS != "" {
			return advanceSlackCursor(ctx, store, message.MessageTS)
		}
		return nil
	}
	_, _ = r.observability.EmitExecution(context.Background(), observability.ExecutionInput{
		TaskID: message.MessageID, Kind: "slack.protocol_message", Status: "received", Message: message.Subject,
		Data: map[string]any{"message_id": message.MessageID, "channel_id": message.ChannelID, "thread_ts": message.ThreadTS},
	})
	r.mu.RLock()
	handler := r.handler
	store := r.stateStore
	r.mu.RUnlock()
	if handler == nil {
		return nil
	}
	if err := handler(ctx, message); err != nil {
		return err
	}
	if store != nil && message.MessageTS != "" {
		if err := advanceSlackCursor(ctx, store, message.MessageTS); err != nil {
			return err
		}
	}
	return nil
}

func advanceSlackCursor(ctx context.Context, store *state.Store, candidate string) error {
	current, found, err := store.Metadata(ctx, slackCursorKey)
	if err != nil {
		return err
	}
	if found && compareSlackTimestamp(candidate, current) <= 0 {
		return nil
	}
	return store.SetMetadata(ctx, slackCursorKey, candidate)
}

func compareSlackTimestamp(left, right string) int {
	leftSec, leftFrac := splitSlackTimestamp(left)
	rightSec, rightFrac := splitSlackTimestamp(right)
	if leftSec < rightSec {
		return -1
	}
	if leftSec > rightSec {
		return 1
	}
	if leftFrac < rightFrac {
		return -1
	}
	if leftFrac > rightFrac {
		return 1
	}
	return 0
}

func splitSlackTimestamp(value string) (int64, int64) {
	secondsText, fractionText, _ := strings.Cut(strings.TrimSpace(value), ".")
	seconds, _ := strconv.ParseInt(secondsText, 10, 64)
	fractionText = strings.TrimRight(fractionText, "0")
	if len(fractionText) > 9 {
		fractionText = fractionText[:9]
	}
	for len(fractionText) < 9 {
		fractionText += "0"
	}
	fraction, _ := strconv.ParseInt(fractionText, 10, 64)
	return seconds, fraction
}

func formatSlackTimestamp(value time.Time) string {
	return fmt.Sprintf("%d.%06d", value.Unix(), value.Nanosecond()/1000)
}

func (r *slackRuntime) Configure(ctx context.Context, command ConfigureSlackCommand) (SlackSnapshot, error) {
	channelID, err := config.CanonicalSlackChannelID(command.ChannelID)
	if err != nil || channelID == "" {
		if err == nil {
			err = errors.New("SLACK_CHANNEL_REQUIRED")
		}
		return r.Snapshot(), err
	}
	pair := credentials.Pair{AppToken: command.AppToken, BotToken: command.BotToken}
	if err := credentials.ValidatePair(pair); err != nil {
		return r.Snapshot(), err
	}
	candidate, err := slackcore.NewClient(pair.AppToken, pair.BotToken, channelID, nil)
	if err != nil {
		return r.Snapshot(), err
	}
	probeContext, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	readiness, err := slackcore.ProbeCandidate(probeContext, candidate)
	if err != nil {
		r.recordError("slack.credentials.readiness", err)
		return r.Snapshot(), err
	}

	previousCredentials, err := r.credentials.ReplacePair(pair)
	if err != nil {
		r.recordError("slack.credentials.replace", err)
		return r.Snapshot(), err
	}
	_, err = r.config.Update(func(candidateConfig *config.Config) error {
		candidateConfig.Slack.ChannelID = channelID
		return nil
	})
	if err != nil {
		rollbackErr := r.credentials.Restore(previousCredentials)
		if rollbackErr != nil {
			return r.Snapshot(), fmt.Errorf("SLACK_CONFIG_SAVE_FAILED: %v; CREDENTIAL_ROLLBACK_FAILED: %w", err, rollbackErr)
		}
		return r.Snapshot(), fmt.Errorf("SLACK_CONFIG_SAVE_FAILED: %w", err)
	}
	r.setReadiness(readiness)
	r.setState("ready", "candidate Slack credentials validated and stored")
	r.restart()
	return r.Snapshot(), nil
}

func (r *slackRuntime) Post(ctx context.Context, subject, body, threadTS string) (slackcore.Message, error) {
	pair, err := r.credentials.RequirePair()
	if err != nil {
		return slackcore.Message{}, err
	}
	channelID := r.config.Snapshot().Slack.ChannelID
	if channelID == "" {
		return slackcore.Message{}, errors.New("SLACK_CHANNEL_REQUIRED")
	}
	client, err := slackcore.NewClient(pair.AppToken, pair.BotToken, channelID, nil)
	if err != nil {
		return slackcore.Message{}, err
	}
	message, err := client.PostProtocol(ctx, subject, body, threadTS)
	if err != nil {
		r.recordError("slack.chat.postMessage", err)
		return slackcore.Message{}, err
	}
	r.index.Add(message)
	_, _ = r.observability.LogRuntime(context.Background(), observability.RuntimeInput{
		Level: "info", Component: "slack", Message: "protocol message delivered",
		Fields: map[string]any{"message_id": message.MessageID, "thread_ts": message.ThreadTS},
	})
	return message, nil
}

func (r *slackRuntime) Snapshot() SlackSnapshot {
	status, statusErr := r.credentials.Status()
	cfg := r.config.Snapshot()
	r.mu.RLock()
	stateValue := r.state
	detail := r.detail
	readiness := r.readiness
	r.mu.RUnlock()
	if statusErr != nil && stateValue != "setup_required" {
		stateValue = "degraded"
		detail = "Windows Credential Manager unavailable"
	}
	configured := cfg.Slack.ChannelID != "" && status.AppTokenPresent && status.BotTokenPresent
	return SlackSnapshot{
		Configured: configured, Ready: configured && readiness.Ready, State: stateValue, Detail: detail,
		CredentialStore: credentials.StoreName, AppTokenPresent: status.AppTokenPresent, BotTokenPresent: status.BotTokenPresent,
		ChannelID: cfg.Slack.ChannelID, ChannelName: readiness.ChannelName, Team: readiness.Team, TeamID: readiness.TeamID,
		User: readiness.User, UserID: readiness.UserID, BotID: readiness.BotID, SocketReady: readiness.SocketReady,
		RecentIndexSize: r.index.Len(),
	}
}

func (r *slackRuntime) setReadiness(readiness slackcore.Readiness) {
	r.mu.Lock()
	r.readiness = readiness
	r.mu.Unlock()
}

func (r *slackRuntime) setState(stateValue, detail string) {
	r.mu.Lock()
	changed := r.state != stateValue || r.detail != detail
	r.state = stateValue
	r.detail = detail
	r.mu.Unlock()
	r.observability.SetComponent("slack", stateValue, detail)
	if changed {
		_, _ = r.observability.LogRuntime(context.Background(), observability.RuntimeInput{
			Level: "info", Component: "slack", Message: "Slack runtime state changed",
			Fields: map[string]any{"state": stateValue, "detail": detail},
		})
	}
}

func (r *slackRuntime) recordError(operation string, err error) {
	if r == nil || r.observability == nil || err == nil {
		return
	}
	_, _ = r.observability.LogRuntime(context.Background(), observability.RuntimeInput{
		Level: "error", Component: "slack", Message: operation + ": " + err.Error(),
		Fields: map[string]any{"operation": operation},
	})
}

func waitRetry(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func growBackoff(current time.Duration) time.Duration {
	current *= 2
	if current > 30*time.Second {
		return 30 * time.Second
	}
	return current
}
