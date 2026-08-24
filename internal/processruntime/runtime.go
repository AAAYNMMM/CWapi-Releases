package processruntime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/childenv"
	"github.com/AAAYNMMM/CWapi/internal/codex"
	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/executionpolicy"
	"github.com/AAAYNMMM/CWapi/internal/gateway"
	"github.com/AAAYNMMM/CWapi/internal/invocation"
	"github.com/AAAYNMMM/CWapi/internal/observability"
	"github.com/AAAYNMMM/CWapi/internal/processcontract"
	"github.com/AAAYNMMM/CWapi/internal/protocol"
	"github.com/AAAYNMMM/CWapi/internal/repository"
)

type Runtime struct {
	codex         *codex.Service
	registry      *Registry
	resolver      *invocation.Resolver
	authorization *Authorization
	dataRoot      string
}

func NewRuntime(codexService *codex.Service, manager *config.Manager, dataRoot string) (*Runtime, error) {
	if codexService == nil || manager == nil || !filepath.IsAbs(dataRoot) {
		return nil, errors.New("PROCESS_RUNTIME_DEPENDENCY_INVALID")
	}
	resolver, err := invocation.New(childenv.Canonical())
	if err != nil {
		return nil, err
	}
	authorization, err := NewAuthorization(manager)
	if err != nil {
		return nil, err
	}
	return &Runtime{
		codex: codexService, registry: NewRegistry(), resolver: resolver,
		authorization: authorization, dataRoot: filepath.Clean(dataRoot),
	}, nil
}

func (r *Runtime) Start(_ context.Context, request protocol.MCPRequest, arguments processcontract.StartArguments, execution gateway.MCPExecutionContext, release func()) (protocol.MCPResponse, bool) {
	if request.SystemToken != "" {
		return r.startSystemFallback(request, arguments), false
	}
	if release == nil || execution.CWD == "" || execution.Repository == "" {
		return processError(request.RequestID, protocol.MCPStatusUnavailable, "UNKNOWN_FAILURE", "repository workspace is unavailable"), false
	}
	lease := newWorkspaceLease(release)
	attemptMode := r.authorization.AttemptMode()
	final, err := r.resolver.Resolve(execution.CWD, arguments)
	if err != nil {
		lease.registryDone()
		return processError(request.RequestID, protocol.MCPStatusFailed, FailureUnknown, "process invocation could not be resolved"), true
	}
	if err := executionpolicy.Check(policyInvocation(final), execution.CWD, r.dataRoot); err != nil {
		lease.registryDone()
		return processError(request.RequestID, protocol.MCPStatusFailed, FailurePolicy, err.Error()), true
	}
	workingDirectory, err := relativeWorkingDirectory(execution.CWD, final.CWD)
	if err != nil {
		lease.registryDone()
		return processError(request.RequestID, protocol.MCPStatusFailed, FailureUnknown, err.Error()), true
	}
	window := newDenialWindow(lease)
	result, err := r.registry.Start(Spec{
		Backend: BackendCodex, Repository: execution.Repository,
		ExpectedCommit: request.ExpectedCommit, WorkingDirectory: workingDirectory,
		Cleanup: lease.registryDone,
		Launch: func(ctx context.Context, processID string, _ *Tails) (Handle, error) {
			return r.startCodex(ctx, processID, execution.CWD, final, window)
		},
	})
	held := window.close()
	if err != nil {
		if held {
			lease.releaseHeld()
		} else {
			lease.registryDone()
		}
		return processError(request.RequestID, protocol.MCPStatusFailed, processErrorCode(err), err.Error()), true
	}
	if held || (result.Completion != nil && result.Completion.Failure != nil && result.Completion.Failure.Code == FailurePermission) {
		return r.permissionDenied(request, execution, final, lease, attemptMode, held), true
	}
	if held {
		lease.releaseHeld()
	}
	return processRecordResponse(request.RequestID, result.Record), true
}

func (r *Runtime) startSystemFallback(request protocol.MCPRequest, arguments processcontract.StartArguments) protocol.MCPResponse {
	grant, err := r.authorization.Peek(request.SystemToken)
	if err != nil {
		return processError(request.RequestID, protocol.MCPStatusBlocked, ErrTokenInvalid.Error(), "System Token is invalid or expired")
	}
	identity, err := repository.Parse(request.RepositoryURL)
	if err != nil || !strings.EqualFold(identity.Repository, grant.context.Repository) ||
		!strings.EqualFold(request.ExpectedCommit, grant.context.ExpectedCommit) {
		return processError(request.RequestID, protocol.MCPStatusBlocked, ErrTokenBinding.Error(), "System Token binding does not match this request")
	}
	final, err := r.resolver.Resolve(grant.context.RepositoryRoot, arguments)
	if err != nil {
		return processError(request.RequestID, protocol.MCPStatusBlocked, ErrTokenBinding.Error(), "System Token invocation binding changed")
	}
	if err := executionpolicy.Check(policyInvocation(final), grant.context.RepositoryRoot, r.dataRoot); err != nil {
		return processError(request.RequestID, protocol.MCPStatusFailed, FailurePolicy, err.Error())
	}
	granted, err := r.authorization.Consume(request.SystemToken, grant, identity.Repository, request.ExpectedCommit, final)
	if err != nil {
		return processError(request.RequestID, protocol.MCPStatusBlocked, err.Error(), tokenGateMessage(err))
	}
	workingDirectory, err := relativeWorkingDirectory(granted.RepositoryRoot, final.CWD)
	if err != nil {
		granted.Lease.registryDone()
		return processError(request.RequestID, protocol.MCPStatusFailed, FailureUnknown, err.Error())
	}
	result, err := r.registry.Start(Spec{
		Backend: BackendSystem, Repository: granted.Repository,
		ExpectedCommit: granted.ExpectedCommit, WorkingDirectory: workingDirectory,
		Cleanup: granted.Lease.registryDone,
		Launch: func(ctx context.Context, _ string, tails *Tails) (Handle, error) {
			return startSystem(ctx, final, tails)
		},
	})
	if err != nil {
		granted.Lease.registryDone()
		return processError(request.RequestID, protocol.MCPStatusFailed, processErrorCode(err), err.Error())
	}
	return processRecordResponse(request.RequestID, result.Record)
}

func (r *Runtime) permissionDenied(request protocol.MCPRequest, execution gateway.MCPExecutionContext, final invocation.Final, lease *workspaceLease, attemptMode string, held bool) protocol.MCPResponse {
	if !held {
		return processError(request.RequestID, protocol.MCPStatusBlocked, FailurePermission, "Codex denied this invocation")
	}
	token, err := r.authorization.Issue(attemptMode, grantContext{
		RepositoryURL: request.RepositoryURL, Repository: execution.Repository,
		ExpectedCommit: request.ExpectedCommit, RepositoryRoot: execution.CWD,
		Final: final, Lease: lease,
	})
	if err != nil {
		lease.releaseHeld()
		if errors.Is(err, ErrTokenLimit) {
			return processError(request.RequestID, protocol.MCPStatusBlocked, ErrTokenLimit.Error(), "System Token limit reached")
		}
		return processError(request.RequestID, protocol.MCPStatusFailed, FailureUnknown, err.Error())
	}
	if token == "" {
		lease.releaseHeld()
		return processError(request.RequestID, protocol.MCPStatusBlocked, FailurePermission, "Codex denied this invocation")
	}
	response := processError(request.RequestID, protocol.MCPStatusBlocked, FailurePermission, "Codex denied this invocation; retry with the one-time System Token")
	response.SystemToken = token
	return response
}

func (r *Runtime) Status(_ context.Context, requestID, processID string) protocol.MCPResponse {
	record, err := r.registry.Status(processID)
	if err != nil {
		return processError(requestID, protocol.MCPStatusFailed, processErrorCode(err), err.Error())
	}
	return processRecordResponse(requestID, record)
}

func (r *Runtime) Stop(_ context.Context, requestID, processID string) protocol.MCPResponse {
	record, err := r.registry.Stop(processID)
	if err != nil {
		return processError(requestID, protocol.MCPStatusFailed, processErrorCode(err), err.Error())
	}
	return processRecordResponse(requestID, record)
}

func (r *Runtime) Records(limit int) []Record {
	if r == nil || r.registry == nil {
		return nil
	}
	return r.registry.Snapshot(limit)
}

func (r *Runtime) StopRecord(processID string) (Record, error) {
	if r == nil || r.registry == nil {
		return Record{}, errors.New("PROCESS_RUNTIME_UNAVAILABLE")
	}
	return r.registry.Stop(strings.TrimSpace(processID))
}

func (r *Runtime) UpdatePermissionMode(mode string) (config.Config, error) {
	return r.authorization.UpdateMode(mode)
}

func (r *Runtime) RevokeSystemToken(token string) {
	if r != nil && r.authorization != nil {
		r.authorization.Revoke(token)
	}
}

func (r *Runtime) Close() {
	if r == nil {
		return
	}
	r.authorization.Close()
	r.registry.Close()
}

func processRecordResponse(requestID string, record Record) protocol.MCPResponse {
	payload, err := json.Marshal(record)
	if err != nil {
		return processError(requestID, protocol.MCPStatusFailed, FailureUnknown, "process result could not be encoded")
	}
	return protocol.MCPResponse{
		Schema: protocol.MCPResponseSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: requestID, Status: protocol.MCPStatusCompleted, Result: payload,
	}
}

func processError(requestID string, status protocol.MCPStatus, code, message string) protocol.MCPResponse {
	message = strings.TrimSpace(observability.Redact(message))
	if len(message) > protocol.MaxMCPErrorMessageBytes {
		message = message[:protocol.MaxMCPErrorMessageBytes]
	}
	if message == "" {
		message = code
	}
	return protocol.MCPResponse{
		Schema: protocol.MCPResponseSchema, ProtocolVersion: protocol.MCPProtocolVersion,
		RequestID: requestID, Status: status,
		Error: &protocol.MCPError{Code: code, Category: "process", Message: strings.ToValidUTF8(message, "?"), Retryable: false},
	}
}

func processErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrLimitReached):
		return "PROCESS_LIMIT_REACHED"
	case errors.Is(err, ErrNotFound):
		return "PROCESS_NOT_FOUND"
	case errors.Is(err, ErrStopFailed):
		return "PROCESS_STOP_FAILED"
	default:
		return FailureUnknown
	}
}

func tokenGateMessage(err error) string {
	if errors.Is(err, ErrTokenBinding) {
		return "System Token binding does not match this request"
	}
	return "System Token is invalid or expired"
}

func policyInvocation(final invocation.Final) executionpolicy.Invocation {
	executable := final.Executable
	argv := final.Argv
	if final.TargetExecutable != "" {
		executable = final.TargetExecutable
		argv = final.TargetArgv
	}
	return executionpolicy.Invocation{Executable: executable, Argv: argv, CWD: final.CWD}
}

func relativeWorkingDirectory(root, cwd string) (string, error) {
	relative, err := filepath.Rel(root, cwd)
	if err != nil || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("PROCESS_WORKING_DIRECTORY_INVALID")
	}
	if relative == "" {
		relative = "."
	}
	return filepath.ToSlash(relative), nil
}
