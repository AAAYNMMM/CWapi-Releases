package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const Version = "1.5.1"

type Server struct {
	gmail  *GmailClient
	secret string
	health *HealthState
	events *EventLog
	oauth  *OAuthManager
}

func NewServer(
	gmail *GmailClient,
	secret string,
	health *HealthState,
	events *EventLog,
) *Server {
	return &Server{
		gmail:  gmail,
		secret: secret,
		health: health,
		events: events,
	}
}

func (s *Server) SetOAuthManager(oauth *OAuthManager) {
	s.oauth = oauth
	if oauth != nil && s.gmail != nil {
		oauth.SetCompletionValidator(func(ctx context.Context) error {
			_, err := s.gmail.Profile(ctx)
			return err
		})
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /v1/events", s.authorize(s.handleEvents))
	mux.HandleFunc("GET /v1/profile", s.authorize(s.handleProfile))
	mux.HandleFunc("POST /v1/oauth/authorize", s.authorize(s.handleOAuthAuthorize))
	mux.HandleFunc("POST /v1/drafts/list", s.authorize(s.handleList))
	mux.HandleFunc("POST /v1/drafts/get", s.authorize(s.handleGet))
	mux.HandleFunc("POST /v1/drafts/find", s.authorize(s.handleFind))
	mux.HandleFunc("POST /v1/drafts/create", s.authorize(s.handleCreate))
	return mux
}

func (s *Server) authorize(next http.HandlerFunc) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if s.secret != "" && request.Header.Get("Authorization") != "Bearer "+s.secret {
			writeError(writer, http.StatusUnauthorized, errors.New("invalid transport secret"))
			return
		}
		next(writer, request)
	}
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, s.health.Snapshot())
}

func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request) {
	afterID := int64(0)
	if raw := request.URL.Query().Get("after"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 0 {
			writeError(writer, http.StatusBadRequest, errors.New("after must be a non-negative integer"))
			return
		}
		afterID = parsed
	}
	writeJSON(writer, http.StatusOK, map[string]any{"events": s.events.After(afterID)})
}

func (s *Server) handleProfile(writer http.ResponseWriter, request *http.Request) {
	ctx, cancel := requestContext(request)
	defer cancel()
	profile, err := s.gmail.Profile(ctx)
	if err != nil {
		writeError(writer, http.StatusBadGateway, err)
		return
	}
	writeJSON(writer, http.StatusOK, profile)
}

func (s *Server) handleOAuthAuthorize(writer http.ResponseWriter, request *http.Request) {
	if s.oauth == nil {
		writeError(writer, http.StatusServiceUnavailable, errors.New("OAuth manager is not configured"))
		return
	}
	ctx, cancel := context.WithTimeout(request.Context(), 10*time.Minute)
	defer cancel()
	if err := s.oauth.Authorize(ctx); err != nil {
		writeError(writer, http.StatusBadGateway, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "authorized"})
}

func decodeJSON(request *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}

func requestContext(request *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(request.Context(), 120*time.Second)
}

func (s *Server) handleList(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Query      string `json:"query"`
		MaxResults int    `json:"max_results"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := requestContext(request)
	defer cancel()
	drafts, err := s.gmail.ListDraftsIncremental(ctx, input.Query, input.MaxResults)
	if err != nil {
		writeError(writer, http.StatusBadGateway, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"drafts": drafts})
}

func (s *Server) handleGet(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		DraftID string `json:"draft_id"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := requestContext(request)
	defer cancel()
	draft, err := s.gmail.GetDraft(ctx, input.DraftID)
	if err != nil {
		writeError(writer, http.StatusBadGateway, err)
		return
	}
	writeJSON(writer, http.StatusOK, draft)
}

func (s *Server) handleFind(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Subject string `json:"subject"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	ctx, cancel := requestContext(request)
	defer cancel()
	draftID, err := s.gmail.FindExactDraftBySubject(ctx, input.Subject)
	if err != nil {
		writeError(writer, http.StatusBadGateway, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"draft_id": draftID})
}

func (s *Server) handleCreate(writer http.ResponseWriter, request *http.Request) {
	var input struct {
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	if err := decodeJSON(request, &input); err != nil {
		writeError(writer, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(input.Subject) == "" {
		writeError(writer, http.StatusBadRequest, errors.New("subject is required"))
		return
	}
	ctx, cancel := requestContext(request)
	defer cancel()
	draftID, err := s.gmail.CreateDraft(ctx, input.Subject, input.Body)
	if err != nil {
		writeError(writer, http.StatusBadGateway, err)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"draft_id": draftID})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func writeError(writer http.ResponseWriter, status int, err error) {
	writeJSON(writer, status, map[string]string{"error": err.Error()})
}
