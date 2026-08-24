//go:build !windows

package processscope

type Scope struct{}

func Attach(int) (*Scope, error) { return &Scope{}, nil }
func (s *Scope) Close() error    { return nil }
