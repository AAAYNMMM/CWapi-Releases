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
	PinnedVersion          = "0.144.4-cwapi.1"
	PinnedExecutableSHA256 = "51398051c2332b6afe08dc3b9dbb4056085c197f35ca57a307ee303d450cada5"
	MaxMCPArgumentsBytes   = 262144
)

type Service struct {
	dataRoot    string
	installRoot string
	codexExe    string
	home        string
	stderrLog   string
}

type RuntimeSnapshot struct {
	Configured     bool   `json:"configured"`
	Version        string `json:"version"`
	ExecutablePath string `json:"executable_path"`
	ExecutableSHA  string `json:"executable_sha256,omitempty"`
	MCPReady       bool   `json:"mcp_ready"`
	NodePath       string `json:"node_path,omitempty"`
	BrowserPath    string `json:"browser_path,omitempty"`
}

func NewService(dataRoot string) (*Service, error) {
	if !filepath.IsAbs(dataRoot) {
		return nil, errors.New("CODEX_DATA_ROOT_NOT_ABSOLUTE")
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("CODEX_INSTALL_ROOT_RESOLVE_FAILED: %w", err)
	}
	installRoot := filepath.Dir(executable)
	return NewServiceWithInstallRoot(dataRoot, installRoot)
}

func newService(dataRoot, installRoot string) *Service {
	dataRoot = filepath.Clean(dataRoot)
	installRoot = filepath.Clean(installRoot)
	return &Service{
		dataRoot:    dataRoot,
		installRoot: installRoot,
		codexExe:    filepath.Join(installRoot, "runtime", "codex", "current", "bin", "codex.exe"),
		home:        filepath.Join(dataRoot, "state", "codex-home"),
		stderrLog:   filepath.Join(dataRoot, "logs", "codex-app-server.log"),
	}
}

func (s *Service) Snapshot() RuntimeSnapshot {
	value := RuntimeSnapshot{Version: PinnedVersion, ExecutablePath: s.codexExe}
	if info, err := os.Stat(s.codexExe); err == nil && info.Mode().IsRegular() {
		value.Configured = true
		value.ExecutableSHA, _ = hashFile(s.codexExe)
	}
	node, cli, browser := s.mcpPaths()
	if fileExists(node) && fileExists(cli) && fileExists(browser) {
		value.MCPReady = true
		value.NodePath = node
		value.BrowserPath = browser
	}
	return value
}

func (s *Service) ensureHome(permission PermissionConfig) error {
	permission, err := permission.canonical(s.dataRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.home, 0o700); err != nil {
		return fmt.Errorf("CODEX_HOME_CREATE_FAILED: %w", err)
	}
	node, cli, browser := s.mcpPaths()
	browserOutput := filepath.Join(s.dataRoot, "logs", "browser")
	temp := filepath.Join(s.dataRoot, "temp", "codex")
	globalRoot := filepath.Join(s.dataRoot, "temp", "mcp-global")
	if err := os.MkdirAll(browserOutput, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(temp, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(globalRoot, 0o700); err != nil {
		return err
	}
	if err := s.writeBaseExecPolicy(); err != nil {
		return err
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "approval_policy = \"never\"\ndefault_permissions = %s\n", tomlString(permission.ProfileID))
	builder.WriteString("\n[windows]\nsandbox = \"unelevated\"\n")
	builder.WriteString("\n[permissions.cwapi-safe]\n")
	builder.WriteString("description = \"CWapi safe: current execution root and global MCP root only\"\n")
	builder.WriteString("extends = \":workspace\"\n")
	builder.WriteString("\n[permissions.cwapi-safe.workspace_roots]\n")
	fmt.Fprintf(&builder, "%s = true\n", tomlString(globalRoot))
	builder.WriteString("\n[permissions.cwapi-safe.network]\nenabled = true\n")

	builder.WriteString("\n[permissions.cwapi-full-access]\n")
	builder.WriteString("description = \"CWapi full disk access with permanent base-layer protection\"\n")
	builder.WriteString("\n[permissions.cwapi-full-access.filesystem]\n\":root\" = \"write\"\n")
	for _, protected := range executionpolicy.ProtectedFilesystemRoots() {
		fmt.Fprintf(&builder, "%s = \"deny\"\n", tomlString(protected))
	}
	fmt.Fprintf(&builder, "%s = \"write\"\n", tomlString(globalRoot))
	builder.WriteString("\n[permissions.cwapi-full-access.network]\nenabled = true\n")

	if fileExists(node) && fileExists(cli) && fileExists(browser) {
		fmt.Fprintf(
			&builder,
			"\n[mcp_servers.playwright]\ncommand = %s\nargs = [%s, \"--browser\", \"chromium\", \"--executable-path\", %s, \"--isolated\", \"--headless\", \"--allowed-origins\", \"http://localhost:*;http://127.0.0.1:*;http://[::1]:*;https://github.com;https://api.github.com;https://raw.githubusercontent.com\", \"--block-service-workers\", \"--output-dir\", %s, \"--output-max-size\", \"536870912\"]\nstartup_timeout_ms = 45000\ntool_timeout_sec = 120\nenabled = true\n\n[mcp_servers.playwright.env]\nSystemRoot = %s\nAPPDATA = %s\nLOCALAPPDATA = %s\nHOME = %s\nTEMP = %s\nTMP = %s\nPLAYWRIGHT_BROWSERS_PATH = %s\nNODE_OPTIONS = \"--dns-result-order=ipv4first\"\n",
			tomlString(node), tomlString(cli), tomlString(browser), tomlString(browserOutput),
			tomlString(os.Getenv("SystemRoot")), tomlString(os.Getenv("APPDATA")), tomlString(os.Getenv("LOCALAPPDATA")),
			tomlString(os.Getenv("USERPROFILE")), tomlString(temp), tomlString(temp), tomlString(filepath.Dir(filepath.Dir(browser))),
		)
	}
	return writeAtomic(filepath.Join(s.home, "config.toml"), []byte(builder.String()), 0o600)
}

func (s *Service) writeBaseExecPolicy() error {
	return writeBaseExecPolicyAt(s.home)
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

func (s *Service) mcpPaths() (string, string, string) {
	node := filepath.Join(s.installRoot, "runtime", "node", "node.exe")
	cli := filepath.Join(s.installRoot, "runtime", "mcp", "playwright", "node_modules", "@playwright", "mcp", "cli.js")
	browser := filepath.Join(s.installRoot, "runtime", "browser", "chromium_headless_shell-1237", "chrome-headless-shell-win64", "chrome-headless-shell.exe")
	return node, cli, browser
}

func toml(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\"", "\\\"")
}

func tomlString(value string) string {
	return `"` + toml(value) + `"`
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
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
