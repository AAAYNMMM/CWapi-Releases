package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"sort"
	"strings"
)

const (
	PermissionProfileSafe       = "cwapi-safe"
	PermissionProfileFullAccess = "cwapi-full-access"
)

// PermissionConfig selects a Codex-native permission profile and supplies the
// configured project roots that the safe profile may write.
type PermissionConfig struct {
	ProfileID    string
	ProjectPaths []string
}

func (c PermissionConfig) canonical(dataRoot string) (PermissionConfig, error) {
	profileID := strings.TrimSpace(c.ProfileID)
	if profileID == "" {
		profileID = PermissionProfileSafe
	}
	switch profileID {
	case PermissionProfileSafe, PermissionProfileFullAccess:
	default:
		return PermissionConfig{}, errors.New("CODEX_PERMISSION_PROFILE_INVALID")
	}

	seen := map[string]struct{}{}
	paths := make([]string, 0, len(c.ProjectPaths)+1)
	for _, value := range append([]string{dataRoot}, c.ProjectPaths...) {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		cleaned := filepath.Clean(value)
		key := strings.ToLower(cleaned)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, cleaned)
	}
	sort.Slice(paths, func(i, j int) bool {
		return strings.ToLower(paths[i]) < strings.ToLower(paths[j])
	})
	return PermissionConfig{ProfileID: profileID, ProjectPaths: paths}, nil
}

func (c PermissionConfig) key(dataRoot string) (string, error) {
	canonical, err := c.canonical(dataRoot)
	if err != nil {
		return "", err
	}
	payload := canonical.ProfileID + "\x00" + strings.Join(canonical.ProjectPaths, "\x00")
	digest := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(digest[:]), nil
}

func (c PermissionConfig) threadCWD(dataRoot string) string {
	// Global MCP/status calls have no project identity. Keep their context in
	// CWapi data instead of silently picking one configured project. Project
	// work supplies an exact detached-worktree CWD per request.
	return filepath.Clean(dataRoot)
}
