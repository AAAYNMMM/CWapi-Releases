package transport

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

const GmailComposeScope = "https://www.googleapis.com/auth/gmail.compose"

const oauthCallbackPath = "/oauth2/callback"

const oauthSuccessHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>CWapi Gmail 授权成功</title>
<style>
body{font-family:Segoe UI,Microsoft YaHei,sans-serif;background:#f6f7f9;color:#111827;margin:0;display:grid;place-items:center;min-height:100vh}
main{max-width:560px;background:white;border:1px solid #e5e7eb;border-radius:16px;padding:32px;box-shadow:0 12px 40px rgba(0,0,0,.08)}
h1{margin-top:0;font-size:26px}p{line-height:1.7;color:#4b5563}.ok{font-weight:700;color:#047857}
</style>
</head>
<body><main><h1>CWapi Gmail 授权成功</h1><p class="ok">Google 授权已完成，token 和 Gmail 账号均已验证。</p><p>CWapi 正在完成本地连接，软件窗口会自动更新。这个页面可以关闭。</p></main></body>
</html>`

type installedCredentials struct {
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	AuthURI      string   `json:"auth_uri"`
	TokenURI     string   `json:"token_uri"`
	RedirectURIs []string `json:"redirect_uris"`
}

type credentialsFile struct {
	Installed installedCredentials `json:"installed"`
}

type authorizationTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

type OAuthManager struct {
	credentialsPath     string
	tokens              *TokenManager
	client              HTTPDoer
	events              *EventLog
	now                 func() time.Time
	openBrowser         func(string) error
	completionValidator func(context.Context) error
}

func NewOAuthManager(
	credentialsPath string,
	tokens *TokenManager,
	client HTTPDoer,
	events *EventLog,
) *OAuthManager {
	return &OAuthManager{
		credentialsPath: credentialsPath,
		tokens:          tokens,
		client:          client,
		events:          events,
		now:             time.Now,
		openBrowser:     openSystemBrowser,
	}
}

func (m *OAuthManager) SetCompletionValidator(validator func(context.Context) error) {
	m.completionValidator = validator
}

func (m *OAuthManager) Authorize(ctx context.Context) error {
	credentials, err := loadInstalledCredentials(m.credentialsPath)
	if err != nil {
		m.appendFailure(err)
		return err
	}
	if m.tokens == nil {
		err := errors.New("OAuth token manager is not configured")
		m.appendFailure(err)
		return err
	}
	if m.client == nil {
		err := errors.New("OAuth HTTP client is not configured")
		m.appendFailure(err)
		return err
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		err = fmt.Errorf("start OAuth callback listener: %w", err)
		m.appendFailure(err)
		return err
	}
	defer listener.Close()

	state, err := randomOAuthState()
	if err != nil {
		m.appendFailure(err)
		return err
	}
	redirectURI := "http://" + listener.Addr().String() + oauthCallbackPath
	authorizationURL, err := buildAuthorizationURL(credentials, redirectURI, state)
	if err != nil {
		m.appendFailure(err)
		return err
	}

	type callbackResult struct {
		code         string
		err          error
		completion   chan error
		responseDone chan struct{}
	}
	result := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	mux.HandleFunc(oauthCallbackPath, func(writer http.ResponseWriter, request *http.Request) {
		query := request.URL.Query()
		if query.Get("state") != state {
			http.Error(writer, "OAuth state validation failed.", http.StatusBadRequest)
			select {
			case result <- callbackResult{err: errors.New("OAuth callback state mismatch")}:
			default:
			}
			return
		}
		if oauthError := strings.TrimSpace(query.Get("error")); oauthError != "" {
			description := strings.TrimSpace(query.Get("error_description"))
			if description != "" {
				oauthError += ": " + description
			}
			http.Error(writer, "Google authorization was not completed.", http.StatusBadRequest)
			select {
			case result <- callbackResult{err: fmt.Errorf("OAuth callback error: %s", oauthError)}:
			default:
			}
			return
		}
		code := strings.TrimSpace(query.Get("code"))
		if code == "" {
			http.Error(writer, "OAuth callback did not include a code.", http.StatusBadRequest)
			select {
			case result <- callbackResult{err: errors.New("OAuth callback has no authorization code")}:
			default:
			}
			return
		}

		completion := make(chan error, 1)
		responseDone := make(chan struct{})
		select {
		case result <- callbackResult{
			code:         code,
			completion:   completion,
			responseDone: responseDone,
		}:
		case <-request.Context().Done():
			return
		}

		completionErr := <-completion
		if completionErr != nil {
			http.Error(
				writer,
				"CWapi Gmail authorization could not be completed. Return to CWapi and retry.",
				http.StatusInternalServerError,
			)
			close(responseDone)
			return
		}

		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("Connection", "close")
		_, _ = writer.Write([]byte(oauthSuccessHTML))
		close(responseDone)
	})

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- server.Serve(listener)
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		select {
		case <-serveDone:
		default:
		}
	}()

	if m.events != nil {
		m.events.Append("oauth_authorization_started", "INFO", "Google OAuth authorization started.", nil)
	}
	if err := m.openBrowser(authorizationURL); err != nil {
		err = fmt.Errorf("open OAuth browser: %w", err)
		m.appendFailure(err)
		return err
	}

	var callback callbackResult
	select {
	case <-ctx.Done():
		err := fmt.Errorf("wait for OAuth callback: %w", ctx.Err())
		m.appendFailure(err)
		return err
	case callback = <-result:
	}
	if callback.err != nil {
		m.appendFailure(callback.err)
		return callback.err
	}

	finishCallback := func(completionErr error) {
		if callback.completion == nil {
			return
		}
		select {
		case callback.completion <- completionErr:
		case <-ctx.Done():
			return
		}
		if callback.responseDone == nil {
			return
		}
		select {
		case <-callback.responseDone:
		case <-ctx.Done():
		}
	}

	token, err := m.exchangeCode(ctx, credentials, redirectURI, callback.code)
	if err != nil {
		finishCallback(err)
		m.appendFailure(err)
		return err
	}
	if err := m.tokens.StoreAuthorizedToken(token); err != nil {
		err = fmt.Errorf("store authorized token: %w", err)
		finishCallback(err)
		m.appendFailure(err)
		return err
	}
	if m.completionValidator != nil {
		if err := m.completionValidator(ctx); err != nil {
			err = fmt.Errorf("validate authorized Gmail account: %w", err)
			finishCallback(err)
			m.appendFailure(err)
			return err
		}
	}

	finishCallback(nil)
	if m.events != nil {
		m.events.Append("oauth_authorization_completed", "INFO", "Google OAuth authorization completed.", nil)
	}
	return nil
}

func (m *OAuthManager) appendFailure(err error) {
	if m.events == nil || err == nil {
		return
	}
	m.events.Append(
		"oauth_authorization_failed",
		"ERROR",
		"Google OAuth authorization failed.",
		map[string]any{"error": err.Error()},
	)
}

func (m *OAuthManager) exchangeCode(
	ctx context.Context,
	credentials installedCredentials,
	redirectURI string,
	code string,
) (TokenFile, error) {
	form := url.Values{
		"code":          {code},
		"client_id":     {credentials.ClientID},
		"client_secret": {credentials.ClientSecret},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		credentials.TokenURI,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return TokenFile{}, fmt.Errorf("build OAuth token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := m.client.Do(req)
	if err != nil {
		return TokenFile{}, fmt.Errorf("exchange OAuth code: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return TokenFile{}, fmt.Errorf("exchange OAuth code: HTTP %s", response.Status)
	}
	var exchanged authorizationTokenResponse
	if err := json.NewDecoder(response.Body).Decode(&exchanged); err != nil {
		return TokenFile{}, fmt.Errorf("decode OAuth token response: %w", err)
	}
	if strings.TrimSpace(exchanged.AccessToken) == "" {
		return TokenFile{}, errors.New("OAuth token response has no access_token")
	}
	if strings.TrimSpace(exchanged.RefreshToken) == "" {
		return TokenFile{}, errors.New("OAuth token response has no refresh_token")
	}
	if exchanged.ExpiresIn <= 0 {
		exchanged.ExpiresIn = 3600
	}
	scopes := strings.Fields(exchanged.Scope)
	if len(scopes) == 0 {
		scopes = []string{GmailComposeScope}
	}
	return TokenFile{
		Token:        exchanged.AccessToken,
		RefreshToken: exchanged.RefreshToken,
		TokenURI:     credentials.TokenURI,
		ClientID:     credentials.ClientID,
		ClientSecret: credentials.ClientSecret,
		Scopes:       scopes,
		Expiry:       m.now().Add(time.Duration(exchanged.ExpiresIn) * time.Second).UTC().Format(time.RFC3339Nano),
	}, nil
}

func loadInstalledCredentials(path string) (installedCredentials, error) {
	if strings.TrimSpace(path) == "" {
		return installedCredentials{}, errors.New("OAuth credentials path is empty")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return installedCredentials{}, fmt.Errorf("read OAuth credentials: %w", err)
	}
	var decoded credentialsFile
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return installedCredentials{}, fmt.Errorf("decode OAuth credentials: %w", err)
	}
	credentials := decoded.Installed
	if strings.TrimSpace(credentials.ClientID) == "" || strings.TrimSpace(credentials.ClientSecret) == "" {
		return installedCredentials{}, errors.New("OAuth credentials installed client is missing client_id/client_secret")
	}
	if strings.TrimSpace(credentials.AuthURI) == "" {
		credentials.AuthURI = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if strings.TrimSpace(credentials.TokenURI) == "" {
		credentials.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return credentials, nil
}

func buildAuthorizationURL(credentials installedCredentials, redirectURI, state string) (string, error) {
	authURL, err := url.Parse(credentials.AuthURI)
	if err != nil {
		return "", fmt.Errorf("parse OAuth auth_uri: %w", err)
	}
	query := authURL.Query()
	query.Set("client_id", credentials.ClientID)
	query.Set("redirect_uri", redirectURI)
	query.Set("response_type", "code")
	query.Set("scope", GmailComposeScope)
	query.Set("access_type", "offline")
	query.Set("prompt", "select_account consent")
	query.Set("state", state)
	authURL.RawQuery = query.Encode()
	return authURL.String(), nil
}

func randomOAuthState() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func openSystemBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	if err := command.Start(); err != nil {
		return err
	}
	return command.Process.Release()
}
