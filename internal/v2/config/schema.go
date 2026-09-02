package config

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strings"
)

const (
	Schema          = "cwapi.config.v3"
	Version         = "2.0.4"
	previousVersion = "2.0.3"

	DefaultMCPPort   = 32124
	DefaultAgentPort = 32123

	CodingTokenPrefix = "cwapi_coding_"
	AgentTokenPrefix  = "cwapi_agent_"
	AgentAPIKeyPrefix = "cwapi_"

	AccessProfileSafe = "safe"
	AccessProfileFull = "full"
)

var (
	codingTokenPattern = regexp.MustCompile(`^cwapi_coding_[A-Z2-7]{26,128}$`)
	agentTokenPattern  = regexp.MustCompile(`^cwapi_agent_[A-Z2-7]{26,128}$`)
	agentAPIKeyPattern = regexp.MustCompile(`^cwapi_[A-Z2-7]{26,128}$`)
	tunnelIDPattern    = regexp.MustCompile(`^tunnel_[a-z0-9]{32}$`)
)

type MCPConfig struct {
	Port        int    `json:"port"`
	CodingToken string `json:"coding_token"`
	AgentToken  string `json:"agent_token"`
}

type CodexConfig struct {
	Executable       string `json:"executable"`
	AccessProfile    string `json:"access_profile"`
	NetworkAccess    bool   `json:"network_access"`
	RemoteGitRewrite bool   `json:"remote_git_rewrite"`
}

type AgentConfig struct {
	Enabled bool   `json:"enabled"`
	Port    int    `json:"port"`
	APIKey  string `json:"api_key"`
}

// TunnelConfig contains the non-secret OpenAI Secure MCP Tunnel settings.
// The Runtime API key is kept in Windows Credential Manager and is never part
// of cwapi.config.v3.
type TunnelConfig struct {
	Enabled  bool   `json:"enabled"`
	TunnelID string `json:"tunnel_id"`
}

type Config struct {
	Schema      string       `json:"schema"`
	Version     string       `json:"version"`
	MCP         MCPConfig    `json:"mcp"`
	Codex       CodexConfig  `json:"codex"`
	Agent       AgentConfig  `json:"agent"`
	Tunnel      TunnelConfig `json:"tunnel"`
	AgentTunnel TunnelConfig `json:"agent_tunnel"`
}

func Default() Config {
	return Config{
		Schema:  Schema,
		Version: Version,
		MCP: MCPConfig{
			Port:        DefaultMCPPort,
			CodingToken: NewCodingToken(),
			AgentToken:  NewAgentToken(),
		},
		Codex: CodexConfig{AccessProfile: AccessProfileSafe, NetworkAccess: false},
		Agent: AgentConfig{
			Enabled: true,
			Port:    DefaultAgentPort,
			APIKey:  NewAgentAPIKey(),
		},
		Tunnel:      TunnelConfig{},
		AgentTunnel: TunnelConfig{},
	}
}

func NewCodingToken() string { return CodingTokenPrefix + rand.Text() }
func NewAgentToken() string  { return AgentTokenPrefix + rand.Text() }
func NewAgentAPIKey() string { return AgentAPIKeyPrefix + rand.Text() }

func Validate(c Config) error {
	if c.Schema != Schema {
		return fmt.Errorf("CONFIG_SCHEMA_UNSUPPORTED: expected %q, got %q", Schema, c.Schema)
	}
	if c.Version != Version {
		return fmt.Errorf("CONFIG_VERSION_UNSUPPORTED: expected %q, got %q", Version, c.Version)
	}
	if err := ValidateMCP(c.MCP); err != nil {
		return err
	}
	if err := ValidateCodex(c.Codex); err != nil {
		return err
	}
	if err := ValidateAgent(c.Agent, c.MCP.Port); err != nil {
		return err
	}
	if err := ValidateTunnel(c.Tunnel); err != nil {
		return err
	}
	if c.AgentTunnel.Enabled && !c.Agent.Enabled {
		return fmt.Errorf("CONFIG_AGENT_TUNNEL_REQUIRES_AGENT")
	}
	return ValidateTunnel(c.AgentTunnel)
}

func ValidateMCP(c MCPConfig) error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("CONFIG_MCP_PORT_INVALID: %d", c.Port)
	}
	if !codingTokenPattern.MatchString(c.CodingToken) {
		return fmt.Errorf("CONFIG_MCP_CODING_TOKEN_INVALID")
	}
	if !agentTokenPattern.MatchString(c.AgentToken) {
		return fmt.Errorf("CONFIG_MCP_AGENT_TOKEN_INVALID")
	}
	if c.CodingToken == c.AgentToken {
		return fmt.Errorf("CONFIG_MCP_TOKENS_MUST_DIFFER")
	}
	return nil
}

func ValidateCodex(c CodexConfig) error {
	if c.Executable != strings.TrimSpace(c.Executable) {
		return fmt.Errorf("CONFIG_CODEX_EXECUTABLE_INVALID")
	}
	switch c.AccessProfile {
	case AccessProfileSafe, AccessProfileFull:
		return nil
	default:
		return fmt.Errorf("CONFIG_CODEX_ACCESS_PROFILE_INVALID: %q", c.AccessProfile)
	}
}

func ValidateAgent(c AgentConfig, mcpPort int) error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("CONFIG_AGENT_PORT_INVALID: %d", c.Port)
	}
	if c.Enabled && mcpPort > 0 && c.Port == mcpPort {
		return fmt.Errorf("CONFIG_AGENT_PORT_CONFLICT: %d", c.Port)
	}
	if !agentAPIKeyPattern.MatchString(c.APIKey) {
		return fmt.Errorf("CONFIG_AGENT_API_KEY_INVALID")
	}
	return nil
}

func ValidateTunnel(c TunnelConfig) error {
	if c.TunnelID != strings.TrimSpace(c.TunnelID) {
		return fmt.Errorf("CONFIG_TUNNEL_ID_INVALID")
	}
	if c.TunnelID == "" {
		if c.Enabled {
			return fmt.Errorf("CONFIG_TUNNEL_ID_REQUIRED")
		}
		return nil
	}
	if !tunnelIDPattern.MatchString(c.TunnelID) {
		return fmt.Errorf("CONFIG_TUNNEL_ID_INVALID")
	}
	return nil
}

func ValidateTunnelID(value string) error {
	if value != strings.TrimSpace(value) || !tunnelIDPattern.MatchString(value) {
		return fmt.Errorf("CONFIG_TUNNEL_ID_INVALID")
	}
	return nil
}
