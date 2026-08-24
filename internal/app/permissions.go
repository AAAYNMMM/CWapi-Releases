package app

import (
	"errors"

	"github.com/AAAYNMMM/CWapi/internal/config"
)

func (s *Service) UpdatePermissionMode(mode string) (ConfigSnapshot, error) {
	canonical, err := config.CanonicalPermissionMode(mode)
	if err != nil {
		s.recordOperationalError("permissions", "permissions.update", err)
		return s.ConfigSnapshot(), err
	}
	if s.processRuntime == nil {
		err = errors.New("PERMISSION_RUNTIME_UNAVAILABLE")
		s.recordOperationalError("permissions", "permissions.update", err)
		return s.ConfigSnapshot(), err
	}
	cfg, err := s.processRuntime.UpdatePermissionMode(canonical)
	if err != nil {
		s.recordOperationalError("permissions", "permissions.update", err)
		return s.ConfigSnapshot(), err
	}
	s.runtimeInfo("permissions", "permission mode updated", map[string]any{"mode": canonical})
	return snapshotFromConfig(s.config.Path(), cfg), nil
}
