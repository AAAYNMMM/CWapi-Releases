package codex

import (
	"errors"
	"fmt"
	"sync"
)

var clientProcessScopes sync.Map

func (c *Client) ownProcessTree() error {
	if c == nil || c.cmd == nil || c.cmd.Process == nil {
		return errors.New("CODEX_PROCESS_SCOPE_PROCESS_UNAVAILABLE")
	}
	if _, loaded := clientProcessScopes.Load(c); loaded {
		return nil
	}
	scope, err := attachProcessScope(c.cmd.Process.Pid)
	if err != nil {
		return err
	}
	actual, loaded := clientProcessScopes.LoadOrStore(c, scope)
	if loaded {
		_ = scope.Close()
		if actual == nil {
			return errors.New("CODEX_PROCESS_SCOPE_INVALID")
		}
	}
	return nil
}

func (c *Client) releaseProcessTree() error {
	if c == nil {
		return nil
	}
	value, ok := clientProcessScopes.LoadAndDelete(c)
	if !ok {
		return nil
	}
	scope, ok := value.(*processScope)
	if !ok || scope == nil {
		return errors.New("CODEX_PROCESS_SCOPE_INVALID")
	}
	if err := scope.Close(); err != nil {
		return fmt.Errorf("CODEX_PROCESS_SCOPE_CLOSE_FAILED: %w", err)
	}
	return nil
}
