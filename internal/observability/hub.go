package observability

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/state"
)

const (
	defaultLiveLimit       = 250
	defaultPersistentLimit = 5000
	maxSubscriberBuffer    = 100
)

type ExecutionInput struct {
	Timestamp  int64
	TaskID     string
	StepID     string
	Kind       string
	Status     string
	Message    string
	DurationMS int64
	Data       map[string]any
}

type RuntimeInput struct {
	Timestamp int64
	Level     string
	Component string
	Message   string
	Fields    map[string]any
}

type ComponentSnapshot struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Detail    string `json:"detail"`
	UpdatedAt int64  `json:"updated_at"`
}

type Snapshot struct {
	StatePath           string                       `json:"state_path"`
	StateSchema         string                       `json:"state_schema"`
	StructuredExecution []state.ExecutionEventRecord `json:"structured_execution"`
	RuntimeLogs         []state.RuntimeLogRecord     `json:"runtime_logs"`
	Components          []ComponentSnapshot          `json:"components"`
}

// Hub owns bounded live observability state while Store owns durable history.
// Structured execution and CWapi runtime logs remain separate channels and
// separate buffers all the way through this boundary.
type Hub struct {
	mu              sync.RWMutex
	store           *state.Store
	liveLimit       int
	persistentLimit int
	stateSchema     string
	execution       []state.ExecutionEventRecord
	runtimeLogs     []state.RuntimeLogRecord
	components      map[string]ComponentSnapshot

	nextSubscriber       uint64
	executionSubscribers map[uint64]chan state.ExecutionEventRecord
	runtimeSubscribers   map[uint64]chan state.RuntimeLogRecord
}

func New(ctx context.Context, store *state.Store, liveLimit, persistentLimit int) (*Hub, error) {
	if store == nil {
		return nil, fmt.Errorf("OBSERVABILITY_STATE_STORE_REQUIRED")
	}
	if liveLimit < 1 {
		liveLimit = defaultLiveLimit
	}
	if persistentLimit < liveLimit {
		persistentLimit = defaultPersistentLimit
	}
	schema, err := store.SchemaVersion(ctx)
	if err != nil {
		return nil, err
	}
	execution, err := store.RecentExecutionEvents(ctx, liveLimit)
	if err != nil {
		return nil, err
	}
	runtimeLogs, err := store.RecentRuntimeLogs(ctx, liveLimit)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	return &Hub{
		store:                store,
		liveLimit:            liveLimit,
		persistentLimit:      persistentLimit,
		stateSchema:          schema,
		execution:            append([]state.ExecutionEventRecord(nil), execution...),
		runtimeLogs:          append([]state.RuntimeLogRecord(nil), runtimeLogs...),
		executionSubscribers: make(map[uint64]chan state.ExecutionEventRecord),
		runtimeSubscribers:   make(map[uint64]chan state.RuntimeLogRecord),
		components: map[string]ComponentSnapshot{
			"state": {Name: "state", State: "healthy", Detail: "SQLite ready", UpdatedAt: now},
		},
	}, nil
}

func (h *Hub) EmitExecution(ctx context.Context, input ExecutionInput) (state.ExecutionEventRecord, error) {
	timestamp := input.Timestamp
	if timestamp == 0 {
		timestamp = time.Now().UnixMilli()
	}
	record := state.ExecutionEventRecord{
		Timestamp:  timestamp,
		TaskID:     Redact(input.TaskID),
		StepID:     Redact(input.StepID),
		Kind:       Redact(input.Kind),
		Status:     Redact(input.Status),
		Message:    Redact(input.Message),
		DurationMS: input.DurationMS,
		DataJSON:   redactJSON(input.Data),
	}
	persisted, err := h.store.AppendExecutionEvent(ctx, record)
	if err != nil {
		return state.ExecutionEventRecord{}, err
	}

	h.mu.Lock()
	h.execution = appendBounded(h.execution, persisted, h.liveLimit)
	h.publishExecutionLocked(persisted)
	h.mu.Unlock()
	_ = h.store.PruneObservability(ctx, h.persistentLimit, h.persistentLimit)
	return persisted, nil
}

func (h *Hub) LogRuntime(ctx context.Context, input RuntimeInput) (state.RuntimeLogRecord, error) {
	timestamp := input.Timestamp
	if timestamp == 0 {
		timestamp = time.Now().UnixMilli()
	}
	message := Redact(input.Message)
	record := state.RuntimeLogRecord{
		Timestamp:  timestamp,
		Level:      Redact(input.Level),
		Component:  Redact(input.Component),
		Message:    message,
		FieldsJSON: redactJSON(input.Fields),
	}
	if record.Level == "error" || record.Level == "fatal" {
		record.Fingerprint = fingerprint(record.Component, "runtime", message)
	}
	persisted, err := h.store.AppendRuntimeLog(ctx, record)
	if err != nil {
		return state.RuntimeLogRecord{}, err
	}

	h.mu.Lock()
	h.runtimeLogs = appendBounded(h.runtimeLogs, persisted, h.liveLimit)
	h.publishRuntimeLocked(persisted)
	h.mu.Unlock()
	_ = h.store.PruneObservability(ctx, h.persistentLimit, h.persistentLimit)
	return persisted, nil
}

// SubscribeExecution returns a bounded non-blocking live stream. If a
// subscriber falls behind, its oldest buffered item is dropped in favour of
// the newest event; producers are never allowed to block task execution.
func (h *Hub) SubscribeExecution(buffer int) (<-chan state.ExecutionEventRecord, func()) {
	buffer = subscriberBuffer(buffer)
	channel := make(chan state.ExecutionEventRecord, buffer)
	h.mu.Lock()
	h.nextSubscriber++
	id := h.nextSubscriber
	h.executionSubscribers[id] = channel
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if existing, ok := h.executionSubscribers[id]; ok {
				delete(h.executionSubscribers, id)
				close(existing)
			}
			h.mu.Unlock()
		})
	}
	return channel, cancel
}

// SubscribeRuntime is the independent bounded stream for CWapi runtime logs.
func (h *Hub) SubscribeRuntime(buffer int) (<-chan state.RuntimeLogRecord, func()) {
	buffer = subscriberBuffer(buffer)
	channel := make(chan state.RuntimeLogRecord, buffer)
	h.mu.Lock()
	h.nextSubscriber++
	id := h.nextSubscriber
	h.runtimeSubscribers[id] = channel
	h.mu.Unlock()
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if existing, ok := h.runtimeSubscribers[id]; ok {
				delete(h.runtimeSubscribers, id)
				close(existing)
			}
			h.mu.Unlock()
		})
	}
	return channel, cancel
}

func (h *Hub) SetComponent(name, stateValue, detail string) {
	name = Redact(name)
	now := time.Now().UnixMilli()
	h.mu.Lock()
	h.components[name] = ComponentSnapshot{
		Name:      name,
		State:     Redact(stateValue),
		Detail:    Redact(detail),
		UpdatedAt: now,
	}
	h.mu.Unlock()
}

func (h *Hub) Snapshot() Snapshot {
	h.mu.RLock()
	defer h.mu.RUnlock()

	components := make([]ComponentSnapshot, 0, len(h.components))
	for _, component := range h.components {
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool { return components[i].Name < components[j].Name })

	return Snapshot{
		StatePath:           h.store.Path(),
		StateSchema:         h.stateSchema,
		StructuredExecution: append([]state.ExecutionEventRecord(nil), h.execution...),
		RuntimeLogs:         append([]state.RuntimeLogRecord(nil), h.runtimeLogs...),
		Components:          components,
	}
}

func (h *Hub) publishExecutionLocked(record state.ExecutionEventRecord) {
	for _, channel := range h.executionSubscribers {
		pushLatest(channel, record)
	}
}

func (h *Hub) publishRuntimeLocked(record state.RuntimeLogRecord) {
	for _, channel := range h.runtimeSubscribers {
		pushLatest(channel, record)
	}
}

func pushLatest[T any](channel chan T, value T) {
	select {
	case channel <- value:
		return
	default:
	}
	select {
	case <-channel:
	default:
	}
	select {
	case channel <- value:
	default:
	}
}

func redactJSON(value map[string]any) string {
	if len(value) == 0 {
		return "{}"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "{\"redaction_error\":\"unserializable fields\"}"
	}
	return Redact(string(encoded))
}

func fingerprint(parts ...string) string {
	hash := sha256.Sum256([]byte(joinFingerprint(parts)))
	return hex.EncodeToString(hash[:])
}

func joinFingerprint(parts []string) string {
	result := ""
	for index, part := range parts {
		if index > 0 {
			result += "\x00"
		}
		result += part
	}
	return result
}

func appendBounded[T any](items []T, item T, limit int) []T {
	items = append(items, item)
	if len(items) <= limit {
		return items
	}
	trimmed := make([]T, limit)
	copy(trimmed, items[len(items)-limit:])
	return trimmed
}

func subscriberBuffer(buffer int) int {
	if buffer < 1 {
		return 1
	}
	if buffer > maxSubscriberBuffer {
		return maxSubscriberBuffer
	}
	return buffer
}
