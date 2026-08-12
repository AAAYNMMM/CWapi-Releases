package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type TokenFile struct {
	Token        string   `json:"token"`
	RefreshToken string   `json:"refresh_token"`
	TokenURI     string   `json:"token_uri"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Scopes       []string `json:"scopes,omitempty"`
	Expiry       string   `json:"expiry,omitempty"`
}

type refreshResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token,omitempty"`
	Scope        string `json:"scope,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
}

type TokenManager struct {
	path   string
	client HTTPDoer
	now    func() time.Time
	health *HealthState

	mu     sync.Mutex
	loaded bool
	token  TokenFile
}

func NewTokenManager(path string, client HTTPDoer) *TokenManager {
	return &TokenManager{path: path, client: client, now: time.Now}
}

func (m *TokenManager) SetHealthState(health *HealthState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health = health
}

func (m *TokenManager) AccessToken(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if err := m.loadLocked(); err != nil {
		var syntaxError *json.SyntaxError
		if errors.Is(err, os.ErrNotExist) || errors.As(err, &syntaxError) {
			m.requireAuthorizationLocked()
		}
		return "", err
	}
	if m.token.Token != "" && !m.expiredLocked() {
		return m.token.Token, nil
	}
	if err := m.refreshLocked(ctx); err != nil {
		return "", err
	}
	return m.token.Token, nil
}

func (m *TokenManager) ForceRefresh(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.loadLocked(); err != nil {
		m.requireAuthorizationLocked()
		return "", err
	}
	m.token.Token = ""
	if err := m.refreshLocked(ctx); err != nil {
		return "", err
	}
	return m.token.Token, nil
}

func (m *TokenManager) StoreAuthorizedToken(token TokenFile) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.TrimSpace(token.Token) == "" {
		return errors.New("authorized token has no access token")
	}
	if strings.TrimSpace(token.RefreshToken) == "" {
		return errors.New("authorized token has no refresh token")
	}
	if strings.TrimSpace(token.ClientID) == "" || strings.TrimSpace(token.ClientSecret) == "" {
		return errors.New("authorized token has no OAuth client credentials")
	}
	if strings.TrimSpace(token.TokenURI) == "" {
		token.TokenURI = "https://oauth2.googleapis.com/token"
	}
	m.token = token
	m.loaded = true
	if err := m.persistLocked(); err != nil {
		return err
	}
	if m.health != nil {
		m.health.Success()
	}
	return nil
}

func (m *TokenManager) loadLocked() error {
	if m.loaded {
		return nil
	}
	raw, err := os.ReadFile(m.path)
	if err != nil {
		return fmt.Errorf("read token file: %w", err)
	}
	if err := json.Unmarshal(raw, &m.token); err != nil {
		return fmt.Errorf("decode token file: %w", err)
	}
	m.loaded = true
	return nil
}

func (m *TokenManager) expiredLocked() bool {
	if strings.TrimSpace(m.token.Expiry) == "" {
		return m.token.Token == ""
	}
	expiry, err := time.Parse(time.RFC3339Nano, m.token.Expiry)
	if err != nil {
		return true
	}
	return !m.now().Add(60 * time.Second).Before(expiry)
}

func (m *TokenManager) refreshLocked(ctx context.Context) error {
	if m.token.RefreshToken == "" || m.token.ClientID == "" || m.token.ClientSecret == "" {
		m.requireAuthorizationLocked()
		return errors.New("token file has no refresh credentials")
	}
	tokenURI := m.token.TokenURI
	if tokenURI == "" {
		tokenURI = "https://oauth2.googleapis.com/token"
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {m.token.RefreshToken},
		"client_id":     {m.token.ClientID},
		"client_secret": {m.token.ClientSecret},
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenURI,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("build refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("refresh access token: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == http.StatusBadRequest || response.StatusCode == http.StatusUnauthorized {
			m.requireAuthorizationLocked()
		}
		return fmt.Errorf("refresh access token: HTTP %s", response.Status)
	}

	var refreshed refreshResponse
	if err := json.NewDecoder(response.Body).Decode(&refreshed); err != nil {
		return fmt.Errorf("decode refresh response: %w", err)
	}
	if refreshed.AccessToken == "" {
		return errors.New("refresh response has no access_token")
	}

	m.token.Token = refreshed.AccessToken
	if refreshed.RefreshToken != "" {
		m.token.RefreshToken = refreshed.RefreshToken
	}
	if refreshed.Scope != "" {
		m.token.Scopes = strings.Fields(refreshed.Scope)
	}
	if refreshed.ExpiresIn <= 0 {
		refreshed.ExpiresIn = 3600
	}
	m.token.Expiry = m.now().Add(time.Duration(refreshed.ExpiresIn) * time.Second).
		UTC().Format(time.RFC3339Nano)
	if err := m.persistLocked(); err != nil {
		return err
	}
	if m.health != nil {
		m.health.Success()
	}
	return nil
}

func (m *TokenManager) requireAuthorizationLocked() {
	if m.health != nil {
		m.health.RequireAuthorization()
	}
}

func (m *TokenManager) persistLocked() error {
	raw, err := json.MarshalIndent(m.token, "", "  ")
	if err != nil {
		return fmt.Errorf("encode token file: %w", err)
	}
	raw = append(raw, '\n')
	directory := filepath.Dir(m.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".token-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary token file: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("protect temporary token file: %w", err)
	}
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return fmt.Errorf("write temporary token file: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync temporary token file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary token file: %w", err)
	}
	if err := os.Rename(temporaryName, m.path); err != nil {
		return fmt.Errorf("replace token file: %w", err)
	}
	return nil
}
