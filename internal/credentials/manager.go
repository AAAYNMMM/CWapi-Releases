package credentials

import (
	"errors"
	"fmt"
	"strings"
)

const (
	SlackAppTokenTarget = "CWapi/v1.6.0/Slack/AppToken"
	SlackBotTokenTarget = "CWapi/v1.6.0/Slack/BotToken"
	StoreName           = "windows_credential_manager"
	maxTokenBytes       = 4096
)

type Pair struct {
	AppToken string
	BotToken string
}

type Snapshot struct {
	AppToken        string
	BotToken        string
	AppTokenPresent bool
	BotTokenPresent bool
}

type Status struct {
	Store           string `json:"store"`
	AppTokenPresent bool   `json:"app_token_present"`
	BotTokenPresent bool   `json:"bot_token_present"`
}

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

func ValidatePair(pair Pair) error {
	if err := validateToken(pair.AppToken, "xapp-", "SLACK_APP_TOKEN_INVALID"); err != nil {
		return err
	}
	if err := validateToken(pair.BotToken, "xoxb-", "SLACK_BOT_TOKEN_INVALID"); err != nil {
		return err
	}
	return nil
}

func validateToken(value, prefix, code string) error {
	if value == "" || value != strings.TrimSpace(value) || !strings.HasPrefix(value, prefix) {
		return errors.New(code)
	}
	if len([]byte(value)) > maxTokenBytes || strings.IndexFunc(value, func(r rune) bool { return r == '\x00' || r == '\r' || r == '\n' || r == '\t' || r == ' ' }) >= 0 {
		return errors.New(code)
	}
	return nil
}

func (m *Manager) Snapshot() (Snapshot, error) {
	if m == nil || m.store == nil {
		return Snapshot{}, errors.New("CREDENTIAL_STORE_UNAVAILABLE")
	}
	appToken, appPresent, err := m.store.Read(SlackAppTokenTarget)
	if err != nil {
		return Snapshot{}, fmt.Errorf("CREDENTIAL_READ_APP_FAILED: %w", err)
	}
	botToken, botPresent, err := m.store.Read(SlackBotTokenTarget)
	if err != nil {
		return Snapshot{}, fmt.Errorf("CREDENTIAL_READ_BOT_FAILED: %w", err)
	}
	return Snapshot{
		AppToken:        appToken,
		BotToken:        botToken,
		AppTokenPresent: appPresent,
		BotTokenPresent: botPresent,
	}, nil
}

func (m *Manager) Status() (Status, error) {
	snapshot, err := m.Snapshot()
	if err != nil {
		return Status{Store: StoreName}, err
	}
	return Status{
		Store:           StoreName,
		AppTokenPresent: snapshot.AppTokenPresent,
		BotTokenPresent: snapshot.BotTokenPresent,
	}, nil
}

func (m *Manager) RequirePair() (Pair, error) {
	snapshot, err := m.Snapshot()
	if err != nil {
		return Pair{}, err
	}
	if !snapshot.AppTokenPresent || !snapshot.BotTokenPresent {
		return Pair{}, errors.New("SLACK_CREDENTIALS_MISSING")
	}
	pair := Pair{AppToken: snapshot.AppToken, BotToken: snapshot.BotToken}
	if err := ValidatePair(pair); err != nil {
		return Pair{}, err
	}
	return pair, nil
}

// ReplacePair updates App/Bot tokens as one logical pair. Any second-write or
// post-write verification failure restores the exact previous presence/value
// state before returning an error.
func (m *Manager) ReplacePair(pair Pair) (Snapshot, error) {
	if err := ValidatePair(pair); err != nil {
		return Snapshot{}, err
	}
	previous, err := m.Snapshot()
	if err != nil {
		return Snapshot{}, err
	}
	if err := m.store.Write(SlackAppTokenTarget, pair.AppToken); err != nil {
		return Snapshot{}, fmt.Errorf("CREDENTIAL_WRITE_APP_FAILED: %w", err)
	}
	if err := m.store.Write(SlackBotTokenTarget, pair.BotToken); err != nil {
		return Snapshot{}, m.rollbackError("CREDENTIAL_WRITE_BOT_FAILED", err, previous)
	}
	current, err := m.Snapshot()
	if err != nil {
		return Snapshot{}, m.rollbackError("CREDENTIAL_VERIFY_READ_FAILED", err, previous)
	}
	if !current.AppTokenPresent || !current.BotTokenPresent || current.AppToken != pair.AppToken || current.BotToken != pair.BotToken {
		return Snapshot{}, m.rollbackError("CREDENTIAL_VERIFY_MISMATCH", errors.New("stored pair mismatch"), previous)
	}
	return previous, nil
}

func (m *Manager) Restore(snapshot Snapshot) error {
	if m == nil || m.store == nil {
		return errors.New("CREDENTIAL_STORE_UNAVAILABLE")
	}
	var failures []error
	if snapshot.AppTokenPresent {
		if err := m.store.Write(SlackAppTokenTarget, snapshot.AppToken); err != nil {
			failures = append(failures, fmt.Errorf("restore app: %w", err))
		}
	} else if err := m.store.Delete(SlackAppTokenTarget); err != nil {
		failures = append(failures, fmt.Errorf("delete app: %w", err))
	}
	if snapshot.BotTokenPresent {
		if err := m.store.Write(SlackBotTokenTarget, snapshot.BotToken); err != nil {
			failures = append(failures, fmt.Errorf("restore bot: %w", err))
		}
	} else if err := m.store.Delete(SlackBotTokenTarget); err != nil {
		failures = append(failures, fmt.Errorf("delete bot: %w", err))
	}
	return errors.Join(failures...)
}

func (m *Manager) rollbackError(code string, cause error, previous Snapshot) error {
	if rollbackErr := m.Restore(previous); rollbackErr != nil {
		return fmt.Errorf("%s: %v; CREDENTIAL_ROLLBACK_FAILED: %w", code, cause, rollbackErr)
	}
	return fmt.Errorf("%s: %w", code, cause)
}
