package zzmifixer

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/db"
)

func useToolsTestAppData(t *testing.T, service *Service, home string) string {
	t.Helper()
	data, err := appdata.Open(home)
	if err != nil {
		t.Fatalf("appdata.Open: %v", err)
	}
	service.UseAppData(data)
	return data.Root()
}

func openToolsTestDB(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "tools.db"))
	if err != nil {
		t.Fatalf("db.New: %v", err)
	}
	if err := client.Reconcile(context.Background()); err != nil {
		_ = client.Close()
		t.Fatalf("Reconcile: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

type toolsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f toolsRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func parseRFC3339(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}
