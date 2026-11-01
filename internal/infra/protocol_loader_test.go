package infra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"
)

func TestProtocolMemoryLoaderLifetimeAndRange(t *testing.T) {
	p := NewProtocol()
	session := p.CreateMemorySession()
	calls := 0
	url, err := p.StoreMemoryLoader(
		session,
		"lazy",
		func(context.Context) ([]byte, error) { calls++; return []byte("0123456789"), nil },
	)
	if err != nil || calls != 0 {
		t.Fatalf("registration calls=%d err=%v", calls, err)
	}
	request := httptest.NewRequest(http.MethodGet, url, nil)
	request.Header.Set("Range", "bytes=2-5")
	response := httptest.NewRecorder()
	p.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "2345" {
		t.Fatalf("response=%d %q", response.Code, response.Body.String())
	}
	p.CleanupMemorySession(session)
	response = httptest.NewRecorder()
	p.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
	if response.Code != http.StatusNotFound || calls != 1 {
		t.Fatalf("cleaned response=%d calls=%d", response.Code, calls)
	}
}

func TestProtocolMemoryLoaderRejectsCompletionAfterCleanup(t *testing.T) {
	p := NewProtocol()
	session := p.CreateMemorySession()
	url, err := p.StoreMemoryLoader(
		session,
		"lazy",
		func(context.Context) ([]byte, error) { p.CleanupMemorySession(session); return []byte("old"), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	p.ServeHTTP(response, httptest.NewRequest(http.MethodGet, url, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("late completion=%d", response.Code)
	}
}

func TestProtocolMemoryLoaderReceivesRequestCancellation(t *testing.T) {
	p := NewProtocol()
	session := p.CreateMemorySession()
	url, err := p.StoreMemoryLoader(session, "lazy", func(ctx context.Context) ([]byte, error) {
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatal("request cancellation lost")
		}
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response := httptest.NewRecorder()
	p.ServeHTTP(response, httptest.NewRequestWithContext(ctx, http.MethodGet, url, nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("cancelled response=%d", response.Code)
	}
}

func TestProtocolMemoryLoaderReceivesSessionCancellation(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "cleanup"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				p := NewProtocol()
				session := p.CreateMemorySession()
				other := p.CreateMemorySession()
				otherURL, err := p.StoreMemoryBuffer(other, "other", []byte("active"), "text/plain")
				if err != nil {
					t.Fatal(err)
				}
				started := make(chan context.Context, 1)
				release := make(chan struct{})
				url, err := p.StoreMemoryLoader(session, "lazy", func(ctx context.Context) ([]byte, error) {
					started <- ctx
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-release:
						return []byte("late"), nil
					}
				})
				if err != nil {
					t.Fatal(err)
				}
				type requestKey struct{}
				requestCtx, cancel := context.WithTimeout(
					context.WithValue(context.Background(), requestKey{}, "value"),
					time.Minute,
				)
				defer cancel()
				response := httptest.NewRecorder()
				done := make(chan struct{})
				go func() {
					p.ServeHTTP(response, httptest.NewRequestWithContext(requestCtx, http.MethodGet, url, nil))
					close(done)
				}()
				loadCtx := <-started
				if loadCtx.Value(requestKey{}) != "value" {
					t.Error("loader lost request value")
				}
				deadline, ok := loadCtx.Deadline()
				wantDeadline, _ := requestCtx.Deadline()
				if !ok || deadline != wantDeadline {
					t.Error("loader lost request deadline")
				}

				if shutdown {
					if err := p.ServiceShutdown(); err != nil {
						t.Error(err)
					}
				} else {
					p.CleanupMemorySession(session)
					p.CleanupMemorySession(session)
				}
				synctest.Wait()
				if !errors.Is(loadCtx.Err(), context.Canceled) {
					t.Errorf("loader context after session end: %v", loadCtx.Err())
				}
				select {
				case <-done:
				default:
					t.Error("cancelled loader did not finish")
				}
				if requestCtx.Err() != nil {
					t.Errorf("session end cancelled request parent: %v", requestCtx.Err())
				}
				close(release)
				<-done
				if response.Code != http.StatusNotFound || response.Body.String() == "late" {
					t.Errorf("late response=%d %q", response.Code, response.Body.String())
				}
				otherResponse := httptest.NewRecorder()
				p.ServeHTTP(otherResponse, httptest.NewRequest(http.MethodGet, otherURL, nil))
				if !shutdown && (otherResponse.Code != http.StatusOK || otherResponse.Body.String() != "active") {
					t.Errorf("other session response=%d %q", otherResponse.Code, otherResponse.Body.String())
				}
				p.CleanupMemorySession(other)
			})
		})
	}
}

func TestProtocolMemoryLoaderContentType(t *testing.T) {
	p := NewProtocol()
	session := p.CreateMemorySession()
	url, err := p.StoreMemoryLoaderWithContentType(
		session,
		"image",
		"image/png",
		func(context.Context) ([]byte, error) { return []byte("png"), nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, url, nil)
	response := httptest.NewRecorder()
	p.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
	}
}
