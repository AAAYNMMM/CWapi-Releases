package transport

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
)

const maxDraftScanResults = 5000

type Draft struct {
	DraftID   string `json:"draft_id"`
	MessageID string `json:"message_id"`
	Subject   string `json:"subject"`
	Body      string `json:"body"`
}

type GmailClient struct {
	account string
	baseURL string
	client  HTTPDoer
	tokens  *TokenManager
}

func NewGmailClient(account string, client HTTPDoer, tokens *TokenManager) *GmailClient {
	return &GmailClient{
		account: account,
		baseURL: "https://gmail.googleapis.com/gmail/v1",
		client:  client,
		tokens:  tokens,
	}
}

func (g *GmailClient) authorizedDo(ctx context.Context, request *http.Request) (*http.Response, error) {
	token, err := g.tokens.AccessToken(ctx)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := g.client.Do(request)
	if err != nil {
		return nil, err
	}

	if response.StatusCode == http.StatusUnauthorized {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		response.Body.Close()
		freshToken, refreshErr := g.tokens.ForceRefresh(ctx)
		if refreshErr != nil {
			return nil, refreshErr
		}
		retry := request.Clone(ctx)
		if request.Body != nil {
			if request.GetBody == nil {
				return nil, errors.New("Gmail request body cannot be replayed after token refresh")
			}
			body, bodyErr := request.GetBody()
			if bodyErr != nil {
				return nil, fmt.Errorf("recreate Gmail request body: %w", bodyErr)
			}
			retry.Body = body
		}
		retry.Header.Set("Authorization", "Bearer "+freshToken)
		response, err = g.client.Do(retry)
		if err != nil {
			return nil, err
		}
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		defer response.Body.Close()
		limited, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf(
			"Gmail API HTTP %s: %s",
			response.Status,
			strings.TrimSpace(string(limited)),
		)
	}
	return response, nil
}

func (g *GmailClient) ListDrafts(ctx context.Context, query string, maxResults int) ([]Draft, error) {
	if maxResults < 1 {
		maxResults = 1
	}
	if maxResults > maxDraftScanResults {
		maxResults = maxDraftScanResults
	}

	drafts := make([]Draft, 0, maxResults)
	pageToken := ""
	for len(drafts) < maxResults {
		endpoint, err := url.Parse(g.baseURL + "/users/me/drafts")
		if err != nil {
			return nil, err
		}
		pageSize := maxResults - len(drafts)
		if pageSize > 500 {
			pageSize = 500
		}
		values := endpoint.Query()
		values.Set("q", query)
		values.Set("maxResults", strconv.Itoa(pageSize))
		if pageToken != "" {
			values.Set("pageToken", pageToken)
		}
		endpoint.RawQuery = values.Encode()

		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
		if err != nil {
			return nil, err
		}
		response, err := g.authorizedDo(ctx, request)
		if err != nil {
			return nil, err
		}
		var listing struct {
			Drafts []struct {
				ID string `json:"id"`
			} `json:"drafts"`
			NextPageToken string `json:"nextPageToken"`
		}
		decodeErr := json.NewDecoder(response.Body).Decode(&listing)
		response.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode draft list: %w", decodeErr)
		}
		for _, item := range listing.Drafts {
			if item.ID == "" {
				continue
			}
			draft, err := g.GetDraft(ctx, item.ID)
			if err != nil {
				return nil, err
			}
			drafts = append(drafts, draft)
			if len(drafts) >= maxResults {
				return drafts, nil
			}
		}
		pageToken = listing.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return drafts, nil
}

func decodeBase64URL(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	decoded, rawErr := base64.RawURLEncoding.DecodeString(value)
	if rawErr == nil {
		return decoded, nil
	}
	decoded, paddedErr := base64.URLEncoding.DecodeString(value)
	if paddedErr == nil {
		return decoded, nil
	}
	return nil, fmt.Errorf(
		"invalid base64url data (raw: %v; padded: %v)",
		rawErr,
		paddedErr,
	)
}

func (g *GmailClient) GetDraft(ctx context.Context, draftID string) (Draft, error) {
	if draftID == "" {
		return Draft{}, errors.New("draft_id is required")
	}
	endpoint := g.baseURL + "/users/me/drafts/" + url.PathEscape(draftID) + "?format=raw"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return Draft{}, err
	}
	response, err := g.authorizedDo(ctx, request)
	if err != nil {
		return Draft{}, err
	}
	defer response.Body.Close()

	var payload struct {
		ID      string `json:"id"`
		Message struct {
			ID  string `json:"id"`
			Raw string `json:"raw"`
		} `json:"message"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return Draft{}, fmt.Errorf("decode draft: %w", err)
	}
	decoded, err := decodeBase64URL(payload.Message.Raw)
	if err != nil {
		return Draft{}, fmt.Errorf("decode draft MIME: %w", err)
	}
	message, err := mail.ReadMessage(bytes.NewReader(decoded))
	if err != nil {
		return Draft{}, fmt.Errorf("parse draft MIME: %w", err)
	}
	subject, err := (&mime.WordDecoder{}).DecodeHeader(message.Header.Get("Subject"))
	if err != nil {
		subject = message.Header.Get("Subject")
	}
	body, err := readTextBody(message.Header, message.Body)
	if err != nil {
		return Draft{}, fmt.Errorf("read draft body: %w", err)
	}
	return Draft{
		DraftID:   payload.ID,
		MessageID: payload.Message.ID,
		Subject:   subject,
		Body:      body,
	}, nil
}

func readTextBody(header mail.Header, body io.Reader) (string, error) {
	mediaType, parameters, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediaType == "" {
		mediaType = "text/plain"
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := parameters["boundary"]
		if boundary == "" {
			return "", errors.New("multipart message has no boundary")
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", err
			}
			partHeader := mail.Header(part.Header)
			partType, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
			if partType == "text/plain" || strings.HasPrefix(partType, "multipart/") {
				text, readErr := readTextBody(partHeader, part)
				part.Close()
				if readErr == nil && text != "" {
					return text, nil
				}
			} else {
				part.Close()
			}
		}
		return "", nil
	}

	var decoded io.Reader = body
	switch strings.ToLower(header.Get("Content-Transfer-Encoding")) {
	case "base64":
		decoded = base64.NewDecoder(base64.StdEncoding, body)
	case "quoted-printable":
		decoded = quotedprintable.NewReader(body)
	}
	raw, err := io.ReadAll(decoded)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func sanitizeHeader(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n") {
		return "", errors.New("header contains a newline")
	}
	return value, nil
}

func (g *GmailClient) CreateDraft(ctx context.Context, subject string, body string) (string, error) {
	subject, err := sanitizeHeader(subject)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString([]byte(
		"From: " + g.account + "\r\n" +
			"To: " + g.account + "\r\n" +
			"Subject: " + subject + "\r\n" +
			"MIME-Version: 1.0\r\n" +
			"Content-Type: text/plain; charset=UTF-8\r\n" +
			"Content-Transfer-Encoding: 8bit\r\n\r\n" + body,
	))
	payload, err := json.Marshal(map[string]any{
		"message": map[string]string{"raw": encoded},
	})
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		g.baseURL+"/users/me/drafts",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := g.authorizedDo(ctx, request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		return "", fmt.Errorf("decode created draft: %w", err)
	}
	if created.ID == "" {
		return "", errors.New("create draft response has no id")
	}
	return created.ID, nil
}

func (g *GmailClient) FindExactDraftBySubject(ctx context.Context, subject string) (string, error) {
	query := `in:drafts subject:"` + strings.ReplaceAll(subject, `"`, `\"`) + `"`
	drafts, err := g.ListDrafts(ctx, query, 50)
	if err != nil {
		return "", err
	}
	for _, draft := range drafts {
		if draft.Subject == subject {
			return draft.DraftID, nil
		}
	}
	return "", nil
}
