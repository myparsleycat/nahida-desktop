package infra

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
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
