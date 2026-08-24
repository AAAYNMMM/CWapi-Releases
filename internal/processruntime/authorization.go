package processruntime

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/invocation"
)

const (
	MaxSystemTokens = 3
	SystemTokenTTL  = 60 * time.Second
)

var (
	ErrTokenInvalid = errors.New("SYSTEM_TOKEN_INVALID_OR_EXPIRED")
	ErrTokenBinding = errors.New("SYSTEM_TOKEN_BINDING_MISMATCH")
	ErrTokenLimit   = errors.New("SYSTEM_TOKEN_LIMIT_REACHED")
)

type grantContext struct {
	RepositoryURL    string
	Repository       string
	ExpectedCommit   string
	RepositoryRoot   string
	WorkingDirectory string
	Final            invocation.Final
	Lease            *workspaceLease
}

type tokenGrant struct {
	token     string
	context   grantContext
	expiresAt time.Time
	timer     *time.Timer
}

type Authorization struct {
	mu     sync.Mutex
	config *config.Manager
	grants map[string]*tokenGrant
	ttl    time.Duration
	now    func() time.Time
	closed bool
}

func NewAuthorization(manager *config.Manager) (*Authorization, error) {
	if manager == nil {
		return nil, errors.New("AUTHORIZATION_CONFIG_REQUIRED")
	}
	return &Authorization{
		config: manager, grants: make(map[string]*tokenGrant),
		ttl: SystemTokenTTL, now: time.Now,
	}, nil
}

func (a *Authorization) AttemptMode() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return config.EffectivePermissionMode(a.config.Snapshot().PermissionMode)
}

func (a *Authorization) Issue(attemptMode string, context grantContext) (string, error) {
	a.mu.Lock()
	if a.closed || attemptMode != config.PermissionModeFullAccess ||
		config.EffectivePermissionMode(a.config.Snapshot().PermissionMode) != config.PermissionModeFullAccess {
		a.mu.Unlock()
		return "", nil
	}
	expired := a.pruneExpiredLocked()
	if len(a.grants) >= MaxSystemTokens {
		a.mu.Unlock()
		releaseGrants(expired)
		return "", ErrTokenLimit
	}
	token := ""
	for attempt := 0; attempt < 8; attempt++ {
		candidate, err := newSystemToken()
		if err != nil {
			a.mu.Unlock()
			releaseGrants(expired)
			return "", err
		}
		if _, exists := a.grants[candidate]; !exists {
			token = candidate
			break
		}
	}
	if token == "" {
		a.mu.Unlock()
		releaseGrants(expired)
		return "", errors.New("SYSTEM_TOKEN_CREATE_FAILED")
	}
	if context.Lease == nil || !context.Lease.issueToken() {
		a.mu.Unlock()
		releaseGrants(expired)
		return "", errors.New("SYSTEM_TOKEN_LEASE_INVALID")
	}
	grant := &tokenGrant{token: token, context: cloneGrantContext(context), expiresAt: a.now().Add(a.ttl)}
	a.grants[token] = grant
	grant.timer = time.AfterFunc(a.ttl, func() { a.expire(token, grant) })
	a.mu.Unlock()
	releaseGrants(expired)
	return token, nil
}

func (a *Authorization) Peek(token string) (*tokenGrant, error) {
	a.mu.Lock()
	grant := a.grants[token]
	if grant == nil || !a.now().Before(grant.expiresAt) {
		if grant != nil {
			delete(a.grants, token)
			if grant.timer != nil {
				grant.timer.Stop()
			}
		}
		a.mu.Unlock()
		if grant != nil {
			grant.context.Lease.releaseToken()
		}
		return nil, ErrTokenInvalid
	}
	a.mu.Unlock()
	return grant, nil
}

func (a *Authorization) Consume(token string, expected *tokenGrant, repository, commit string, final invocation.Final) (grantContext, error) {
	a.mu.Lock()
	grant := a.grants[token]
	expired := grant != nil && !a.now().Before(grant.expiresAt)
	if grant == nil || grant != expected || expired {
		if expired {
			delete(a.grants, token)
			if grant.timer != nil {
				grant.timer.Stop()
			}
		}
		a.mu.Unlock()
		if expired {
			grant.context.Lease.releaseToken()
		}
		return grantContext{}, ErrTokenInvalid
	}
	if !strings.EqualFold(grant.context.Repository, repository) || !strings.EqualFold(grant.context.ExpectedCommit, commit) ||
		!sameFinalInvocation(grant.context.Final, final) {
		a.mu.Unlock()
		return grantContext{}, ErrTokenBinding
	}
	delete(a.grants, token)
	if grant.timer != nil {
		grant.timer.Stop()
	}
	if !grant.context.Lease.consumeToken() {
		a.mu.Unlock()
		return grantContext{}, ErrTokenInvalid
	}
	context := cloneGrantContext(grant.context)
	a.mu.Unlock()
	return context, nil
}

func (a *Authorization) UpdateMode(mode string) (config.Config, error) {
	canonical, err := config.CanonicalPermissionMode(mode)
	if err != nil {
		return a.config.Snapshot(), err
	}
	a.mu.Lock()
	updated, err := a.config.Update(func(candidate *config.Config) error {
		candidate.PermissionMode = canonical
		return nil
	})
	if err != nil {
		a.mu.Unlock()
		return a.config.Snapshot(), err
	}
	cleared := a.clearLocked()
	a.mu.Unlock()
	releaseGrants(cleared)
	return updated, nil
}

func (a *Authorization) Revoke(token string) {
	a.mu.Lock()
	grant := a.grants[token]
	if grant != nil {
		delete(a.grants, token)
		if grant.timer != nil {
			grant.timer.Stop()
		}
	}
	a.mu.Unlock()
	if grant != nil {
		grant.context.Lease.releaseToken()
	}
}

func (a *Authorization) Close() {
	if a == nil {
		return
	}
	a.mu.Lock()
	a.closed = true
	cleared := a.clearLocked()
	a.mu.Unlock()
	releaseGrants(cleared)
}

func (a *Authorization) expire(token string, expected *tokenGrant) {
	a.mu.Lock()
	grant := a.grants[token]
	if grant != expected || a.now().Before(expected.expiresAt) {
		a.mu.Unlock()
		return
	}
	delete(a.grants, token)
	a.mu.Unlock()
	grant.context.Lease.releaseToken()
}

func (a *Authorization) pruneExpiredLocked() []*tokenGrant {
	now := a.now()
	var expired []*tokenGrant
	for token, grant := range a.grants {
		if now.Before(grant.expiresAt) {
			continue
		}
		delete(a.grants, token)
		if grant.timer != nil {
			grant.timer.Stop()
		}
		expired = append(expired, grant)
	}
	return expired
}

func (a *Authorization) clearLocked() []*tokenGrant {
	cleared := make([]*tokenGrant, 0, len(a.grants))
	for token, grant := range a.grants {
		delete(a.grants, token)
		if grant.timer != nil {
			grant.timer.Stop()
		}
		cleared = append(cleared, grant)
	}
	return cleared
}

func releaseGrants(grants []*tokenGrant) {
	for _, grant := range grants {
		grant.context.Lease.releaseToken()
	}
}

func newSystemToken() (string, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", err
	}
	return hex.EncodeToString(payload), nil
}

func cloneGrantContext(value grantContext) grantContext {
	value.Final.Argv = append([]string(nil), value.Final.Argv...)
	value.Final.Environment = append([]string(nil), value.Final.Environment...)
	value.Final.TargetArgv = append([]string(nil), value.Final.TargetArgv...)
	return value
}

func sameFinalInvocation(left, right invocation.Final) bool {
	if !strings.EqualFold(left.Executable, right.Executable) || !strings.EqualFold(left.CWD, right.CWD) || len(left.Argv) != len(right.Argv) {
		return false
	}
	for index := range left.Argv {
		if left.Argv[index] != right.Argv[index] {
			return false
		}
	}
	return true
}
