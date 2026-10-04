package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"nahida.live/desktop/internal/infra"
)

var (
	errIWantToLogin  = errors.New("Failed to get iWantToLogin data") //nolint:staticcheck // Electron contract text.
	errLoginExchange = errors.New("Failed to complete login")        //nolint:staticcheck // Shown in the login dialog.
)

// errLoginReplaced cancels a login a newer StartLogin call took over from.
var errLoginReplaced = errors.New("login replaced by a newer login")

type loginStart struct {
	State   string `json:"state"`
	PageURL string `json:"pageUrl"`
	valid   bool
}

func (s *loginStart) UnmarshalJSON(data []byte) error {
	type wire struct {
		State   json.RawMessage `json:"state"`
		PageURL json.RawMessage `json:"pageUrl"`
	}
	var value wire
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	decodeString := func(field string, raw json.RawMessage, destination *string) error {
		if raw == nil {
			return fmt.Errorf("missing login field: %s (expected string)", field)
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return err
		}
		value, ok := decoded.(string)
		if !ok {
			return fmt.Errorf("invalid login field: %s (expected string, got %T)", field, decoded)
		}
		*destination = value
		return nil
	}
	for _, field := range []struct {
		name   string
		raw    json.RawMessage
		target *string
	}{
		{"state", value.State, &s.State}, {"pageUrl", value.PageURL, &s.PageURL},
	} {
		if err := decodeString(field.name, field.raw, field.target); err != nil {
			return infra.WithCause(errIWantToLogin, err)
		}
	}
	s.valid = true
	return nil
}

// pendingLogin is the handshake StartLogin is running. Its verifier never
// leaves StartLogin, so a code completes only the login that asked for it.
// state is empty until the backend answers and is guarded by Auth.mu.
type pendingLogin struct {
	state  string
	code   chan string
	cancel context.CancelCauseFunc
}

// StartLogin runs the deep-link handshake: it registers a PKCE challenge with
// the backend, opens the web sign-in page, and waits for the nahida://auth link
// that page opens to deliver a one-time code through CompleteLogin. The code and
// the verifier together are exchanged for the session token.
func (a *Auth) StartLogin(ctx context.Context) (err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// The newest call owns the login from here on, whichever backend answer
	// arrives first: taking over cancels every step of the one before.
	loginCtx, cancel := context.WithCancelCause(ctx)
	pending := &pendingLogin{code: make(chan string, 1), cancel: cancel}
	a.mu.Lock()
	previous := a.login
	a.login = pending
	a.mu.Unlock()
	if previous != nil {
		previous.cancel(errLoginReplaced)
	}

	stage := "prepare"
	saved := false
	defer func() {
		// A login replaced before it saved a token has nothing to report.
		if !saved && errors.Is(context.Cause(loginCtx), errLoginReplaced) {
			err = ctx.Err()
		}
		cancel(nil)
		a.mu.Lock()
		if a.login == pending {
			a.login = nil
		}
		a.mu.Unlock()
		err = infra.AnnotateError(err, infra.Diagnostic{Operation: "start-login", Stage: stage})
	}()
	if a.http == nil {
		return errors.New("auth http is not configured")
	}
	a.info("start login")

	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return err
	}
	verifier := base64.RawURLEncoding.EncodeToString(seed)
	challenge := sha256.Sum256([]byte(verifier))
	base := strings.TrimRight(a.http.BackendURL(), "/")

	stage = "login-request"
	var start loginStart
	if err := a.loginPost(
		loginCtx,
		base+loginPath,
		map[string]string{"challenge": base64.RawURLEncoding.EncodeToString(challenge[:])},
		&start,
		errIWantToLogin,
	); err != nil {
		return err
	}
	if !start.valid {
		return infra.AnnotateError(
			infra.WithCause(errIWantToLogin, errors.New("login response is not an object")),
			infra.HTTPDiagnostic(http.MethodPost, base+loginPath, "login-validate", nil),
		)
	}

	a.mu.Lock()
	pending.state = start.State
	current := a.login == pending
	a.mu.Unlock()
	if !current {
		return errLoginReplaced
	}

	stage = "open-browser"
	if a.openURL == nil {
		return errors.New("auth openURL is not configured")
	}
	if err := a.openURL(start.PageURL); err != nil {
		return err
	}

	// A newer login replaces this one, and the backend drops the state after
	// loginWait, so either ends the wait.
	stage = "wait-code"
	waitCtx, stopWait := context.WithTimeout(loginCtx, loginWait)
	defer stopWait()
	var code string
	select {
	case code = <-pending.code:
	case <-waitCtx.Done():
		// Replaced or never approved: there is nothing to report to the user.
		return ctx.Err()
	}

	stage = "exchange"
	var exchanged struct {
		Session *struct {
			Token string `json:"token"`
		} `json:"session"`
	}
	if err := a.loginPost(
		loginCtx,
		base+loginExchangePath,
		map[string]string{"state": start.State, "code": code, "verifier": verifier},
		&exchanged,
		errLoginExchange,
	); err != nil {
		return err
	}
	if exchanged.Session == nil || exchanged.Session.Token == "" {
		return infra.WithCause(errLoginExchange, errors.New("exchange response has no session token"))
	}

	// The save and what follows run on ctx: once this login wins the check, a
	// later replacement must not leave its token half applied.
	stage = "save-token"
	if err := a.saveTokenIf(ctx, exchanged.Session.Token, func() bool { return a.login == pending }); err != nil {
		return err
	}
	saved = true
	a.info("Login successful: Session saved.")

	stage = "session"
	session, err := a.GetSession(ctx)
	if err != nil {
		return err
	}
	a.broadcast("auth:update", session)
	if a.afterLogin != nil {
		a.afterLogin()
	}
	return nil
}

// CompleteLogin hands the code a nahida://auth link carried to the login that
// is waiting for it. It reports false for a link no pending login asked for,
// which covers every link another client's handshake produced.
//
//wails:ignore
func (a *Auth) CompleteLogin(state, code string) bool {
	if a == nil || state == "" || code == "" {
		return false
	}
	a.mu.Lock()
	pending := a.login
	matches := pending != nil && pending.state == state
	a.mu.Unlock()
	if !matches {
		return false
	}
	select {
	case pending.code <- code:
		return true
	default:
		return false
	}
}

// loginPost sends one handshake request and decodes its JSON answer. A failure
// keeps sentinel as its message, which is the text the renderer shows.
func (a *Auth) loginPost(ctx context.Context, endpoint string, payload, dest any, sentinel error) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	headers, err := a.http.GetHeaders(endpoint)
	if err != nil {
		return err
	}
	req.Header = headers
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.do(req)
	if err != nil {
		return infra.AnnotateError(err, infra.HTTPDiagnostic(http.MethodPost, endpoint, "request", nil))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return infra.AnnotateError(
			infra.WithCause(sentinel, &infra.HTTPError{Status: resp.StatusCode}),
			infra.HTTPDiagnostic(http.MethodPost, endpoint, "response", resp),
		)
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return infra.AnnotateError(
			infra.WithCause(sentinel, err),
			infra.HTTPDiagnostic(http.MethodPost, endpoint, "decode", resp),
		)
	}
	return nil
}
