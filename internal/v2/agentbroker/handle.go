package agentbroker

import (
	"context"
	"errors"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/v2/agentprotocol"
)

type RequestResult struct {
	ID         string
	Model      string
	Stream     bool
	Created    time.Time
	Completion agentprotocol.Completion
}

type RequestHandle struct {
	broker   *Broker
	id       string
	done     <-chan struct{}
	deadline time.Time
}

func (h *RequestHandle) Wait(ctx context.Context) (RequestResult, error) {
	if h == nil || h.broker == nil || h.id == "" {
		return RequestResult{}, errors.New("AGENT_REQUEST_INVALID")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		deadline, ok := h.currentDeadline()
		if !ok {
			return RequestResult{}, errors.New("REQUEST_NO_LONGER_ACTIVE")
		}
		duration := time.Until(deadline)
		if duration < 0 {
			duration = 0
		}
		timer := time.NewTimer(duration)
		select {
		case <-h.done:
			timer.Stop()
			return h.result()
		case <-ctx.Done():
			timer.Stop()
			h.terminateAndForget(StateDisconnected, "AGENT_CLIENT_DISCONNECTED")
			return RequestResult{}, ctx.Err()
		case <-timer.C:
			current, exists := h.currentDeadline()
			if !exists {
				return RequestResult{}, errors.New("REQUEST_NO_LONGER_ACTIVE")
			}
			if time.Now().Before(current) {
				continue
			}
			h.terminateAndForget(StateTimedOut, "AGENT_REQUEST_TIMEOUT")
			return RequestResult{}, errors.New("AGENT_REQUEST_TIMEOUT")
		}
	}
}

func (h *RequestHandle) currentDeadline() (time.Time, bool) {
	b := h.broker
	b.mu.Lock()
	defer b.mu.Unlock()
	req := b.requests[h.id]
	if req == nil {
		return time.Time{}, false
	}
	return req.deadline, true
}

func (h *RequestHandle) result() (RequestResult, error) {
	b := h.broker
	b.mu.Lock()
	defer b.mu.Unlock()
	req := b.requests[h.id]
	if req == nil {
		return RequestResult{}, errors.New("REQUEST_NO_LONGER_ACTIVE")
	}
	delete(b.requests, h.id)
	b.compactQueueLocked(h.id)
	if req.state != StateCompleted && req.state != StateWaitingTool {
		if req.errCode == "" {
			return RequestResult{}, errors.New("AGENT_REQUEST_FAILED")
		}
		return RequestResult{}, errors.New(req.errCode)
	}
	return RequestResult{ID: req.id, Model: req.model, Stream: req.stream, Created: req.created, Completion: req.result}, nil
}

func (h *RequestHandle) terminateAndForget(state, code string) {
	b := h.broker
	b.mu.Lock()
	defer b.mu.Unlock()
	req := b.requests[h.id]
	if req == nil {
		return
	}
	if isActiveRequestState(req.state) {
		b.finishLocked(req, state, code, Completion{})
		b.lastState = state
	}
	delete(b.requests, h.id)
	b.compactQueueLocked(h.id)
}
