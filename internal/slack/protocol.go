package slack

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	mcpprotocol "github.com/AAAYNMMM/CWapi/internal/protocol"
)

const (
	MCPProtocolPrefix = "[CWapi/MCP/2]"
	protocolFrame     = "+++"
	maxProtocolBytes  = 64 * 1024
)

var slackAutoURLPattern = regexp.MustCompile(`<((?:https?)://[^\s<>"|]+)>`)

var slackEntityDecoder = strings.NewReplacer(
	"&lt;", "<",
	"&gt;", ">",
	"&amp;", "&",
)

type Message struct {
	MessageID     string `json:"message_id"`
	ChannelID     string `json:"channel_id"`
	MessageTS     string `json:"message_ts"`
	ThreadTS      string `json:"thread_ts,omitempty"`
	Subject       string `json:"subject"`
	Body          string `json:"body"`
	BotID         string `json:"bot_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	ProtocolError string `json:"protocol_error,omitempty"`
}

func EncodeProtocol(subject, body string) (string, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" || !strings.HasPrefix(subject, MCPProtocolPrefix) {
		return "", errors.New("SLACK_PROTOCOL_SUBJECT_INVALID")
	}
	if strings.ContainsAny(subject, "\r\n") {
		return "", errors.New("SLACK_PROTOCOL_SUBJECT_MULTILINE")
	}
	if _, err := mcpprotocol.ParseMCPSubject(subject); err != nil {
		return "", errors.New("SLACK_PROTOCOL_SUBJECT_INVALID")
	}
	text := protocolFrame + "\n" + subject + "\n" + body + "\n" + protocolFrame
	if len([]byte(text)) > maxProtocolBytes {
		return "", fmt.Errorf("SLACK_PROTOCOL_TOO_LARGE: %d", len([]byte(text)))
	}
	return text, nil
}

func DecodeProtocol(text string) (subject, body string, ok bool) {
	if len([]byte(text)) > maxProtocolBytes {
		return "", "", false
	}
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != protocolFrame {
		return "", "", false
	}

	closing := -1
	for index := 2; index < len(lines); index++ {
		if isProtocolClosingLine(lines[index]) {
			closing = index
			break
		}
	}
	if closing < 2 {
		return "", "", false
	}

	subject = strings.TrimSpace(lines[1])
	if subject == "" || !strings.HasPrefix(subject, MCPProtocolPrefix) || strings.ContainsAny(subject, "\r\n") {
		return "", "", false
	}
	if _, err := mcpprotocol.ParseMCPSubject(subject); err != nil {
		return "", "", false
	}
	body = strings.Join(lines[2:closing], "\n")
	body = slackAutoURLPattern.ReplaceAllString(body, `$1`)
	return subject, slackEntityDecoder.Replace(body), true
}

// IsProtocolCandidate identifies caller text that was intended for CWapi but
// cannot be decoded as a complete frame. Ordinary channel conversation remains
// ignored; malformed CWapi requests are surfaced so the app can reply with the
// current v2 protocol instructions.
func IsProtocolCandidate(text string) bool {
	normalized := strings.ToLower(text)
	return strings.Contains(normalized, "[cwapi/") ||
		strings.Contains(normalized, "cwapi.mcp.request.v1") ||
		strings.Contains(normalized, "cwapi.mcp.request.v2") ||
		strings.Contains(normalized, "cwapi-mcp/1") ||
		strings.Contains(normalized, "cwapi-mcp/2")
}

// Slack integrations may append attribution metadata after the user's text.
// The protocol payload ends at the frame token; any same-line suffix must be
// separated from the token by whitespace so strings such as "++++" do not
// accidentally terminate a frame. The suffix remains out-of-band and is never
// included in the protocol body.
func isProtocolClosingLine(line string) bool {
	line = strings.TrimLeft(line, " \t")
	if line == protocolFrame {
		return true
	}
	if len(line) <= len(protocolFrame) || !strings.HasPrefix(line, protocolFrame) {
		return false
	}
	next := line[len(protocolFrame)]
	return next == ' ' || next == '\t'
}

func MessageID(channelID, messageTS string) string {
	return "slack:" + strings.TrimSpace(channelID) + ":" + strings.TrimSpace(messageTS)
}
