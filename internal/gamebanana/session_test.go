package gamebanana

import (
	"bytes"
	"context"
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

func TestAuthenticationResponseClassification(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body, code string
		invalid    bool
	}{
		{"unauthorized", 401, "", "GAMEBANANA_INVALID_RMC", true},
		{"login-required", 200, `{"_sErrorCode":"LOGIN_REQUIRED"}`, "GAMEBANANA_INVALID_RMC", true},
		{"forbidden-login-required", 403, `{"_sErrorCode":"LOGIN_REQUIRED"}`, "GAMEBANANA_INVALID_RMC", true},
		{"forbidden", 403, "<html>Access denied</html>", errCodeAuthCheckFailed, false},
		{"html", 200, "<html>Challenge</html>", errCodeAuthCheckFailed, false},
		{"malformed", 200, "{", errCodeAuthCheckFailed, false},
		{"schema", 200, `{"_sUsername":12}`, errCodeAuthCheckFailed, false},
		{"empty", 200, "", errCodeAuthCheckFailed, false},
		{"limited", 429, "", errCodeServerUnreachable, false},
		{"server", 503, "", errCodeServerUnreachable, false},
		{"server-login-body", 503, `{"_sErrorCode":"LOGIN_REQUIRED"}`, errCodeServerUnreachable, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Add("Set-Cookie", "rmc=untrusted-response; Path=/")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			}))
			t.Cleanup(server.Close)
			service, _ := gameBananaTestService(t, server)
			var logs bytes.Buffer
			service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true})
			ctx := context.Background()
			if err := service.saveCookie(ctx, "rmc=private-saved"); err != nil {
				t.Fatal(err)
			}
			result, err := service.SetManualRMCToken(ctx, "private-candidate")
			if err != nil || result.OK || result.ErrorCode != tc.code {
				t.Fatalf("result=%+v, err=%v", result, err)
			}
			cookie, err := service.getCookie(ctx)
			if err != nil || cookie != "rmc=private-saved" {
				t.Fatal("manual validation modified saved session")
			}
			var opens atomic.Int32
			service.openLogin = func(context.Context, CookieValidator) (string, error) { opens.Add(1); return "", ErrLoginCancelled }
			err = service.EnsureSession(ctx)
			if tc.invalid {
				if !errors.Is(err, ErrLoginCancelled) || opens.Load() != 1 {
					t.Fatalf("expected fresh login: %v", err)
				}
			} else {
				if err == nil || err.Error() != tc.code || opens.Load() != 0 {
					t.Fatalf("unexpected auth handling: %v; opens=%d", err, opens.Load())
				}
				cookie, _ = service.getCookie(ctx)
				if cookie != "rmc=private-saved" {
					t.Fatal("uncertain response cleared cookie")
				}
			}
			if strings.Contains(logs.String(), "private-") || strings.Contains(logs.String(), "untrusted-response") {
				t.Fatal("auth diagnostic leaked secret")
			}
		})
	}
}

func TestManualSaveWinsPendingAutomaticLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	validated, release := make(chan struct{}), make(chan struct{})
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		ok, err := validate(ctx, "rmc=automatic")
		if err != nil || !ok {
			return "", ErrAuthFailed
		}
		close(validated)
		<-release
		return "rmc=automatic", nil
	}
	done := make(chan error, 1)
	go func() { done <- service.EnsureSession(context.Background()) }()
	<-validated
	result, err := service.SetManualRMCToken(context.Background(), "manual")
	close(release)
	if err != nil || !result.OK {
		t.Fatalf("manual=%+v err=%v", result, err)
	}
	if err := <-done; err != nil {
		t.Fatalf("accepted manual session was reported as failure: %v", err)
	}
	cookie, _ := service.getCookie(context.Background())
	if cookie != "rmc=manual" {
		t.Fatal("automatic response overwrote manual session")
	}
}

func TestLogoutBlocksNewAuthenticationUntilCleanupCompletes(t *testing.T) {
	server := validProfileServer(t, "rmc=candidate")
	service, _ := gameBananaTestService(t, server)
	started, release := make(chan struct{}), make(chan struct{})
	service.clearLoginCookies = func(context.Context) error { close(started); <-release; return nil }
	done := make(chan error, 1)
	go func() { done <- service.Logout(context.Background()) }()
	<-started
	err := service.EnsureSession(context.Background())
	result, manualErr := service.SetManualRMCToken(context.Background(), "candidate")
	close(release)
	if !errors.Is(err, ErrLoginCancelled) || manualErr != nil || result.OK {
		t.Fatalf("authentication ran during logout: %v, %+v", err, result)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

type failingSaveCrypto struct{ Crypto }

func (f failingSaveCrypto) EncryptString(string) (string, error) {
	return "", errors.New("encryption unavailable")
}

func TestRotatedCookiePersistenceFailureIsNotSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Add("Set-Cookie", "rmc=rotated; Path=/")
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	defer server.Close()
	service, client := gameBananaTestService(t, server)
	ctx := context.Background()
	if err := service.saveCookie(ctx, "rmc=original"); err != nil {
		t.Fatal(err)
	}
	service.crypto = failingSaveCrypto{service.crypto}
	if err := service.EnsureSession(ctx); err == nil {
		t.Fatal("persistence failure returned success")
	}
	cookie, _ := service.getCookie(ctx)
	stored, err := client.Settings.GetValue(ctx, cookieSettingKey)
	if cookie != "rmc=original" || err != nil || stored == nil || *stored != "rmc=original" {
		t.Fatal("failed save partially changed session")
	}
}

func TestUnchangedValidationDoesNotInvalidatePendingUpdates(t *testing.T) {
	server := validProfileServer(t, "rmc=saved")
	service, _ := gameBananaTestService(t, server)
	ctx := context.Background()
	if err := service.saveCookie(ctx, "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	_, revision, err := service.cookieSnapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureSession(ctx); err != nil {
		t.Fatal(err)
	}
	if applied, err := service.updateCookie(ctx, revision, "rmc=next"); err != nil || !applied {
		t.Fatalf("unchanged validation invalidated update: %v", err)
	}
}

func TestConcurrentStoredValidationSharesCallAndAllowsWaiterCancel(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	owner := make(chan error, 1)
	go func() { owner <- service.EnsureSession(context.Background()) }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := service.EnsureSession(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter=%v", err)
	}
	unblock()
	if err := <-owner; err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("requests=%d", requests.Load())
	}
}

func TestStoredValidationDiscardsStaleResult(t *testing.T) {
	for _, valid := range []bool{false, true} {
		t.Run(map[bool]string{false: "invalid", true: "valid"}[valid], func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if cookieValue(r.Header.Get("Cookie"), "rmc") == "old" {
					close(started)
					<-release
					if !valid {
						_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
						return
					}
					w.Header().Add("Set-Cookie", "rmc=stale-rotation; Path=/")
				}
				_, _ = io.WriteString(w, validMemberJSON)
			}))
			defer server.Close()
			service, _ := gameBananaTestService(t, server)
			ctx := context.Background()
			if err := service.saveCookie(ctx, "rmc=old"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- service.EnsureSession(ctx) }()
			<-started
			result, err := service.SetManualRMCToken(ctx, "new")
			if err != nil || !result.OK {
				close(release)
				t.Fatalf("manual=%+v %v", result, err)
			}
			close(release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			cookie, _ := service.getCookie(ctx)
			if cookie != "rmc=new" {
				t.Fatal("stale validation overwrote new session")
			}
		})
	}
}

func TestLogoutInvalidatesPendingManualSave(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Personal") {
			close(started)
			<-release
			_, _ = io.WriteString(w, validMemberJSON)
		}
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	ctx := context.Background()
	if err := service.saveCookie(ctx, "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	done := make(chan ManualRMCSaveResult, 1)
	go func() { result, _ := service.SetManualRMCToken(ctx, "pending"); done <- result }()
	<-started
	err := service.Logout(ctx)
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if result := <-done; result.OK {
		t.Fatal("late manual save resurrected logout")
	}
	cookie, _ := service.getCookie(ctx)
	if cookie != "" {
		t.Fatal("logout did not persist")
	}
}

func TestLogoutInvalidatesPendingAutomaticLogin(t *testing.T) {
	server := validProfileServer(t, "rmc=candidate")
	service, _ := gameBananaTestService(t, server)
	validated := make(chan struct{})
	release := make(chan struct{})
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		ok, err := validate(ctx, "rmc=candidate")
		if err != nil || !ok {
			return "", ErrAuthFailed
		}
		close(validated)
		<-release
		return "rmc=candidate", nil
	}
	done := make(chan error, 1)
	go func() { done <- service.EnsureSession(context.Background()) }()
	<-validated
	err := service.Logout(context.Background())
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil {
		t.Fatal("cancelled login succeeded")
	}
	cookie, _ := service.getCookie(context.Background())
	if cookie != "" {
		t.Fatal("automatic login resurrected logout")
	}
}

func TestRequestCannotMutateReplacedSession(t *testing.T) {
	for _, status := range []int{200, 401, 403} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				close(started)
				<-release
				w.Header().Add("Set-Cookie", "rmc=stale; Path=/")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, `{}`)
			}))
			defer server.Close()
			service, _ := gameBananaTestService(t, server)
			ctx := context.Background()
			if err := service.saveCookie(ctx, "rmc=old"); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				r, err := service.request(
					ctx,
					http.MethodGet,
					server.URL+"/apiv13/test",
					nil,
					requestPolicy{PersistResponseCookies: true, ClearStoredCookieOnAuth: true, SkipAuthRetry: true},
				)
				if r != nil {
					_ = r.Body.Close()
				}
				done <- err
			}()
			<-started
			if err := service.saveCookie(ctx, "rmc=new"); err != nil {
				close(release)
				t.Fatal(err)
			}
			close(release)
			<-done
			cookie, _ := service.getCookie(ctx)
			if cookie != "rmc=new" {
				t.Fatal("old response changed new cookie")
			}
		})
	}
}
