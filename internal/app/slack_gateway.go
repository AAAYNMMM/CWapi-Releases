package app

import (
	"context"

	"github.com/AAAYNMMM/CWapi/internal/gateway"
)

// gatewaySlackPoster is the tiny adapter between the MCP relay and Slack
// transport. It contains no legacy Runner/Action behavior.
type gatewaySlackPoster struct {
	runtime *slackRuntime
}

func (p gatewaySlackPoster) Post(ctx context.Context, subject, body, threadTS string) (gateway.PostedMessage, error) {
	message, err := p.runtime.Post(ctx, subject, body, threadTS)
	if err != nil {
		return gateway.PostedMessage{}, err
	}
	return gateway.PostedMessage{
		MessageID: message.MessageID,
		MessageTS: message.MessageTS,
		ThreadTS:  message.ThreadTS,
	}, nil
}

func (p gatewaySlackPoster) UploadFile(ctx context.Context, filename, mediaType string, data []byte, threadTS string) (gateway.UploadedFile, error) {
	file, err := p.runtime.UploadFile(ctx, filename, mediaType, data, threadTS)
	if err != nil {
		return gateway.UploadedFile{}, err
	}
	return gateway.UploadedFile{
		FileID:    file.FileID,
		Name:      file.Name,
		Size:      file.Size,
		Permalink: file.Permalink,
	}, nil
}
