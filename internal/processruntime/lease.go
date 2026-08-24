package processruntime

import "sync"

const (
	leaseRegistry = iota
	leaseHeld
	leaseToken
	leaseReleased
)

type workspaceLease struct {
	mu      sync.Mutex
	owner   int
	release func()
}

func newWorkspaceLease(release func()) *workspaceLease {
	if release == nil {
		release = func() {}
	}
	return &workspaceLease{owner: leaseRegistry, release: release}
}

func (l *workspaceLease) hold() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != leaseRegistry {
		return false
	}
	l.owner = leaseHeld
	return true
}

func (l *workspaceLease) registryDone() {
	l.releaseOwner(leaseRegistry)
}

func (l *workspaceLease) issueToken() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != leaseHeld {
		return false
	}
	l.owner = leaseToken
	return true
}

func (l *workspaceLease) releaseHeld() {
	l.releaseOwner(leaseHeld)
}

func (l *workspaceLease) consumeToken() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.owner != leaseToken {
		return false
	}
	l.owner = leaseRegistry
	return true
}

func (l *workspaceLease) releaseToken() {
	l.releaseOwner(leaseToken)
}

func (l *workspaceLease) releaseOwner(owner int) {
	l.mu.Lock()
	if l.owner != owner {
		l.mu.Unlock()
		return
	}
	l.owner = leaseReleased
	release := l.release
	l.mu.Unlock()
	release()
}

type denialWindow struct {
	mu    sync.Mutex
	open  bool
	held  bool
	lease *workspaceLease
}

func newDenialWindow(lease *workspaceLease) *denialWindow {
	return &denialWindow{open: true, lease: lease}
}

func (w *denialWindow) denied() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.open && !w.held {
		w.held = w.lease.hold()
	}
	return w.held
}

func (w *denialWindow) close() bool {
	w.mu.Lock()
	w.open = false
	held := w.held
	w.mu.Unlock()
	return held
}
