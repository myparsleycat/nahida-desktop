package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nahida.live/desktop/internal/infra"
)

var errIWantToLogin = errors.New("Failed to get iWantToLogin data") //nolint:staticcheck // Electron contract text.

type loginStart struct {
	State         string `json:"state"`
	PageURL       string `json:"pageUrl"`
	StateResponse string `json:"stateResponse"`
	valid         bool
}

func (s *loginStart) UnmarshalJSON(data []byte) error {
	type wire struct {
		State         json.RawMessage `json:"state"`
		PageURL       json.RawMessage `json:"pageUrl"`
		StateResponse json.RawMessage `json:"stateResponse"`
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
		{"state", value.State, &s.State}, {"pageUrl", value.PageURL, &s.PageURL}, {"stateResponse", value.StateResponse, &s.StateResponse},
	} {
		if err := decodeString(field.name, field.raw, field.target); err != nil {
			return infra.WithCause(errIWantToLogin, err)
		}
	}
	s.valid = true
	return nil
}

type loginEvent struct {
	State   string `json:"state"`
	Status  string `json:"status"`
	Session *struct {
		UserID string `json:"userId"`
		Token  string `json:"token"`
	} `json:"session"`
}

func (a *Auth) StartLogin(ctx context.Context) (err error) {
	stage := "prepare"
	defer func() { err = infra.AnnotateError(err, infra.Diagnostic{Operation: "start-login", Stage: stage}) }()
	if ctx == nil {
		ctx = context.Background()
	}
	if a.http == nil {
		return errors.New("auth http is not configured")
	}
	a.info("start login")

	loginURL := strings.TrimRight(a.http.BackendURL(), "/") + loginPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loginURL, nil)
	if err != nil {
		return err
	}
	headers, err := a.http.GetHeaders(loginURL)
	if err != nil {
		return err
	}
	req.Header = headers

	loginCtx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	req = req.WithContext(loginCtx)

	stage = "login-request"
	resp, err := a.do(req)
	if err != nil {
		return infra.AnnotateError(err, infra.HTTPDiagnostic(http.MethodGet, loginURL, stage, nil))
	}
	defer func() { _ = resp.Body.Close() }()
	stage = "login-response"
	if resp.StatusCode >= 400 {
		return infra.AnnotateError(infra.WithCause(errIWantToLogin, &infra.HTTPError{Status: resp.StatusCode}), infra.HTTPDiagnostic(http.MethodGet, loginURL, "login-response", resp))
	}
	var start loginStart
	stage = "login-decode"
	if err := json.NewDecoder(resp.Body).Decode(&start); err != nil {
		return infra.AnnotateError(infra.WithCause(errIWantToLogin, err), infra.HTTPDiagnostic(http.MethodGet, loginURL, "login-decode", resp))
	}
	if !start.valid {
		return infra.AnnotateError(infra.WithCause(errIWantToLogin, errors.New("login response is not an object")), infra.HTTPDiagnostic(http.MethodGet, loginURL, "login-validate", resp))
	}

	stage = "open-browser"
	if a.openURL == nil {
		return errors.New("auth openURL is not configured")
	}
	if err := a.openURL(start.PageURL); err != nil {
		return err
	}

	stage = "sse-request"
	sseReq, err := http.NewRequestWithContext(ctx, http.MethodGet, start.StateResponse, nil)
	if err != nil {
		return err
	}
	sseHeaders, err := a.http.GetHeaders(start.StateResponse)
	if err != nil {
		return err
	}
	sseReq.Header = sseHeaders
	for _, cookie := range resp.Cookies() {
		sseReq.AddCookie(cookie)
	}
	sseResp, err := a.do(sseReq)
	if err != nil {
		return err
	}
	defer func() { _ = sseResp.Body.Close() }()
	if sseResp.Body == nil {
		return errors.New("SSE response body is null")
	}

	a.info("start parse sse")
	stage = "sse-read"
	err = parseSSE(sseResp.Body, func(event, data string) error {
		if event != "state-response" {
			return nil
		}
		return a.handleLoginEvent(ctx, data)
	})
	if errors.Is(err, errAuthExpired) {
		return nil
	}
	return err
}

func (a *Auth) handleLoginEvent(ctx context.Context, data string) error {
	var payload loginEvent
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		a.error(err)
		return nil
	}
	if payload.Status == "" {
		a.error(errors.New("invalid login event"))
		return nil
	}
	if payload.Status == "loggedin" && payload.Session != nil && payload.Session.Token != "" {
		if err := a.saveToken(ctx, payload.Session.Token); err != nil {
			a.error(err)
			return nil
		}
		a.info("Login successful: Session saved.")
		session, err := a.GetSession(ctx)
		if err != nil {
			a.error(err)
			return nil
		}
		a.broadcast("auth:update", session)
		if a.afterLogin != nil {
			a.afterLogin()
		}
	}
	if payload.Status == "expired" {
		a.error(errAuthExpired)
		return errAuthExpired
	}
	return nil
}

func parseSSE(r io.Reader, fn func(event, data string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var event, data string
	hasData := false
	flush := func() error {
		if !hasData && event == "" {
			return nil
		}
		name := event
		if name == "" {
			name = "message"
		}
		err := fn(name, data)
		event = ""
		data = ""
		hasData = false
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			event = value
		case "data":
			if hasData {
				data += "\n" + value
			} else {
				data = value
				hasData = true
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return scanner.Err()
}
