package app

import "github.com/AAAYNMMM/CWapi/internal/config"

func (s *Service) UpdatePermissionMode(mode string) (ConfigSnapshot, error) {
	canonical, err := config.CanonicalPermissionMode(mode)
	if err != nil {
		s.recordOperationalError("permissions", "permissions.update", err)
		return s.ConfigSnapshot(), err
	}
	cfg, err := s.config.Update(func(candidate *config.Config) error {
		candidate.PermissionMode = canonical
		return nil
	})
	if err != nil {
		s.recordOperationalError("permissions", "permissions.update", err)
		return s.ConfigSnapshot(), err
	}
	s.runtimeInfo("permissions", "permission mode updated", map[string]any{"mode": canonical})
	return snapshotFromConfig(s.config.Path(), cfg), nil
}
