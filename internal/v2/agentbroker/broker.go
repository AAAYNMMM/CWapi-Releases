package agentbroker

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/v2/mcpserver"
)

const (
	DefaultMaxPending     = 16
	DefaultMaxInflight    = 4
	DefaultMaxBatchBytes  = 1024 * 1024
	DefaultRequestTimeout = 180 * time.Second
	DefaultWaitTimeout    = 45 * time.Second
	DefaultBridgeLease    = 2 * time.Minute
	DefaultReceiptTTL     = 5 * time.Minute
)

const (
	StateQueued       = "QUEUED"
	StateClaimed      = "CLAIMED"
	StateCompleted    = "COMPLETED"
	StateCanceled     = "CANCELED"
	StateTimedOut     = "TIMED_OUT"
	StateDisconnected = "DISCONNECTED"
	StateFailed       = "FAILED"
)

type Config struct {
	MaxPending     int
	MaxInflight    int
	MaxBatchBytes  int
	RequestTimeout time.Duration
	WaitTimeout    time.Duration
	BridgeLease    time.Duration
	ReceiptTTL     time.Duration
}

type Snapshot struct {
	BridgeState string `json:"bridge_state"`
	Pending     int    `json:"pending"`
	Claimed     int    `json:"claimed"`
	Active      int    `json:"active"`
	Completed   uint64 `json:"completed"`
	Revision    uint64 `json:"revision"`
	IdleCount   int    `json:"idle_count"`
	LastState   string `json:"last_state,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

type Completion struct {
	Content      any
	ToolCalls    any
	FinishReason string
}

type request struct {
	id            string
	bridgeID      string
	taskID        string
	correlationID string
	payload       map[string]any
	payloadBytes  int
	model         string
	stream        bool
	created       time.Time
	claimed       time.Time
	lastDelivered time.Time
	deadline      time.Time
	state         string
	delivery      int
	result        Completion
	errCode       string
	done          chan struct{}
}

type receipt struct {
	bridgeID    string
	fingerprint string
	expires     time.Time
}

type Broker struct {
	mu sync.Mutex

	cfg            Config
	bridgeID       string
	bridgeDeadline time.Time
	queue          []string
	requests       map[string]*request
	receipts       map[string]receipt
	notify         chan struct{}
	closed         bool
	completed      uint64
	revision       uint64
	lastReported   uint64
	idleCount      int
	lastState      string
	lastError      string
}

func New(cfg Config) *Broker {
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = DefaultMaxPending
	}
	if cfg.MaxInflight <= 0 || cfg.MaxInflight > cfg.MaxPending {
		cfg.MaxInflight = DefaultMaxInflight
		if cfg.MaxInflight > cfg.MaxPending {
			cfg.MaxInflight = cfg.MaxPending
		}
	}
	if cfg.MaxBatchBytes <= 0 {
		cfg.MaxBatchBytes = DefaultMaxBatchBytes
	}
	if cfg.RequestTimeout <= 0 {
		cfg.RequestTimeout = DefaultRequestTimeout
	}
	if cfg.WaitTimeout <= 0 {
		cfg.WaitTimeout = DefaultWaitTimeout
	}
	if cfg.BridgeLease <= cfg.WaitTimeout {
		cfg.BridgeLease = DefaultBridgeLease
	}
	if cfg.ReceiptTTL <= 0 {
		cfg.ReceiptTTL = DefaultReceiptTTL
	}
	return &Broker{
		cfg: cfg, requests: make(map[string]*request), receipts: make(map[string]receipt), notify: make(chan struct{}),
	}
}
func (b *Broker) Open(_ context.Context, _ mcpserver.AgentOpenInput) (mcpserver.AgentOpenOutput, error) {
	if b == nil {
		return mcpserver.AgentOpenOutput{}, errors.New("AGENT_BROKER_UNAVAILABLE")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return mcpserver.AgentOpenOutput{}, errors.New("AGENT_BROKER_CLOSED")
	}
	now := time.Now()
	b.expireRequestsLocked(now)
	b.expireBridgeLocked(now)
	if b.bridgeID != "" {
		b.touchBridgeLocked(now)
		return mcpserver.AgentOpenOutput{State: "ready", Resumed: true, MaxInflight: b.cfg.MaxInflight, Revision: b.revision}, nil
	}
	b.bridgeID = "bridge_" + rand.Text()
	b.touchBridgeLocked(now)
	b.lastReported = 0
	b.idleCount = 0
	b.transitionLocked("READY", "")
	b.signalLocked()
	return mcpserver.AgentOpenOutput{State: "ready", MaxInflight: b.cfg.MaxInflight, Revision: b.revision}, nil
}

func (b *Broker) Exchange(ctx context.Context, input mcpserver.AgentExchangeInput) (mcpserver.AgentExchangeOutput, error) {
	if b == nil {
		return mcpserver.AgentExchangeOutput{}, errors.New("AGENT_BROKER_UNAVAILABLE")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	capacity := input.Capacity
	if capacity <= 0 || capacity > b.cfg.MaxInflight {
		capacity = b.cfg.MaxInflight
	}
	started := time.Now()

	b.mu.Lock()
	now := time.Now()
	b.cleanupReceiptsLocked(now)
	b.expireRequestsLocked(now)
	b.expireBridgeLocked(now)
	if b.closed {
		b.mu.Unlock()
		return mcpserver.AgentExchangeOutput{}, errors.New("AGENT_BROKER_CLOSED")
	}
	if b.bridgeID == "" {
		b.mu.Unlock()
		return mcpserver.AgentExchangeOutput{}, errors.New("AGENT_BRIDGE_NOT_ACTIVE")
	}
	bridgeID := b.bridgeID
	b.mu.Unlock()

	timer := time.NewTimer(b.cfg.WaitTimeout)
	defer timer.Stop()
	var results []mcpserver.AgentExchangeResult
	responsesProcessed := false
	followupExpected := false

	for {
		b.mu.Lock()
		now = time.Now()
		b.cleanupReceiptsLocked(now)
		b.expireRequestsLocked(now)
		b.expireBridgeLocked(now)
		if err := b.requireBridgeLocked(bridgeID); err != nil {
			b.mu.Unlock()
			return mcpserver.AgentExchangeOutput{}, err
		}
		b.touchBridgeLocked(now)
		if !responsesProcessed {
			results, followupExpected = b.acceptResponsesLocked(bridgeID, input.Responses, now)
			responsesProcessed = true
		}
		requests := b.nextBatchLocked(bridgeID, capacity, now)
		if len(requests) > 0 {
			b.touchBridgeLocked(now)
			output := b.exchangeOutputLocked("requests", results, requests, started)
			b.mu.Unlock()
			return output, nil
		}
		if len(results) > 0 && !followupExpected {
			output := b.exchangeOutputLocked("responses", results, nil, started)
			b.mu.Unlock()
			return output, nil
		}
		notify := b.notify
		b.mu.Unlock()

		select {
		case <-ctx.Done():
			return mcpserver.AgentExchangeOutput{}, ctx.Err()
		case <-timer.C:
			b.mu.Lock()
			now = time.Now()
			b.expireRequestsLocked(now)
			b.expireBridgeLocked(now)
			if err := b.requireBridgeLocked(bridgeID); err != nil {
				b.mu.Unlock()
				return mcpserver.AgentExchangeOutput{}, err
			}
			b.touchBridgeLocked(now)
			output := b.exchangeOutputLocked("no_request", results, nil, started)
			b.mu.Unlock()
			return output, nil
		case <-notify:
		}
	}
}
func (b *Broker) Close(_ context.Context, _ mcpserver.AgentCloseInput) (mcpserver.AgentCloseOutput, error) {
	if b == nil {
		return mcpserver.AgentCloseOutput{}, errors.New("AGENT_BROKER_UNAVAILABLE")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.expireRequestsLocked(now)
	b.expireBridgeLocked(now)
	if b.closed {
		return mcpserver.AgentCloseOutput{}, errors.New("AGENT_BROKER_CLOSED")
	}
	if b.bridgeID == "" {
		return mcpserver.AgentCloseOutput{State: "no_active_bridge"}, nil
	}
	b.closeBridgeLocked("AGENT_BRIDGE_CLOSED")
	return mcpserver.AgentCloseOutput{State: "closed"}, nil
}
func (b *Broker) Enqueue(payload map[string]any, model string, stream bool) (*RequestHandle, error) {
	if b == nil {
		return nil, errors.New("AGENT_BROKER_UNAVAILABLE")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errors.New("AGENT_BROKER_CLOSED")
	}
	now := time.Now()
	b.expireRequestsLocked(now)
	b.expireBridgeLocked(now)
	if b.bridgeID == "" {
		return nil, errors.New("AGENT_BRIDGE_UNAVAILABLE")
	}
	if b.activeCountLocked() >= b.cfg.MaxPending {
		return nil, errors.New("AGENT_BUSY")
	}
	payloadCopy := cloneMap(payload)
	payloadJSON, _ := json.Marshal(payloadCopy)
	if len(payloadJSON) > b.cfg.MaxBatchBytes {
		return nil, errors.New("AGENT_REQUEST_TOO_LARGE")
	}
	now = now.UTC()
	requestID := "request_" + rand.Text()
	taskID, correlationID := requestIdentity(payloadCopy)
	req := &request{
		id: requestID, bridgeID: b.bridgeID, taskID: taskID, correlationID: correlationID,
		payload: payloadCopy, payloadBytes: len(payloadJSON),
		model: strings.TrimSpace(model), stream: stream, created: now, deadline: now.Add(b.cfg.RequestTimeout),
		state: StateQueued, done: make(chan struct{}),
	}
	b.requests[req.id] = req
	b.queue = append(b.queue, req.id)
	b.transitionLocked(StateQueued, "")
	b.signalLocked()
	return &RequestHandle{broker: b, id: req.id, done: req.done, deadline: req.deadline}, nil
}
func (b *Broker) Snapshot() Snapshot {
	if b == nil {
		return Snapshot{BridgeState: "OFFLINE"}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.cleanupReceiptsLocked(now)
	b.expireRequestsLocked(now)
	b.expireBridgeLocked(now)
	pending, claimed := 0, 0
	for _, req := range b.requests {
		if req == nil {
			continue
		}
		switch req.state {
		case StateQueued:
			pending++
		case StateClaimed:
			claimed++
		}
	}
	state := "OFFLINE"
	if b.bridgeID != "" {
		state = "READY"
		if claimed > 0 {
			state = "BUSY"
		}
	}
	return Snapshot{
		BridgeState: state, Pending: pending, Claimed: claimed, Active: pending + claimed,
		Completed: b.completed, Revision: b.revision, IdleCount: b.idleCount,
		LastState: b.lastState, LastError: b.lastError,
	}
}

func (b *Broker) Shutdown() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	b.closeBridgeLocked("AGENT_BROKER_CLOSED")
}

func (b *Broker) acceptResponsesLocked(bridgeID string, responses []mcpserver.AgentExchangeResponse, now time.Time) ([]mcpserver.AgentExchangeResult, bool) {
	if len(responses) == 0 {
		return nil, false
	}
	results := make([]mcpserver.AgentExchangeResult, 0, len(responses))
	followupExpected := false
	for _, response := range responses {
		requestID := strings.TrimSpace(response.RequestID)
		result := mcpserver.AgentExchangeResult{RequestID: requestID}
		if requestID == "" || response.Response == nil {
			result.State, result.Error = "rejected", "AGENT_RESPONSE_INVALID"
			results = append(results, result)
			continue
		}
		canonical, err := normalizeCompletion(response.Response, nil)
		if err != nil {
			result.State, result.Error = "rejected", errorCode(err)
			results = append(results, result)
			continue
		}
		fingerprint, fingerprintOK := completionFingerprint(canonical)
		if !fingerprintOK {
			result.State, result.Error = "rejected", "AGENT_RESPONSE_INVALID"
			results = append(results, result)
			continue
		}
		if prior, ok := b.receipts[requestID]; ok {
			if prior.bridgeID == bridgeID && prior.fingerprint == fingerprint {
				result.State = "duplicate"
				followupExpected = followupExpected || canonical.FinishReason == "tool_calls"
			} else {
				result.State, result.Error = "rejected", "AGENT_RESPONSE_CONFLICT"
			}
			results = append(results, result)
			continue
		}
		req := b.requests[requestID]
		if req == nil || req.bridgeID != bridgeID || req.state != StateClaimed || !now.Before(req.deadline) {
			if req != nil && (req.state == StateQueued || req.state == StateClaimed) && !now.Before(req.deadline) {
				b.finishLocked(req, StateTimedOut, "AGENT_REQUEST_TIMEOUT", Completion{})
			}
			result.State, result.Error = "rejected", "REQUEST_NO_LONGER_ACTIVE"
			results = append(results, result)
			continue
		}
		completion, err := normalizeCompletion(response.Response, req.payload)
		if err != nil {
			result.State, result.Error = "rejected", errorCode(err)
			results = append(results, result)
			continue
		}
		b.receipts[requestID] = receipt{bridgeID: bridgeID, fingerprint: fingerprint, expires: now.Add(b.cfg.ReceiptTTL)}
		b.finishLocked(req, StateCompleted, "", completion)
		b.completed++
		followupExpected = followupExpected || completion.FinishReason == "tool_calls"
		result.State = "completed"
		results = append(results, result)
	}
	return results, followupExpected
}

func (b *Broker) nextBatchLocked(bridgeID string, capacity int, now time.Time) []mcpserver.AgentExchangeRequest {
	b.expireRequestsLocked(now)
	claimed := make([]*request, 0, b.cfg.MaxInflight)
	for _, req := range b.requests {
		if req != nil && req.bridgeID == bridgeID && req.state == StateClaimed {
			claimed = append(claimed, req)
		}
	}
	sort.Slice(claimed, func(i, j int) bool {
		if claimed[i].created.Equal(claimed[j].created) {
			return claimed[i].id < claimed[j].id
		}
		return claimed[i].created.Before(claimed[j].created)
	})
	batch := make([]mcpserver.AgentExchangeRequest, 0, capacity)
	batchBytes := 0
	appendRequest := func(req *request) bool {
		if req == nil || len(batch) >= capacity {
			return false
		}
		if batchBytes+req.payloadBytes > b.cfg.MaxBatchBytes {
			return false
		}
		if req.claimed.IsZero() {
			req.claimed = now.UTC()
		}
		req.delivery++
		req.lastDelivered = now.UTC()
		batchBytes += req.payloadBytes
		batch = append(batch, mcpserver.AgentExchangeRequest{
			RequestID: req.id, TaskID: req.taskID, CorrelationID: req.correlationID, State: "claimed",
			Delivery: req.delivery, CreatedAt: req.created.UTC().Format(time.RFC3339Nano),
			ClaimedAt: req.claimed.UTC().Format(time.RFC3339Nano), LastDeliveredAt: req.lastDelivered.UTC().Format(time.RFC3339Nano),
			DeadlineAt: req.deadline.UTC().Format(time.RFC3339Nano), Request: cloneMap(req.payload),
		})
		return true
	}
	for _, req := range claimed {
		if !appendRequest(req) {
			break
		}
	}
	claimedCount := len(claimed)
	for len(batch) < capacity && claimedCount < b.cfg.MaxInflight && len(b.queue) > 0 {
		requestID := b.queue[0]
		req := b.requests[requestID]
		if req == nil || req.state != StateQueued || req.bridgeID != bridgeID {
			b.queue = b.queue[1:]
			continue
		}
		if !now.Before(req.deadline) {
			b.queue = b.queue[1:]
			b.finishLocked(req, StateTimedOut, "AGENT_REQUEST_TIMEOUT", Completion{})
			continue
		}
		if batchBytes+req.payloadBytes > b.cfg.MaxBatchBytes {
			break
		}
		b.queue = b.queue[1:]
		req.state = StateClaimed
		b.transitionLocked(StateClaimed, "")
		claimedCount++
		if !appendRequest(req) {
			break
		}
	}
	return batch
}
func (b *Broker) requireBridgeLocked(bridgeID string) error {
	if b.closed {
		return errors.New("AGENT_BROKER_CLOSED")
	}
	if b.bridgeID == "" {
		return errors.New("AGENT_BRIDGE_NOT_ACTIVE")
	}
	if bridgeID == "" || bridgeID != b.bridgeID {
		return errors.New("AGENT_BRIDGE_STALE_OPERATION")
	}
	return nil
}
func (b *Broker) touchBridgeLocked(now time.Time) {
	if b.bridgeID != "" {
		b.bridgeDeadline = now.Add(b.cfg.BridgeLease)
	}
}

func (b *Broker) expireBridgeLocked(now time.Time) {
	if b.bridgeID == "" || b.bridgeDeadline.IsZero() || now.Before(b.bridgeDeadline) {
		return
	}
	if b.claimedCountLocked(b.bridgeID) > 0 {
		return
	}
	b.closeBridgeLocked("AGENT_BRIDGE_STALE")
}

func (b *Broker) expireRequestsLocked(now time.Time) {
	for _, req := range b.requests {
		if req != nil && (req.state == StateQueued || req.state == StateClaimed) && !now.Before(req.deadline) {
			b.finishLocked(req, StateTimedOut, "AGENT_REQUEST_TIMEOUT", Completion{})
		}
	}
}

func (b *Broker) cleanupReceiptsLocked(now time.Time) {
	for requestID, item := range b.receipts {
		if !now.Before(item.expires) {
			delete(b.receipts, requestID)
		}
	}
}

func (b *Broker) closeBridgeLocked(code string) {
	active := b.bridgeID
	b.bridgeID = ""
	b.bridgeDeadline = time.Time{}
	b.queue = nil
	for _, req := range b.requests {
		if req != nil && req.bridgeID == active && (req.state == StateQueued || req.state == StateClaimed) {
			b.finishLocked(req, StateFailed, code, Completion{})
		}
	}
	b.transitionLocked("OFFLINE", code)
	b.signalLocked()
}

func (b *Broker) activeCountLocked() int {
	count := 0
	for _, req := range b.requests {
		if req != nil && (req.state == StateQueued || req.state == StateClaimed) {
			count++
		}
	}
	return count
}

func (b *Broker) claimedCountLocked(bridgeID string) int {
	count := 0
	for _, req := range b.requests {
		if req != nil && req.bridgeID == bridgeID && req.state == StateClaimed {
			count++
		}
	}
	return count
}

func (b *Broker) finishLocked(req *request, state, code string, completion Completion) {
	if req == nil || req.state == StateCompleted || req.state == StateCanceled || req.state == StateTimedOut || req.state == StateDisconnected || req.state == StateFailed {
		return
	}
	req.state, req.errCode, req.result = state, code, completion
	b.transitionLocked(state, code)
	close(req.done)
	b.signalLocked()
}

func (b *Broker) transitionLocked(state, code string) {
	b.revision++
	b.lastState = state
	b.lastError = code
}

func (b *Broker) exchangeOutputLocked(state string, results []mcpserver.AgentExchangeResult, requests []mcpserver.AgentExchangeRequest, started time.Time) mcpserver.AgentExchangeOutput {
	pending, inflight := 0, 0
	for _, req := range b.requests {
		if req == nil {
			continue
		}
		switch req.state {
		case StateQueued:
			pending++
		case StateClaimed:
			inflight++
		}
	}
	changed := b.revision != b.lastReported
	nextAction := "process_requests"
	switch state {
	case "no_request":
		if changed {
			b.idleCount = 0
			nextAction = "advance_or_finish"
		} else {
			b.idleCount++
			nextAction = "reassess_before_waiting"
		}
	case "responses":
		b.idleCount = 0
		nextAction = "advance_or_finish"
	default:
		b.idleCount = 0
	}
	b.lastReported = b.revision
	waited := time.Since(started).Milliseconds()
	if waited < 0 {
		waited = 0
	}
	return mcpserver.AgentExchangeOutput{
		State: state,
		Activity: mcpserver.AgentExchangeActivity{
			Revision: b.revision, Changed: changed, Pending: pending, Inflight: inflight,
			Active: pending + inflight, IdleCount: b.idleCount, WaitedMillis: waited,
			LastState: b.lastState, LastError: b.lastError, NextAction: nextAction,
		},
		Results: results, Requests: requests,
	}
}

func (b *Broker) compactQueueLocked(removeID string) {
	if len(b.queue) == 0 {
		return
	}
	filtered := b.queue[:0]
	for _, id := range b.queue {
		if id != removeID {
			filtered = append(filtered, id)
		}
	}
	b.queue = filtered
}

func (b *Broker) signalLocked() {
	close(b.notify)
	b.notify = make(chan struct{})
}

func completionFingerprint(value Completion) (string, bool) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", false
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), true
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return nil
	}
	copy := make(map[string]any, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}

func requestIdentity(payload map[string]any) (string, string) {
	metadata, _ := payload["metadata"].(map[string]any)
	if len(metadata) == 0 {
		return "", ""
	}
	return strings.TrimSpace(stringValue(metadata["task_id"])), strings.TrimSpace(stringValue(metadata["correlation_id"]))
}
