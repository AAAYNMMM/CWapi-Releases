package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/repository"
)

// DeleteAt removes exactly one durable repository workspace. It does not try
// to repair Git and is intentionally kept outside MCP Coding tools. Callers
// must first stop the owning Coding service so no session can race deletion.
func DeleteAt(dataRoot, repositoryName string) error {
	dataRoot = strings.TrimSpace(dataRoot)
	if dataRoot == "" || !filepath.IsAbs(dataRoot) {
		return errors.New("WORKSPACE_DATA_ROOT_INVALID")
	}
	repositoryName = strings.ToLower(strings.TrimSpace(repositoryName))
	identity, err := repository.Parse("https://github.com/" + repositoryName)
	if err != nil || identity.Repository != repositoryName {
		return errors.New("WORKSPACE_REPOSITORY_INVALID")
	}
	root := filepath.Join(filepath.Clean(dataRoot), "workspaces")
	container := filepath.Join(root, workspaceKey(identity.Repository))
	info, err := os.Lstat(container)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("WORKSPACE_NOT_FOUND")
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("WORKSPACE_CONTAINER_INVALID")
	}
	meta, err := loadMetadata(filepath.Join(container, "workspace.json"))
	if err != nil {
		return err
	}
	if meta.Repository != identity.Repository {
		return errors.New("WORKSPACE_METADATA_REPOSITORY_MISMATCH")
	}
	if err := os.RemoveAll(container); err != nil {
		return errors.New("WORKSPACE_DELETE_FAILED")
	}
	return nil
}
