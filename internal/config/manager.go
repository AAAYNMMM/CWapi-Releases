package config

import (
	"errors"
	"fmt"
	"os"
	"sync"
)

// Manager owns the one authoritative in-memory configuration snapshot.
// Mutations are persisted first; the in-memory snapshot advances only after
// the atomic write succeeds, preventing disk/runtime split-brain state.
type Manager struct {
	mu      sync.RWMutex
	path    string
	current Config
}

func Open(path string) (*Manager, error) {
	cfg, err := Load(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		cfg = Default()
		if err := SaveAtomic(path, cfg); err != nil {
			return nil, fmt.Errorf("CONFIG_INITIAL_SAVE_FAILED: %w", err)
		}
	}
	return &Manager{path: path, current: cfg.Clone()}, nil
}

func (m *Manager) Path() string {
	return m.path
}

func (m *Manager) Snapshot() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current.Clone()
}

func (m *Manager) Update(mutator func(*Config) error) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	candidate := m.current.Clone()
	if err := mutator(&candidate); err != nil {
		return m.current.Clone(), err
	}
	if err := Validate(candidate); err != nil {
		return m.current.Clone(), err
	}
	if err := SaveAtomic(m.path, candidate); err != nil {
		return m.current.Clone(), err
	}
	m.current = candidate.Clone()
	return m.current.Clone(), nil
}
