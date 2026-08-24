package processruntime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AAAYNMMM/CWapi/internal/config"
	"github.com/AAAYNMMM/CWapi/internal/invocation"
)

func TestAuthorizationLinearizesPermissionIssueAndConsume(t *testing.T) {
	authorization := testAuthorization(t)
	defer authorization.Close()
	for iteration := 0; iteration < 8; iteration++ {
		if _, err := authorization.UpdateMode(config.PermissionModeFullAccess); err != nil {
			t.Fatal(err)
		}
		var release atomic.Int32
		candidate := testGrantContext(&release)
		start := make(chan struct{})
		var token string
		var issueErr, updateErr error
		var wait sync.WaitGroup
		wait.Add(2)
		go func() {
			defer wait.Done()
			<-start
			token, issueErr = authorization.Issue(config.PermissionModeFullAccess, candidate)
		}()
		go func() {
			defer wait.Done()
			<-start
			_, updateErr = authorization.UpdateMode(config.PermissionModeSafe)
		}()
		close(start)
		wait.Wait()
		if issueErr != nil || updateErr != nil {
			t.Fatalf("iteration %d issue=%v update=%v", iteration, issueErr, updateErr)
		}
		if token == "" {
			candidate.Lease.releaseHeld()
		} else if _, err := authorization.Peek(token); !errors.Is(err, ErrTokenInvalid) {
			t.Fatalf("iteration %d token survived safe update: %v", iteration, err)
		}
		if release.Load() != 1 {
			t.Fatalf("iteration %d issue/safe releases=%d", iteration, release.Load())
		}
	}

	if _, err := authorization.UpdateMode(config.PermissionModeFullAccess); err != nil {
		t.Fatal(err)
	}
	var release atomic.Int32
	context := testGrantContext(&release)
	token, err := authorization.Issue(config.PermissionModeFullAccess, context)
	if err != nil {
		t.Fatal(err)
	}
	grant, err := authorization.Peek(token)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var consumed grantContext
	var consumeErr, updateErr error
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		consumed, consumeErr = authorization.Consume(token, grant, "owner/repo", strings.Repeat("a", 40), grant.context.Final)
	}()
	go func() {
		defer wait.Done()
		<-start
		_, updateErr = authorization.UpdateMode(config.PermissionModeSafe)
	}()
	close(start)
	wait.Wait()
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	if consumeErr == nil {
		consumed.Lease.registryDone()
	} else if !errors.Is(consumeErr, ErrTokenInvalid) {
		t.Fatalf("consume race error=%v", consumeErr)
	}
	if release.Load() != 1 {
		t.Fatalf("consume/safe releases=%d consumeErr=%v", release.Load(), consumeErr)
	}
}

func TestAuthorizationThreeTokenLimitBindingAndConsume(t *testing.T) {
	authorization := testAuthorization(t)
	defer authorization.Close()
	if _, err := authorization.UpdateMode(config.PermissionModeFullAccess); err != nil {
		t.Fatal(err)
	}

	tokens := make([]string, 0, MaxSystemTokens)
	grants := make([]*tokenGrant, 0, MaxSystemTokens)
	releases := make([]atomic.Int32, MaxSystemTokens+1)
	for index := 0; index < MaxSystemTokens; index++ {
		context := testGrantContext(&releases[index])
		token, err := authorization.Issue(config.PermissionModeFullAccess, context)
		if err != nil || len(token) != 64 || strings.ToLower(token) != token {
			t.Fatalf("issue %d: token=%q err=%v", index, token, err)
		}
		grant, err := authorization.Peek(token)
		if err != nil {
			t.Fatal(err)
		}
		tokens, grants = append(tokens, token), append(grants, grant)
	}
	fourth := testGrantContext(&releases[MaxSystemTokens])
	if token, err := authorization.Issue(config.PermissionModeFullAccess, fourth); token != "" || !errors.Is(err, ErrTokenLimit) {
		t.Fatalf("fourth token=%q err=%v", token, err)
	}
	fourth.Lease.releaseHeld()

	changed := grants[0].context.Final
	changed.Argv = []string{"different"}
	if _, err := authorization.Consume(tokens[0], grants[0], "owner/repo", strings.Repeat("a", 40), changed); !errors.Is(err, ErrTokenBinding) {
		t.Fatalf("binding mismatch error = %v", err)
	}
	consumed, err := authorization.Consume(tokens[0], grants[0], "owner/repo", strings.Repeat("a", 40), grants[0].context.Final)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	consumed.Lease.mu.Lock()
	owner := consumed.Lease.owner
	consumed.Lease.mu.Unlock()
	if owner != leaseRegistry {
		t.Fatalf("consumed lease owner = %d", owner)
	}
	consumed.Lease.registryDone()
	if _, err := authorization.Peek(tokens[0]); !errors.Is(err, ErrTokenInvalid) {
		t.Fatalf("consumed token remained valid: %v", err)
	}
	if releases[0].Load() != 1 {
		t.Fatalf("consumed lease releases = %d", releases[0].Load())
	}
}

func TestAuthorizationExpiryAndSuccessfulModeUpdateClear(t *testing.T) {
	authorization := testAuthorization(t)
	authorization.ttl = 25 * time.Millisecond
	defer authorization.Close()
	if _, err := authorization.UpdateMode(config.PermissionModeFullAccess); err != nil {
		t.Fatal(err)
	}
	var expiredRelease atomic.Int32
	token, err := authorization.Issue(config.PermissionModeFullAccess, testGrantContext(&expiredRelease))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for expiredRelease.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if _, err := authorization.Peek(token); !errors.Is(err, ErrTokenInvalid) || expiredRelease.Load() != 1 {
		t.Fatalf("expiry mismatch: err=%v releases=%d", err, expiredRelease.Load())
	}

	var clearedRelease atomic.Int32
	second, err := authorization.Issue(config.PermissionModeFullAccess, testGrantContext(&clearedRelease))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := authorization.UpdateMode(config.PermissionModeSafe)
	if err != nil || updated.PermissionMode != config.PermissionModeSafe {
		t.Fatalf("safe update: cfg=%#v err=%v", updated, err)
	}
	if _, err := authorization.Peek(second); !errors.Is(err, ErrTokenInvalid) || clearedRelease.Load() != 1 {
		t.Fatalf("mode clear mismatch: err=%v releases=%d", err, clearedRelease.Load())
	}
}

func TestAuthorizationRevokesUndurableTokenAndReleasesTree(t *testing.T) {
	authorization := testAuthorization(t)
	defer authorization.Close()
	if _, err := authorization.UpdateMode(config.PermissionModeFullAccess); err != nil {
		t.Fatal(err)
	}
	var release atomic.Int32
	token, err := authorization.Issue(config.PermissionModeFullAccess, testGrantContext(&release))
	if err != nil {
		t.Fatal(err)
	}
	authorization.Revoke(token)
	if _, err := authorization.Peek(token); !errors.Is(err, ErrTokenInvalid) || release.Load() != 1 {
		t.Fatalf("undurable token remained: err=%v releases=%d", err, release.Load())
	}
}

func TestAuthorizationFailedConfigWriteKeepsModeAndToken(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config", "cwapi.json")
	manager, err := config.Open(configPath)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := NewAuthorization(manager)
	if err != nil {
		t.Fatal(err)
	}
	defer authorization.Close()
	if _, err := authorization.UpdateMode(config.PermissionModeFullAccess); err != nil {
		t.Fatal(err)
	}
	var release atomic.Int32
	token, err := authorization.Issue(config.PermissionModeFullAccess, testGrantContext(&release))
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Dir(configPath)
	if err := os.RemoveAll(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directory, []byte("not-a-directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := authorization.UpdateMode(config.PermissionModeSafe); err == nil {
		t.Fatal("permission update unexpectedly succeeded")
	}
	if manager.Snapshot().PermissionMode != config.PermissionModeFullAccess {
		t.Fatalf("runtime mode changed after failed write: %#v", manager.Snapshot())
	}
	if _, err := authorization.Peek(token); err != nil || release.Load() != 0 {
		t.Fatalf("token cleared after failed write: err=%v releases=%d", err, release.Load())
	}
}

func testAuthorization(t *testing.T) *Authorization {
	t.Helper()
	manager, err := config.Open(filepath.Join(t.TempDir(), "config", "cwapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := NewAuthorization(manager)
	if err != nil {
		t.Fatal(err)
	}
	return authorization
}

func testGrantContext(release *atomic.Int32) grantContext {
	lease := newWorkspaceLease(func() { release.Add(1) })
	lease.hold()
	return grantContext{
		RepositoryURL: "https://github.com/owner/repo", Repository: "owner/repo",
		ExpectedCommit: strings.Repeat("a", 40), RepositoryRoot: `C:\work\repo`,
		Final: invocation.Final{Executable: `C:\tools\node.exe`, Argv: []string{"script.js"}, CWD: `C:\work\repo`},
		Lease: lease,
	}
}
