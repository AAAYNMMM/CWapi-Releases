package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/AAAYNMMM/CWapi/internal/executionpolicy"
)

const (
	PinnedVersion          = "0.150.1"
	PinnedExecutableSHA256 = "cbd657ddfe151d1a6ebad660beffdbd3265dc5aff4b3a6095124d3e2f0156f2f"
)

type Service struct {
	dataRoot string
	codexExe string
}

type RuntimeSnapshot struct {
	Configured     bool   `json:"configured"`
	Version        string `json:"version"`
	ExecutablePath string `json:"executable_path"`
	ExecutableSHA  string `json:"executable_sha256,omitempty"`
}

func NewService(dataRoot string) (*Service, error) {
	if !filepath.IsAbs(dataRoot) {
		return nil, errors.New("CODEX_DATA_ROOT_NOT_ABSOLUTE")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("CODEX_INSTALL_ROOT_RESOLVE_FAILED: %w", err)
	}
	return NewServiceWithInstallRoot(dataRoot, filepath.Dir(executable))
}

// NewCommandService builds the model-free command runtime around either the
// bundled Codex executable or an explicitly pinned executable used by tests and
// development builds. It never reads the user's native CODEX_HOME.
func NewCommandService(dataRoot, executable string) (*Service, error) {
	if !filepath.IsAbs(dataRoot) {
		return nil, errors.New("CODEX_DATA_ROOT_NOT_ABSOLUTE")
	}
	executable = strings.TrimSpace(executable)
	if executable == "" {
		return NewService(dataRoot)
	}
	if !filepath.IsAbs(executable) {
		return nil, errors.New("CODEX_EXECUTABLE_NOT_ABSOLUTE")
	}
	executable = filepath.Clean(executable)
	info, err := os.Stat(executable)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("CODEX_EXECUTABLE_UNAVAILABLE")
	}
	service := newService(filepath.Clean(dataRoot), filepath.Dir(executable))
	service.codexExe = executable
	return service, nil
}

func newService(dataRoot, installRoot string) *Service {
	return &Service{
		dataRoot: filepath.Clean(dataRoot),
		codexExe: filepath.Join(filepath.Clean(installRoot), "runtime", "codex", "current", "bin", "codex.exe"),
	}
}

func (s *Service) Snapshot() RuntimeSnapshot {
	value := RuntimeSnapshot{Version: PinnedVersion}
	if s == nil {
		return value
	}
	value.ExecutablePath = s.codexExe
	if info, err := os.Stat(s.codexExe); err == nil && info.Mode().IsRegular() {
		value.Configured = true
		value.ExecutableSHA, _ = hashFile(s.codexExe)
	}
	return value
}

func writeBaseExecPolicyAt(home string) error {
	rulesDir := filepath.Join(home, "rules")
	if err := os.MkdirAll(rulesDir, 0o700); err != nil {
		return fmt.Errorf("CODEX_RULES_DIR_CREATE_FAILED: %w", err)
	}
	return writeAtomic(filepath.Join(rulesDir, "default.rules"), []byte(executionpolicy.CodexRules()), 0o600)
}

func pathWithin(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if strings.EqualFold(path, root) {
		return true
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func writeAtomic(target string, data []byte, mode os.FileMode) error {
	temporary := target + ".tmp"
	if err := os.WriteFile(temporary, data, mode); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}

func hashFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}
