package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
)

type passCrypto struct{}

func (passCrypto) EncryptString(s string) (string, error) { return s, nil }
func (passCrypto) DecryptString(s string) (string, error) { return s, nil }

type doneObservedContext struct {
	context.Context
	once     sync.Once
	observed chan struct{}
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type memStore struct {
	mu           sync.Mutex
	value        *string
	getStarted   chan struct{}
	allowGet     chan struct{}
	blockNextGet bool
	updates      int
}

func (s *memStore) GetValue(ctx context.Context, key string) (*string, error) {
	s.mu.Lock()
	block := s.blockNextGet
	if block {
		s.blockNextGet = false
	}
	s.mu.Unlock()
	if block {
		if s.getStarted != nil {
			s.getStarted <- struct{}{}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.allowGet:
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value == nil {
		return nil, nil
	}
	v := *s.value
	return &v, nil
}

func (s *memStore) Upsert(ctx context.Context, key string, value *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if value == nil {
		s.value = nil
		return nil
	}
	v := *value
	s.value = &v
	return nil
}

func (s *memStore) UpdateValue(ctx context.Context, key string, value *string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updates++
	if value == nil {
		s.value = nil
		return nil
	}
	v := *value
	s.value = &v
	return nil
}

func (s *memStore) token() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.value == nil {
		return ""
	}
	return *s.value
}

func sessionJSON(token string) string {
	raw, _ := json.Marshal(Session{
		Session: SessionInfo{
			ID:        "session-id",
			UserID:    "user-id",
			CreatedAt: "2026-01-01T00:00:00.000Z",
			UpdatedAt: "2026-01-01T00:00:00.000Z",
			ExpiresAt: "2027-01-01T00:00:00.000Z",
			Token:     token,
		},
		User: User{
			ID:    "user-id",
			Name:  "User",
			Email: "user@example.com",
			Role:  "user",
		},
		Drive: DriveInfo{ID: "drive-id", RootID: "root-id"},
	})
	return string(raw)
}

func TestValidateSessionJSONRequiresElectronSchemaFieldsAndTypes(t *testing.T) {
	validEmptyStrings := `{"session":{"id":"","userId":"","createdAt":"","updatedAt":"","expiresAt":"","token":""},"user":{"id":"","name":"","email":"","role":"","image":null},"drive":{"id":"","rootId":""}}`
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{name: "empty strings remain valid zod strings", payload: validEmptyStrings},
		{name: "missing session token", payload: `{"session":{"id":"","userId":"","createdAt":"","updatedAt":"","expiresAt":""},"user":{"id":"","name":"","email":"","role":"","image":null},"drive":{"id":"","rootId":""}}`, wantErr: true},
		{name: "missing nullable image", payload: `{"session":{"id":"","userId":"","createdAt":"","updatedAt":"","expiresAt":"","token":""},"user":{"id":"","name":"","email":"","role":""},"drive":{"id":"","rootId":""}}`, wantErr: true},
		{name: "wrong nullable image type", payload: `{"session":{"id":"","userId":"","createdAt":"","updatedAt":"","expiresAt":"","token":""},"user":{"id":"","name":"","email":"","role":"","image":1},"drive":{"id":"","rootId":""}}`, wantErr: true},
		{name: "wrong drive root ID type", payload: `{"session":{"id":"","userId":"","createdAt":"","updatedAt":"","expiresAt":"","token":""},"user":{"id":"","name":"","email":"","role":"","image":"avatar"},"drive":{"id":"","rootId":1}}`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateSessionJSON([]byte(test.payload))
			if (err != nil) != test.wantErr {
				t.Fatalf("validateSessionJSON() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestLoginStartRequiresAllStringFields(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		valid   bool
	}{
		{name: "all empty strings", payload: `{"state":"","pageUrl":"","stateResponse":""}`, valid: true},
		{name: "missing state", payload: `{"pageUrl":"page","stateResponse":"response"}`},
		{name: "null page URL", payload: `{"state":"state","pageUrl":null,"stateResponse":"response"}`},
		{name: "numeric state response", payload: `{"state":"state","pageUrl":"page","stateResponse":1}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var start loginStart
			err := json.Unmarshal([]byte(test.payload), &start)
			if test.valid {
				if err != nil || !start.valid {
					t.Fatalf("loginStart = %#v, error = %v", start, err)
				}
				return
			}
			if err == nil || start.valid {
				t.Fatalf("loginStart = %#v, error = %v, want invalid", start, err)
			}
		})
	}
}

func textResp(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Status:     http.StatusText(status),
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func testHTTP(t *testing.T, transport http.RoundTripper) *infra.Client {
	t.Helper()
	zero := 0
	none := time.Duration(0)
	return infra.NewClientWithOptions(infra.ClientOptions{
		Version:    "test-version",
		RetryLimit: &zero,
		RetryWait:  &none,
		Status:     infra.BackendOnline,
		Transport:  transport,
	})
}

func newTestAuth(store *memStore, httpClient *infra.Client, emit func(string, any)) *Auth {
	if store != nil && store.value == nil {
		old := "old-token"
		store.value = &old
	}
	return NewWithOptions(Options{
		Store:  store,
		Crypto: passCrypto{},
		HTTP:   httpClient,
		Emit:   emit,
	})
}

func TestSaveAndGetToken(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	a := NewWithOptions(Options{Store: store, Crypto: passCrypto{}})
	ctx := context.Background()
	if err := a.saveToken(ctx, "secret"); err != nil {
		t.Fatalf("saveToken: %v", err)
	}
	if store.token() != "secret" {
		t.Fatalf("stored %q", store.token())
	}
	got, err := a.getToken(ctx)
	if err != nil || got != "secret" {
		t.Fatalf("getToken = %q, %v", got, err)
	}
	ok, err := a.HasToken(ctx)
	if err != nil || !ok {
		t.Fatalf("HasToken = %v, %v", ok, err)
	}
}

func TestGetTokenClearsUndecryptableValue(t *testing.T) {
	t.Parallel()
	stored := "ciphertext"
	store := &memStore{value: &stored}
	a := NewWithOptions(Options{
		Store:  store,
		Crypto: failCrypto{},
	})
	got, err := a.getToken(context.Background())
	if err != nil || got != "" {
		t.Fatalf("getToken = %q, %v", got, err)
	}
	if store.token() != "" || store.updates != 1 {
		t.Fatalf("store token=%q updates=%d", store.token(), store.updates)
	}
}

type failCrypto struct{}

func (failCrypto) EncryptString(string) (string, error) { return "", errors.New("no") }
func (failCrypto) DecryptString(string) (string, error) { return "", errors.New("no") }

func TestOldToken401DoesNotDropSavedToken(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	old := "old-token"
	store.value = &old
	allowOld := make(chan struct{})
	started := make(chan struct{})
	var fetches atomic.Int32
	httpClient := testHTTP(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		fetches.Add(1)
		if token == "old-token" {
			close(started)
			<-allowOld
			return textResp(r, 401, ""), nil
		}
		return textResp(r, 200, sessionJSON(token)), nil
	}))
	a := newTestAuth(store, httpClient, func(string, any) {})

	errc := make(chan error, 1)
	var session *Session
	go func() {
		var err error
		session, err = a.GetSession(context.Background())
		errc <- err
	}()
	<-started
	if err := a.saveToken(context.Background(), "new-token"); err != nil {
		t.Fatalf("saveToken: %v", err)
	}
	close(allowOld)
	if err := <-errc; err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil || session.Session.Token != "new-token" {
		t.Fatalf("session = %#v", session)
	}
	if store.token() != "new-token" {
		t.Fatalf("stored %q", store.token())
	}
	if store.updates != 0 {
		t.Fatalf("updates = %d", store.updates)
	}
}

func TestGuardedLogoutDoesNotRemoveNewerToken(t *testing.T) {
	t.Parallel()
	store := &memStore{
		getStarted:   make(chan struct{}, 1),
		allowGet:     make(chan struct{}),
		blockNextGet: true,
	}
	old := "old-token"
	store.value = &old
	var broadcasts []string
	a := NewWithOptions(Options{
		Store:  store,
		Crypto: passCrypto{},
		Emit: func(name string, _ any) {
			broadcasts = append(broadcasts, name)
		},
	})

	errc := make(chan error, 1)
	go func() {
		errc <- a.startLogout(context.Background(), ptr(0))
	}()
	<-store.getStarted
	if err := a.saveToken(context.Background(), "new-token"); err != nil {
		t.Fatalf("saveToken: %v", err)
	}
	close(store.allowGet)
	if err := <-errc; err != nil {
		t.Fatalf("logout: %v", err)
	}
	if store.token() != "new-token" {
		t.Fatalf("stored %q", store.token())
	}
	if store.updates != 0 {
		t.Fatalf("updates = %d", store.updates)
	}
	if len(broadcasts) != 0 {
		t.Fatalf("broadcasts = %v", broadcasts)
	}
}

func ptr(v int) *int { return &v }

func TestGetSessionRetriesAfterTokenSavedMidRequest(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	old := "old-token"
	store.value = &old
	started := make(chan struct{})
	allow := make(chan struct{})
	var mu sync.Mutex
	var tokens []string
	httpClient := testHTTP(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		mu.Lock()
		tokens = append(tokens, token)
		mu.Unlock()
		if token == "old-token" {
			close(started)
			<-allow
		}
		return textResp(r, 200, sessionJSON(token)), nil
	}))
	a := newTestAuth(store, httpClient, func(string, any) {})

	errc := make(chan error, 1)
	var session *Session
	go func() {
		var err error
		session, err = a.GetSession(context.Background())
		errc <- err
	}()
	<-started
	if err := a.saveToken(context.Background(), "new-token"); err != nil {
		t.Fatalf("saveToken: %v", err)
	}
	close(allow)
	if err := <-errc; err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil || session.Session.Token != "new-token" {
		t.Fatalf("session = %#v", session)
	}
	mu.Lock()
	got := append([]string(nil), tokens...)
	mu.Unlock()
	if len(got) != 2 || got[0] != "old-token" || got[1] != "new-token" {
		t.Fatalf("tokens = %v", got)
	}
	if store.updates != 0 {
		t.Fatalf("updates = %d", store.updates)
	}
}

func TestGetSessionWaitsForInFlightTokenRead(t *testing.T) {
	t.Parallel()
	store := &memStore{
		getStarted:   make(chan struct{}, 1),
		allowGet:     make(chan struct{}),
		blockNextGet: true,
	}
	old := "old-token"
	store.value = &old
	var fetches int
	httpClient := testHTTP(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		fetches++
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		return textResp(r, 200, sessionJSON(token)), nil
	}))
	a := newTestAuth(store, httpClient, func(string, any) {})

	errc := make(chan error, 1)
	var session *Session
	go func() {
		var err error
		session, err = a.GetSession(context.Background())
		errc <- err
	}()
	<-store.getStarted
	if err := a.saveToken(context.Background(), "new-token"); err != nil {
		t.Fatalf("saveToken: %v", err)
	}
	close(store.allowGet)
	if err := <-errc; err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil || session.Session.Token != "new-token" {
		t.Fatalf("session = %#v", session)
	}
	if store.updates != 0 {
		t.Fatalf("updates = %d", store.updates)
	}
}

func TestGetSessionDeduplicatesInFlightCalls(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	tok := "session-token"
	store.value = &tok
	allow := make(chan struct{})
	fetchStarted := make(chan struct{})
	var fetches atomic.Int32
	httpClient := testHTTP(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if fetches.Add(1) == 1 {
			close(fetchStarted)
		}
		<-allow
		return textResp(r, 200, sessionJSON(tok)), nil
	}))
	a := newTestAuth(store, httpClient, func(string, any) {})

	err1 := make(chan error, 1)
	err2 := make(chan error, 1)
	var s1, s2 *Session
	go func() {
		var err error
		s1, err = a.GetSession(context.Background())
		err1 <- err
	}()
	<-fetchStarted
	waitCtx := &doneObservedContext{Context: context.Background(), observed: make(chan struct{})}
	go func() {
		var err error
		s2, err = a.GetSession(waitCtx)
		err2 <- err
	}()
	// The in-flight branch evaluates ctx.Done before waiting on the first
	// request. Do not release that request until the second caller is there.
	<-waitCtx.observed
	close(allow)
	if err := <-err1; err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := <-err2; err != nil {
		t.Fatalf("second: %v", err)
	}
	if s1 == nil || s2 == nil || s1 != s2 {
		t.Fatalf("sessions not shared: %p %p", s1, s2)
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetches = %d", fetches.Load())
	}
}

func TestNullSessionLogoutDoesNotDropNewerToken(t *testing.T) {
	t.Parallel()
	store := &memStore{
		getStarted: make(chan struct{}, 1),
		allowGet:   make(chan struct{}),
	}
	old := "old-token"
	store.value = &old
	var tokenReads atomic.Int32
	origGet := store
	httpClient := testHTTP(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "old-token" {
			return textResp(r, 200, "null"), nil
		}
		return textResp(r, 200, sessionJSON(token)), nil
	}))
	counting := &countingStore{memStore: origGet, n: &tokenReads, blockAt: 2}
	counting.getStarted = store.getStarted
	counting.allowGet = store.allowGet
	a := NewWithOptions(Options{Store: counting, Crypto: passCrypto{}, HTTP: httpClient, Emit: func(string, any) {}})

	errc := make(chan error, 1)
	var session *Session
	go func() {
		var err error
		session, err = a.GetSession(context.Background())
		errc <- err
	}()
	<-counting.getStarted
	if err := a.saveToken(context.Background(), "new-token"); err != nil {
		t.Fatalf("saveToken: %v", err)
	}
	close(counting.allowGet)
	if err := <-errc; err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if session == nil || session.Session.Token != "new-token" {
		t.Fatalf("session = %#v", session)
	}
	if counting.token() != "new-token" {
		t.Fatalf("stored %q", counting.token())
	}
	if counting.updates != 0 {
		t.Fatalf("updates = %d", counting.updates)
	}
}

type countingStore struct {
	*memStore
	n       *atomic.Int32
	blockAt int32
}

func (s *countingStore) GetValue(ctx context.Context, key string) (*string, error) {
	n := s.n.Add(1)
	if n == s.blockAt {
		s.mu.Lock()
		s.blockNextGet = true
		s.mu.Unlock()
	}
	return s.memStore.GetValue(ctx, key)
}

func TestStartLoginSavesTokenAndBroadcasts(t *testing.T) {
	t.Parallel()
	var opened string
	var events []string
	var after bool
	var sseCookie string
	store := &memStore{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/desktop/auth/i-want-to-login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "login", Value: "handshake", Path: "/"})
		_ = json.NewEncoder(w).Encode(loginStart{
			State:         "st",
			PageURL:       "https://nahida.live/login",
			StateResponse: "http://" + r.Host + "/sse",
		})
	})
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		sseCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: ping\ndata: {}\n\n")
		_, _ = io.WriteString(w, "event: state-response\ndata: {\"state\":\"st\",\"status\":\"loggedin\",\"session\":{\"userId\":\"user-id\",\"token\":\"login-token\"}}\n\n")
	})
	mux.HandleFunc("/api/auth/get-session", func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		_, _ = io.WriteString(w, sessionJSON(token))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	zero := 0
	none := time.Duration(0)
	httpClient := infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: srv.URL,
		HTTPClient: srv.Client(),
		RetryLimit: &zero,
		RetryWait:  &none,
		Status:     infra.BackendOnline,
	})
	a := NewWithOptions(Options{
		Store:  store,
		Crypto: passCrypto{},
		HTTP:   httpClient,
		Shell:  nil,
		Emit: func(name string, _ any) {
			events = append(events, name)
		},
		AfterLogin: func() { after = true },
		Do:         srv.Client().Do,
	})
	a.openURL = func(u string) error {
		opened = u
		return nil
	}

	if err := a.StartLogin(context.Background()); err != nil {
		t.Fatalf("StartLogin: %v", err)
	}
	if opened != "https://nahida.live/login" {
		t.Fatalf("opened %q", opened)
	}
	if store.token() != "login-token" {
		t.Fatalf("stored %q", store.token())
	}
	if !after {
		t.Fatal("afterLogin not called")
	}
	if len(events) == 0 || events[0] != "auth:update" {
		t.Fatalf("events = %v", events)
	}
	if sseCookie != "login=handshake" {
		t.Fatalf("SSE cookie = %q", sseCookie)
	}
}

func TestStartLogoutBroadcastsAndSignsOut(t *testing.T) {
	t.Parallel()
	store := &memStore{}
	tok := "bye-token"
	store.value = &tok
	var events []string
	var signedOut string
	mux := http.NewServeMux()
	mux.HandleFunc("/api/auth/sign-out", func(w http.ResponseWriter, r *http.Request) {
		signedOut = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	zero := 0
	none := time.Duration(0)
	httpClient := infra.NewClientWithOptions(infra.ClientOptions{
		BackendURL: srv.URL,
		HTTPClient: srv.Client(),
		RetryLimit: &zero,
		RetryWait:  &none,
		Status:     infra.BackendOnline,
	})
	a := NewWithOptions(Options{
		Store:  store,
		Crypto: passCrypto{},
		HTTP:   httpClient,
		Emit: func(name string, data any) {
			events = append(events, name)
		},
		Do: srv.Client().Do,
	})
	if err := a.StartLogout(context.Background()); err != nil {
		t.Fatalf("StartLogout: %v", err)
	}
	if store.token() != "" {
		t.Fatalf("stored %q", store.token())
	}
	if signedOut != "bye-token" {
		t.Fatalf("sign-out token %q", signedOut)
	}
	if len(events) != 1 || events[0] != "auth:update" {
		t.Fatalf("events = %v", events)
	}
}

func TestGetBackendStatus(t *testing.T) {
	t.Parallel()
	httpClient := testHTTP(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return textResp(r, 200, `{"status":"online"}`), nil
	}))
	a := NewWithOptions(Options{HTTP: httpClient, Emit: func(string, any) {}})
	if got := a.GetBackendStatus(); got != "online" {
		t.Fatalf("status = %q", got)
	}
}

func TestProbeUsesHTTP(t *testing.T) {
	t.Parallel()
	httpClient := testHTTP(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.HasSuffix(r.URL.Path, "/status") {
			t.Fatalf("path %s", r.URL.Path)
		}
		return textResp(r, 200, `{"status":"maintenance"}`), nil
	}))
	httpClient.SetStatus(infra.BackendUnknown)
	var events []string
	a := NewWithOptions(Options{
		HTTP: httpClient,
		Emit: func(name string, data any) {
			events = append(events, name+":"+data.(string))
		},
	})
	if got := a.probe(context.Background()); got != "maintenance" {
		t.Fatalf("probe = %q", got)
	}
	if len(events) != 1 || events[0] != "backend:status:maintenance" {
		t.Fatalf("events = %v", events)
	}
}
