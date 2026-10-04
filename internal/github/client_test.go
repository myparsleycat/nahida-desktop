package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
)

var testRepo = Repo{Owner: "SpectrumQT", Name: "XXMI-Libs-Package"}

const testReleases = `[
	{"tag_name":"v3-rc.1","prerelease":true},
	{"tag_name":"v2"},
	{"tag_name":"v2-draft","draft":true},
	{"tag_name":"v1"}
]`

func TestReleasesSendsAPIHeadersAndFiltersPrereleases(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) (int, string) {
		if request.URL.String() != "https://api.github.com/repos/SpectrumQT/XXMI-Libs-Package/releases" {
			t.Errorf("URL = %s", request.URL)
		}
		if request.Header.Get("Accept") != "application/vnd.github+json" ||
			request.Header.Get("X-GitHub-Api-Version") != apiVersion ||
			!strings.Contains(request.Header.Get("User-Agent"), "Chrome/138.0.0.0") {
			t.Errorf("headers = %v", request.Header)
		}
		return http.StatusOK, testReleases
	})

	stable, err := client.Releases(context.Background(), testRepo)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(VersionTags(stable), ","); got != "v2,v1" {
		t.Fatalf("stable releases = %s", got)
	}
}

func TestLatestReleaseDecodesAssets(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) (int, string) {
		if request.URL.Path != "/repos/Moonholder/Wuwa_Mod_Fixer/releases/latest" {
			t.Errorf("URL = %s", request.URL)
		}
		return http.StatusOK, `{"tag_name":"v1.2.3","published_at":"2026-01-02T03:04:05Z","assets":[` +
			`{"name":"fixer.exe","browser_download_url":"https://example.test/fixer.exe","digest":"sha256:00"},` +
			`{"name":"notes.txt","browser_download_url":"https://example.test/notes.txt","digest":null}]}`
	})

	release, err := client.LatestRelease(context.Background(), Repo{Owner: "Moonholder", Name: "Wuwa_Mod_Fixer"})
	if err != nil {
		t.Fatal(err)
	}
	if release.TagName != "v1.2.3" || len(release.Assets) != 2 || release.Assets[0].Digest != "sha256:00" ||
		release.Assets[1].Digest != "" {
		t.Fatalf("release = %+v", release)
	}
}

func TestGetBytesReportsStatusAndSizeLimit(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) (int, string) {
		if strings.HasSuffix(request.URL.Path, "/missing") {
			return http.StatusServiceUnavailable, "unavailable"
		}
		return http.StatusOK, "0123456789"
	})

	_, err := client.GetBytes(context.Background(), "https://api.github.com/missing", 64)
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != http.StatusServiceUnavailable || err.Error() != "HTTP 503" {
		t.Fatalf("status error = %v", err)
	}
	if _, err := client.GetBytes(context.Background(), "https://api.github.com/large", 4); err == nil ||
		!strings.Contains(err.Error(), "exceeds 4 bytes") {
		t.Fatalf("size error = %v", err)
	}
}

func TestGetBytesRejectsNonAPIHosts(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) (int, string) {
		t.Errorf("unexpected request: %s", request.URL)
		return http.StatusOK, ""
	})
	for _, rawURL := range []string{
		"http://api.github.com/repos/a/b",
		"https://github.com/a/b",
		"https://api.github.com.evil.test/repos/a/b",
	} {
		if _, err := client.GetBytes(context.Background(), rawURL, 64); err == nil {
			t.Fatalf("GetBytes(%q) accepted a non-API URL", rawURL)
		}
	}
}

func TestGetBytesGatesOnExhaustedRateAndCapturesHeaders(t *testing.T) {
	store := &memoryRateStore{}
	exhausted, _ := json.Marshal(infra.GitHubRateState{
		Limit: 60, Remaining: 0, Reset: time.Now().Add(time.Hour).Unix(), Resource: "core",
	})
	if err := store.Upsert(context.Background(), "github:core-rate", string(exhausted), ""); err != nil {
		t.Fatal(err)
	}
	rate := infra.NewGitHubRateCoordinator()
	rate.UseAppState(store)
	var requests atomic.Int32
	client := New(Options{
		HTTP: newTestHTTP(func(*http.Request) (int, string) {
			requests.Add(1)
			return http.StatusOK, "[]"
		}),
		Rate: rate,
	})

	_, err := client.GetBytes(context.Background(), "https://api.github.com/repos/a/b/releases", 64)
	var rateErr *RateLimitError
	if !errors.Is(err, ErrRateLimited) || !errors.As(err, &rateErr) || rateErr.ResetAt().IsZero() {
		t.Fatalf("rate error = %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("rate-limited call reached the network %d time(s)", requests.Load())
	}

	store.values = nil
	rate.UseAppState(store)
	capturing := New(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			Status: infra.BackendOnline,
			HTTPClient: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				response := &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
					Body: io.NopCloser(strings.NewReader("[]")),
				}
				response.Header.Set("X-RateLimit-Limit", "60")
				response.Header.Set("X-RateLimit-Remaining", "41")
				response.Header.Set("X-RateLimit-Reset", "2000000000")
				response.Header.Set("X-RateLimit-Used", "19")
				return response, nil
			})},
		}),
		Rate: rate,
	})
	if _, err := capturing.GetBytes(context.Background(), "https://api.github.com/repos/a/b/releases", 64); err != nil {
		t.Fatal(err)
	}
	state, err := capturing.RateState(context.Background())
	if err != nil || state == nil || state.Remaining != 41 || capturing.IsRateLimited(state) {
		t.Fatalf("captured state = %+v, %v", state, err)
	}
}

func TestResolveTagCommitFollowsAnnotatedTags(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	repo := Repo{Owner: "Vonksdesu", Name: "ZZZ-Mod-Fixer"}
	tagURL := "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/git/tags/deadbeef"
	client := newTestClient(t, func(request *http.Request) (int, string) {
		switch request.URL.String() {
		case "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/git/ref/tags/v1":
			return http.StatusOK, `{"object":{"type":"tag","sha":"deadbeef","url":"` + tagURL + `"}}`
		case tagURL:
			return http.StatusOK, `{"object":{"type":"commit","sha":"` + commit + `"}}`
		default:
			t.Errorf("unexpected request: %s", request.URL)
			return http.StatusNotFound, ""
		}
	})

	got, err := client.ResolveTagCommit(context.Background(), repo, "v1")
	if err != nil || got != commit {
		t.Fatalf("commit = %q, %v", got, err)
	}
}

func TestResolveTagCommitRejectsForeignTagURL(t *testing.T) {
	client := newTestClient(t, func(request *http.Request) (int, string) {
		return http.StatusOK, `{"object":{"type":"tag","url":"https://api.github.com/repos/other/repo/git/tags/x"}}`
	})
	_, err := client.ResolveTagCommit(context.Background(), Repo{Owner: "Vonksdesu", Name: "ZZZ-Mod-Fixer"}, "v1")
	if err == nil || !strings.Contains(err.Error(), "unsafe annotated tag URL") {
		t.Fatalf("err = %v", err)
	}
}

func TestTreeRejectsTruncatedTrees(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	client := newTestClient(t, func(request *http.Request) (int, string) {
		if request.URL.String() != "https://api.github.com/repos/a/b/git/trees/"+commit+"?recursive=1" {
			t.Errorf("URL = %s", request.URL)
		}
		return http.StatusOK, `{"truncated":true,"tree":[]}`
	})
	if _, err := client.Tree(context.Background(), Repo{Owner: "a", Name: "b"}, commit); !errors.Is(
		err,
		ErrTreeTruncated,
	) {
		t.Fatalf("err = %v", err)
	}
	if _, err := client.Tree(context.Background(), Repo{Owner: "a", Name: "b"}, "../x"); err == nil {
		t.Fatal("invalid commit accepted")
	}
}
