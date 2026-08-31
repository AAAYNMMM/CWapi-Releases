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

	"github.com/AAAYNMMM/CWapi/internal/v2/attachments"
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
	Completed   uint64 `json:"completed"`
	LastState   string `json:"last_state,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

type Completion struct {
	Content      any
	ToolCalls    any
	FinishReason string
}

type request struct {
	id              string
	bridgeID        string
	payload         map[string]any
	payloadBytes    int
	attachmentBytes int64
	attachments     []attachments.Stored
	model           string
	stream          bool
	created         time.Time
	deadline        time.Time
	state           string
	delivery        int
	result          Completion
	errCode         string
	done            chan struct{}
}

type receipt struct {
	bridgeID    string
	fingerprint string
	expires     time.Time
}

type Broker struct {
	mu sync.Mutex

	cfg             Config
	attachmentStore *attachments.Store
	bridgeID        string
	bridgeDeadline  time.Time
	queue           []string
	requests        map[string]*request
	receipts        map[string]receipt
	notify          chan struct{}
	closed          bool
	completed       uint64
	lastState       string
	lastError       string
}

func New(cfg Config) *Broker {
	return NewWithAttachmentStore(cfg, nil)
}

func NewWithAttachmentStore(cfg Config, store *attachments.Store) *Broker {
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
		cfg: cfg, attachmentStore: store, requests: make(map[string]*request), receipts: make(map[string]receipt), notify: make(chan struct{}),
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
		b.lastState, b.lastError = "READY", ""
		return mcpserver.AgentOpenOutput{State: "ready", Resumed: true, MaxInflight: b.cfg.MaxInflight}, nil
	}
	b.bridgeID = "bridge_" + rand.Text()
	b.touchBridgeLocked(now)
	b.lastState, b.lastError = "READY", ""
	b.signalLocked()
	return mcpserver.AgentOpenOutput{State: "ready", MaxInflight: b.cfg.MaxInflight}, nil
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
			results = b.acceptResponsesLocked(bridgeID, input.Responses, now)
			responsesProcessed = true
		}
		requests := b.nextBatchLocked(bridgeID, capacity, now)
		if len(requests) > 0 {
			b.touchBridgeLocked(now)
			b.mu.Unlock()
			return mcpserver.AgentExchangeOutput{State: "requests", Results: results, Requests: requests}, nil
		}
		if len(results) > 0 && b.activeCountLocked() == 0 {
			b.mu.Unlock()
			return mcpserver.AgentExchangeOutput{State: "no_request", Results: results}, nil
		}
		b.lastState = "READY"
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
			b.mu.Unlock()
			return mcpserver.AgentExchangeOutput{State: "no_request", Results: results}, nil
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
	return b.EnqueueWithAttachments(payload, model, stream, attachments.Batch{})
}

func (b *Broker) EnqueueWithAttachments(payload map[string]any, model string, stream bool, batch attachments.Batch) (*RequestHandle, error) {
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
	if batch.TotalBytes > attachments.AgentPolicy().MaxBatchBytes {
		return nil, errors.New("ATTACHMENT_BATCH_TOO_LARGE")
	}
	if len(batch.Items) > 0 && b.attachmentStore == nil {
		return nil, errors.New("AGENT_ATTACHMENT_STORE_UNAVAILABLE")
	}
	now = now.UTC()
	requestID := "request_" + rand.Text()
	var stored []attachments.Stored
	if len(batch.Items) > 0 {
		var err error
		stored, err = b.attachmentStore.Put(requestID, batch)
		if err != nil {
			return nil, err
		}
	}
	req := &request{
		id: requestID, bridgeID: b.bridgeID, payload: payloadCopy, payloadBytes: len(payloadJSON),
		attachmentBytes: batch.TotalBytes, attachments: stored,
		model: strings.TrimSpace(model), stream: stream, created: now, deadline: now.Add(b.cfg.RequestTimeout),
		state: StateQueued, done: make(chan struct{}),
	}
	b.requests[req.id] = req
	b.queue = append(b.queue, req.id)
	b.lastState = StateQueued
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
	return Snapshot{BridgeState: state, Pending: pending, Claimed: claimed, Completed: b.completed, LastState: b.lastState, LastError: b.lastError}
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
	if b.attachmentStore != nil {
		if err := b.attachmentStore.Close(); err != nil {
			b.lastError = "AGENT_ATTACHMENT_CLEANUP_FAILED"
		}
	}
}

func (b *Broker) acceptResponsesLocked(bridgeID string, responses []mcpserver.AgentExchangeResponse, now time.Time) []mcpserver.AgentExchangeResult {
	if len(responses) == 0 {
		return nil
	}
	results := make([]mcpserver.AgentExchangeResult, 0, len(responses))
	for _, response := range responses {
		requestID := strings.TrimSpace(response.RequestID)
		result := mcpserver.AgentExchangeResult{RequestID: requestID}
		fingerprint, fingerprintOK := responseFingerprint(response.Response)
		if requestID == "" || response.Response == nil || !fingerprintOK {
			result.State, result.Error = "rejected", "AGENT_RESPONSE_INVALID"
			results = append(results, result)
			continue
		}
		if prior, ok := b.receipts[requestID]; ok {
			if prior.bridgeID == bridgeID && prior.fingerprint == fingerprint {
				result.State = "duplicate"
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
		b.lastState = StateCompleted
		result.State = "completed"
		results = append(results, result)
	}
	return results
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
	attachmentBytes := int64(0)
	attachmentLimit := attachments.AgentPolicy().MaxBatchBytes
	appendRequest := func(req *request) bool {
		if req == nil || len(batch) >= capacity {
			return false
		}
		if batchBytes+req.payloadBytes > b.cfg.MaxBatchBytes || attachmentBytes+req.attachmentBytes > attachmentLimit {
			return false
		}
		var contentItems []attachments.Item
		var metadata []attachments.Metadata
		if len(req.attachments) > 0 {
			if b.attachmentStore == nil {
				b.finishLocked(req, StateFailed, "AGENT_ATTACHMENT_STORE_UNAVAILABLE", Completion{})
				return false
			}
			loaded, err := b.attachmentStore.Load(req.attachments)
			if err != nil {
				b.finishLocked(req, StateFailed, "AGENT_ATTACHMENT_READ_FAILED", Completion{})
				return false
			}
			contentItems = loaded
			metadata = make([]attachments.Metadata, 0, len(loaded))
			for _, item := range loaded {
				metadata = append(metadata, item.Metadata)
			}
		}
		req.delivery++
		batchBytes += req.payloadBytes
		attachmentBytes += req.attachmentBytes
		batch = append(batch, mcpserver.AgentExchangeRequest{
			RequestID: req.id, Delivery: req.delivery, DeadlineAt: req.deadline.UTC().Format(time.RFC3339Nano),
			Request: cloneMap(req.payload), Attachments: metadata, ContentItems: contentItems,
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
		if batchBytes+req.payloadBytes > b.cfg.MaxBatchBytes || attachmentBytes+req.attachmentBytes > attachmentLimit {
			break
		}
		b.queue = b.queue[1:]
		req.state = StateClaimed
		claimedCount++
		if !appendRequest(req) {
			break
		}
	}
	if len(batch) > 0 {
		b.lastState = StateClaimed
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
	b.lastState, b.lastError = "OFFLINE", code
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
	if len(req.attachments) > 0 && b.attachmentStore != nil {
		if err := b.attachmentStore.Remove(req.id); err != nil {
			b.lastError = "AGENT_ATTACHMENT_CLEANUP_FAILED"
		}
		req.attachments = nil
		req.attachmentBytes = 0
	}
	if code != "" {
		b.lastError = code
	}
	close(req.done)
	b.signalLocked()
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

func responseFingerprint(value map[string]any) (string, bool) {
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
