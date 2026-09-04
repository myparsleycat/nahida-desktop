package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	tokenKey = "token"

	loginPath    = "/api/auth/desktop/auth/i-want-to-login"
	sessionPath  = "/api/auth/get-session"
	signOutPath  = "/api/auth/sign-out"
	signOutWait  = 10 * time.Second
	loginTimeout = 100 * time.Second

	defaultOnlineProbe  = 120 * time.Second
	defaultOfflineProbe = 30 * time.Second
)

var errAuthExpired = errors.New("auth state expired")

type tokenStore interface {
	GetValue(ctx context.Context, key string) (*string, error)
	Upsert(ctx context.Context, key string, value *string) error
	UpdateValue(ctx context.Context, key string, value *string) error
}

type cryptor interface {
	EncryptString(string) (string, error)
	DecryptString(string) (string, error)
}

// Session matches Electron src/shared/schemas/auth.ts.
type Session struct {
	Session SessionInfo `json:"session"`
	User    User        `json:"user"`
	Drive   DriveInfo   `json:"drive"`
}

type SessionInfo struct {
	ID        string `json:"id"`
	UserID    string `json:"userId"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
	ExpiresAt string `json:"expiresAt"`
	Token     string `json:"token"`
}

type User struct {
	ID    string  `json:"id"`
	Name  string  `json:"name"`
	Email string  `json:"email"`
	Role  string  `json:"role"`
	Image *string `json:"image"`
}

type DriveInfo struct {
	ID     string `json:"id"`
	RootID string `json:"rootId"`
}

// Options wire Auth to store, HTTP, and side effects. Tray and window stay out.
type Options struct {
	Store        tokenStore
	Crypto       cryptor
	HTTP         *infra.Client
	Log          *infra.Log
	Shell        *platform.Shell
	Emit         func(name string, data any)
	AfterLogin   func()
	Do           func(*http.Request) (*http.Response, error)
	OnlineDelay  time.Duration
	OfflineDelay time.Duration
}

type sessionCall struct {
	done    chan struct{}
	session *Session
	err     error
}

// Auth is the Go port of Electron src/main/services/auth.ts and backend:getStatus.
type Auth struct {
	mu sync.Mutex

	store      tokenStore
	crypto     cryptor
	http       *infra.Client
	log        *infra.Log
	openURL    func(string) error
	emit       func(string, any)
	afterLogin func()
	doFn       func(*http.Request) (*http.Response, error)

	generation      int
	mutateDone      chan struct{}
	sessionInFlight *sessionCall

	probeMu      sync.Mutex
	probing      bool
	onlineDelay  time.Duration
	offlineDelay time.Duration
	cancelProbe  context.CancelFunc
}

func New() *Auth {
	return NewWithOptions(Options{})
}

func NewWithOptions(opts Options) *Auth {
	done := make(chan struct{})
	close(done)
	a := &Auth{
		mutateDone:   done,
		emit:         defaultEmit,
		onlineDelay:  defaultOnlineProbe,
		offlineDelay: defaultOfflineProbe,
	}
	a.apply(opts)
	return a
}

//wails:ignore
func (a *Auth) UseClient(client *db.Client) {
	if a == nil || client == nil {
		return
	}
	a.store = client.Settings
}

func (a *Auth) apply(opts Options) {
	if opts.Store != nil {
		a.store = opts.Store
	}
	if opts.Crypto != nil {
		a.crypto = opts.Crypto
	}
	if opts.HTTP != nil {
		a.http = opts.HTTP
	}
	if opts.Log != nil {
		a.log = opts.Log
	}
	if opts.Shell != nil {
		a.openURL = opts.Shell.OpenExternal
	}
	if opts.Emit != nil {
		a.emit = opts.Emit
	}
	if opts.AfterLogin != nil {
		a.afterLogin = opts.AfterLogin
	}
	if opts.Do != nil {
		a.doFn = opts.Do
	}
	if opts.OnlineDelay > 0 {
		a.onlineDelay = opts.OnlineDelay
	}
	if opts.OfflineDelay > 0 {
		a.offlineDelay = opts.OfflineDelay
	}
	if a.http != nil {
		a.http.UseToken(a.tokenLookup())
		a.http.UseRefreshSession(a.sessionRefresh())
		a.http.UseOnStatus(func(status infra.BackendStatus) {
			a.broadcast("backend:status", string(status))
		})
		a.http.UseProbe(func() {
			go a.probe(context.Background())
		})
	}
}

func defaultEmit(name string, data any) {
	if app := application.Get(); app != nil {
		app.Event.Emit(name, data)
	}
}

func (a *Auth) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.start(ctx)
	return nil
}

func (a *Auth) ServiceShutdown() error {
	a.stop()
	return nil
}

func (a *Auth) tokenLookup() infra.TokenLookup {
	return func() (string, error) {
		return a.getToken(context.Background())
	}
}

func (a *Auth) sessionRefresh() infra.SessionRefresh {
	return func() error {
		_, err := a.GetSession(context.Background())
		return err
	}
}

func (a *Auth) saveToken(ctx context.Context, token string) error {
	return a.mutateToken(ctx, func() error {
		if a.crypto == nil {
			return errors.New("auth crypto is not configured")
		}
		if a.store == nil {
			return errors.New("auth store is not configured")
		}
		encrypted, err := a.crypto.EncryptString(token)
		if err != nil {
			return err
		}
		return a.store.Upsert(ctx, tokenKey, &encrypted)
	})
}

func (a *Auth) getToken(ctx context.Context) (string, error) {
	if err := a.waitMutation(ctx); err != nil {
		return "", err
	}
	if a.store == nil {
		return "", nil
	}
	key, err := a.store.GetValue(ctx, tokenKey)
	if err != nil || key == nil || *key == "" {
		return "", err
	}
	if a.crypto == nil {
		return "", nil
	}
	plain, decryptErr := a.crypto.DecryptString(*key)
	if decryptErr == nil {
		return plain, nil
	}
	_ = a.removeToken(ctx)
	return "", nil
}

func (a *Auth) removeToken(ctx context.Context) error {
	return a.mutateToken(ctx, func() error {
		if a.store == nil {
			return errors.New("auth store is not configured")
		}
		return a.store.UpdateValue(ctx, tokenKey, nil)
	})
}

func (a *Auth) HasToken(ctx context.Context) (bool, error) {
	token, err := a.getToken(ctx)
	return token != "", err
}

func (a *Auth) GetSession(ctx context.Context) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	if a.sessionInFlight != nil {
		call := a.sessionInFlight
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-call.done:
			return call.session, call.err
		}
	}
	call := &sessionCall{done: make(chan struct{})}
	a.sessionInFlight = call
	a.mu.Unlock()

	session, err := a.fetchSession(ctx)

	a.mu.Lock()
	if a.sessionInFlight == call {
		a.sessionInFlight = nil
	}
	call.session = session
	call.err = err
	close(call.done)
	a.mu.Unlock()
	return session, err
}

func (a *Auth) IsLoggedIn(ctx context.Context) (bool, error) {
	session, err := a.GetSession(ctx)
	return session != nil, err
}

func (a *Auth) StartLogout(ctx context.Context) error {
	return a.startLogout(ctx, nil)
}

func (a *Auth) GetBackendStatus() string {
	if a == nil || a.http == nil {
		return string(infra.BackendUnknown)
	}
	return string(a.http.GetStatus())
}

func (a *Auth) probe(ctx context.Context) string {
	a.probeMu.Lock()
	if a.probing {
		a.probeMu.Unlock()
		return a.GetBackendStatus()
	}
	a.probing = true
	a.probeMu.Unlock()

	defer func() {
		a.probeMu.Lock()
		a.probing = false
		a.probeMu.Unlock()
	}()

	if a.http == nil {
		return string(infra.BackendUnknown)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return string(a.http.Probe(ctx))
}

func (a *Auth) start(ctx context.Context) {
	if a == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	a.mu.Lock()
	if a.cancelProbe != nil {
		a.cancelProbe()
	}
	a.cancelProbe = cancel
	a.mu.Unlock()
	go a.probeLoop(ctx)
}

func (a *Auth) stop() {
	if a == nil {
		return
	}
	a.mu.Lock()
	cancel := a.cancelProbe
	a.cancelProbe = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (a *Auth) mutateToken(_ context.Context, fn func() error) error {
	a.mu.Lock()
	a.sessionInFlight = nil
	a.generation++
	prev := a.mutateDone
	done := make(chan struct{})
	a.mutateDone = done
	a.mu.Unlock()

	if prev != nil {
		<-prev
	}
	err := fn()
	close(done)
	return err
}

func (a *Auth) waitMutation(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	wait := a.mutateDone
	a.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-wait:
		return nil
	}
}

func (a *Auth) currentGeneration() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.generation
}

func (a *Auth) fetchSession(ctx context.Context) (*Session, error) {
	captured := a.currentGeneration()
	token, err := a.getToken(ctx)
	if err != nil {
		return nil, err
	}
	if a.currentGeneration() != captured {
		return a.fetchSession(ctx)
	}
	if token == "" {
		return nil, nil
	}
	if a.http == nil {
		return nil, errors.New("auth http is not configured")
	}

	url := strings.TrimRight(a.http.BackendURL(), "/") + sessionPath
	header := make(http.Header)
	header.Set("Authorization", "Bearer "+token)
	resp, err := a.http.Fetch(ctx, url, infra.FetchOptions{
		DisableHTTPErrors: true,
		Header:            header,
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if a.currentGeneration() != captured {
		return a.fetchSession(ctx)
	}
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			if err := a.startLogout(ctx, &captured); err != nil {
				return nil, err
			}
			if a.currentGeneration() != captured {
				return a.fetchSession(ctx)
			}
		}
		return nil, nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if a.currentGeneration() != captured {
		return a.fetchSession(ctx)
	}
	if string(body) == "null" {
		if err := a.startLogout(ctx, &captured); err != nil {
			return nil, err
		}
		if a.currentGeneration() != captured {
			return a.fetchSession(ctx)
		}
		return nil, nil
	}

	var session Session
	if err := validateSessionJSON(body); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func validateSessionJSON(body []byte) error {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		return err
	}
	requireRecord := func(key string) (map[string]any, error) {
		value, ok := root[key].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid session payload: %s", key)
		}
		return value, nil
	}
	requireStrings := func(record map[string]any, prefix string, keys ...string) error {
		for _, key := range keys {
			if _, ok := record[key].(string); !ok {
				return fmt.Errorf("invalid session payload: %s.%s", prefix, key)
			}
		}
		return nil
	}
	session, err := requireRecord("session")
	if err != nil {
		return err
	}
	if err := requireStrings(session, "session", "id", "userId", "createdAt", "updatedAt", "expiresAt", "token"); err != nil {
		return err
	}
	user, err := requireRecord("user")
	if err != nil {
		return err
	}
	if err := requireStrings(user, "user", "id", "name", "email", "role"); err != nil {
		return err
	}
	if image, ok := user["image"]; !ok || image != nil {
		if _, stringOK := image.(string); !stringOK {
			return fmt.Errorf("invalid session payload: user.image")
		}
	}
	drive, err := requireRecord("drive")
	if err != nil {
		return err
	}
	return requireStrings(drive, "drive", "id", "rootId")
}

func (a *Auth) startLogout(ctx context.Context, expected *int) error {
	if expected != nil && a.currentGeneration() != *expected {
		return nil
	}
	token, err := a.getToken(ctx)
	if err != nil {
		return err
	}
	if expected != nil && a.currentGeneration() != *expected {
		return nil
	}
	if err := a.removeToken(ctx); err != nil {
		return err
	}
	a.broadcast("auth:update", nil)
	if token == "" {
		return nil
	}
	a.signOut(ctx, token)
	return nil
}

func (a *Auth) signOut(ctx context.Context, token string) {
	if a.http == nil {
		return
	}
	signCtx, cancel := context.WithTimeout(ctx, signOutWait)
	defer cancel()
	url := strings.TrimRight(a.http.BackendURL(), "/") + signOutPath
	req, err := http.NewRequestWithContext(signCtx, http.MethodPost, url, nil)
	if err != nil {
		a.reportBackgroundError(err, "sign-out", "create-request", signOutPath)
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "Nahida Desktop/"+platform.AppVersion)
	resp, err := a.do(req)
	if err != nil {
		a.reportBackgroundError(err, "sign-out", "request", signOutPath)
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}

func (a *Auth) do(req *http.Request) (*http.Response, error) {
	if a.doFn != nil {
		return a.doFn(req)
	}
	return http.DefaultClient.Do(req)
}

func (a *Auth) broadcast(name string, data any) {
	if a.emit != nil {
		a.emit(name, data)
	}
}

func (a *Auth) info(msg string) {
	if a.log != nil {
		a.log.Info(msg, "Auth")
	}
}

func (a *Auth) error(err any) {
	if err == nil {
		return
	}
	failure, ok := err.(error)
	if !ok {
		failure = fmt.Errorf("%v", err)
	}
	a.reportBackgroundError(failure, "background", "callback", "")
}

func (a *Auth) reportBackgroundError(err error, operation, stage, endpoint string) {
	fields := map[string]any{}
	if endpoint != "" {
		fields["endpoint"] = endpoint
	}
	_ = infra.ReportError(a.log, err, "Auth", infra.Diagnostic{
		Operation: operation, Stage: stage, Fields: fields,
	})
}

func (a *Auth) probeLoop(ctx context.Context) {
	a.probe(ctx)
	for {
		delay := a.onlineDelay
		switch infra.BackendStatus(a.GetBackendStatus()) {
		case infra.BackendOffline, infra.BackendMaintenance:
			delay = a.offlineDelay
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			a.probe(ctx)
		}
	}
}
