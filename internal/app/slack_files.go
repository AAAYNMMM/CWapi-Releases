package app

import (
	"context"
	"errors"

	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

const MaxCWapiSlackUploadBytes = gateway.MaxSlackArtifactBytes

func (r *slackRuntime) UploadFile(ctx context.Context, filename, mediaType string, data []byte, threadTS string) (slackcore.FileUpload, error) {
	if len(data) == 0 {
		return slackcore.FileUpload{}, errors.New("CWAPI_SLACK_FILE_EMPTY")
	}
	if len(data) > MaxCWapiSlackUploadBytes {
		return slackcore.FileUpload{}, errors.New("CWAPI_SLACK_FILE_TOO_LARGE")
	}
	pair, err := r.credentials.RequirePair()
	if err != nil {
		return slackcore.FileUpload{}, err
	}
	channelID := r.config.Snapshot().Slack.ChannelID
	if channelID == "" {
		return slackcore.FileUpload{}, errors.New("SLACK_CHANNEL_REQUIRED")
	}
	client, err := slackcore.NewClient(pair.AppToken, pair.BotToken, channelID, nil)
	if err != nil {
		return slackcore.FileUpload{}, err
	}
	file, err := client.UploadFile(ctx, filename, mediaType, data, threadTS)
	if err != nil {
		r.recordError("slack.files.upload", err)
		return slackcore.FileUpload{}, err
	}
	_, _ = r.observability.LogRuntime(context.Background(), observability.RuntimeInput{
		Level:     "info",
		Component: "slack",
		Message:   "Slack file delivered",
		Fields: map[string]any{
			"file_id": file.FileID,
			"name":    file.Name,
			"size":    file.Size,
		},
	})
	return file, nil
}
