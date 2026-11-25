package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"nahida.live/desktop/internal/infra"
)

var (
	errIWantToLogin  = errors.New("Failed to get iWantToLogin data") //nolint:staticcheck // Electron contract text.
	errLoginExchange = errors.New("Failed to complete login")        //nolint:staticcheck // Shown in the login dialog.
)

const (
	// slowLoginRequest is how long the start request may take before a login
	// that otherwise ends normally is recorded at WARN.
	slowLoginRequest = 10 * time.Second
	// abandonedLoginStage is how long a stage outside the browser wait must
	// have run before a cancellation in it counts as a stall rather than a
	// change of mind.
	abandonedLoginStage = 2 * time.Second
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
// state is empty until the backend answers. state and mismatchLogged are
// guarded by Auth.mu.
type pendingLogin struct {
	state          string
	mismatchLogged bool
	code           chan string
	cancel         context.CancelCauseFunc
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

	started := time.Now()
	stage, stageStarted := "prepare", started
	var requestTook time.Duration
	setStage := func(next string) {
		if stage == "login-request" {
			requestTook = time.Since(stageStarted)
		}
		stage, stageStarted = next, time.Now()
	}
	progress := &loginProgress{}
	base := ""
	saved := false
	defer func() {
		// A login replaced before it saved a token has nothing to report. Stop
		// cancels a login before the next one replaces it, which leaves
		// context.Canceled as the cause and errLoginReplaced only in err.
		replaced := errors.Is(context.Cause(loginCtx), errLoginReplaced) || errors.Is(err, errLoginReplaced)
		if !saved && replaced {
			err = ctx.Err()
		}
		cancel(nil)
		a.mu.Lock()
		if a.login == pending {
			a.login = nil
		}
		a.mu.Unlock()

		if stage == "login-request" {
			requestTook = time.Since(stageStarted)
		}
		fields := map[string]any{
			"elapsedMs":      time.Since(started).Milliseconds(),
			"stageElapsedMs": time.Since(stageStarted).Milliseconds(),
		}
		if stage == "login-request" || stage == "exchange" {
			fields["reached"] = progress.reached()
		}
		if requestTook > 0 {
			fields["requestMs"] = requestTook.Milliseconds()
		}

		// Failures reach the log through the caller. Logins that end without
		// one are recorded here, at WARN when a stage outside the browser wait
		// stalled until the user gave up, when the wait expired, or when the
		// start request was slow.
		if err == nil || infra.IsCancellationError(err) {
			outcome := "ended"
			switch {
			case saved:
				outcome = "success"
			case ctx.Err() != nil:
				outcome = "canceled"
			case replaced:
				outcome = "replaced"
			case stage == "wait-code":
				outcome = "wait-expired"
			}
			record := maps.Clone(fields)
			record["operation"], record["outcome"], record["stage"] = "start-login", outcome, stage
			if base != "" {
				record["backend"] = infra.SanitizeLogURL(base)
			}
			abandoned := (outcome == "canceled" || outcome == "replaced") && stage != "wait-code" &&
				time.Since(stageStarted) >= abandonedLoginStage
			switch {
			case abandoned || outcome == "wait-expired" || requestTook >= slowLoginRequest:
				a.log.Warn(record, "Auth.StartLogin")
			case outcome != "success":
				a.log.Info(record, "Auth.StartLogin")
			}
		}
		err = infra.AnnotateError(err, infra.Diagnostic{Operation: "start-login", Stage: stage, Fields: fields})
	}()
	if a.http == nil {
		return errors.New("auth http is not configured")
	}
	a.info("start login")

	seed := make([]byte, 32)
	rand.Read(seed)
	verifier := base64.RawURLEncoding.EncodeToString(seed)
	challenge := sha256.Sum256([]byte(verifier))
	base = strings.TrimRight(a.http.BackendURL(), "/")

	setStage("login-request")
	var start loginStart
	if err := a.loginPost(
		loginCtx,
		progress,
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

	setStage("open-browser")
	if a.openURL == nil {
		return errors.New("auth openURL is not configured")
	}
	if err := a.openURL(start.PageURL); err != nil {
		return err
	}

	// A newer login replaces this one, and the backend drops the state after
	// loginWait, so either ends the wait.
	setStage("wait-code")
	waitCtx, stopWait := context.WithTimeout(loginCtx, loginWait)
	defer stopWait()
	var code string
	select {
	case code = <-pending.code:
	case <-waitCtx.Done():
		// Replaced or never approved: there is nothing to report to the user.
		return ctx.Err()
	}

	setStage("exchange")
	progress = &loginProgress{}
	var exchanged struct {
		Session *struct {
			Token string `json:"token"`
		} `json:"session"`
	}
	if err := a.loginPost(
		loginCtx,
		progress,
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
	setStage("save-token")
	if err := a.saveTokenIf(ctx, exchanged.Session.Token, func() bool { return a.login == pending }); err != nil {
		return err
	}
	saved = true
	a.info("Login successful: Session saved.")

	setStage("session")
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
func (a *Auth) CompleteLogin(state, code string) bool {
	if a == nil || state == "" || code == "" {
		return false
	}
	a.mu.Lock()
	pending := a.login
	pendingState := ""
	if pending != nil {
		pendingState = pending.state
	}
	a.mu.Unlock()
	if pending == nil {
		return false
	}

	reason := ""
	switch {
	case pendingState == "":
		reason = "start-pending"
	case pendingState != state:
		reason = "state-mismatch"
	default:
		select {
		case pending.code <- code:
			return true
		default:
			reason = "code-already-delivered"
		}
	}

	// Any page can open these links, so only the first one a waiting login
	// rejects is recorded, without either value.
	a.mu.Lock()
	first := !pending.mismatchLogged
	pending.mismatchLogged = true
	a.mu.Unlock()
	if first {
		a.log.Warn(map[string]any{"operation": "complete-login", "reason": reason}, "Auth.CompleteLogin")
	}
	return false
}

// loginStep is how far a login request got. Headers follows the token lookup
// in the database. When the app proxy is off, the transport resolves the
// system proxy between headers and get-conn.
type loginStep int32

const (
	loginStepPrepared loginStep = iota
	loginStepHeaders
	loginStepGetConn
	loginStepDNSDone
	loginStepConnectDone
	loginStepTLSDone
	loginStepGotConn
	loginStepWroteRequest
	loginStepFirstByte
	loginStepDecoded
)

var loginStepNames = [...]string{
	"prepared", "headers", "get-conn", "dns-done", "connect-done", "tls-done", "got-conn", "wrote-request",
	"first-byte", "decoded",
}

// loginProgress keeps the furthest step a request reached. Steps never move
// back, because a dial that loses a race can report after the connection that
// won.
type loginProgress struct {
	step atomic.Int32
}

func (p *loginProgress) mark(step loginStep) {
	for {
		current := p.step.Load()
		if int32(step) <= current || p.step.CompareAndSwap(current, int32(step)) {
			return
		}
	}
}

func (p *loginProgress) reached() string {
	return loginStepNames[p.step.Load()]
}

func (p *loginProgress) trace() *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		GetConn:              func(string) { p.mark(loginStepGetConn) },
		DNSDone:              func(httptrace.DNSDoneInfo) { p.mark(loginStepDNSDone) },
		ConnectDone:          func(string, string, error) { p.mark(loginStepConnectDone) },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { p.mark(loginStepTLSDone) },
		GotConn:              func(httptrace.GotConnInfo) { p.mark(loginStepGotConn) },
		WroteRequest:         func(httptrace.WroteRequestInfo) { p.mark(loginStepWroteRequest) },
		GotFirstResponseByte: func() { p.mark(loginStepFirstByte) },
	}
}

// loginPost sends one handshake request and decodes its JSON answer. A failure
// keeps sentinel as its message, which is the text the renderer shows.
func (a *Auth) loginPost(
	ctx context.Context,
	progress *loginProgress,
	endpoint string,
	payload, dest any,
	sentinel error,
) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(httptrace.WithClientTrace(ctx, progress.trace()), loginTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	headers, err := a.http.GetHeaders(endpoint)
	if err != nil {
		return err
	}
	progress.mark(loginStepHeaders)
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
	progress.mark(loginStepDecoded)
	return nil
}
