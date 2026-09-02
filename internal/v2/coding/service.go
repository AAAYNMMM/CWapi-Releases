package coding

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/repository"
	"github.com/AAAYNMMM/CWapi/internal/v2/codextoolhost"
	"github.com/AAAYNMMM/CWapi/internal/v2/mcpserver"
	"github.com/AAAYNMMM/CWapi/internal/v2/workspace"
)

const openingRepository = "<opening>"

type prepareFunc func(context.Context, workspace.PrepareInput) (workspace.Result, error)
type inspectFunc func(context.Context, string) (workspace.Snapshot, error)
type execFunc func(context.Context, string, codextoolhost.ExecInput) (codextoolhost.ExecResult, error)
type readyFunc func() error
type stopProcessesFunc func(context.Context, string) error

type Service struct {
	mu sync.RWMutex

	prepare             prepareFunc
	inspect             inspectFunc
	execute             execFunc
	ready               readyFunc
	setAccessProfile    func(string) error
	setNetworkAccess    func(bool) error
	setRemoteGitRewrite func(bool) error
	stopProcesses       stopProcessesFunc
	closeRuntime        func(context.Context) error
	sessions            map[string]*record
	active              map[string]string
	opening             map[string]*openingRecord
	closed              bool
}

type openingRecord struct {
	cancel context.CancelFunc
	done   chan struct{}
}

type record struct {
	mu sync.Mutex

	repository     string
	repositoryURL  string
	path           string
	targetRef      string
	resolvedCommit string
	currentHead    string
	currentBranch  string
	detached       bool
	trackedDirty   bool
	resumed        bool
	busy           bool
	closing        bool
	cancel         context.CancelFunc
	done           chan struct{}
}

func New(manager *workspace.Manager, host *codextoolhost.Host) (*Service, error) {
	if manager == nil {
		return nil, errors.New("CODING_WORKSPACE_MANAGER_REQUIRED")
	}
	if host == nil {
		return nil, errors.New("CODING_CODEX_TOOLHOST_REQUIRED")
	}
	service, err := newService(
		manager.Prepare,
		func(ctx context.Context, path string, input codextoolhost.ExecInput) (codextoolhost.ExecResult, error) {
			return host.Exec(ctx, path, input)
		},
		manager.Inspect,
	)
	if err != nil {
		return nil, err
	}
	service.setAccessProfile = host.SetAccessProfile
	service.setNetworkAccess = host.SetNetworkAccess
	service.setRemoteGitRewrite = host.SetRemoteGitRewrite
	service.stopProcesses = host.StopWorkspace
	service.closeRuntime = host.Close
	return service, nil
}

func newService(prepare prepareFunc, execute execFunc, inspect inspectFunc, readiness ...readyFunc) (*Service, error) {
	if prepare == nil || execute == nil {
		return nil, errors.New("CODING_DEPENDENCY_REQUIRED")
	}
	var ready readyFunc
	if len(readiness) > 0 {
		ready = readiness[0]
	}
	return &Service{
		prepare: prepare, inspect: inspect, execute: execute, ready: ready,
		sessions: make(map[string]*record), active: make(map[string]string), opening: make(map[string]*openingRecord),
	}, nil
}

func (s *Service) SetAccessProfile(profile string) error {
	if s == nil {
		return errors.New("CODING_SERVICE_UNAVAILABLE")
	}
	s.mu.RLock()
	closed := s.closed
	setter := s.setAccessProfile
	s.mu.RUnlock()
	if closed {
		return errors.New("CODING_SERVICE_CLOSED")
	}
	if setter == nil {
		return errors.New("CODING_ACCESS_PROFILE_UNAVAILABLE")
	}
	return setter(profile)
}

func (s *Service) SetNetworkAccess(allowed bool) error {
	if s == nil {
		return errors.New("CODING_SERVICE_UNAVAILABLE")
	}
	s.mu.RLock()
	closed := s.closed
	setter := s.setNetworkAccess
	s.mu.RUnlock()
	if closed {
		return errors.New("CODING_SERVICE_CLOSED")
	}
	if setter == nil {
		return errors.New("CODING_NETWORK_ACCESS_UNAVAILABLE")
	}
	return setter(allowed)
}

func (s *Service) SetRemoteGitRewrite(allowed bool) error {
	if s == nil {
		return errors.New("CODING_SERVICE_UNAVAILABLE")
	}
	s.mu.RLock()
	closed := s.closed
	setter := s.setRemoteGitRewrite
	s.mu.RUnlock()
	if closed {
		return errors.New("CODING_SERVICE_CLOSED")
	}
	if setter == nil {
		return errors.New("CODING_REMOTE_GIT_REWRITE_UNAVAILABLE")
	}
	return setter(allowed)
}

func (s *Service) Open(ctx context.Context, input mcpserver.CodingOpenInput) (mcpserver.CodingOpenOutput, error) {
	if s == nil {
		return mcpserver.CodingOpenOutput{}, errors.New("CODING_SERVICE_UNAVAILABLE")
	}
	identity, err := repository.Parse(input.RepositoryURL)
	if err != nil {
		return mcpserver.CodingOpenOutput{}, err
	}
	repositoryKey := identity.Repository
	if ctx == nil {
		ctx = context.Background()
	}
	prepareCtx, cancelPrepare := context.WithCancel(ctx)
	opening := &openingRecord{cancel: cancelPrepare, done: make(chan struct{})}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancelPrepare()
		return mcpserver.CodingOpenOutput{}, errors.New("CODING_SERVICE_CLOSED")
	}
	if owner := s.active[repositoryKey]; owner != "" {
		if !input.Resume || owner == openingRepository {
			s.mu.Unlock()
			cancelPrepare()
			return mcpserver.CodingOpenOutput{}, fmt.Errorf("CODING_WORKSPACE_BUSY: %s", repositoryKey)
		}
		output, resumeErr := s.resumeActiveLocked(input, owner)
		s.mu.Unlock()
		cancelPrepare()
		return output, resumeErr
	}
	s.active[repositoryKey] = openingRepository
	s.opening[repositoryKey] = opening
	s.mu.Unlock()

	defer func() {
		cancelPrepare()
		s.mu.Lock()
		if s.opening[repositoryKey] == opening {
			delete(s.opening, repositoryKey)
		}
		if s.active[repositoryKey] == openingRepository {
			delete(s.active, repositoryKey)
		}
		s.mu.Unlock()
		close(opening.done)
	}()
	if s.ready != nil {
		if err := s.ready(); err != nil {
			return mcpserver.CodingOpenOutput{}, err
		}
	}
	prepared, err := s.prepare(prepareCtx, workspace.PrepareInput{
		RepositoryURL: input.RepositoryURL, TargetRef: input.TargetRef,
		ExpectedCommit: input.ExpectedCommit, Resume: input.Resume,
	})
	if err != nil {
		return mcpserver.CodingOpenOutput{}, err
	}
	if prepared.Repository != repositoryKey || strings.TrimSpace(prepared.Path) == "" {
		return mcpserver.CodingOpenOutput{}, errors.New("CODING_WORKSPACE_REPOSITORY_MISMATCH")
	}

	codingID := "coding_" + rand.Text()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return mcpserver.CodingOpenOutput{}, errors.New("CODING_SERVICE_CLOSED")
	}
	for s.sessions[codingID] != nil {
		codingID = "coding_" + rand.Text()
	}
	s.sessions[codingID] = &record{
		repository: prepared.Repository, repositoryURL: identity.NormalizedURL, path: prepared.Path,
		targetRef: prepared.TargetRef, resolvedCommit: prepared.ResolvedCommit,
		currentHead: prepared.CurrentHead, trackedDirty: prepared.TrackedDirty, resumed: prepared.Resumed,
		currentBranch: prepared.CurrentBranch, detached: prepared.Detached,
	}
	s.active[repositoryKey] = codingID
	s.mu.Unlock()

	return mcpserver.CodingOpenOutput{
		Repository: prepared.Repository, TargetRef: prepared.TargetRef,
		ResolvedCommit: prepared.ResolvedCommit, CurrentHead: prepared.CurrentHead,
		CurrentBranch: prepared.CurrentBranch, Detached: prepared.Detached,
		TrackedDirty: prepared.TrackedDirty, Resumed: prepared.Resumed, State: "ready",
	}, nil
}

// resumeActiveLocked returns public state for the repository while retaining its internal session generation.
// s.mu must be held by the caller. This lets a new Web GPT conversation
// resume an active internal session without weakening the one-session-per-repo
// protection.
func (s *Service) resumeActiveLocked(input mcpserver.CodingOpenInput, owner string) (mcpserver.CodingOpenOutput, error) {
	record := s.sessions[owner]
	if record == nil {
		return mcpserver.CodingOpenOutput{}, errors.New("CODING_ACTIVE_SESSION_MISSING")
	}
	record.mu.Lock()
	defer record.mu.Unlock()
	if record.closing {
		return mcpserver.CodingOpenOutput{}, errors.New("CODING_SESSION_CLOSING")
	}
	if !sameTargetRef(input.TargetRef, record.targetRef) {
		return mcpserver.CodingOpenOutput{}, fmt.Errorf("CODING_RESUME_TARGET_MISMATCH: requested=%s active=%s", input.TargetRef, record.targetRef)
	}
	if expected := strings.TrimSpace(input.ExpectedCommit); expected != "" && !strings.EqualFold(expected, record.resolvedCommit) {
		return mcpserver.CodingOpenOutput{}, fmt.Errorf("CODING_RESUME_COMMIT_MISMATCH: expected=%s active=%s", expected, record.resolvedCommit)
	}
	state := "ready"
	if record.busy {
		state = "busy"
	}
	return mcpserver.CodingOpenOutput{
		Repository: record.repository, TargetRef: record.targetRef,
		ResolvedCommit: record.resolvedCommit, CurrentHead: record.currentHead,
		CurrentBranch: record.currentBranch, Detached: record.detached,
		TrackedDirty: record.trackedDirty, Resumed: true, State: state,
	}, nil
}

func sameTargetRef(left, right string) bool {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if left == right {
		return true
	}
	left = strings.TrimPrefix(left, "refs/heads/")
	right = strings.TrimPrefix(right, "refs/heads/")
	return left == right
}

func (s *Service) Exec(ctx context.Context, input mcpserver.CodingExecInput) (mcpserver.CodingExecOutput, error) {
	_, record, err := s.lookupRepository(input.RepositoryURL)
	if err != nil {
		return mcpserver.CodingExecOutput{}, err
	}
	commandCtx, finish, err := record.beginOperation(ctx)
	if err != nil {
		return mcpserver.CodingExecOutput{}, err
	}
	defer finish()

	result, err := s.execute(commandCtx, record.path, codextoolhost.ExecInput{
		Action: input.Action, ProcessID: input.ProcessID, Command: input.Command,
		Argv: input.Argv, CWD: input.CWD, TimeoutSeconds: input.TimeoutSeconds,
	})
	if err != nil {
		return mcpserver.CodingExecOutput{}, err
	}
	return mcpserver.CodingExecOutput{
		State: result.State, ProcessID: result.ProcessID, PID: result.PID, StartedAt: result.StartedAt, ExitCode: result.ExitCode,
		Stdout: result.Stdout, Stderr: result.Stderr, Truncated: result.Truncated,
	}, nil
}
func (r *record) beginOperation(ctx context.Context) (context.Context, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	operationCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		cancel()
		return nil, nil, errors.New("CODING_SESSION_CLOSING")
	}
	if r.busy {
		r.mu.Unlock()
		cancel()
		return nil, nil, errors.New("CODING_COMMAND_ACTIVE")
	}
	r.busy, r.cancel, r.done = true, cancel, done
	r.mu.Unlock()
	finish := func() {
		cancel()
		r.mu.Lock()
		r.busy, r.cancel, r.done = false, nil, nil
		close(done)
		r.mu.Unlock()
	}
	return operationCtx, finish, nil
}

func (s *Service) Status(ctx context.Context, input mcpserver.CodingStatusInput) (mcpserver.CodingStatusOutput, error) {
	_, record, err := s.lookupRepository(input.RepositoryURL)
	if err != nil {
		return mcpserver.CodingStatusOutput{}, err
	}
	record.mu.Lock()
	busy := record.busy
	record.mu.Unlock()
	state := "ready"
	if busy {
		state = "busy"
	}
	output := mcpserver.CodingStatusOutput{
		State: state, Repository: record.repository,
		TargetRef: record.targetRef, ResolvedCommit: record.resolvedCommit,
		CurrentHead: record.currentHead, TrackedDirty: record.trackedDirty,
		CurrentBranch: record.currentBranch, Detached: record.detached,
	}
	if busy || s.inspect == nil {
		return output, nil
	}
	workspaceState, inspectErr := s.inspect(ctx, record.repositoryURL)
	if inspectErr != nil {
		output.LastError = "WORKSPACE_INSPECT_FAILED: " + inspectErr.Error()
		return output, nil
	}
	output.TargetRef = workspaceState.TargetRef
	output.ResolvedCommit = workspaceState.ResolvedCommit
	output.CurrentHead = workspaceState.CurrentHead
	output.CurrentBranch = workspaceState.CurrentBranch
	output.Detached = workspaceState.Detached
	output.TrackingHead = workspaceState.TrackingHead
	output.TrackedDirty = workspaceState.TrackedDirty
	output.Divergence = workspaceState.Divergence
	return output, nil
}
func (s *Service) Close(ctx context.Context, input mcpserver.CodingCloseInput) (mcpserver.CodingCloseOutput, error) {
	if s == nil {
		return mcpserver.CodingCloseOutput{}, errors.New("CODING_SERVICE_UNAVAILABLE")
	}
	identity, err := repository.Parse(input.RepositoryURL)
	if err != nil {
		return mcpserver.CodingCloseOutput{}, err
	}
	repositoryKey := identity.Repository
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return mcpserver.CodingCloseOutput{}, errors.New("CODING_SERVICE_CLOSED")
	}
	codingID := s.active[repositoryKey]
	if codingID == "" {
		s.mu.Unlock()
		return mcpserver.CodingCloseOutput{Repository: repositoryKey, State: "no_active_session"}, nil
	}
	if codingID == openingRepository {
		opening := s.opening[repositoryKey]
		if opening == nil {
			delete(s.active, repositoryKey)
			s.mu.Unlock()
			return mcpserver.CodingCloseOutput{Repository: repositoryKey, State: "no_active_session"}, nil
		}
		opening.cancel()
		done := opening.done
		s.mu.Unlock()
		select {
		case <-done:
			return mcpserver.CodingCloseOutput{Repository: repositoryKey, State: "closed"}, nil
		case <-ctx.Done():
			return mcpserver.CodingCloseOutput{}, fmt.Errorf("CODING_CLOSE_TIMEOUT: %w", ctx.Err())
		}
	}
	record := s.sessions[codingID]
	if record == nil {
		s.mu.Unlock()
		return mcpserver.CodingCloseOutput{}, errors.New("CODING_ACTIVE_SESSION_MISSING")
	}
	record.mu.Lock()
	if record.closing {
		record.mu.Unlock()
		s.mu.Unlock()
		return mcpserver.CodingCloseOutput{}, errors.New("CODING_SESSION_CLOSING")
	}
	record.closing = true
	cancel, done := record.cancel, record.done
	record.mu.Unlock()
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
			go func() {
				<-done
				if s.stopProcesses != nil {
					stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = s.stopProcesses(stopCtx, record.path)
					cancel()
				}
				s.finalizeClose(codingID, record)
			}()
			return mcpserver.CodingCloseOutput{}, fmt.Errorf("CODING_CLOSE_TIMEOUT: %w", ctx.Err())
		}
	}
	var stopErr error
	if s.stopProcesses != nil {
		stopErr = s.stopProcesses(ctx, record.path)
	}
	s.finalizeClose(codingID, record)
	return mcpserver.CodingCloseOutput{Repository: repositoryKey, State: "closed"}, stopErr
}
func (s *Service) finalizeClose(codingID string, record *record) {
	s.mu.Lock()
	if s.sessions[codingID] == record {
		delete(s.sessions, codingID)
		if s.active[record.repository] == codingID {
			delete(s.active, record.repository)
		}
	}
	s.mu.Unlock()
}

func (s *Service) CloseAll(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	openings := make([]*openingRecord, 0, len(s.opening))
	for _, opening := range s.opening {
		opening.cancel()
		openings = append(openings, opening)
	}
	records := make([]*record, 0, len(s.sessions))
	for _, record := range s.sessions {
		record.mu.Lock()
		record.closing = true
		if record.cancel != nil {
			record.cancel()
		}
		record.mu.Unlock()
		records = append(records, record)
	}
	s.sessions = make(map[string]*record)
	s.active = make(map[string]string)
	s.opening = make(map[string]*openingRecord)
	s.mu.Unlock()
	var closeErr error
	for _, opening := range openings {
		select {
		case <-opening.done:
		case <-ctx.Done():
			closeErr = errors.Join(closeErr, fmt.Errorf("CODING_CLOSE_ALL_TIMEOUT: %w", ctx.Err()))
		}
	}
	for _, record := range records {
		record.mu.Lock()
		done := record.done
		record.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-ctx.Done():
				closeErr = errors.Join(closeErr, fmt.Errorf("CODING_CLOSE_ALL_TIMEOUT: %w", ctx.Err()))
			}
		}
		if s.stopProcesses != nil {
			closeErr = errors.Join(closeErr, s.stopProcesses(ctx, record.path))
		}
	}
	if s.closeRuntime != nil {
		closeErr = errors.Join(closeErr, s.closeRuntime(ctx))
	}
	return closeErr
}

func (s *Service) lookupRepository(repositoryURL string) (string, *record, error) {
	if s == nil {
		return "", nil, errors.New("CODING_SERVICE_UNAVAILABLE")
	}
	identity, err := repository.Parse(repositoryURL)
	if err != nil {
		return "", nil, err
	}
	repositoryKey := identity.Repository
	s.mu.RLock()
	closed := s.closed
	codingID := s.active[repositoryKey]
	record := s.sessions[codingID]
	s.mu.RUnlock()
	if closed {
		return "", nil, errors.New("CODING_SERVICE_CLOSED")
	}
	if codingID == "" {
		return "", nil, errors.New("CODING_SESSION_NOT_ACTIVE")
	}
	if codingID == openingRepository {
		return "", nil, errors.New("CODING_SESSION_OPENING")
	}
	if record == nil {
		return "", nil, errors.New("CODING_ACTIVE_SESSION_MISSING")
	}
	record.mu.Lock()
	closing := record.closing
	record.mu.Unlock()
	if closing {
		return "", nil, errors.New("CODING_SESSION_CLOSING")
	}
	return codingID, record, nil
}
