//go:build windows

package xxmi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestRepairLibsCacheRestoresCorruptCacheWhenDownloadFails(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "packages", "xxmi-libs", "1.0.0")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(folder, "Manifest.json")
	if err := os.WriteFile(manifest, []byte("corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New().repairLibsCache(context.Background(), "1.0.0"); err == nil {
		t.Fatal("expected download failure")
	}
	if data, err := os.ReadFile(manifest); err != nil || string(data) != "corrupt" {
		t.Fatalf("original cache was not restored: data = %q, err = %v", data, err)
	}
}

func TestRepairLegacyCacheRedownloadsRemoteRuntime(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	zipBody := writeXXMITestZip(t, map[string]string{
		"3dmigoto/3DMigoto Loader.exe": "loader",
		"3dmigoto/d3d11.dll":           "module",
	})
	digest := sha256.Sum256(zipBody)
	zipHash := hex.EncodeToString(digest[:])
	id := zipHash[:12]
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "packages", "legacy-3dmigoto", id)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal(LegacyRuntimeSource{
		URL: legacyRuntimeURL, ZipSHA256: zipHash,
		Files: map[string]string{
			"3DMigoto Loader.exe": hashBytes([]byte("loader")),
			"d3d11.dll":           hashBytes([]byte("module")),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"source.json": source, "3DMigoto Loader.exe": []byte("loader"), "d3d11.dll": []byte("corrupt"),
	} {
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != legacyRuntimeURL {
			t.Fatalf("unexpected download URL %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(zipBody)), Request: request}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	service := NewWithOptions(Options{HTTP: infraClient})
	cfg, warnings, err := service.repairLegacyCache(context.Background(), ImporterConfig{LegacyRuntime: id})
	if err != nil || cfg.LegacyRuntime != id || len(warnings) != 1 {
		t.Fatalf("repair: cfg = %+v, warnings = %v, err = %v", cfg, warnings, err)
	}
	assertFileContent(t, filepath.Join(folder, "d3d11.dll"), "module")
}

func TestRepairLegacyCacheRequestsOriginalLocalZIP(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	id := "abcdef123456"
	folder := filepath.Join(root, "packages", "legacy-3dmigoto", id)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	source, err := json.Marshal(LegacyRuntimeSource{
		ZipSHA256: strings.Repeat("a", 64),
		Files:     map[string]string{"d3d11.dll": strings.Repeat("b", 64)},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "source.json"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, err = New().repairLegacyCache(context.Background(), ImporterConfig{LegacyRuntime: id})
	if err == nil || !strings.Contains(err.Error(), "re-import the local legacy runtime ZIP") {
		t.Fatalf("repair error = %v", err)
	}
}
