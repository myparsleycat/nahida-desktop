package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// ChatGPT Pro/Plus login. The flow mirrors the OpenAI Codex client: authorization code with PKCE
// against the OpenAI issuer, a loopback callback on a fixed port, and requests afterwards routed
// to the Codex backend with the account id.
const (
	oauthIssuer       = "https://auth.openai.com"
	oauthClientID     = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauthCallbackPort = 1455
	oauthLoginTimeout = 5 * time.Minute
)

// providerOAuthConfig holds the endpoints of the ChatGPT login; tests point them at a local
// server.
type providerOAuthConfig struct {
	Issuer       string
	ClientID     string
	CallbackPort int
	Originator   string
}

var defaultOpenAIOAuth = providerOAuthConfig{
	Issuer:       oauthIssuer,
	ClientID:     oauthClientID,
	CallbackPort: oauthCallbackPort,
	Originator:   codexOriginator,
}

func (c providerOAuthConfig) redirectURI() string {
	return fmt.Sprintf("http://localhost:%d/auth/callback", c.CallbackPort)
}

func (c providerOAuthConfig) tokenURL() string {
	return strings.TrimRight(c.Issuer, "/") + "/oauth/token"
}

func (c providerOAuthConfig) authorizeURL(redirect, challenge, state string) string {
	query := url.Values{
		"response_type":              {"code"},
		"client_id":                  {c.ClientID},
		"redirect_uri":               {redirect},
		"scope":                      {"openid profile email offline_access"},
		"code_challenge":             {challenge},
		"code_challenge_method":      {"S256"},
		"id_token_add_organizations": {"true"},
		"codex_cli_simplified_flow":  {"true"},
		"state":                      {state},
		"originator":                 {c.Originator},
	}
	return strings.TrimRight(c.Issuer, "/") + "/oauth/authorize?" + query.Encode()
}

// oauthTokenResponse is the token endpoint payload of both the code exchange and a refresh.
type oauthTokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// oauthClaims is the subset of the id and access token claims the agent uses.
type oauthClaims struct {
	AccountID     string `json:"chatgpt_account_id"`
	Email         string `json:"email"`
	Organizations []struct {
		ID string `json:"id"`
	} `json:"organizations"`
	Auth struct {
		AccountID string `json:"chatgpt_account_id"`
	} `json:"https://api.openai.com/auth"`
}

// providerLogin is one pending ChatGPT login: the loopback listener, the PKCE verifier, and the
// channel that carries the exchanged credential back to CompleteProviderLogin.
type providerLogin struct {
	providerID string
	config     providerOAuthConfig
	redirect   string
	verifier   string
	state      string
	http       *http.Client
	listener   net.Listener
	server     *http.Server
	result     chan providerCredential
	failed     chan error
	done       chan struct{}
}

// StartProviderLogin begins a ChatGPT login, opens the authorization page, and returns its URL.
// The renderer copies the URL when the browser does not open.
func (s *Service) StartProviderLogin(ctx context.Context, providerID string) (AgentLoginView, error) {
	spec, ok := providerSpecFor(strings.TrimSpace(providerID))
	if !ok || !spec.OAuth {
		return AgentLoginView{}, fmt.Errorf("provider %q does not support account login", providerID)
	}
	client, err := s.dbClient()
	if err != nil {
		return AgentLoginView{}, err
	}
	s.credentialMu.Lock()
	err = s.migrateLegacyAPIKey(ctx, client)
	s.credentialMu.Unlock()
	if err != nil {
		return AgentLoginView{}, err
	}
	s.cancelLogin()

	login, err := newProviderLogin(spec.ID, s.oauth, s.http)
	if err != nil {
		return AgentLoginView{}, err
	}
	s.loginMu.Lock()
	s.login = login
	s.loginMu.Unlock()

	s.authorize(login)
	return AgentLoginView{Provider: spec.ID, URL: login.url()}, nil
}

// CompleteProviderLogin waits for the browser callback, exchanges the code, and stores the
// resulting credential.
func (s *Service) CompleteProviderLogin(ctx context.Context) (AgentSettingsView, error) {
	login := s.currentLogin()
	if login == nil {
		return AgentSettingsView{}, errors.New("no account login is in progress")
	}
	defer s.clearLogin(login)

	waitCtx, cancel := context.WithTimeout(ctx, oauthLoginTimeout)
	defer cancel()
	select {
	case credential := <-login.result:
		client, err := s.dbClient()
		if err != nil {
			return AgentSettingsView{}, err
		}
		s.credentialMu.Lock()
		defer s.credentialMu.Unlock()
		credentials, err := readCredentials(waitCtx, client, s.crypto)
		if err != nil {
			return AgentSettingsView{}, err
		}
		credentials[login.providerID] = credential
		if err := writeCredentials(waitCtx, client, s.crypto, credentials); err != nil {
			return AgentSettingsView{}, err
		}
		if err := applyPlanModelDefault(waitCtx, client, s.crypto, login.providerID); err != nil {
			return AgentSettingsView{}, err
		}
		return readSettings(waitCtx, client, s.crypto)
	case failure := <-login.failed:
		return AgentSettingsView{}, failure
	case <-login.done:
		return AgentSettingsView{}, errors.New("the account login was cancelled")
	case <-waitCtx.Done():
		return AgentSettingsView{}, errors.New("the account login timed out")
	}
}

// CancelProviderLogin stops a pending login and closes its callback listener.
func (s *Service) CancelProviderLogin(ctx context.Context) error {
	_ = ctx
	s.cancelLogin()
	return nil
}

// SignOutProvider forgets the stored account login of one provider.
func (s *Service) SignOutProvider(ctx context.Context, providerID string) (AgentProviderView, error) {
	spec, ok := providerSpecFor(strings.TrimSpace(providerID))
	if !ok {
		return AgentProviderView{}, fmt.Errorf("unsupported agent provider %q", providerID)
	}
	client, err := s.dbClient()
	if err != nil {
		return AgentProviderView{}, err
	}
	s.credentialMu.Lock()
	defer s.credentialMu.Unlock()
	credentials, err := readCredentials(ctx, client, s.crypto)
	if err != nil {
		return AgentProviderView{}, err
	}
	if credential := credentials[spec.ID]; credential.Type == credentialKindOAuth {
		delete(credentials, spec.ID)
		if err := writeCredentials(ctx, client, s.crypto, credentials); err != nil {
			return AgentProviderView{}, err
		}
	}
	credentials, err = readCredentials(ctx, client, s.crypto)
	if err != nil {
		return AgentProviderView{}, err
	}
	return providerView(spec, credentials[spec.ID]), nil
}

func (s *Service) currentLogin() *providerLogin {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	return s.login
}

func (s *Service) clearLogin(login *providerLogin) {
	s.loginMu.Lock()
	if s.login == login {
		s.login = nil
	}
	s.loginMu.Unlock()
	login.close()
}

func (s *Service) cancelLogin() {
	s.loginMu.Lock()
	login := s.login
	s.login = nil
	s.loginMu.Unlock()
	if login != nil {
		login.close()
	}
}

// authorize opens the browser and leaves the login pending for CompleteProviderLogin.
func (s *Service) authorize(login *providerLogin) {
	if s.shell == nil {
		return
	}
	if err := s.shell.OpenExternal(login.url()); err != nil {
		_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "agent-provider-auth", Stage: "open-browser",
			Fields: map[string]any{"provider": login.providerID},
		})
	}
}

func newProviderLogin(providerID string, config providerOAuthConfig, client *http.Client) (*providerLogin, error) {
	verifier, err := randomBase64(32)
	if err != nil {
		return nil, err
	}
	state, err := randomBase64(32)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", config.CallbackPort))
	if err != nil {
		return nil, fmt.Errorf(
			"the ChatGPT login callback port %d is busy: close the program using it and retry",
			config.CallbackPort,
		)
	}
	// A configured port is the one the login server redirects to; port zero asks the system for
	// one and is used by tests.
	if address, ok := listener.Addr().(*net.TCPAddr); ok && config.CallbackPort == 0 {
		config.CallbackPort = address.Port
	}
	if client == nil {
		client = http.DefaultClient
	}

	login := &providerLogin{
		providerID: providerID,
		config:     config,
		redirect:   config.redirectURI(),
		verifier:   verifier,
		state:      state,
		http:       client,
		listener:   listener,
		result:     make(chan providerCredential, 1),
		failed:     make(chan error, 1),
		done:       make(chan struct{}),
	}
	login.server = &http.Server{
		Handler:           http.HandlerFunc(login.handleCallback),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = login.server.Serve(listener) }()
	return login, nil
}

func (l *providerLogin) url() string {
	challenge := pkceChallenge(l.verifier)
	return l.config.authorizeURL(l.redirect, challenge, l.state)
}

func (l *providerLogin) handleCallback(response http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/auth/callback" {
		http.NotFound(response, request)
		return
	}
	query := request.URL.Query()
	message := query.Get("error_description")
	if message == "" {
		message = query.Get("error")
	}
	if message != "" {
		writeCallbackPage(response, http.StatusBadRequest, "Login failed", message)
		l.fail(errors.New(message))
		return
	}
	code := query.Get("code")
	if code == "" || query.Get("state") != l.state {
		reason := "The login response did not match this request."
		if code == "" {
			reason = "The login response carried no authorization code."
		}
		writeCallbackPage(response, http.StatusBadRequest, "Login failed", reason)
		l.fail(errors.New(reason))
		return
	}

	credential, err := l.exchange(code)
	if err != nil {
		writeCallbackPage(response, http.StatusBadGateway, "Login failed", err.Error())
		l.fail(err)
		return
	}
	writeCallbackPage(
		response,
		http.StatusOK,
		"Login complete",
		"You can close this window and return to Nahida Desktop.",
	)
	l.succeed(credential)
}

// exchange trades the authorization code for tokens and derives the account identity.
func (l *providerLogin) exchange(code string) (providerCredential, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {l.redirect},
		"client_id":     {l.config.ClientID},
		"code_verifier": {l.verifier},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return requestOAuthToken(ctx, l.http, l.config, form)
}

func (l *providerLogin) succeed(credential providerCredential) {
	select {
	case l.result <- credential:
	default:
	}
}

func (l *providerLogin) fail(err error) {
	select {
	case l.failed <- err:
	default:
	}
}

func (l *providerLogin) close() {
	select {
	case <-l.done:
		return
	default:
	}
	close(l.done)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = l.server.Shutdown(ctx)
}

// refreshProviderCredential renews an account login before it expires.
func refreshProviderCredential(
	ctx context.Context,
	client *http.Client,
	config providerOAuthConfig,
	credential providerCredential,
) (providerCredential, error) {
	if credential.Type != credentialKindOAuth || credential.Refresh == "" {
		return providerCredential{}, errors.New("provider is not connected with an account")
	}
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {credential.Refresh},
		"client_id":     {config.ClientID},
	}
	refreshed, err := requestOAuthToken(ctx, client, config, form)
	if err != nil {
		return providerCredential{}, err
	}
	// A refresh response may omit the identity tokens or the rotated refresh token, so keep what
	// the login established.
	if refreshed.AccountID == "" {
		refreshed.AccountID = credential.AccountID
	}
	if refreshed.Email == "" {
		refreshed.Email = credential.Email
	}
	if refreshed.Refresh == "" {
		refreshed.Refresh = credential.Refresh
	}
	return refreshed, nil
}

func requestOAuthToken(
	ctx context.Context,
	client *http.Client,
	config providerOAuthConfig,
	form url.Values,
) (providerCredential, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		config.tokenURL(),
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return providerCredential{}, err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", platform.UserAgent())

	response, err := client.Do(request)
	if err != nil {
		return providerCredential{}, err
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	if err != nil {
		return providerCredential{}, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return providerCredential{}, fmt.Errorf("the login server rejected the request (%d)", response.StatusCode)
	}

	var tokens oauthTokenResponse
	if err := json.Unmarshal(body, &tokens); err != nil {
		return providerCredential{}, fmt.Errorf("decode the login response: %w", err)
	}
	if tokens.AccessToken == "" {
		return providerCredential{}, errors.New("the login response carried no access token")
	}
	expiresIn := tokens.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	claims := oauthTokenClaims(tokens.IDToken)
	if accountID := oauthTokenClaims(tokens.AccessToken).AccountID; claims.AccountID == "" {
		claims.AccountID = accountID
	}
	return providerCredential{
		Type:      credentialKindOAuth,
		Access:    tokens.AccessToken,
		Refresh:   tokens.RefreshToken,
		Expires:   time.Now().Add(time.Duration(expiresIn) * time.Second).UnixMilli(),
		AccountID: claims.AccountID,
		Email:     claims.Email,
	}, nil
}

// oauthTokenClaims reads the account identity from a JWT payload. The token signature is not
// verified: the value comes straight from the login server over TLS.
func oauthTokenClaims(token string) oauthClaims {
	segments := strings.Split(token, ".")
	if len(segments) < 2 {
		return oauthClaims{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(segments[1])
	if err != nil {
		return oauthClaims{}
	}
	var claims oauthClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return oauthClaims{}
	}
	if claims.AccountID == "" {
		claims.AccountID = claims.Auth.AccountID
	}
	if claims.AccountID == "" && len(claims.Organizations) > 0 {
		claims.AccountID = claims.Organizations[0].ID
	}
	return claims
}

// pkceChallenge derives the S256 code challenge of a verifier.
func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomBase64(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func writeCallbackPage(response http.ResponseWriter, status int, title, message string) {
	response.Header().Set("Content-Type", "text/html; charset=utf-8")
	response.WriteHeader(status)
	_, _ = fmt.Fprintf(response, oauthCallbackPage, html.EscapeString(title), html.EscapeString(message))
}

const oauthCallbackPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Nahida Desktop</title>
<style>
body { font-family: system-ui, sans-serif; background: #101014; color: #f4f4f5; display: flex;
  align-items: center; justify-content: center; height: 100vh; margin: 0; }
main { text-align: center; max-width: 32rem; padding: 2rem; }
h1 { font-size: 1.25rem; margin-bottom: 0.5rem; }
p { color: #a1a1aa; margin: 0; }
</style>
</head>
<body>
<main><h1>%s</h1><p>%s</p></main>
<script>setTimeout(function () { window.close() }, 2500)</script>
</body>
</html>
`

// credentialRefresher renews one provider credential and stores the new tokens, so every run
// keeps using a valid access token.
func (s *Service) credentialRefresher(
	client *db.Client,
	providerID string,
) func(context.Context, providerCredential) (providerCredential, error) {
	return func(ctx context.Context, current providerCredential) (providerCredential, error) {
		// Refreshes are serialized: a rotated refresh token cannot be redeemed twice, and two
		// runs can expire at the same moment.
		s.refreshMu.Lock()
		defer s.refreshMu.Unlock()

		stored, found, err := s.readStoredCredential(ctx, client, providerID)
		if err != nil {
			return providerCredential{}, err
		}
		if found && stored.Refresh != "" {
			// Another run may have refreshed the tokens while this one waited for the lock.
			if !stored.needsRefresh(time.Now()) {
				return stored, nil
			}
			current = stored
		}

		refreshed, err := refreshProviderCredential(ctx, s.http, s.oauth, current)
		if err != nil {
			_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "agent-provider-auth", Stage: "refresh",
				Fields: map[string]any{"provider": providerID},
			})
			return providerCredential{}, err
		}

		s.credentialMu.Lock()
		defer s.credentialMu.Unlock()
		credentials, err := readCredentials(ctx, client, s.crypto)
		if err != nil {
			return providerCredential{}, err
		}
		// A sign-out or a new login during the refresh replaces the stored tokens; keep those
		// instead of writing back the ones the refresh produced.
		if stored, ok := credentials[providerID]; !ok || stored.Refresh != current.Refresh {
			return refreshed, nil
		}
		credentials[providerID] = refreshed
		if err := writeCredentials(ctx, client, s.crypto, credentials); err != nil {
			return providerCredential{}, err
		}
		return refreshed, nil
	}
}
