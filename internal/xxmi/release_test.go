package xxmi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

func TestListReleasesAllowsStaleUIReadWithoutWeakeningStrictReads(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := &releaseCacheTestStore{values: make(map[string]string)}
	fail := false
	requests := 0
	httpClient := infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
			requests++
			status, body := http.StatusOK, `[{"tag_name":"v1.2.3"}]`
			if fail {
				status, body = http.StatusServiceUnavailable, "unavailable"
			}
			return &http.Response{
				StatusCode: status, Header: make(http.Header), Request: request,
				Body: io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
	})
	newService := func() *XXMI {
		rate := infra.NewGitHubRateCoordinator()
		rate.UseAppState(store)
		return NewWithOptions(Options{GitHub: github.New(github.Options{HTTP: httpClient, Rate: rate})})
	}
	if _, err := newService().ListReleases(ctx, "importer:GIMI"); err != nil {
		t.Fatal(err)
	}
	fetchedAt := time.Now().Add(-2 * time.Hour).UTC()
	store.backdate(t, "v1.2.3", fetchedAt)
	fail = true
	x := newService()
	var info infra.GitHubResponseInfo
	releases, err := x.ListReleases(infra.WithGitHubResponseInfo(ctx, &info), "importer:GIMI")
	if err != nil || len(releases) != 1 || releases[0].Version != "1.2.3" ||
		!info.Cached || !info.Stale || !info.FetchedAt.Equal(fetchedAt) {
		t.Fatalf("stale UI releases = %+v, info = %+v, error = %v", releases, info, err)
	}
	if _, err := x.listReleases(ctx, "importer:GIMI", false); err == nil {
		t.Fatal("strict release read accepted stale metadata after failed refresh")
	}
	if requests != 2 {
		t.Fatalf("release requests = %d, want 2 with failed-refresh cooldown", requests)
	}
}

// Each test owns its persistent store, including the cache ages used to simulate a restart.
type releaseCacheTestStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *releaseCacheTestStore) GetValue(_ context.Context, key string) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, nil
	}
	return &value, nil
}

func (s *releaseCacheTestStore) Upsert(_ context.Context, key, value, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

func (s *releaseCacheTestStore) backdate(t *testing.T, tag string, fetchedAt time.Time) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range s.values {
		if !strings.HasPrefix(key, "github:http-cache:") || !strings.Contains(value, tag) {
			continue
		}
		var record map[string]json.RawMessage
		if err := json.Unmarshal([]byte(value), &record); err != nil {
			t.Fatal(err)
		}
		encodedTime, err := json.Marshal(fetchedAt)
		if err != nil {
			t.Fatal(err)
		}
		record["fetchedAt"] = encodedTime
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		s.values[key] = string(raw)
		return
	}
	t.Fatal("release was not persisted in the test cache")
}
