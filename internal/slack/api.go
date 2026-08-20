package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultAPIBaseURL = "https://slack.com/api/"
	maxAPIResponse    = 1 << 20
	maxHistoryResults = 100
)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type Profile struct {
	Team   string `json:"team"`
	TeamID string `json:"team_id"`
	User   string `json:"user"`
	UserID string `json:"user_id"`
	BotID  string `json:"bot_id"`
}

type ChannelInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IsMember bool   `json:"is_member"`
}

type Client struct {
	appToken  string
	botToken  string
	channelID string
	http      HTTPDoer
	baseURL   string
}

type slackResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type rawMessage struct {
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	Text     string `json:"text"`
	BotID    string `json:"bot_id"`
	User     string `json:"user"`
}

func NewClient(appToken, botToken, channelID string, client HTTPDoer) (*Client, error) {
	appToken = strings.TrimSpace(appToken)
	botToken = strings.TrimSpace(botToken)
	channelID = strings.TrimSpace(channelID)
	if !strings.HasPrefix(appToken, "xapp-") || strings.ContainsAny(appToken, " \t\r\n") {
		return nil, errors.New("SLACK_APP_TOKEN_INVALID")
	}
	if !strings.HasPrefix(botToken, "xoxb-") || strings.ContainsAny(botToken, " \t\r\n") {
		return nil, errors.New("SLACK_BOT_TOKEN_INVALID")
	}
	if channelID == "" {
		return nil, errors.New("SLACK_CHANNEL_REQUIRED")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		appToken:  appToken,
		botToken:  botToken,
		channelID: channelID,
		http:      client,
		baseURL:   defaultAPIBaseURL,
	}, nil
}

func (c *Client) setBaseURLForTest(baseURL string) {
	c.baseURL = strings.TrimRight(baseURL, "/") + "/"
}

func (c *Client) Profile(ctx context.Context) (Profile, error) {
	var response struct {
		slackResponse
		Team   string `json:"team"`
		TeamID string `json:"team_id"`
		User   string `json:"user"`
		UserID string `json:"user_id"`
		BotID  string `json:"bot_id"`
	}
	if err := c.doJSON(ctx, c.botToken, "auth.test", nil, &response); err != nil {
		return Profile{}, err
	}
	if strings.TrimSpace(response.UserID) == "" {
		return Profile{}, errors.New("SLACK_AUTH_TEST_NO_USER_ID")
	}
	return Profile{
		Team:   strings.TrimSpace(response.Team),
		TeamID: strings.TrimSpace(response.TeamID),
		User:   strings.TrimSpace(response.User),
		UserID: strings.TrimSpace(response.UserID),
		BotID:  strings.TrimSpace(response.BotID),
	}, nil
}

func (c *Client) Channel(ctx context.Context) (ChannelInfo, error) {
	form := url.Values{}
	form.Set("channel", c.channelID)
	var response struct {
		slackResponse
		Channel ChannelInfo `json:"channel"`
	}
	if err := c.doForm(ctx, c.botToken, "conversations.info", form, &response); err != nil {
		return ChannelInfo{}, err
	}
	response.Channel.ID = strings.TrimSpace(response.Channel.ID)
	response.Channel.Name = strings.TrimSpace(response.Channel.Name)
	if response.Channel.ID != c.channelID {
		return ChannelInfo{}, errors.New("SLACK_CHANNEL_ID_MISMATCH")
	}
	if !response.Channel.IsMember {
		return ChannelInfo{}, errors.New("SLACK_BOT_NOT_CHANNEL_MEMBER")
	}
	return response.Channel, nil
}

func (c *Client) OpenSocketURL(ctx context.Context) (string, error) {
	var response struct {
		slackResponse
		URL string `json:"url"`
	}
	if err := c.doJSON(ctx, c.appToken, "apps.connections.open", nil, &response); err != nil {
		return "", err
	}
	websocketURL := strings.TrimSpace(response.URL)
	if err := validateSocketURL(websocketURL); err != nil {
		return "", err
	}
	return websocketURL, nil
}

func validateSocketURL(raw string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return errors.New("SLACK_SOCKET_URL_INVALID")
	}
	if parsed.Scheme == "wss" {
		return nil
	}
	if parsed.Scheme == "ws" {
		host := parsed.Hostname()
		if host == "127.0.0.1" || host == "localhost" || host == "::1" {
			return nil
		}
	}
	return errors.New("SLACK_SOCKET_URL_NOT_TLS")
}

func (c *Client) PostProtocol(ctx context.Context, subject, body, threadTS string) (Message, error) {
	text, err := EncodeProtocol(subject, body)
	if err != nil {
		return Message{}, err
	}
	return c.postText(ctx, text, threadTS, subject, body)
}

func (c *Client) PostText(ctx context.Context, text, threadTS string) (Message, error) {
	text = strings.TrimSpace(text)
	if text == "" || len([]byte(text)) > 12*1024 {
		return Message{}, errors.New("SLACK_TEXT_SIZE_INVALID")
	}
	return c.postText(ctx, text, threadTS, "", text)
}

func (c *Client) postText(ctx context.Context, text, threadTS, subject, body string) (Message, error) {
	payload := map[string]any{
		"channel":      c.channelID,
		"text":         text,
		"mrkdwn":       false,
		"unfurl_links": false,
		"unfurl_media": false,
	}
	threadTS = strings.TrimSpace(threadTS)
	if threadTS != "" {
		payload["thread_ts"] = threadTS
	}
	var response struct {
		slackResponse
		Channel string `json:"channel"`
		TS      string `json:"ts"`
		Message struct {
			TS       string `json:"ts"`
			ThreadTS string `json:"thread_ts"`
			BotID    string `json:"bot_id"`
			User     string `json:"user"`
		} `json:"message"`
	}
	if err := c.doJSON(ctx, c.botToken, "chat.postMessage", payload, &response); err != nil {
		return Message{}, err
	}
	channelID := strings.TrimSpace(response.Channel)
	if channelID == "" {
		channelID = c.channelID
	}
	if channelID != c.channelID {
		return Message{}, errors.New("SLACK_POST_CHANNEL_MISMATCH")
	}
	messageTS := strings.TrimSpace(response.TS)
	if messageTS == "" {
		messageTS = strings.TrimSpace(response.Message.TS)
	}
	if messageTS == "" {
		return Message{}, errors.New("SLACK_POST_NO_TIMESTAMP")
	}
	return Message{
		MessageID: MessageID(channelID, messageTS),
		ChannelID: channelID,
		MessageTS: messageTS,
		ThreadTS:  strings.TrimSpace(response.Message.ThreadTS),
		Subject:   strings.TrimSpace(subject),
		Body:      body,
		BotID:     strings.TrimSpace(response.Message.BotID),
		UserID:    strings.TrimSpace(response.Message.User),
	}, nil
}

// DeleteMessage removes a message posted by this bot. The S1.4 live smoke uses
// this immediately after its temporary post so validation leaves no channel noise.
func (c *Client) DeleteMessage(ctx context.Context, messageTS string) error {
	messageTS = strings.TrimSpace(messageTS)
	if messageTS == "" {
		return errors.New("SLACK_DELETE_TIMESTAMP_REQUIRED")
	}
	payload := map[string]any{
		"channel": c.channelID,
		"ts":      messageTS,
	}
	var response struct {
		slackResponse
		Channel string `json:"channel"`
		TS      string `json:"ts"`
	}
	if err := c.doJSON(ctx, c.botToken, "chat.delete", payload, &response); err != nil {
		return err
	}
	if strings.TrimSpace(response.Channel) != "" && strings.TrimSpace(response.Channel) != c.channelID {
		return errors.New("SLACK_DELETE_CHANNEL_MISMATCH")
	}
	if strings.TrimSpace(response.TS) != "" && strings.TrimSpace(response.TS) != messageTS {
		return errors.New("SLACK_DELETE_TIMESTAMP_MISMATCH")
	}
	return nil
}

// History returns caller messages in chronological order so recovery dispatch
// cannot invert request sequencing. Malformed messages are preserved for the
// same usage reply they would receive through the live Socket connection.
func (c *Client) History(ctx context.Context, limit int, selfUserID, selfBotID string) ([]Message, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > maxHistoryResults {
		limit = maxHistoryResults
	}
	form := url.Values{}
	form.Set("channel", c.channelID)
	form.Set("limit", strconv.Itoa(limit))
	var response struct {
		slackResponse
		Messages []rawMessage `json:"messages"`
	}
	if err := c.doForm(ctx, c.botToken, "conversations.history", form, &response); err != nil {
		return nil, err
	}
	result := make([]Message, 0, len(response.Messages))
	for index := len(response.Messages) - 1; index >= 0; index-- {
		raw := response.Messages[index]
		if selfUserID != "" && strings.TrimSpace(raw.User) == selfUserID {
			continue
		}
		if selfBotID != "" && strings.TrimSpace(raw.BotID) == selfBotID {
			continue
		}
		messageTS := strings.TrimSpace(raw.TS)
		if messageTS == "" {
			continue
		}
		subject, body, ok := DecodeProtocol(raw.Text)
		if !ok && !IsProtocolCandidate(raw.Text) {
			continue
		}
		message := Message{
			MessageID: MessageID(c.channelID, messageTS),
			ChannelID: c.channelID,
			MessageTS: messageTS,
			ThreadTS:  strings.TrimSpace(raw.ThreadTS),
			BotID:     strings.TrimSpace(raw.BotID),
			UserID:    strings.TrimSpace(raw.User),
		}
		if ok {
			message.Subject = subject
			message.Body = body
		} else {
			message.ProtocolError = "invalid_format"
		}
		result = append(result, message)
	}
	return result, nil
}

func (c *Client) doJSON(ctx context.Context, token, method string, payload any, target any) error {
	var body io.Reader
	contentType := ""
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("SLACK_REQUEST_ENCODE_FAILED: %w", err)
		}
		body = bytes.NewReader(encoded)
		contentType = "application/json; charset=utf-8"
	}
	return c.doRequest(ctx, token, method, body, contentType, target)
}

func (c *Client) doForm(ctx context.Context, token, method string, form url.Values, target any) error {
	return c.doRequest(ctx, token, method, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", target)
}

func (c *Client) doRequest(ctx context.Context, token, method string, body io.Reader, contentType string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+method, body)
	if err != nil {
		return fmt.Errorf("SLACK_REQUEST_CREATE_FAILED: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("SLACK_REQUEST_FAILED: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("SLACK_HTTP_STATUS_%d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxAPIResponse+1))
	if err != nil {
		return errors.New("SLACK_RESPONSE_READ_FAILED")
	}
	if len(raw) > maxAPIResponse {
		return errors.New("SLACK_RESPONSE_TOO_LARGE")
	}
	var base slackResponse
	if err := json.Unmarshal(raw, &base); err != nil {
		return errors.New("SLACK_RESPONSE_INVALID_JSON")
	}
	if !base.OK {
		return fmt.Errorf("SLACK_API_ERROR_%s", boundedCode(base.Error))
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return errors.New("SLACK_RESPONSE_INVALID_SHAPE")
	}
	return nil
}

func boundedCode(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown_error"
	}
	if len(value) > 96 {
		return value[:96]
	}
	return value
}
