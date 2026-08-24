package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/buildinfo"
	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	slackcore "github.com/AAAYNMMM/CWapi/internal/slack"
)

func (r *slackRuntime) replyProtocolHelp(ctx context.Context, message slackcore.Message) error {
	pair, err := r.credentials.RequirePair()
	if err != nil {
		return err
	}
	cfg := r.config.Snapshot()
	client, err := slackcore.NewClient(pair.AppToken, pair.BotToken, cfg.Slack.ChannelID, nil)
	if err != nil {
		return err
	}
	threadTS := message.ThreadTS
	if strings.TrimSpace(threadTS) == "" {
		threadTS = message.MessageTS
	}
	if _, err := client.PostText(ctx, protocolHelpText(cfg, protocolHelpRequestID(message)), threadTS); err != nil {
		return err
	}
	_, _ = r.observability.EmitExecution(context.Background(), observability.ExecutionInput{
		TaskID: message.MessageID, Kind: "slack.protocol_message", Status: "invalid",
		Message: "Invalid Slack message received; CWapi v2 usage help replied",
		Data:    map[string]any{"message_id": message.MessageID, "reason": message.ProtocolError},
	})
	return nil
}

func protocolHelpText(_ config.Config, requestID string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "CWapi v%s（source_commit=%s）：未识别该消息，未执行任何本地操作。\n", buildinfo.Version, buildinfo.Commit())
	builder.WriteString("请使用完整 MCP v2 帧：\n")
	fmt.Fprintf(&builder, "+++\n[CWapi/MCP/2][MCP_REQUEST][%s]\n", requestID)
	fmt.Fprintf(&builder, `{"schema":"cwapi.mcp.request.v2","protocol_version":"cwapi-mcp/2","request_id":"%s","method":"mcpServerStatus/list","params":{}}`, requestID)
	builder.WriteString("\n+++\n")
	builder.WriteString("只支持 mcpServerStatus/list、mcpServer/resource/read、mcpServer/tool/call；旧 v1 帧不会执行。\n")
	builder.WriteString("进程 Tool：cwapi/process_start、process_status、process_stop；start arguments 只含 command、argv、cwd。\n")
	builder.WriteString("process_start 顶层必须同时给出 ASCII GitHub HTTPS repository_url 与完整 40 位 expected_commit；status/stop/status-list 必须是 global。\n")
	builder.WriteString("Windows remote path 使用 /；threadId、本地 workspace 与内部环境由 CWapi 管理。System Token 只允许放在 v2 request 顶层。")
	return builder.String()
}

func protocolHelpRequestID(message slackcore.Message) string {
	token := strings.Map(func(character rune) rune {
		if character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, message.MessageTS)
	if token == "" {
		token = "REQUEST"
	}
	return "CWAPIHELP" + token
}
