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
		Message: "Invalid Slack message received; CWapi usage help replied",
		Data:    map[string]any{"message_id": message.MessageID, "reason": message.ProtocolError},
	})
	return nil
}

func protocolHelpText(cfg config.Config, requestID string) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "CWapi v%s（source_commit=%s）：未识别该消息，未执行任何本地操作。\n", buildinfo.Version, buildinfo.Commit())
	builder.WriteString("请发送完整 MCP 帧：\n")
	fmt.Fprintf(&builder, "+++\n[CWapi/MCP/1][MCP_REQUEST][%s]\n", requestID)
	fmt.Fprintf(&builder, `{"schema":"cwapi.mcp.request.v1","protocol_version":"cwapi-mcp/1","request_id":"%s","method":"projects/list","params":{}}`, requestID)
	builder.WriteString("\n+++\n")
	builder.WriteString("可用方法：projects/list、mcpServerStatus/list、mcpServer/resource/read、mcpServer/tool/call。\n")
	builder.WriteString("项目命令 Tool：cwapi/process_start（推荐 command + argv，可直接调用 powershell.exe/cmd.exe；兼容 runtime=python|node + entrypoint）、process_status、process_stop。\n")
	builder.WriteString("command 支持 PATH 名称、绝对路径和 cwd 相对路径（如 .venv/Scripts/python.exe、node_modules/.bin/tool.cmd）；Windows JSON 路径推荐 C:/...，反斜杠必须写成 \\\\，command 外层不要再加引号。\n")
	builder.WriteString("command/argv 以当前 Windows 用户权限执行；CWapi 不安装或管理语言环境，也不要在命令参数中发送 secret。\n")
	builder.WriteString("每个新请求使用唯一 request_id。项目调用必须同时提供 project_id 与 40 位 expected_commit；threadId 和本地 workspace 路径由 CWapi 管理。")
	if len(cfg.Projects) > 0 {
		builder.WriteString("\n当前已配置项目：")
		const maxHelpProjects = 20
		for index, project := range cfg.Projects {
			if index >= maxHelpProjects {
				fmt.Fprintf(&builder, "\n- 其余 %d 个项目请通过 projects/list 查询", len(cfg.Projects)-maxHelpProjects)
				break
			}
			fmt.Fprintf(&builder, "\n- %s | project_id=%s | repository=%s", helpField(project.DisplayName, 120), project.ID, helpField(project.Repository, 200))
		}
	}
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

func helpField(value string, maxRunes int) string {
	value = strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(value)
	characters := []rune(value)
	if len(characters) > maxRunes {
		characters = characters[:maxRunes]
	}
	return string(characters)
}
