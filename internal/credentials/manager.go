package credentials

import (
	"errors"
	"fmt"
	"strings"
)

const (
	OpenAITunnelAPIKeyTarget      = "CWapi/2.0/OpenAI/Tunnel/APIKey"
	OpenAITunnelAgentAPIKeyTarget = "CWapi/2.0/OpenAI/Tunnel/Agent/APIKey"
	StoreName                     = "windows_credential_manager"
	maxTokenBytes                 = 4096
)

type secretStore interface {
	Read(target string) (string, bool, error)
	Write(target, value string) error
	Delete(target string) error
}

type Manager struct {
	store secretStore
}

func New() *Manager {
	return &Manager{store: newPlatformStore()}
}

func newWithStore(store secretStore) *Manager {
	return &Manager{store: store}
}

func ValidateOpenAITunnelAPIKey(value string) error {
	if value == "" || value != strings.TrimSpace(value) {
		return errors.New("OPENAI_TUNNEL_API_KEY_INVALID")
	}
	if len([]byte(value)) > maxTokenBytes || strings.IndexFunc(value, func(r rune) bool {
		return r == '\x00' || r == '\r' || r == '\n' || r == '\t' || r == ' '
	}) >= 0 {
		return errors.New("OPENAI_TUNNEL_API_KEY_INVALID")
	}
	return nil
}

func (m *Manager) ReadOpenAITunnelAPIKey() (string, bool, error) {
	return m.readOpenAITunnelAPIKey(OpenAITunnelAPIKeyTarget, "CREDENTIAL_READ_OPENAI_TUNNEL_FAILED")
}

func (m *Manager) WriteOpenAITunnelAPIKey(value string) error {
	return m.writeOpenAITunnelAPIKey(OpenAITunnelAPIKeyTarget, value, "CREDENTIAL_WRITE_OPENAI_TUNNEL_FAILED")
}

func (m *Manager) DeleteOpenAITunnelAPIKey() error {
	return m.deleteOpenAITunnelAPIKey(OpenAITunnelAPIKeyTarget, "CREDENTIAL_DELETE_OPENAI_TUNNEL_FAILED")
}

func (m *Manager) ReadOpenAITunnelAgentAPIKey() (string, bool, error) {
	return m.readOpenAITunnelAPIKey(OpenAITunnelAgentAPIKeyTarget, "CREDENTIAL_READ_OPENAI_AGENT_TUNNEL_FAILED")
}

func (m *Manager) WriteOpenAITunnelAgentAPIKey(value string) error {
	return m.writeOpenAITunnelAPIKey(OpenAITunnelAgentAPIKeyTarget, value, "CREDENTIAL_WRITE_OPENAI_AGENT_TUNNEL_FAILED")
}

func (m *Manager) DeleteOpenAITunnelAgentAPIKey() error {
	return m.deleteOpenAITunnelAPIKey(OpenAITunnelAgentAPIKeyTarget, "CREDENTIAL_DELETE_OPENAI_AGENT_TUNNEL_FAILED")
}

func (m *Manager) readOpenAITunnelAPIKey(target, readCode string) (string, bool, error) {
	if m == nil || m.store == nil {
		return "", false, errors.New("CREDENTIAL_STORE_UNAVAILABLE")
	}
	value, present, err := m.store.Read(target)
	if err != nil {
		return "", false, fmt.Errorf("%s: %w", readCode, err)
	}
	if present {
		if err := ValidateOpenAITunnelAPIKey(value); err != nil {
			return "", false, err
		}
	}
	return value, present, nil
}

func (m *Manager) writeOpenAITunnelAPIKey(target, value, writeCode string) error {
	if err := ValidateOpenAITunnelAPIKey(value); err != nil {
		return err
	}
	if m == nil || m.store == nil {
		return errors.New("CREDENTIAL_STORE_UNAVAILABLE")
	}
	if err := m.store.Write(target, value); err != nil {
		return fmt.Errorf("%s: %w", writeCode, err)
	}
	return nil
}

func (m *Manager) deleteOpenAITunnelAPIKey(target, deleteCode string) error {
	if m == nil || m.store == nil {
		return errors.New("CREDENTIAL_STORE_UNAVAILABLE")
	}
	if err := m.store.Delete(target); err != nil {
		return fmt.Errorf("%s: %w", deleteCode, err)
	}
	return nil
}
