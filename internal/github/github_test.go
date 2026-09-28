package github

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"

	"nahida.live/desktop/internal/infra"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

func newTestClient(t *testing.T, handler func(*http.Request) (int, string)) *Client {
	t.Helper()
	return New(Options{HTTP: newTestHTTP(handler)})
}

func newTestHTTP(handler func(*http.Request) (int, string)) *infra.Client {
	noRetries := 0
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		status, body := handler(request)
		return &http.Response{
			StatusCode: status,
			Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    request,
		}, nil
	})
	return infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: transport},
		Status:     infra.BackendOnline,
		RetryLimit: &noRetries,
	})
}

type memoryRateStore struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *memoryRateStore) GetValue(_ context.Context, key string) (*string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, nil
	}
	return &value, nil
}

func (s *memoryRateStore) Upsert(_ context.Context, key, value, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = make(map[string]string)
	}
	s.values[key] = value
	return nil
}

func TestRepoValidateRejectsPathSegments(t *testing.T) {
	valid := Repo{Owner: "SpectrumQT", Name: "XXMI-Libs-Package"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid repo rejected: %v", err)
	}
	for _, repo := range []Repo{
		{Owner: "", Name: "repo"},
		{Owner: "owner", Name: ".."},
		{Owner: ".", Name: "repo"},
		{Owner: "owner/evil", Name: "repo"},
		{Owner: "owner", Name: "repo?x=1"},
	} {
		if err := repo.Validate(); !errors.Is(err, ErrInvalidRepo) {
			t.Fatalf("Validate(%q) = %v, want ErrInvalidRepo", repo, err)
		}
	}
}

func TestReleaseURLsEscapeTagAndAssetName(t *testing.T) {
	repo := Repo{Owner: "SilentNightSound", Name: "GIMI-Package"}
	got := ReleaseFileURL(repo, "v1/2", "GIMI PACKAGE.zip")
	want := "https://github.com/SilentNightSound/GIMI-Package/releases/download/v1%2F2/GIMI%20PACKAGE.zip"
	if got != want {
		t.Fatalf("ReleaseFileURL = %q, want %q", got, want)
	}
	if got := TagArchiveURL(repo, "v1.2.3"); got !=
		"https://github.com/SilentNightSound/GIMI-Package/archive/refs/tags/v1.2.3.zip" {
		t.Fatalf("TagArchiveURL = %q", got)
	}
}

func TestVersionTagsDropsEmptyAndBranchTags(t *testing.T) {
	got := VersionTags([]Release{
		{TagName: "v2"}, {TagName: " "}, {TagName: "Main"}, {TagName: "master"}, {TagName: " v1 "},
	})
	if strings.Join(got, ",") != "v2,v1" {
		t.Fatalf("VersionTags = %v", got)
	}
}
