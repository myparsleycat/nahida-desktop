package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/appdata"
)

func TestFetchCatalogDocumentSendsProductUserAgent(t *testing.T) {
	t.Parallel()
	requests := make(chan http.Header, 1)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests <- request.Header.Clone()
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(sampleModelsDevDocument))
	}))
	defer server.Close()

	raw, err := fetchCatalogDocument(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("fetchCatalogDocument returned no data")
	}
	header := <-requests
	if header.Get("User-Agent") == "" || header.Get("User-Agent") == "Go-http-client/1.1" {
		t.Fatalf("user agent = %q", header.Get("User-Agent"))
	}
}

func TestFetchCatalogDocumentRejectsErrorStatus(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	if _, err := fetchCatalogDocument(context.Background(), server.Client(), server.URL); err == nil {
		t.Fatal("fetchCatalogDocument accepted an error status")
	}
}

func TestNewestCatalogPrefersNewerSnapshot(t *testing.T) {
	t.Parallel()
	older := &ModelCatalog{UpdatedAt: "2026-01-01T00:00:00Z"}
	newer := &ModelCatalog{UpdatedAt: "2026-09-18T00:00:00Z"}
	if newestCatalog(older, newer) != nil {
		t.Fatal("an older cache replaced the embedded catalog")
	}
	if newestCatalog(newer, older) != newer {
		t.Fatal("a newer cache was ignored")
	}
	if newestCatalog(newer, &ModelCatalog{}) != newer {
		t.Fatal("an undated cache was not preferred")
	}
	if newestCatalog(&ModelCatalog{}, newer) != nil {
		t.Fatal("an undated cache replaced a dated catalog")
	}
	if newestCatalog(nil, newer) != nil {
		t.Fatal("a missing cache replaced the embedded catalog")
	}
}

func TestStoreCatalogRoundTrip(t *testing.T) {
	t.Parallel()
	service := newSettingsService(t, Options{})
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(store); err != nil {
		t.Fatal(err)
	}

	snapshot, err := ProjectCatalog([]byte(sampleModelsDevDocument), "test", "2026-09-18T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := service.storeCatalog(snapshot); err != nil {
		t.Fatal(err)
	}

	path, err := service.catalogCachePath()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cached, err := DecodeCatalog(raw)
	if err != nil {
		t.Fatal(err)
	}
	if cached.UpdatedAt != snapshot.UpdatedAt || len(cached.Providers) != len(snapshot.Providers) {
		t.Fatalf("cached catalog = %#v", cached)
	}
}

func TestLoadCatalogCacheKeepsEmbeddedCatalogOnGarbage(t *testing.T) {
	t.Parallel()
	service := newSettingsService(t, Options{})
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(store); err != nil {
		t.Fatal(err)
	}
	path, err := service.catalogCachePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := catalogStatus()
	service.loadCatalogCache()
	if after := catalogStatus(); after != before {
		t.Fatalf("catalog status changed to %#v from %#v", after, before)
	}
}

func TestLoadCatalogCacheInstallsNewerSnapshot(t *testing.T) {
	t.Parallel()
	service := newSettingsService(t, Options{})
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UseAppData(store); err != nil {
		t.Fatal(err)
	}

	// A cache holds the same providers as the embedded snapshot but a newer publication date, so
	// installing it never depends on the shipped catalog contents.
	snapshot := catalog()
	cached := &ModelCatalog{Source: "test-cache", UpdatedAt: "2999-01-01T00:00:00Z", Providers: snapshot.Providers}
	if newestCatalog(cached, snapshot) != cached {
		t.Fatal("a newer cache was not selected")
	}
	if err := service.storeCatalog(cached); err != nil {
		t.Fatal(err)
	}

	defer func() { catalogCurrent.Store(nil) }()
	service.loadCatalogCache()
	if status := catalogStatus(); status.Source != "test-cache" {
		t.Fatalf("catalog status = %#v", status)
	}
}
