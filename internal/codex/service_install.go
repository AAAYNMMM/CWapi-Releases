package codex

import (
	"errors"
	"path/filepath"
)

// NewServiceWithInstallRoot is used by release validation to exercise the
// exact packaged private Codex runtime before the package is accepted.
func NewServiceWithInstallRoot(dataRoot, installRoot string) (*Service, error) {
	if !filepath.IsAbs(dataRoot) {
		return nil, errors.New("CODEX_DATA_ROOT_NOT_ABSOLUTE")
	}
	if !filepath.IsAbs(installRoot) {
		return nil, errors.New("CODEX_INSTALL_ROOT_NOT_ABSOLUTE")
	}
	return newService(dataRoot, installRoot), nil
}
