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

func TestEnsureSessionOpensLoginWhenCookieMissing(t *testing.T) {
	var opens atomic.Int32
	server := validProfileServer(t, "rmc=fresh")
	service, client := gameBananaTestService(t, server)
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		opens.Add(1)
		ok, err := validate(ctx, "rmc=fresh")
		if err != nil || !ok {
			t.Fatalf("validate = %v, %v", ok, err)
		}
		return "rmc=fresh", nil
	}

	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 1 {
		t.Fatalf("opens = %d", opens.Load())
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=fresh" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestEnsureSessionSkipsLoginWhenStoredCookieValid(t *testing.T) {
	var opens atomic.Int32
	server := validProfileServer(t, "rmc=saved")
	service, _ := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	service.openLogin = func(context.Context, CookieValidator) (string, error) {
		opens.Add(1)
		return "", errors.New("should not open")
	}
	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 0 {
		t.Fatalf("opens = %d", opens.Load())
	}
}

func TestEnsureSessionPersistsEncryptedRMCOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !strings.Contains(request.Header.Get("Cookie"), "rmc=fresh") {
			t.Fatalf("cookie = %q", request.Header.Get("Cookie"))
		}
		w.Header().Add("Set-Cookie", "session=temporary; Path=/; HttpOnly")
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	service.crypto = prefixCrypto{}
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		ok, err := validate(ctx, "rmc=fresh; session=temporary")
		if err != nil || !ok {
			t.Fatalf("validate = %v, %v", ok, err)
		}
		return "rmc=fresh; session=temporary", nil
	}
	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
	if *stored == "rmc=fresh" || strings.Contains(*stored, "session=temporary") {
		t.Fatalf("DB stored plaintext cookie %q", *stored)
	}
	if !strings.HasPrefix(*stored, "enc:") {
		t.Fatalf("stored = %q, want encrypted prefix", *stored)
	}
	plain, decErr := prefixCrypto{}.DecryptString(*stored)
	if decErr != nil || plain != "rmc=fresh" {
		t.Fatalf("decrypted = %q, err = %v", plain, decErr)
	}
}

func TestEnsureSessionPersistsRotatedRMCWithoutRevalidatingOldCookie(t *testing.T) {
	var initialRequests atomic.Int32
	var rotatedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.Header.Get("Cookie") {
		case "rmc=initial":
			if initialRequests.Add(1) > 1 {
				_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
				return
			}
			w.Header().Add("Set-Cookie", "rmc=rotated; Path=/; HttpOnly")
			_, _ = io.WriteString(w, validMemberJSON)
		case "rmc=rotated":
			rotatedRequests.Add(1)
			_, _ = io.WriteString(w, validMemberJSON)
		default:
			t.Fatalf("unexpected cookie %q", request.Header.Get("Cookie"))
		}
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	var opens atomic.Int32
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		opens.Add(1)
		ok, err := validate(ctx, "rmc=initial")
		if err != nil || !ok {
			t.Fatalf("validate = %v, %v", ok, err)
		}
		return "rmc=initial", nil
	}

	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if initialRequests.Load() != 1 {
		t.Fatalf("initial requests = %d, want 1", initialRequests.Load())
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=rotated" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}

	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 1 {
		t.Fatalf("opens = %d, want 1", opens.Load())
	}
	if rotatedRequests.Load() != 1 {
		t.Fatalf("rotated requests = %d, want 1", rotatedRequests.Load())
	}
}

func TestEnsureSessionDoesNotPersistRotatedStoredCookie(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Cookie") != "rmc=initial" {
			t.Fatalf("cookie = %q, want stored cookie", request.Header.Get("Cookie"))
		}
		requests.Add(1)
		w.Header().Add("Set-Cookie", "rmc=rotated; Path=/; HttpOnly")
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=initial"); err != nil {
		t.Fatal(err)
	}

	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("requests = %d, want 2", requests.Load())
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=initial" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestEnsureSessionRejectsCookieNotReturnedByValidator(t *testing.T) {
	server := validProfileServer(t, "rmc=validated")
	service, client := gameBananaTestService(t, server)
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		ok, err := validate(ctx, "rmc=validated")
		if err != nil || !ok {
			t.Fatalf("validate = %v, %v", ok, err)
		}
		return "rmc=unvalidated", nil
	}

	err := service.EnsureSession(context.Background())
	if !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("err = %v, want %v", err, ErrAuthFailed)
	}
	stored, getErr := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if getErr != nil || stored != nil {
		t.Fatalf("stored = %v, error = %v", stored, getErr)
	}
}

func TestEnsureSessionRemovesInvalidStoredCookieThenLogsIn(t *testing.T) {
	var opens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Cookie") == "rmc=stale" {
			_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
			return
		}
		if request.Header.Get("Cookie") != "rmc=fresh" {
			t.Fatalf("cookie = %q", request.Header.Get("Cookie"))
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=stale"); err != nil {
		t.Fatal(err)
	}
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		opens.Add(1)
		ok, err := validate(ctx, "rmc=fresh")
		if err != nil || !ok {
			t.Fatalf("validate = %v, %v", ok, err)
		}
		return "rmc=fresh", nil
	}
	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 1 {
		t.Fatalf("opens = %d", opens.Load())
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=fresh" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestEnsureSessionTreatsUnauthorizedStoredCookieAsInvalidThenLogsIn(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.Header.Get("Cookie") {
		case "rmc=stale":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
		case "rmc=fresh":
			_, _ = io.WriteString(w, validMemberJSON)
		default:
			t.Fatalf("unexpected cookie %q", request.Header.Get("Cookie"))
		}
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=stale"); err != nil {
		t.Fatal(err)
	}
	var opens atomic.Int32
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		opens.Add(1)
		ok, err := validate(ctx, "rmc=fresh")
		if err != nil || !ok {
			t.Fatalf("validate = %v, %v", ok, err)
		}
		return "rmc=fresh", nil
	}

	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 1 {
		t.Fatalf("opens = %d, want 1", opens.Load())
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=fresh" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestEnsureSessionTreatsBadProfileAsInvalidThenLogsIn(t *testing.T) {
	var opens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Cookie") == "rmc=stale" {
			_, _ = io.WriteString(w, `{"_sUsername":1}`)
			return
		}
		if request.Header.Get("Cookie") != "rmc=fresh" {
			t.Fatalf("cookie = %q", request.Header.Get("Cookie"))
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=stale"); err != nil {
		t.Fatal(err)
	}
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		opens.Add(1)
		ok, err := validate(ctx, "rmc=fresh")
		if err != nil || !ok {
			t.Fatalf("validate = %v, %v", ok, err)
		}
		return "rmc=fresh", nil
	}
	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	if opens.Load() != 1 {
		t.Fatalf("opens = %d", opens.Load())
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=fresh" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestEnsureSessionKeepsStoredCookieOnTransientHTTPError(t *testing.T) {
	var opens atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	service.openLogin = func(context.Context, CookieValidator) (string, error) {
		opens.Add(1)
		return "", errors.New("should not open")
	}
	err := service.EnsureSession(context.Background())
	if !errors.Is(err, ErrServerUnreachable) {
		t.Fatalf("err = %v", err)
	}
	if opens.Load() != 0 {
		t.Fatalf("opens = %d", opens.Load())
	}
	stored, getErr := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if getErr != nil || stored == nil || *stored != "rmc=saved" {
		t.Fatalf("stored = %v, error = %v", stored, getErr)
	}
}

func TestEnsureSessionDoesNotPersistInvalidLoginCandidate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Cookie") == "rmc=bad" {
			_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
			return
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		ok, err := validate(ctx, "rmc=bad")
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("invalid candidate accepted")
		}
		ok, err = validate(ctx, "rmc=good")
		if err != nil || !ok {
			t.Fatalf("valid candidate rejected: %v %v", ok, err)
		}
		return "rmc=good", nil
	}
	if err := service.EnsureSession(context.Background()); err != nil {
		t.Fatal(err)
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=good" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestEnsureSessionSingleFlightAndWaiterCancel(t *testing.T) {
	var opens atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	server := validProfileServer(t, "rmc=shared")
	service, _ := gameBananaTestService(t, server)
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		opens.Add(1)
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
		ok, err := validate(ctx, "rmc=shared")
		if err != nil || !ok {
			return "", errors.New("validate failed")
		}
		return "rmc=shared", nil
	}

	waitCtx, cancelWait := context.WithCancel(context.Background())
	firstErr := make(chan error, 1)
	secondErr := make(chan error, 1)
	go func() { firstErr <- service.EnsureSession(context.Background()) }()
	<-started
	go func() { secondErr <- service.EnsureSession(waitCtx) }()
	time.Sleep(20 * time.Millisecond)
	cancelWait()
	if err := <-secondErr; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiter err = %v", err)
	}
	close(release)
	if err := <-firstErr; err != nil {
		t.Fatalf("owner err = %v", err)
	}
	if opens.Load() != 1 {
		t.Fatalf("opens = %d", opens.Load())
	}
}

func TestEnsureSessionUserCancel(t *testing.T) {
	server := validProfileServer(t, "rmc=x")
	service, _ := gameBananaTestService(t, server)
	service.openLogin = func(context.Context, CookieValidator) (string, error) {
		return "", errors.New(errCodeLoginCancelled)
	}
	err := service.EnsureSession(context.Background())
	if err == nil || err.Error() != errCodeLoginCancelled {
		t.Fatalf("err = %v", err)
	}
}

func TestEnsureSessionUnsupportedWithoutOpener(t *testing.T) {
	server := validProfileServer(t, "rmc=x")
	service, _ := gameBananaTestService(t, server)
	err := service.EnsureSession(context.Background())
	if !errors.Is(err, ErrAutoLoginUnsupported) || err.Error() != errCodeAutoLoginUnsupported {
		t.Fatalf("err = %v", err)
	}
}

func TestLogoutClearsWebViewCookieEvenIfSiteLogoutFails(t *testing.T) {
	var cleared atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "logout") {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	service.clearLoginCookies = func(context.Context) error {
		cleared.Add(1)
		return nil
	}
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if cleared.Load() != 1 {
		t.Fatalf("cleared = %d", cleared.Load())
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored != nil {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestLogoutProcessesWebViewSessionBeforeBackendLogout(t *testing.T) {
	var stages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.Contains(request.URL.Path, "logout") {
			stages = append(stages, "backend")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, _ := gameBananaTestService(t, server)
	service.clearLoginCookies = func(context.Context) error {
		stages = append(stages, "webview")
		return nil
	}
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(stages) != 2 || stages[0] != "webview" || stages[1] != "backend" {
		t.Fatalf("stages = %v", stages)
	}
}

func TestValidateCandidateDoesNotPersistOrClearStoredCookie(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Add("Set-Cookie", "session=temporary; Path=/; HttpOnly")
		if request.Header.Get("Cookie") == "rmc=candidate" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	valid, merged, err := service.validateCandidateRMCCookie(context.Background(), "rmc=candidate")
	if valid || merged != "" || err != nil {
		t.Fatalf("valid=%v merged=%q err=%v", valid, merged, err)
	}
	stored, getErr := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if getErr != nil || stored == nil || *stored != "rmc=saved" {
		t.Fatalf("stored = %v, error = %v", stored, getErr)
	}
}

func TestManualAndAutoValidationShareRules(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Cookie") != "rmc=secret" {
			t.Fatalf("cookie = %q", request.Header.Get("Cookie"))
		}
		w.Header().Add("Set-Cookie", "session=temporary; Path=/; HttpOnly")
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	service, client := gameBananaTestService(t, server)
	valid, merged, err := service.validateCandidateRMCCookie(context.Background(), "rmc=secret")
	if err != nil || !valid || !strings.Contains(merged, "rmc=secret") {
		t.Fatalf("auto valid=%v merged=%q err=%v", valid, merged, err)
	}
	result, err := service.SetManualRMCToken(context.Background(), "secret")
	if err != nil || !result.OK {
		t.Fatalf("manual = %+v, %v", result, err)
	}
	stored, err := client.Settings.GetValue(context.Background(), cookieSettingKey)
	if err != nil || stored == nil || *stored != "rmc=secret" {
		t.Fatalf("stored = %v, error = %v", stored, err)
	}
}

func TestCookieValuesAreAbsentFromLoggerOutput(t *testing.T) {
	var buf bytes.Buffer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	service, _ := gameBananaTestService(t, server)
	service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &buf, DisableFile: true})
	service.log.SetLevel("warn")
	if err := service.saveCookie(context.Background(), "rmc=super-secret-token"); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "super-secret-token") || strings.Contains(buf.String(), "rmc=") {
		t.Fatalf("log leaked cookie: %q", buf.String())
	}
}

func TestConcurrentEnsureSessionOpensOnce(t *testing.T) {
	var opens atomic.Int32
	var wg sync.WaitGroup
	server := validProfileServer(t, "rmc=once")
	service, _ := gameBananaTestService(t, server)
	service.openLogin = func(ctx context.Context, validate CookieValidator) (string, error) {
		opens.Add(1)
		time.Sleep(30 * time.Millisecond)
		ok, err := validate(ctx, "rmc=once")
		if err != nil || !ok {
			return "", errors.New("validate failed")
		}
		return "rmc=once", nil
	}
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- service.EnsureSession(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if opens.Load() != 1 {
		t.Fatalf("opens = %d", opens.Load())
	}
}

type prefixCrypto struct{}

func (prefixCrypto) EncryptString(value string) (string, error) { return "enc:" + value, nil }
func (prefixCrypto) DecryptString(value string) (string, error) {
	if !strings.HasPrefix(value, "enc:") {
		return "", errors.New("not encrypted")
	}
	return strings.TrimPrefix(value, "enc:"), nil
}

const validMemberJSON = `{"_sUsername":"member","_sProfileUrl":"https://gamebanana.com/members/1","_sAvatarUrl":"avatar"}`

func validProfileServer(t *testing.T, wantCookie string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if wantCookie != "" && request.Header.Get("Cookie") != wantCookie {
			t.Fatalf("cookie = %q, want %q", request.Header.Get("Cookie"), wantCookie)
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	t.Cleanup(server.Close)
	return server
}
