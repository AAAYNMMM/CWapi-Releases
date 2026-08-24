package processruntime

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	StateStarting  = "starting"
	StateRunning   = "running"
	StateCompleted = "completed"
	StateFailed    = "failed"
	StateStopped   = "stopped"

	BackendCodex  = "codex"
	BackendSystem = "system"

	FailureProgram    = "PROGRAM_FAILURE"
	FailurePermission = "PERMISSION_DENIED"
	FailurePolicy     = "PERMANENT_POLICY_DENIED"
	FailureUnknown    = "UNKNOWN_FAILURE"

	MaxActive   = 8
	MaxTerminal = 48
	TailBytes   = 8192
)

var (
	ErrLimitReached = errors.New("PROCESS_LIMIT_REACHED")
	ErrNotFound     = errors.New("PROCESS_NOT_FOUND")
	ErrStopFailed   = errors.New("PROCESS_STOP_FAILED")
)

type Failure struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Record struct {
	ProcessID        string   `json:"process_id"`
	State            string   `json:"state"`
	Backend          string   `json:"backend"`
	Repository       string   `json:"repository"`
	ExpectedCommit   string   `json:"expected_commit"`
	WorkingDirectory string   `json:"working_directory"`
	StartedAt        int64    `json:"started_at"`
	UpdatedAt        int64    `json:"updated_at"`
	ExitCode         *int     `json:"exit_code,omitempty"`
	StdoutTail       string   `json:"stdout_tail"`
	StderrTail       string   `json:"stderr_tail"`
	Error            *Failure `json:"error,omitempty"`
	LatestStream     string   `json:"-"`
	LatestOutputAt   int64    `json:"-"`
}

type Spec struct {
	Backend          string
	Repository       string
	ExpectedCommit   string
	WorkingDirectory string
	Cleanup          func()
	Launch           Launcher
}

type Completion struct {
	ExitCode      *int
	Stdout        string
	Stderr        string
	Failure       *Failure
	DiscardRecord bool
	SystemToken   string
}

type Handle interface {
	Done() <-chan Completion
	Stop() error
}

type Launcher func(context.Context, string, *Tails) (Handle, error)

type StartResult struct {
	Record     Record
	Completion *Completion
}

type Registry struct {
	mu            sync.Mutex
	entries       map[string]*entry
	terminalOrder []string
	active        int
	closed        bool
	ctx           context.Context
	cancel        context.CancelFunc
	observe       time.Duration
	stopTimeout   time.Duration
}

type entry struct {
	record        Record
	tails         *Tails
	handle        Handle
	cleanup       func()
	done          chan struct{}
	ctx           context.Context
	cancel        context.CancelFunc
	terminalOnce  sync.Once
	stopOnce      sync.Once
	stopRequested bool
	completion    Completion
}

func NewRegistry() *Registry {
	ctx, cancel := context.WithCancel(context.Background())
	return &Registry{
		entries: make(map[string]*entry), ctx: ctx, cancel: cancel,
		observe: 700 * time.Millisecond, stopTimeout: 4 * time.Second,
	}
}

func (r *Registry) Start(spec Spec) (StartResult, error) {
	if err := validateSpec(spec); err != nil {
		return StartResult{}, err
	}
	now := time.Now().UnixMilli()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return StartResult{}, errors.New("PROCESS_REGISTRY_CLOSED")
	}
	if r.active >= MaxActive {
		r.mu.Unlock()
		return StartResult{}, ErrLimitReached
	}
	id, err := r.newIDLocked()
	if err != nil {
		r.mu.Unlock()
		return StartResult{}, err
	}
	launchCtx, cancel := context.WithCancel(r.ctx)
	item := &entry{
		record: Record{
			ProcessID: id, State: StateStarting, Backend: spec.Backend,
			Repository: spec.Repository, ExpectedCommit: spec.ExpectedCommit,
			WorkingDirectory: spec.WorkingDirectory, StartedAt: now, UpdatedAt: now,
		},
		tails: NewTails(), cleanup: spec.Cleanup, done: make(chan struct{}),
		ctx: launchCtx, cancel: cancel,
	}
	r.entries[id] = item
	r.active++
	r.mu.Unlock()

	handle, launchErr := spec.Launch(item.ctx, id, item.tails)
	if launchErr != nil {
		r.terminalize(item, Completion{Failure: &Failure{Code: FailureUnknown, Message: "process backend failed to start"}})
		return r.startSnapshot(item), nil
	}
	if handle == nil || handle.Done() == nil {
		r.terminalize(item, Completion{Failure: &Failure{Code: FailureUnknown, Message: "PROCESS_HANDLE_INVALID"}})
		return r.startSnapshot(item), nil
	}
	r.mu.Lock()
	item.handle = handle
	alreadyDone := channelClosed(item.done)
	shouldStop := item.stopRequested || r.closed
	if !alreadyDone && !shouldStop {
		item.record.State = StateRunning
		item.record.UpdatedAt = time.Now().UnixMilli()
	}
	r.mu.Unlock()
	if alreadyDone || shouldStop {
		_ = handle.Stop()
	}
	if !alreadyDone {
		go func() {
			completion, ok := <-handle.Done()
			if !ok {
				completion = Completion{Failure: &Failure{Code: FailureUnknown, Message: "PROCESS_BACKEND_CLOSED"}}
			}
			r.terminalize(item, completion)
		}()
	}

	timer := time.NewTimer(r.observe)
	defer timer.Stop()
	select {
	case <-item.done:
	case <-timer.C:
	}
	return r.startSnapshot(item), nil
}

func (r *Registry) Status(processID string) (Record, error) {
	r.mu.Lock()
	item := r.entries[processID]
	if item == nil {
		r.mu.Unlock()
		return Record{}, ErrNotFound
	}
	record := item.record
	r.mu.Unlock()
	return publicRecord(record, item.tails), nil
}

func (r *Registry) Snapshot(limit int) []Record {
	if r == nil || limit <= 0 {
		return nil
	}
	type itemSnapshot struct {
		record Record
		tails  *Tails
	}
	r.mu.Lock()
	items := make([]itemSnapshot, 0, len(r.entries))
	for _, item := range r.entries {
		items = append(items, itemSnapshot{record: item.record, tails: item.tails})
	}
	r.mu.Unlock()

	sort.Slice(items, func(left, right int) bool {
		leftTerminal := terminalState(items[left].record.State)
		rightTerminal := terminalState(items[right].record.State)
		if leftTerminal != rightTerminal {
			return !leftTerminal
		}
		if items[left].record.UpdatedAt != items[right].record.UpdatedAt {
			return items[left].record.UpdatedAt > items[right].record.UpdatedAt
		}
		return items[left].record.StartedAt > items[right].record.StartedAt
	})
	if len(items) > limit {
		items = items[:limit]
	}
	records := make([]Record, len(items))
	for index, item := range items {
		records[index] = publicRecord(item.record, item.tails)
	}
	return records
}

func (r *Registry) Stop(processID string) (Record, error) {
	r.mu.Lock()
	item := r.entries[processID]
	if item == nil {
		r.mu.Unlock()
		return Record{}, ErrNotFound
	}
	if terminalState(item.record.State) {
		record := item.record
		r.mu.Unlock()
		return publicRecord(record, item.tails), nil
	}
	item.stopRequested = true
	handle := item.handle
	item.cancel()
	r.mu.Unlock()
	item.stopOnce.Do(func() {
		if handle != nil {
			_ = handle.Stop()
		}
	})

	timer := time.NewTimer(r.stopTimeout)
	defer timer.Stop()
	select {
	case <-item.done:
		return r.Status(processID)
	case <-timer.C:
		return Record{}, ErrStopFailed
	}
}

func (r *Registry) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	r.cancel()
	type closeTarget struct {
		item   *entry
		handle Handle
	}
	active := make([]closeTarget, 0, r.active)
	for _, item := range r.entries {
		if terminalState(item.record.State) {
			continue
		}
		item.stopRequested = true
		item.cancel()
		active = append(active, closeTarget{item: item, handle: item.handle})
	}
	r.mu.Unlock()
	for _, target := range active {
		target.item.stopOnce.Do(func() {
			if target.handle != nil {
				_ = target.handle.Stop()
			}
		})
	}
	deadline := time.NewTimer(35 * time.Second)
	defer deadline.Stop()
	for _, target := range active {
		select {
		case <-target.item.done:
		case <-deadline.C:
			return
		}
	}
}

func (r *Registry) terminalize(item *entry, completion Completion) {
	item.terminalOnce.Do(func() {
		if completion.Stdout != "" {
			_, _ = item.tails.Stdout.Write([]byte(completion.Stdout))
		}
		if completion.Stderr != "" {
			_, _ = item.tails.Stderr.Write([]byte(completion.Stderr))
		}
		item.cancel()
		r.mu.Lock()
		item.completion = completion
		item.record.UpdatedAt = time.Now().UnixMilli()
		item.record.ExitCode = completion.ExitCode
		if item.stopRequested {
			item.record.State = StateStopped
			item.record.Error = nil
		} else if completion.Failure != nil || (completion.ExitCode != nil && *completion.ExitCode != 0) {
			item.record.State = StateFailed
			item.record.Error = normalizedFailure(completion)
		} else {
			item.record.State = StateCompleted
		}
		if r.active > 0 {
			r.active--
		}
		if completion.DiscardRecord {
			delete(r.entries, item.record.ProcessID)
		} else {
			r.terminalOrder = append(r.terminalOrder, item.record.ProcessID)
			r.trimLocked()
		}
		r.mu.Unlock()
		if item.cleanup != nil {
			item.cleanup()
		}
		close(item.done)
	})
}

func (r *Registry) startSnapshot(item *entry) StartResult {
	r.mu.Lock()
	record := item.record
	completion := item.completion
	discarded := completion.DiscardRecord && channelClosed(item.done)
	r.mu.Unlock()
	result := StartResult{Record: publicRecord(record, item.tails)}
	if discarded {
		copyCompletion := completion
		result.Record = Record{}
		result.Completion = &copyCompletion
	}
	return result
}
