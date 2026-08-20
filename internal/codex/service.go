package codex

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Configured      bool   `json:"configured"`
	Version         string `json:"version"`
	ExecutablePath  string `json:"executable_path"`
	ExecutableSHA   string `json:"executable_sha256,omitempty"`
	MCPReady        bool   `json:"mcp_ready"`
	ProcessMCPReady bool   `json:"process_mcp_ready"`
	NodePath        string `json:"node_path,omitempty"`
	BrowserPath     string `json:"browser_path,omitempty"`
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
	return newService(dataRoot, installRoot), nil
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
	if fileExists(node) && fileExists(s.processMCPPath()) && fileExists(s.processMCPInvocationPath()) && fileExists(s.processMCPOutputPath()) {
		value.ProcessMCPReady = true
	}
	return value
}

func (s *Service) ensureHome(permission PermissionConfig) error {
	permission, err := permission.canonical(s.dataRoot)
	if err != nil {
		return err
	}
	if err := validateProjectRootsAgainstPermanentDenies(permission, s.dataRoot); err != nil {
		return err
	}
	if err := os.MkdirAll(s.home, 0o700); err != nil {
		return fmt.Errorf("CODEX_HOME_CREATE_FAILED: %w", err)
	}
	node, cli, browser := s.mcpPaths()
	processServer := s.processMCPPath()
	browserOutput := filepath.Join(s.dataRoot, "logs", "browser")
	processOutput := filepath.Join(s.dataRoot, "logs", "processes")
	temp := filepath.Join(s.dataRoot, "temp", "codex")
	if err := os.MkdirAll(browserOutput, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(processOutput, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(temp, 0o700); err != nil {
		return err
	}
	if err := s.writeBaseExecPolicy(); err != nil {
		return err
	}

	var builder strings.Builder
	fmt.Fprintf(&builder, "approval_policy = \"never\"\ndefault_permissions = %s\n", tomlString(permission.ProfileID))
	builder.WriteString("\n[permissions.cwapi-safe]\n")
	builder.WriteString("description = \"CWapi safe: configured projects and CWapi data only\"\n")
	builder.WriteString("extends = \":workspace\"\n")
	builder.WriteString("\n[permissions.cwapi-safe.workspace_roots]\n")
	for _, root := range permission.ProjectPaths {
		fmt.Fprintf(&builder, "%s = true\n", tomlString(root))
	}
	builder.WriteString("\n[permissions.cwapi-safe.network]\nenabled = true\n")

	builder.WriteString("\n[permissions.cwapi-full-access]\n")
	builder.WriteString("description = \"CWapi full disk access with permanent base-layer protection\"\n")
	builder.WriteString("\n[permissions.cwapi-full-access.filesystem]\n\":root\" = \"write\"\n")
	for _, protected := range baseProtectedPaths() {
		fmt.Fprintf(&builder, "%s = \"deny\"\n", tomlString(protected))
	}
	// Configured projects and CWapi data are explicit exceptions to the broad
	// user-profile deny. System paths and Downloads are never reopened.
	for _, root := range permission.ProjectPaths {
		fmt.Fprintf(&builder, "%s = \"write\"\n", tomlString(root))
		for _, metadata := range []string{".git", ".agents", ".codex"} {
			fmt.Fprintf(&builder, "%s = \"deny\"\n", tomlString(filepath.Join(root, metadata)))
		}
	}
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
	if fileExists(node) && fileExists(processServer) && fileExists(s.processMCPInvocationPath()) && fileExists(s.processMCPOutputPath()) {
		fmt.Fprintf(
			&builder,
			"\n[mcp_servers.cwapi]\ncommand = %s\nargs = [%s]\nstartup_timeout_ms = 10000\ntool_timeout_sec = 30\nenabled = true\n\n[mcp_servers.cwapi.env]\nSystemRoot = %s\nWINDIR = %s\nPATH = %s\nTEMP = %s\nTMP = %s\nCWAPI_PROCESS_LOG_ROOT = %s\n",
			tomlString(node), tomlString(processServer), tomlString(os.Getenv("SystemRoot")),
			tomlString(os.Getenv("WINDIR")), tomlString(os.Getenv("PATH")), tomlString(temp),
			tomlString(temp), tomlString(processOutput),
		)
	}

	return writeAtomic(filepath.Join(s.home, "config.toml"), []byte(builder.String()), 0o600)
}

func (s *Service) writeBaseExecPolicy() error {
	rulesDir := filepath.Join(s.home, "rules")
	if err := os.MkdirAll(rulesDir, 0o700); err != nil {
		return fmt.Errorf("CODEX_RULES_DIR_CREATE_FAILED: %w", err)
	}
	const rules = `# CWapi permanent base safety rules. These are loaded by stock Codex execpolicy.
prefix_rule(pattern=["format"], decision="forbidden", justification="CWapi never permits filesystem formatting.")
prefix_rule(pattern=["format.com"], decision="forbidden", justification="CWapi never permits filesystem formatting.")
prefix_rule(pattern=["diskpart"], decision="forbidden", justification="CWapi never permits disk partition mutation.")
prefix_rule(pattern=["diskpart.exe"], decision="forbidden", justification="CWapi never permits disk partition mutation.")
prefix_rule(pattern=["bcdedit"], decision="forbidden", justification="CWapi does not modify system boot configuration.")
prefix_rule(pattern=["bcdedit.exe"], decision="forbidden", justification="CWapi does not modify system boot configuration.")
prefix_rule(pattern=["regedit"], decision="forbidden", justification="CWapi does not expose system registry mutation as a generic execution path.")
prefix_rule(pattern=["regedit.exe"], decision="forbidden", justification="CWapi does not expose system registry mutation as a generic execution path.")
prefix_rule(pattern=["taskkill"], decision="forbidden", justification="Generic process termination is forbidden; typed CWapi process lifecycle owns project shutdown.")
prefix_rule(pattern=["taskkill.exe"], decision="forbidden", justification="Generic process termination is forbidden; typed CWapi process lifecycle owns project shutdown.")
prefix_rule(pattern=["Stop-Process"], decision="forbidden", justification="Generic process termination is forbidden; typed CWapi process lifecycle owns project shutdown.")
prefix_rule(pattern=["git", "add"], decision="forbidden", justification="MCP execution cannot stage repository changes.")
prefix_rule(pattern=["git", "commit"], decision="forbidden", justification="MCP execution cannot create commits.")
prefix_rule(pattern=["git", "checkout"], decision="forbidden", justification="CWapi owns the exact-commit workspace.")
prefix_rule(pattern=["git", "switch"], decision="forbidden", justification="CWapi owns the exact-commit workspace.")
prefix_rule(pattern=["git", "restore"], decision="forbidden", justification="MCP execution cannot mutate tracked files through Git.")
prefix_rule(pattern=["git", "reset"], decision="forbidden", justification="MCP execution cannot reset repository state or history.")
prefix_rule(pattern=["git", "clean"], decision="forbidden", justification="MCP execution cannot delete untracked repository content.")
prefix_rule(pattern=["git", "merge"], decision="forbidden", justification="MCP execution cannot mutate repository history.")
prefix_rule(pattern=["git", "rebase"], decision="forbidden", justification="MCP execution cannot rewrite repository history.")
prefix_rule(pattern=["git", "cherry-pick"], decision="forbidden", justification="MCP execution cannot mutate repository history.")
prefix_rule(pattern=["git", "revert"], decision="forbidden", justification="MCP execution cannot create history mutations.")
prefix_rule(pattern=["git", "filter-branch"], decision="forbidden", justification="CWapi never permits Git history rewriting through MCP.")
prefix_rule(pattern=["git", "push"], decision="forbidden", justification="MCP execution cannot mutate remotes.")
prefix_rule(pattern=["git", "pull"], decision="forbidden", justification="CWapi owns exact-commit synchronization.")
prefix_rule(pattern=["git", "fetch"], decision="forbidden", justification="CWapi owns exact-commit synchronization.")
prefix_rule(pattern=["git", "branch", ["-d", "-D", "--delete", "-m", "-M", "--move", "-c", "-C", "--copy"]], decision="forbidden", justification="MCP execution cannot mutate local branches.")
prefix_rule(pattern=["git", "tag", ["-d", "--delete", "-f", "--force", "-a", "-s", "-u"]], decision="forbidden", justification="MCP execution cannot create, delete, or rewrite tags.")
`
	return writeAtomic(filepath.Join(rulesDir, "default.rules"), []byte(rules), 0o600)
}

func baseProtectedPaths() []string {
	userProfile := strings.TrimSpace(os.Getenv("USERPROFILE"))
	candidates := []string{
		os.Getenv("SystemRoot"),
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		userProfile,
		os.Getenv("HOME"),
	}
	if userProfile != "" {
		candidates = append(candidates, filepath.Join(userProfile, "Downloads"))
	}
	return canonicalPathSet(candidates)
}

func nonOverridableBasePaths() []string {
	userProfile := strings.TrimSpace(os.Getenv("USERPROFILE"))
	candidates := []string{
		os.Getenv("SystemRoot"),
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
	}
	if userProfile != "" {
		candidates = append(candidates, filepath.Join(userProfile, "Downloads"))
	}
	return canonicalPathSet(candidates)
}

func canonicalPathSet(candidates []string) []string {
	seen := map[string]struct{}{}
	paths := make([]string, 0, len(candidates))
	for _, value := range candidates {
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
	sort.Slice(paths, func(i, j int) bool { return strings.ToLower(paths[i]) < strings.ToLower(paths[j]) })
	return paths
}

func validateProjectRootsAgainstPermanentDenies(permission PermissionConfig, dataRoot string) error {
	dataRoot = filepath.Clean(dataRoot)
	protected := nonOverridableBasePaths()
	for _, root := range permission.ProjectPaths {
		if strings.EqualFold(filepath.Clean(root), dataRoot) {
			continue
		}
		for _, deny := range protected {
			if pathWithin(root, deny) {
				return fmt.Errorf("CODEX_PROJECT_PATH_BASE_PROTECTED: %s", root)
			}
		}
	}
	return nil
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

func (s *Service) processMCPPath() string {
	return filepath.Join(s.installRoot, "runtime", "mcp", "cwapi", "process-server.cjs")
}

func (s *Service) processMCPOutputPath() string {
	return filepath.Join(s.installRoot, "runtime", "mcp", "cwapi", "process-output.cjs")
}

func (s *Service) processMCPInvocationPath() string {
	return filepath.Join(s.installRoot, "runtime", "mcp", "cwapi", "process-invocation.cjs")
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
