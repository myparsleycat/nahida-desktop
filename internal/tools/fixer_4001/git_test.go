package fixer4001

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

func TestLocateGitInstallsPinnedMinGitOnce(t *testing.T) {
	// A Git on PATH must not be picked up: the build uses the pinned release on every machine.
	systemGit := t.TempDir()
	if err := os.WriteFile(filepath.Join(systemGit, "git.exe"), []byte("system"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", systemGit)
	archive := minGitTestArchive(t, map[string]string{"cmd/git.exe": "portable", "etc/gitconfig": "[core]"})
	service, release, toolsRoot, requests, codes := newMinGitTestService(t, archive)
	stale := filepath.Join(toolsRoot, minGitDirPrefix+"0.0.1")
	broken := filepath.Join(toolsRoot, minGitDirPrefix+release.version, "etc")
	for _, dir := range []string{stale, broken} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	want := filepath.Join(toolsRoot, minGitDirPrefix+release.version, "cmd", "git.exe")
	for range 2 {
		got, err := service.locateGit(context.Background(), release)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("locateGit() = %q, want %q", got, want)
		}
	}
	if contents, err := os.ReadFile(want); err != nil || string(contents) != "portable" {
		t.Fatalf("installed git.exe = %q, %v", contents, err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("download requests = %d, want 1", got)
	}
	if len(*codes) != 1 || (*codes)[0] != "XXMI_DOWNLOAD_GIT" {
		t.Fatalf("progress codes = %v", *codes)
	}
	if entries := minGitTestEntries(t, toolsRoot); len(entries) != 1 {
		t.Fatalf("tools directory = %v, want only the pinned release", entries)
	}
}

func TestLocateGitRejectsUnexpectedArchive(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		entries map[string]string
		sha256  string
		want    string
	}{
		{
			name:    "checksum mismatch",
			entries: map[string]string{"cmd/git.exe": "tampered"},
			sha256:  strings.Repeat("0", 64),
			want:    "checksum mismatch",
		},
		{name: "missing executable", entries: map[string]string{"etc/gitconfig": "[core]"}, want: "missing cmd/git.exe"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			service, release, toolsRoot, _, _ := newMinGitTestService(t, minGitTestArchive(t, tc.entries))
			if tc.sha256 != "" {
				release.sha256 = tc.sha256
			}

			_, err := service.locateGit(context.Background(), release)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("locateGit() error = %v", err)
			}
			if entries := minGitTestEntries(t, toolsRoot); len(entries) != 0 {
				t.Fatalf("rejected archive left files behind: %v", entries)
			}
		})
	}
}

func newMinGitTestService(
	t *testing.T,
	archive []byte,
) (service *Service, release minGitRelease, toolsRoot string, requests *atomic.Int32, codes *[]string) {
	t.Helper()
	requests = &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)

	retryLimit := 0
	download := infra.NewDownload()
	download.UseClient(infra.NewClientWithOptions(infra.ClientOptions{
		HTTPClient: server.Client(), RetryLimit: &retryLimit,
	}))
	data, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	toolsRoot, err = data.EnsureDir(appdata.ToolsDir)
	if err != nil {
		t.Fatal(err)
	}

	codes = &[]string{}
	service = NewWithOptions(Options{
		Download: download,
		Archive:  infra.NewArchive(),
		EventEmit: func(_ string, payload ...any) {
			if event, ok := payload[0].(Fixer4001ProgressEvent); ok {
				*codes = append(*codes, event.Code)
			}
		},
	})
	service.UseAppData(data)
	sum := sha256.Sum256(archive)
	return service, minGitRelease{
		version: "9.9.9",
		url:     server.URL + "/MinGit.zip",
		sha256:  hex.EncodeToString(sum[:]),
	}, toolsRoot, requests, codes
}

func minGitTestArchive(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, contents := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func minGitTestEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
