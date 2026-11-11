package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
)

// releaseServer answers the GitHub API and release downloads in process, without a network.
type releaseServer struct {
	mu       sync.Mutex
	releases []github.Release
	files    map[string]string
}

func (s *releaseServer) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	s.mu.Lock()
	switch {
	case request.URL.Host == "api.github.com" && request.URL.Path == "/repos/myparsleycat/nahida-desktop/releases":
		_ = json.NewEncoder(recorder).Encode(s.releases)
	case request.URL.Host == "github.com":
		tag, name := path.Split(strings.TrimPrefix(request.URL.Path, "/myparsleycat/nahida-desktop/releases/download/"))
		body, found := s.files[tag+name]
		if !found {
			recorder.WriteHeader(http.StatusNotFound)
		}
		_, _ = recorder.WriteString(body)
	default:
		recorder.WriteHeader(http.StatusNotFound)
	}
	s.mu.Unlock()
	response := recorder.Result()
	response.Request = request
	return response, nil
}

func (s *releaseServer) setFile(tag, name, body string) {
	s.mu.Lock()
	s.files[tag+"/"+name] = body
	s.mu.Unlock()
}

func newTestEngine(t *testing.T, version string, server *releaseServer) (*Engine, string) {
	t.Helper()
	noRetries := 0
	client := github.New(github.Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: &http.Client{Transport: server}, Status: infra.BackendOnline, RetryLimit: &noRetries,
	})})
	engine, err := New(Options{Version: version, GitHub: client, Quit: func() { t.Error("unexpected quit") }})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "nahida-desktop.exe")
	if err := os.WriteFile(target, []byte("running build"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.executable = func() (string, error) { return target, nil }
	return engine, target
}

func testRelease(tag string, prerelease bool, assets ...string) github.Release {
	release := github.Release{TagName: tag, Prerelease: prerelease, Body: "notes for " + tag}
	for _, name := range assets {
		release.Assets = append(release.Assets, github.Asset{Name: name})
	}
	return release
}

func checksumLine(body, name string) string {
	digest := sha256.Sum256([]byte(body))
	return hex.EncodeToString(digest[:]) + "  " + name + "\r\n"
}

func TestCheckSelectsNewestInstallableRelease(t *testing.T) {
	t.Parallel()
	server := &releaseServer{
		releases: []github.Release{
			testRelease("v3.47.0-beta.1", true, assetName, checksumAsset),
			testRelease("v3.46.0", false, checksumAsset, assetName),
			testRelease("v3.45.0", false, assetName, checksumAsset),
		},
		files: map[string]string{},
	}
	for _, tag := range []string{"v3.47.0-beta.1", "v3.46.0", "v3.45.0"} {
		server.setFile(tag, checksumAsset, checksumLine("installer", "nahida-desktop-windows-amd64-installer.exe")+
			checksumLine(tag, assetName))
	}

	for _, test := range []struct {
		name       string
		version    string
		prerelease bool
		want       *infra.UpdateRelease
	}{
		{
			name: "stable", version: "3.45.0",
			want: &infra.UpdateRelease{Version: "3.46.0", Notes: "notes for v3.46.0"},
		},
		{
			name: "prerelease", version: "3.45.0", prerelease: true,
			want: &infra.UpdateRelease{
				Version: "3.47.0-beta.1", Channel: infra.UpdateChannelPrerelease, Notes: "notes for v3.47.0-beta.1",
			},
		},
		{
			name: "stable after its prerelease", version: "3.46.0-beta.2",
			want: &infra.UpdateRelease{Version: "3.46.0", Notes: "notes for v3.46.0"},
		},
		{name: "up to date", version: "3.46.0"},
		{name: "prerelease up to date", version: "3.47.0-beta.1", prerelease: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			engine, _ := newTestEngine(t, test.version, server)
			got, err := engine.Check(t.Context(), test.prerelease)
			if err != nil {
				t.Fatal(err)
			}
			if (got == nil) != (test.want == nil) || got != nil && *got != *test.want {
				t.Fatalf("release = %+v, want %+v", got, test.want)
			}
			if test.want == nil {
				if engine.pending != nil {
					t.Fatalf("up-to-date check kept pending release %+v", engine.pending)
				}
				return
			}
			want := sha256.Sum256([]byte("v" + test.want.Version))
			if engine.pending == nil || engine.pending.tag != "v"+test.want.Version ||
				string(engine.pending.digest) != string(want[:]) {
				t.Fatalf("pending = %+v", engine.pending)
			}
		})
	}
}

func TestCheckRejectsReleaseWithoutChecksumLine(t *testing.T) {
	t.Parallel()
	server := &releaseServer{
		releases: []github.Release{testRelease("v3.46.0", false, assetName, checksumAsset)},
		files:    map[string]string{},
	}
	server.setFile("v3.46.0", checksumAsset, checksumLine("installer", "nahida-desktop-windows-amd64-installer.exe"))
	engine, _ := newTestEngine(t, "3.45.0", server)

	release, err := engine.Check(t.Context(), false)
	if err == nil || release != nil || engine.pending != nil {
		t.Fatalf("release = %+v, err = %v, pending = %+v", release, err, engine.pending)
	}
}

func TestSelectReleaseSkipsIncompleteReleases(t *testing.T) {
	t.Parallel()
	releases := []github.Release{
		testRelease("nightly", false, assetName, checksumAsset),
		testRelease("v3.46.1", false, checksumAsset),
		testRelease("v3.46.0", false, assetName),
		{TagName: "v3.46.2", Draft: true, Assets: []github.Asset{{Name: assetName}, {Name: checksumAsset}}},
		testRelease("v3.45.1", false, assetName, checksumAsset),
	}
	release, found := selectRelease(releases, "3.45.0", true)
	if !found || release.TagName != "v3.45.1" {
		t.Fatalf("selected %q (found %t), want v3.45.1", release.TagName, found)
	}
}

func TestDownloadAndInstallKeepsOnlyVerifiedBuilds(t *testing.T) {
	t.Parallel()
	server := &releaseServer{
		releases: []github.Release{testRelease("v3.46.0", false, assetName, checksumAsset)},
		files:    map[string]string{},
	}
	server.setFile("v3.46.0", checksumAsset, checksumLine("new build", assetName))
	server.setFile("v3.46.0", assetName, "new build")
	engine, target := newTestEngine(t, "3.45.0", server)
	stale := filepath.Join(filepath.Dir(target), stagingPrefix+"stale")
	if err := os.Mkdir(stale, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Check(t.Context(), false); err != nil {
		t.Fatal(err)
	}

	if err := engine.DownloadAndInstall(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	staged := engine.staged
	if content, err := os.ReadFile(staged); err != nil || string(content) != "new build" {
		t.Fatalf("staged %s = %q, %v", staged, content, err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale staging directory survived: %v", err)
	}

	// A tampered download is discarded and leaves the verified build staged.
	server.setFile("v3.46.0", assetName, "tampered build")
	if err := engine.DownloadAndInstall(t.Context(), nil); !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("tampered download error = %v", err)
	}
	if engine.staged != staged {
		t.Fatalf("staged = %s, want %s", engine.staged, staged)
	}
	if content, err := os.ReadFile(staged); err != nil || string(content) != "new build" {
		t.Fatalf("verified build after tampered download = %q, %v", content, err)
	}
	if dirs := stagingDirs(t, filepath.Dir(target)); len(dirs) != 1 || dirs[0] != filepath.Dir(staged) {
		t.Fatalf("staging directories = %v", dirs)
	}
}

func stagingDirs(t *testing.T, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), stagingPrefix) {
			dirs = append(dirs, filepath.Join(parent, entry.Name()))
		}
	}
	return dirs
}

type fakeHelper struct{ aborted, detached bool }

func (h *fakeHelper) Abort() { h.aborted = true }

func (h *fakeHelper) Detach() { h.detached = true }

func TestRestartQuitsOnlyAfterHelperIsReady(t *testing.T) {
	t.Parallel()
	engine, target := newTestEngine(t, "3.45.0", &releaseServer{files: map[string]string{}})
	dir := filepath.Join(filepath.Dir(target), stagingPrefix+"test")
	staged := filepath.Join(dir, assetName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(staged, []byte("new build"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine.pending = &pendingRelease{tag: "v3.46.0"}
	engine.staged, engine.stagingDir = staged, dir
	quits := 0
	engine.quit = func() { quits++ }

	helper := func(spawnErr error, status string) *fakeHelper {
		process := &fakeHelper{}
		engine.spawn = func(executable string, env []string) (helperProcess, error) {
			values := map[string]string{}
			for _, entry := range env {
				key, value, _ := strings.Cut(entry, "=")
				values[key] = value
			}
			if executable != target || values[envHelper] != "1" || values[envTarget] != target ||
				values[envStaged] != staged || values[envParentPID] == "" {
				t.Errorf("helper started as %s with %v", executable, values)
			}
			if spawnErr != nil {
				return nil, spawnErr
			}
			if err := os.WriteFile(values[envReady], []byte(status), 0o600); err != nil {
				t.Error(err)
			}
			return process, nil
		}
		return process
	}

	spawnErr := errors.New("job denied breakaway")
	helper(spawnErr, "")
	if err := engine.Restart(t.Context()); !errors.Is(err, spawnErr) || quits != 0 {
		t.Fatalf("failed spawn: err = %v, quits = %d", err, quits)
	}

	failed := helper(nil, "error: staged update is unavailable")
	if err := engine.Restart(t.Context()); !errors.Is(err, ErrHelperNotReady) || quits != 0 || !failed.aborted {
		t.Fatalf("failed helper: err = %v, quits = %d, aborted = %t", err, quits, failed.aborted)
	}

	ready := helper(nil, "ready")
	if err := engine.Restart(t.Context()); err != nil || quits != 1 || !ready.detached || ready.aborted {
		t.Fatalf("ready helper: err = %v, quits = %d, helper = %+v", err, quits, ready)
	}

	// The helper owns the staged build until this process exits.
	if err := engine.DownloadAndInstall(context.Background(), nil); !errors.Is(err, ErrRestartPending) {
		t.Fatalf("download during restart = %v", err)
	}
	if err := engine.Restart(t.Context()); !errors.Is(err, ErrRestartPending) || quits != 1 {
		t.Fatalf("second restart = %v, quits = %d", err, quits)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("staged build handed to the helper: %v", err)
	}
}
