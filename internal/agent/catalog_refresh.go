package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	catalogSourceURL = "https://models.dev/api.json"
	// catalogCacheFile is the refreshed catalog under the application data directory.
	catalogCacheFile = "agent/models.json"
	catalogCacheTTL  = 24 * time.Hour
	catalogFetchSize = 32 << 20
)

// catalogStatus reports the catalog the settings screen is showing.
func catalogStatus() AgentCatalogStatus {
	snapshot := catalog()
	return AgentCatalogStatus{Source: snapshot.Source, UpdatedAt: snapshot.UpdatedAt}
}

// loadCatalogCache installs the newest cached catalog when it is fresher than the embedded one.
func (s *Service) loadCatalogCache() {
	path, err := s.catalogCachePath()
	if err != nil {
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	cached, err := DecodeCatalog(raw)
	if err != nil {
		_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "agent-provider-catalog", Stage: "read-cache",
			Fields: map[string]any{"path": path},
		})
		return
	}
	if newest := newestCatalog(cached, catalog()); newest != nil {
		useCatalog(newest)
	}
}

// newestCatalog returns the later publication among a cached snapshot and the embedded one, or nil
// when the cached snapshot is not newer.
func newestCatalog(cached, embedded *ModelCatalog) *ModelCatalog {
	if cached == nil {
		return nil
	}
	if embedded == nil || catalogTimestamp(cached).After(catalogTimestamp(embedded)) {
		return cached
	}
	return nil
}

// storeCatalog writes a snapshot next to the application data so the next start keeps it.
func (s *Service) storeCatalog(snapshot *ModelCatalog) error {
	encoded, err := EncodeCatalog(snapshot)
	if err != nil {
		return err
	}
	path, err := s.catalogCachePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0o600)
}

// scheduleCatalogRefresh downloads the catalog in the background when the current one has aged
// out. Refreshing is best effort: an offline machine keeps the catalog it already has.
func (s *Service) scheduleCatalogRefresh() {
	if time.Since(catalogTimestamp(catalog())) < catalogCacheTTL {
		return
	}
	s.catalogMu.Lock()
	attempted := s.catalogFetchedAt
	s.catalogFetchedAt = time.Now()
	s.catalogMu.Unlock()
	if time.Since(attempted) < catalogCacheTTL {
		return
	}
	go func() {
		if err := s.refreshCatalog(context.Background()); err != nil {
			_ = infra.ReportError(s.log, err, "Agent", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "agent-provider-catalog", Stage: "refresh",
			})
		}
	}()
}

// refreshCatalog downloads the models.dev catalog, installs it for this process, and caches it
// next to the application data.
func (s *Service) refreshCatalog(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	client := s.http
	if client == nil {
		client = http.DefaultClient
	}
	raw, err := fetchCatalogDocument(ctx, client, catalogSourceURL)
	if err != nil {
		return err
	}

	snapshot, err := ProjectCatalog(raw, catalogSourceURL, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	useCatalog(snapshot)
	return s.storeCatalog(snapshot)
}

// fetchCatalogDocument downloads the raw models.dev document.
func fetchCatalogDocument(ctx context.Context, client *http.Client, source string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", platform.UserAgent())

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("download the model catalog: unexpected status %d", response.StatusCode)
	}
	return io.ReadAll(io.LimitReader(response.Body, catalogFetchSize))
}

func (s *Service) catalogCachePath() (string, error) {
	if s.appData == nil {
		return "", fmt.Errorf("application data is unavailable")
	}
	return s.appData.Resolve(catalogCacheFile)
}

// catalogTimestamp reads the publication time of a catalog; an unparsable value sorts oldest so
// the embedded fallback never wins over a real snapshot.
func catalogTimestamp(snapshot *ModelCatalog) time.Time {
	if snapshot == nil {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, snapshot.UpdatedAt)
	if err != nil {
		return time.Time{}
	}
	return parsed
}
