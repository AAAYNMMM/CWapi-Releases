//go:build !windows

package tray

type noopImplementation struct{}

func newImplementation(func(), func()) implementation { return noopImplementation{} }
func (noopImplementation) Start() error               { return nil }
func (noopImplementation) Close() error               { return nil }
