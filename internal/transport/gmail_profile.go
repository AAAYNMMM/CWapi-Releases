package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

type GmailProfile struct {
	EmailAddress  string `json:"email_address"`
	MessagesTotal int64  `json:"messages_total"`
	ThreadsTotal  int64  `json:"threads_total"`
	HistoryID     string `json:"history_id"`
}

func (g *GmailClient) Profile(ctx context.Context) (GmailProfile, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		g.baseURL+"/users/me/profile",
		nil,
	)
	if err != nil {
		return GmailProfile{}, err
	}
	response, err := g.authorizedDo(ctx, request)
	if err != nil {
		return GmailProfile{}, err
	}
	defer response.Body.Close()

	var payload struct {
		EmailAddress  string `json:"emailAddress"`
		MessagesTotal int64  `json:"messagesTotal"`
		ThreadsTotal  int64  `json:"threadsTotal"`
		HistoryID     string `json:"historyId"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return GmailProfile{}, fmt.Errorf("decode Gmail profile: %w", err)
	}
	payload.EmailAddress = strings.TrimSpace(payload.EmailAddress)
	if payload.EmailAddress == "" {
		return GmailProfile{}, errors.New("Gmail API returned no email address")
	}
	return GmailProfile{
		EmailAddress:  payload.EmailAddress,
		MessagesTotal: payload.MessagesTotal,
		ThreadsTotal:  payload.ThreadsTotal,
		HistoryID:     payload.HistoryID,
	}, nil
}
