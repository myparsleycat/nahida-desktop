package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
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
	decodeString := func(raw json.RawMessage, destination *string) bool {
		if raw == nil {
			return false
		}
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return false
		}
		value, ok := decoded.(string)
		if !ok {
			return false
		}
		*destination = value
		return true
	}
	if !decodeString(value.State, &s.State) ||
		!decodeString(value.PageURL, &s.PageURL) ||
		!decodeString(value.StateResponse, &s.StateResponse) {
		return errIWantToLogin
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

func (a *Auth) StartLogin(ctx context.Context) error {
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

	resp, err := a.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return errIWantToLogin
	}
	var start loginStart
	if err := json.NewDecoder(resp.Body).Decode(&start); err != nil {
		return errIWantToLogin
	}
	if !start.valid {
		return errIWantToLogin
	}

	if a.openURL == nil {
		return errors.New("auth openURL is not configured")
	}
	if err := a.openURL(start.PageURL); err != nil {
		return err
	}

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
