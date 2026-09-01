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
	duration := time.Until(h.deadline)
	if duration < 0 {
		duration = 0
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-h.done:
		return h.result()
	case <-ctx.Done():
		h.terminateAndForget(StateDisconnected, "AGENT_CLIENT_DISCONNECTED")
		return RequestResult{}, ctx.Err()
	case <-timer.C:
		h.terminateAndForget(StateTimedOut, "AGENT_REQUEST_TIMEOUT")
		return RequestResult{}, errors.New("AGENT_REQUEST_TIMEOUT")
	}
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
	if req.state != StateCompleted {
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
	if req.state == StateQueued || req.state == StateClaimed {
		b.finishLocked(req, state, code, Completion{})
		b.lastState = state
	}
	// If bridge shutdown completed first, the HTTP waiter is still gone. The
	// terminal result has no consumer, so remove it instead of retaining it.
	delete(b.requests, h.id)
	b.compactQueueLocked(h.id)
}
