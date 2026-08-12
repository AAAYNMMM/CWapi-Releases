package transport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Event struct {
	ID        int64          `json:"id"`
	Timestamp string         `json:"timestamp"`
	Type      string         `json:"type"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}

type EventLog struct {
	mu       sync.Mutex
	nextID   int64
	capacity int
	path     string
	events   []Event
	now      func() time.Time
}

func NewEventLog(capacity int, path string) *EventLog {
	if capacity < 1 {
		capacity = 256
	}
	return &EventLog{capacity: capacity, path: path, now: time.Now}
}

func (l *EventLog) Append(eventType, level, message string, details map[string]any) Event {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.nextID++
	event := Event{
		ID:        l.nextID,
		Timestamp: l.now().UTC().Format(time.RFC3339Nano),
		Type:      eventType,
		Level:     level,
		Message:   message,
		Details:   details,
	}
	l.events = append(l.events, event)
	if len(l.events) > l.capacity {
		l.events = append([]Event(nil), l.events[len(l.events)-l.capacity:]...)
	}
	if l.path != "" {
		_ = l.appendFileLocked(event)
	}
	return event
}

func (l *EventLog) appendFileLocked(event Event) error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return fmt.Errorf("create event directory: %w", err)
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open event file: %w", err)
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(event)
}

func (l *EventLog) After(afterID int64) []Event {
	l.mu.Lock()
	defer l.mu.Unlock()

	result := make([]Event, 0, len(l.events))
	for _, event := range l.events {
		if event.ID > afterID {
			result = append(result, event)
		}
	}
	return result
}

type HealthSnapshot struct {
	Status                string `json:"status"`
	Version               string `json:"version"`
	AuthorizationRequired bool   `json:"authorization_required"`
	ConsecutiveFailures   int    `json:"consecutive_failures"`
	LastError             string `json:"last_error,omitempty"`
	LastFailureAt         string `json:"last_failure_at,omitempty"`
	LastSuccessAt         string `json:"last_success_at,omitempty"`
	NextRetryAt           string `json:"next_retry_at,omitempty"`
}

type HealthState struct {
	mu                    sync.Mutex
	status                string
	authorizationRequired bool
	consecutiveFailures   int
	lastError             string
	lastFailureAt         string
	lastSuccessAt         string
	nextRetryAt           string
	events                *EventLog
	now                   func() time.Time
}

func NewHealthState(events *EventLog) *HealthState {
	return &HealthState{status: "healthy", events: events, now: time.Now}
}

func (h *HealthState) Failure(err error, nextRetry time.Time, attempt int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	wasHealthy := h.consecutiveFailures == 0 && !h.authorizationRequired
	now := h.now().UTC()
	h.status = "backoff"
	h.authorizationRequired = false
	h.consecutiveFailures++
	h.lastError = err.Error()
	h.lastFailureAt = now.Format(time.RFC3339Nano)
	h.nextRetryAt = nextRetry.UTC().Format(time.RFC3339Nano)
	if wasHealthy && h.events != nil {
		h.events.Append(
			"transport_degraded",
			"WARN",
			"Google transport connection degraded.",
			map[string]any{"error": h.lastError},
		)
	}
	if h.events != nil {
		h.events.Append(
			"transport_retry",
			"WARN",
			"Google transport request will retry.",
			map[string]any{
				"attempt":       attempt,
				"next_retry_at": h.nextRetryAt,
				"error":         h.lastError,
			},
		)
	}
}

func (h *HealthState) Exhausted(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.status = "degraded"
	h.authorizationRequired = false
	h.lastError = err.Error()
	h.nextRetryAt = ""
}

func (h *HealthState) RequireAuthorization() {
	h.mu.Lock()
	defer h.mu.Unlock()

	alreadyRequired := h.authorizationRequired
	now := h.now().UTC().Format(time.RFC3339Nano)
	h.status = "authorization_required"
	h.authorizationRequired = true
	h.consecutiveFailures = 0
	h.lastError = "Google OAuth authorization is required."
	h.lastFailureAt = now
	h.nextRetryAt = ""
	if !alreadyRequired && h.events != nil {
		h.events.Append(
			"oauth_reauthorization_required",
			"WARN",
			"Google OAuth authorization must be completed by the user in the browser.",
			nil,
		)
	}
}

func (h *HealthState) Success() {
	h.mu.Lock()
	defer h.mu.Unlock()

	recovered := h.consecutiveFailures > 0 || h.authorizationRequired
	now := h.now().UTC().Format(time.RFC3339Nano)
	h.status = "healthy"
	h.authorizationRequired = false
	h.consecutiveFailures = 0
	h.lastError = ""
	h.lastSuccessAt = now
	h.nextRetryAt = ""
	if recovered && h.events != nil {
		h.events.Append(
			"transport_recovered",
			"INFO",
			"Google transport connection recovered.",
			nil,
		)
	}
}

func (h *HealthState) Snapshot() HealthSnapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	return HealthSnapshot{
		Status:                h.status,
		Version:               Version,
		AuthorizationRequired: h.authorizationRequired,
		ConsecutiveFailures:   h.consecutiveFailures,
		LastError:             h.lastError,
		LastFailureAt:         h.lastFailureAt,
		LastSuccessAt:         h.lastSuccessAt,
		NextRetryAt:           h.nextRetryAt,
	}
}
