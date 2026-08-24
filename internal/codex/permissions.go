package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

const (
	PermissionProfileSafe       = "cwapi-safe"
	PermissionProfileFullAccess = "cwapi-full-access"
)

type PermissionConfig struct {
	ProfileID string
}

func (c PermissionConfig) canonical(string) (PermissionConfig, error) {
	profileID := strings.TrimSpace(c.ProfileID)
	if profileID == "" {
		profileID = PermissionProfileSafe
	}
	switch profileID {
	case PermissionProfileSafe, PermissionProfileFullAccess:
		return PermissionConfig{ProfileID: profileID}, nil
	default:
		return PermissionConfig{}, errors.New("CODEX_PERMISSION_PROFILE_INVALID")
	}
}

func (c PermissionConfig) key(dataRoot string) (string, error) {
	canonical, err := c.canonical(dataRoot)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(canonical.ProfileID))
	return hex.EncodeToString(digest[:]), nil
}

func (c PermissionConfig) threadCWD(dataRoot string) string {
	return filepath.Join(filepath.Clean(dataRoot), "temp", "mcp-global")
}
