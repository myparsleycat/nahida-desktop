package gamebanana

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestGetSessionStatusReportsAnonymousWithoutNetwork(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)

	status, err := service.GetSessionStatus(context.Background())
	if err != nil || status.Authenticated || status.Username != "" {
		t.Fatalf("status = %+v, error = %v", status, err)
	}
	if requests.Load() != 0 {
		t.Fatalf("network requests = %d, want 0", requests.Load())
	}
}

func TestGetSessionStatusReportsSavedMember(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Cookie") != "rmc=saved" {
			t.Errorf("cookie = %q", request.Header.Get("Cookie"))
		}
		_, _ = io.WriteString(w, validMemberJSON)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}

	status, err := service.GetSessionStatus(context.Background())
	if err != nil || !status.Authenticated || status.Username != "member" {
		t.Fatalf("status = %+v, error = %v", status, err)
	}
	cookie, err := service.getCookie(context.Background())
	if err != nil || cookie != "rmc=saved" {
		t.Fatalf("stored session = %q, error = %v", cookie, err)
	}
}

func TestGetSessionStatusClearsRejectedSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"_sErrorCode":"LOGIN_REQUIRED"}`)
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=stale"); err != nil {
		t.Fatal(err)
	}

	status, err := service.GetSessionStatus(context.Background())
	if err != nil || status.Authenticated || status.Username != "" {
		t.Fatalf("status = %+v, error = %v", status, err)
	}
	cookie, err := service.getCookie(context.Background())
	if err != nil || cookie != "" {
		t.Fatalf("stored session = %q, error = %v", cookie, err)
	}
}

func TestGetSessionStatusReportsTransportFailureAndKeepsSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	}))
	defer server.Close()
	service, _ := gameBananaTestService(t, server)
	if err := service.saveCookie(context.Background(), "rmc=saved"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	status, err := service.GetSessionStatus(ctx)
	if err == nil || status.Authenticated {
		t.Fatalf("status = %+v, error = %v", status, err)
	}
	cookie, err := service.getCookie(context.Background())
	if err != nil || cookie != "rmc=saved" {
		t.Fatalf("stored session = %q, error = %v", cookie, err)
	}
}
