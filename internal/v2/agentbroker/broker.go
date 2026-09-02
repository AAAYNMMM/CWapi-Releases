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

	"github.com/AAAYNMMM/CWapi/internal/v2/agentprotocol"
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
	DefaultHeartbeat      = 15 * time.Second
)

const (
	StateQueued          = "QUEUED"
	StateClaimed         = "CLAIMED"
	StateRunning         = "RUNNING"
	StateWaitingTool     = "WAITING_TOOL"
	StateCompleted       = "COMPLETED"
	StateFailedRetryable = "FAILED_RETRYABLE"
	StateFailedFinal     = "FAILED_FINAL"
	StateCanceled        = "CANCELED"
	StateTimedOut        = "TIMED_OUT"
	StateDisconnected    = "DISCONNECTED"
	StateFailed          = StateFailedFinal
)

type Config struct {
	MaxPending     int
	MaxInflight    int
	MaxBatchBytes  int
	RequestTimeout time.Duration
	WaitTimeout    time.Duration
	BridgeLease    time.Duration
	ReceiptTTL     time.Duration
	Heartbeat      time.Duration
}

type Snapshot struct {
	BridgeState     string `json:"bridge_state"`
	Pending         int    `json:"pending"`
	Claimed         int    `json:"claimed"`
	Active          int    `json:"active"`
	Completed       uint64 `json:"completed"`
	Revision        uint64 `json:"revision"`
	IdleCount       int    `json:"idle_count"`
	LastState       string `json:"last_state,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
	LastProgress    string `json:"last_progress,omitempty"`
}

type Completion = agentprotocol.Completion

type request struct {
	id            string
	bridgeID      string
	taskID        string
	correlationID string
	conversation  agentprotocol.Conversation
	payload       map[string]any
	payloadBytes  int
	model         string
	stream        bool
	created       time.Time
	claimed       time.Time
	lastDelivered time.Time
	lastActivity  time.Time
	deadline      time.Time
	state         string
	previousState string
	resumeReason  string
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
	lastHeartbeat  time.Time
	lastProgress   string
	stopHeartbeat  chan struct{}
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
	if cfg.Heartbeat <= 0 {
		cfg.Heartbeat = DefaultHeartbeat
	}
	broker := &Broker{
		cfg: cfg, requests: make(map[string]*request), receipts: make(map[string]receipt), notify: make(chan struct{}), stopHeartbeat: make(chan struct{}),
	}
	go broker.heartbeatLoop()
	return broker
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
		b.heartbeatLocked(now)
		return mcpserver.AgentOpenOutput{State: "ready", Resumed: true, MaxInflight: b.cfg.MaxInflight, Revision: b.revision}, nil
	}
	resumed := b.activeCountLocked() > 0
	b.bridgeID = "bridge_" + rand.Text()
	for _, req := range b.requests {
		if req == nil || !isActiveRequestState(req.state) {
			continue
		}
		if req.delivery > 0 {
			req.previousState = req.state
			if req.resumeReason == "" {
				req.resumeReason = "bridge_resumed"
			}
		}
		req.bridgeID = b.bridgeID
	}
	b.touchBridgeLocked(now)
	b.heartbeatLocked(now)
	b.lastReported = 0
	b.idleCount = 0
	b.transitionLocked("READY", "")
	b.signalLocked()
	return mcpserver.AgentOpenOutput{State: "ready", Resumed: resumed, MaxInflight: b.cfg.MaxInflight, Revision: b.revision}, nil
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
	var events []mcpserver.AgentEvent
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
			var responseEvents []mcpserver.AgentEvent
			results, followupExpected, responseEvents = b.acceptResponsesLocked(bridgeID, input.Responses, now)
			events = append(events, responseEvents...)
			events = append(events, b.acceptProgressLocked(bridgeID, input.Progress, now)...)
			responsesProcessed = true
		}
		requests := b.nextBatchLocked(bridgeID, capacity, now)
		if len(requests) > 0 {
			b.touchBridgeLocked(now)
			output := b.exchangeOutputLocked("requests", results, requests, events, started)
			b.mu.Unlock()
			return output, nil
		}
		if len(results) > 0 && !followupExpected {
			output := b.exchangeOutputLocked("responses", results, nil, events, started)
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
			output := b.exchangeOutputLocked("no_request", results, nil, events, started)
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
func (b *Broker) Enqueue(conversation agentprotocol.Conversation) (*RequestHandle, error) {
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
	payloadCopy, err := agentprotocol.EncodeBridgeRequest(conversation)
	if err != nil {
		return nil, err
	}
	payloadJSON, _ := json.Marshal(payloadCopy)
	if len(payloadJSON) > b.cfg.MaxBatchBytes {
		return nil, errors.New("AGENT_REQUEST_TOO_LARGE")
	}
	now = now.UTC()
	requestID := "request_" + rand.Text()
	taskID, correlationID := requestIdentity(conversation.Metadata)
	req := &request{
		id: requestID, bridgeID: b.bridgeID, taskID: taskID, correlationID: correlationID,
		conversation: conversation, payload: payloadCopy, payloadBytes: len(payloadJSON),
		model: strings.TrimSpace(conversation.Model), stream: conversation.Stream, created: now, lastActivity: now, deadline: now.Add(b.cfg.RequestTimeout),
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
		if req.state == StateQueued {
			pending++
		} else if isInflightRequestState(req.state) {
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
	lastHeartbeat := ""
	if !b.lastHeartbeat.IsZero() {
		lastHeartbeat = b.lastHeartbeat.UTC().Format(time.RFC3339Nano)
	}
	return Snapshot{
		BridgeState: state, Pending: pending, Claimed: claimed, Active: pending + claimed,
		Completed: b.completed, Revision: b.revision, IdleCount: b.idleCount,
		LastState: b.lastState, LastError: b.lastError, LastHeartbeatAt: lastHeartbeat, LastProgress: b.lastProgress,
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
	close(b.stopHeartbeat)
	for _, req := range b.requests {
		if req != nil && isActiveRequestState(req.state) {
			b.finishLocked(req, StateFailedFinal, "AGENT_BROKER_CLOSED", Completion{})
		}
	}
	b.closeBridgeLocked("AGENT_BROKER_CLOSED")
}

func (b *Broker) acceptResponsesLocked(bridgeID string, responses []mcpserver.AgentExchangeResponse, now time.Time) ([]mcpserver.AgentExchangeResult, bool, []mcpserver.AgentEvent) {
	if len(responses) == 0 {
		return nil, false, nil
	}
	results := make([]mcpserver.AgentExchangeResult, 0, len(responses))
	events := make([]mcpserver.AgentEvent, 0, len(responses))
	followupExpected := false
	for _, response := range responses {
		requestID := strings.TrimSpace(response.RequestID)
		result := mcpserver.AgentExchangeResult{RequestID: requestID}
		if requestID == "" || response.Response == nil {
			result.State, result.Error = "rejected", "AGENT_RESPONSE_INVALID"
			result.Detail = responseErrorDetail(errors.New(result.Error), requestID, response.Response, true)
			results = append(results, result)
			continue
		}
		canonical, err := agentprotocol.DecodeBridgeCompletion(response.Response, nil)
		if err != nil {
			code := errorCode(err)
			result.State, result.Error = "rejected", code
			result.Detail = responseErrorDetail(err, requestID, response.Response, true)
			if req := b.requests[requestID]; req != nil && req.bridgeID == bridgeID && isActiveRequestState(req.state) {
				b.markRetryableLocked(req, code, now)
			}
			events = append(events, errorEvent(result.Detail, now))
			results = append(results, result)
			continue
		}
		fingerprint, fingerprintOK := completionFingerprint(canonical)
		if !fingerprintOK {
			result.State, result.Error = "rejected", "AGENT_RESPONSE_INVALID"
			result.Detail = responseErrorDetail(errors.New(result.Error), requestID, response.Response, true)
			if req := b.requests[requestID]; req != nil && req.bridgeID == bridgeID && isActiveRequestState(req.state) {
				b.markRetryableLocked(req, result.Error, now)
			}
			events = append(events, errorEvent(result.Detail, now))
			results = append(results, result)
			continue
		}
		if prior, ok := b.receipts[requestID]; ok {
			if prior.bridgeID == bridgeID && prior.fingerprint == fingerprint {
				result.State = "duplicate"
				followupExpected = followupExpected || canonical.FinishReason == "tool_calls"
			} else {
				result.State, result.Error = "rejected", "AGENT_RESPONSE_CONFLICT"
				result.Detail = responseErrorDetail(errors.New(result.Error), requestID, response.Response, false)
			}
			results = append(results, result)
			continue
		}
		req := b.requests[requestID]
		if req == nil || req.bridgeID != bridgeID || !isActiveRequestState(req.state) || !now.Before(req.deadline) {
			if req != nil && isActiveRequestState(req.state) && !now.Before(req.deadline) {
				b.finishLocked(req, StateTimedOut, "AGENT_REQUEST_TIMEOUT", Completion{})
			}
			result.State, result.Error = "rejected", "REQUEST_NO_LONGER_ACTIVE"
			result.Detail = responseErrorDetail(errors.New(result.Error), requestID, response.Response, false)
			results = append(results, result)
			continue
		}
		completion, err := agentprotocol.DecodeBridgeCompletion(response.Response, &req.conversation)
		if err != nil {
			code := errorCode(err)
			result.State, result.Error = "rejected", code
			result.Detail = responseErrorDetail(err, requestID, response.Response, true)
			b.markRetryableLocked(req, code, now)
			events = append(events, errorEvent(result.Detail, now))
			results = append(results, result)
			continue
		}
		expectedEvent := "completion"
		if completion.FinishReason == "tool_calls" {
			expectedEvent = "tool_call"
		}
		if supplied := strings.TrimSpace(response.Event); supplied != "" && supplied != expectedEvent {
			err := errors.New("AGENT_RESPONSE_EVENT_MISMATCH")
			result.State, result.Error = "rejected", err.Error()
			result.Detail = responseErrorDetail(err, requestID, response.Response, true)
			b.markRetryableLocked(req, result.Error, now)
			events = append(events, errorEvent(result.Detail, now))
			results = append(results, result)
			continue
		}
		b.receipts[requestID] = receipt{bridgeID: bridgeID, fingerprint: fingerprint, expires: now.Add(b.cfg.ReceiptTTL)}
		if completion.FinishReason == "tool_calls" {
			b.finishLocked(req, StateWaitingTool, "", completion)
			for _, call := range completion.ToolCalls {
				events = append(events, mcpserver.AgentEvent{Type: "tool_call", RequestID: requestID, ToolCallID: call.ID, ToolName: call.Name, At: now.UTC().Format(time.RFC3339Nano)})
			}
			followupExpected = true
		} else {
			b.finishLocked(req, StateCompleted, "", completion)
			events = append(events, mcpserver.AgentEvent{Type: "completion", RequestID: requestID, At: now.UTC().Format(time.RFC3339Nano)})
		}
		b.completed++
		result.State = "completed"
		results = append(results, result)
	}
	return results, followupExpected, events
}

func (b *Broker) nextBatchLocked(bridgeID string, capacity int, now time.Time) []mcpserver.AgentExchangeRequest {
	b.expireRequestsLocked(now)
	inflight := make([]*request, 0, b.cfg.MaxInflight)
	for _, req := range b.requests {
		if req != nil && req.bridgeID == bridgeID && isInflightRequestState(req.state) {
			inflight = append(inflight, req)
		}
	}
	sort.Slice(inflight, func(i, j int) bool {
		if inflight[i].created.Equal(inflight[j].created) {
			return inflight[i].id < inflight[j].id
		}
		return inflight[i].created.Before(inflight[j].created)
	})
	batch := make([]mcpserver.AgentExchangeRequest, 0, capacity)
	batchBytes := 0
	appendRequest := func(req *request) bool {
		if req == nil || len(batch) >= capacity || batchBytes+req.payloadBytes > b.cfg.MaxBatchBytes {
			return false
		}
		if req.claimed.IsZero() {
			req.claimed = now.UTC()
		}
		previousState, resumeReason := req.previousState, req.resumeReason
		if req.delivery > 0 {
			previousState = req.state
			if resumeReason == "" {
				resumeReason = "redelivery"
			}
		}
		req.delivery++
		req.lastDelivered = now.UTC()
		req.lastActivity = now.UTC()
		req.deadline = now.Add(b.cfg.RequestTimeout).UTC()
		if req.state != StateRunning {
			req.previousState = req.state
			req.state = StateRunning
			b.transitionLocked(StateRunning, "")
		}
		batchBytes += req.payloadBytes
		batch = append(batch, mcpserver.AgentExchangeRequest{
			RequestID: req.id, TaskID: req.taskID, CorrelationID: req.correlationID, State: "claimed", LifecycleState: req.state,
			Delivery: req.delivery, PreviousState: previousState, ResumeReason: resumeReason,
			CreatedAt: req.created.UTC().Format(time.RFC3339Nano), ClaimedAt: req.claimed.UTC().Format(time.RFC3339Nano),
			LastDeliveredAt: req.lastDelivered.UTC().Format(time.RFC3339Nano), LastActivity: req.lastActivity.UTC().Format(time.RFC3339Nano),
			DeadlineAt: req.deadline.UTC().Format(time.RFC3339Nano), Event: requestEvent(req.conversation), Request: cloneMap(req.payload),
		})
		return true
	}
	for _, req := range inflight {
		if !appendRequest(req) {
			break
		}
	}
	inflightCount := len(inflight)
	for len(batch) < capacity && inflightCount < b.cfg.MaxInflight && len(b.queue) > 0 {
		requestID := b.queue[0]
		req := b.requests[requestID]
		if req == nil || req.state != StateQueued || (req.bridgeID != "" && req.bridgeID != bridgeID) {
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
		req.bridgeID = bridgeID
		req.previousState = req.state
		req.state = StateClaimed
		b.transitionLocked(StateClaimed, "")
		inflightCount++
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

func (b *Broker) heartbeatLoop() {
	if b == nil {
		return
	}
	ticker := time.NewTicker(b.cfg.Heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.mu.Lock()
			if b.closed {
				b.mu.Unlock()
				return
			}
			now := time.Now().UTC()
			if b.bridgeID != "" && b.activeCountLocked() > 0 {
				b.heartbeatLocked(now)
			} else {
				b.expireRequestsLocked(now)
				b.expireBridgeLocked(now)
			}
			b.mu.Unlock()
		case <-b.stopHeartbeat:
			return
		}
	}
}

func (b *Broker) heartbeatLocked(now time.Time) {
	if b.bridgeID == "" {
		return
	}
	now = now.UTC()
	b.lastHeartbeat = now
	b.touchBridgeLocked(now)
	for _, req := range b.requests {
		if req == nil || !isActiveRequestState(req.state) {
			continue
		}
		if req.bridgeID == "" {
			req.bridgeID = b.bridgeID
		}
		if req.bridgeID != b.bridgeID {
			continue
		}
		req.lastActivity = now
		req.deadline = now.Add(b.cfg.RequestTimeout)
	}
}

func (b *Broker) acceptProgressLocked(bridgeID string, progress []mcpserver.AgentProgress, now time.Time) []mcpserver.AgentEvent {
	if len(progress) == 0 {
		return nil
	}
	events := make([]mcpserver.AgentEvent, 0, len(progress))
	for _, item := range progress {
		requestID := strings.TrimSpace(item.RequestID)
		message := strings.TrimSpace(item.Message)
		req := b.requests[requestID]
		if requestID == "" || message == "" || req == nil || req.bridgeID != bridgeID || !isActiveRequestState(req.state) {
			continue
		}
		req.lastActivity = now.UTC()
		req.deadline = now.Add(b.cfg.RequestTimeout).UTC()
		if req.state == StateClaimed {
			req.previousState = req.state
			req.state = StateRunning
			b.transitionLocked(StateRunning, "")
		}
		b.lastProgress = message
		events = append(events, mcpserver.AgentEvent{Type: "progress", RequestID: requestID, Message: message, At: now.UTC().Format(time.RFC3339Nano)})
	}
	return events
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
		if req != nil && isActiveRequestState(req.state) && !now.Before(req.deadline) {
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
	for _, req := range b.requests {
		if req == nil || req.bridgeID != active || !isActiveRequestState(req.state) {
			continue
		}
		req.previousState = req.state
		req.resumeReason = code
		req.bridgeID = ""
	}
	b.transitionLocked("OFFLINE", code)
	b.signalLocked()
}

func (b *Broker) activeCountLocked() int {
	count := 0
	for _, req := range b.requests {
		if req != nil && isActiveRequestState(req.state) {
			count++
		}
	}
	return count
}

func (b *Broker) claimedCountLocked(bridgeID string) int {
	count := 0
	for _, req := range b.requests {
		if req != nil && req.bridgeID == bridgeID && isInflightRequestState(req.state) {
			count++
		}
	}
	return count
}

func (b *Broker) markRetryableLocked(req *request, code string, now time.Time) {
	if req == nil || !isActiveRequestState(req.state) {
		return
	}
	req.previousState = req.state
	req.state = StateFailedRetryable
	req.errCode = code
	req.lastActivity = now.UTC()
	req.deadline = now.Add(b.cfg.RequestTimeout).UTC()
	req.resumeReason = "retry_after_error"
	b.transitionLocked(StateFailedRetryable, code)
	b.signalLocked()
}

func (b *Broker) finishLocked(req *request, state, code string, completion Completion) {
	if req == nil || isTerminalRequestState(req.state) {
		return
	}
	req.previousState = req.state
	req.state, req.errCode, req.result = state, code, completion
	req.lastActivity = time.Now().UTC()
	b.transitionLocked(state, code)
	close(req.done)
	b.signalLocked()
}

func (b *Broker) transitionLocked(state, code string) {
	b.revision++
	b.lastState = state
	b.lastError = code
}

func (b *Broker) exchangeOutputLocked(state string, results []mcpserver.AgentExchangeResult, requests []mcpserver.AgentExchangeRequest, events []mcpserver.AgentEvent, started time.Time) mcpserver.AgentExchangeOutput {
	pending, inflight := 0, 0
	for _, req := range b.requests {
		if req == nil {
			continue
		}
		if req.state == StateQueued {
			pending++
		} else if isInflightRequestState(req.state) {
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
	lastHeartbeat := ""
	if !b.lastHeartbeat.IsZero() {
		lastHeartbeat = b.lastHeartbeat.UTC().Format(time.RFC3339Nano)
	}
	return mcpserver.AgentExchangeOutput{
		State: state,
		Activity: mcpserver.AgentExchangeActivity{
			Revision: b.revision, Changed: changed, Pending: pending, Inflight: inflight,
			Active: pending + inflight, QueuedRequests: pending, ActiveRequests: pending + inflight,
			IdleCount: b.idleCount, WaitedMillis: waited, LastState: b.lastState, LastError: b.lastError,
			LastHeartbeatAt: lastHeartbeat, LastProgress: b.lastProgress, NextAction: nextAction,
		},
		Results: results, Requests: requests, Events: events,
	}
}

func isActiveRequestState(state string) bool {
	switch state {
	case StateQueued, StateClaimed, StateRunning, StateFailedRetryable:
		return true
	default:
		return false
	}
}

func isInflightRequestState(state string) bool {
	switch state {
	case StateClaimed, StateRunning, StateFailedRetryable:
		return true
	default:
		return false
	}
}

func isTerminalRequestState(state string) bool {
	switch state {
	case StateWaitingTool, StateCompleted, StateFailedFinal, StateCanceled, StateTimedOut, StateDisconnected:
		return true
	default:
		return false
	}
}

func requestEvent(conversation agentprotocol.Conversation) string {
	for index := len(conversation.Messages) - 1; index >= 0; index-- {
		if conversation.Messages[index].Role == agentprotocol.RoleTool {
			return "tool_result"
		}
		if conversation.Messages[index].Role == agentprotocol.RoleUser || conversation.Messages[index].Role == agentprotocol.RoleAssistant {
			break
		}
	}
	return "request"
}

func responseErrorDetail(err error, requestID string, response map[string]any, retryable bool) *mcpserver.AgentStructuredError {
	code := errorCode(err)
	if code == "" {
		code = "AGENT_RESPONSE_INVALID"
	}
	message := code
	if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}
	callID, toolName := responseToolIdentity(response)
	return &mcpserver.AgentStructuredError{
		Code: code, Message: message, RequestID: requestID, ToolCallID: callID, ToolName: toolName, Retryable: retryable,
	}
}

func responseToolIdentity(response map[string]any) (string, string) {
	if response == nil {
		return "", ""
	}
	message := response
	if nested, ok := response["message"].(map[string]any); ok {
		message = nested
	}
	calls, _ := message["tool_calls"].([]any)
	if len(calls) == 0 {
		return "", ""
	}
	call, _ := calls[0].(map[string]any)
	function, _ := call["function"].(map[string]any)
	return strings.TrimSpace(textValue(call["id"])), strings.TrimSpace(textValue(function["name"]))
}

func errorEvent(detail *mcpserver.AgentStructuredError, now time.Time) mcpserver.AgentEvent {
	if detail == nil {
		return mcpserver.AgentEvent{Type: "error", At: now.UTC().Format(time.RFC3339Nano)}
	}
	return mcpserver.AgentEvent{
		Type: "error", RequestID: detail.RequestID, ToolCallID: detail.ToolCallID, ToolName: detail.ToolName,
		Code: detail.Code, Message: detail.Message, Retryable: detail.Retryable, At: now.UTC().Format(time.RFC3339Nano),
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

func requestIdentity(metadata map[string]any) (string, string) {
	if len(metadata) == 0 {
		return "", ""
	}
	return strings.TrimSpace(textValue(metadata["task_id"])), strings.TrimSpace(textValue(metadata["correlation_id"]))
}

func textValue(value any) string {
	text, _ := value.(string)
	return text
}
