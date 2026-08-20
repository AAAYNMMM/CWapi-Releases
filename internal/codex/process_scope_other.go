//go:build !windows

package codex

type processScope struct{}

func attachProcessScope(int) (*processScope, error) {
	return &processScope{}, nil
}

func (s *processScope) Close() error {
	return nil
}
