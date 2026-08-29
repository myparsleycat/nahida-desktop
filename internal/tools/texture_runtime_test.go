package tools

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
)

type textureRoundTripFunc func(*http.Request) (*http.Response, error)

func (f textureRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type textureBlockingBody struct{ ctx context.Context }

func (b textureBlockingBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (textureBlockingBody) Close() error { return nil }

func TestInstallTextureRuntimeVerifiesAndAtomicallyPromotes(t *testing.T) {
	t.Parallel()
	archive := buildTextureRuntimeArchive(t, map[string]string{
		"package/upscaler.exe":     "executable",
		"package/runtime.dll":      "sidecar",
		"package/models/a.param":   "parameters",
		"package/models/a.bin":     "weights",
		"package/unrelated/readme": "ignored",
	})
	sum := sha256.Sum256(archive)
	spec := textureRuntimeSpec{
		dirName: "test-upscaler", binaryName: "upscaler.exe", version: "1.2.3",
		settingPrefix: "test:upscaler", displayName: "Test Upscaler",
		downloadURL:   "https://downloads.example.test/upscaler.zip",
		archiveSHA256: hex.EncodeToString(sum[:]), modelsRelative: "models",
		modelDirNames: []string{"models"}, requiredModels: []string{"a"},
	}
	transport := textureRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != spec.downloadURL {
			return nil, fmt.Errorf("unexpected request: %s", request.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Length": []string{fmt.Sprint(len(archive))}},
			Body:       io.NopCloser(bytes.NewReader(archive)),
		}, nil
	})
	httpClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: &http.Client{Transport: transport}})
	download := infra.NewDownload()
	download.UseClient(httpClient)
	client := openToolsTestDB(t)
	service := NewWithOptions(Options{Download: download, Archive: infra.NewArchive()})
	service.UseClient(client)
	userData := useToolsTestAppData(t, service, t.TempDir())
	target := filepath.Join(userData, "tools", spec.dirName)
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	var phases []string
	var downloadPercents []*float64
	status, err := service.installTextureRuntime(context.Background(), spec, func(phase string, percent *float64) {
		phases = append(phases, phase)
		if phase == "download" {
			downloadPercents = append(downloadPercents, percent)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Installed || status.Version == nil || *status.Version != spec.version {
		t.Fatalf("status = %#v", status)
	}
	if got, err := os.ReadFile(filepath.Join(target, spec.binaryName)); err != nil || string(got) != "executable" {
		t.Fatalf("installed binary = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(target, "runtime.dll")); err != nil || string(got) != "sidecar" {
		t.Fatalf("installed DLL = %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); !os.IsNotExist(err) {
		t.Fatalf("old runtime survived promotion: %v", err)
	}
	if len(phases) < 3 || phases[0] != "download" || phases[len(phases)-1] != "extract" {
		t.Fatalf("progress phases = %v", phases)
	}
	for i, percent := range downloadPercents {
		if percent == nil {
			t.Fatalf("download percentage %d is nil despite Content-Length", i)
		}
	}
	if len(downloadPercents) < 2 || *downloadPercents[len(downloadPercents)-1] != 100 {
		t.Fatalf("download percentages = %v", downloadPercents)
	}
	stored, err := client.Settings.GetValue(context.Background(), spec.settingPrefix+":binary-path")
	if err != nil || stored == nil || *stored != filepath.Join(target, spec.binaryName) {
		t.Fatalf("stored binary path = %v, %v", stored, err)
	}
}

func TestDownloadTextureRuntimeArchiveAbortsStalledBody(t *testing.T) {
	t.Parallel()
	spec := textureRuntimeSpec{
		displayName: "Stalled Upscaler",
		downloadURL: "https://downloads.example.test/stalled.zip",
	}
	transport := textureRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode:    http.StatusOK,
			Header:        http.Header{"Content-Length": []string{"128"}},
			ContentLength: 128,
			Body:          textureBlockingBody{ctx: request.Context()},
		}, nil
	})
	download := infra.NewDownload()
	download.UseClient(infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: &http.Client{Transport: transport}}))
	service := NewWithOptions(Options{Download: download})
	err := service.downloadTextureRuntimeArchive(
		context.Background(), spec, filepath.Join(t.TempDir(), "runtime.zip"), nil, 2*time.Second, 50*time.Millisecond,
	)
	if err == nil || !strings.Contains(err.Error(), "stalled") {
		t.Fatalf("stall error = %v", err)
	}
}

func TestDownloadTextureRuntimeArchiveHasOverallTimeout(t *testing.T) {
	t.Parallel()
	spec := textureRuntimeSpec{
		displayName: "Slow Upscaler",
		downloadURL: "https://downloads.example.test/slow.zip",
	}
	transport := textureRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	download := infra.NewDownload()
	download.UseClient(infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: &http.Client{Transport: transport}}))
	service := NewWithOptions(Options{Download: download})
	err := service.downloadTextureRuntimeArchive(
		context.Background(), spec, filepath.Join(t.TempDir(), "runtime.zip"), nil, 50*time.Millisecond, time.Second,
	)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestInstallTextureRuntimeRejectsChecksumMismatch(t *testing.T) {
	t.Parallel()
	payload := []byte("not a zip")
	spec := textureRuntimeSpec{
		dirName: "bad-upscaler", binaryName: "bad.exe", displayName: "Bad Upscaler",
		downloadURL: "https://downloads.example.test/bad.zip", archiveSHA256: "00",
	}
	transport := textureRoundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(payload))}, nil
	})
	httpClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: &http.Client{Transport: transport}})
	download := infra.NewDownload()
	download.UseClient(httpClient)
	service := NewWithOptions(Options{Download: download, Archive: infra.NewArchive()})
	service.UseClient(openToolsTestDB(t))
	useToolsTestAppData(t, service, t.TempDir())
	if _, err := service.installTextureRuntime(context.Background(), spec, nil); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("checksum error = %v", err)
	}
}

func buildTextureRuntimeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func TestRealcuganRuntimeRequiresExecutableAndAllModelFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	binaryPath := filepath.Join(root, realcuganSpec.binaryName)
	if err := os.WriteFile(binaryPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if isRealcuganRuntimeInstalled(binaryPath, root) {
		t.Fatal("binary alone should not count as installed")
	}
	for _, model := range realcuganSpec.requiredModels {
		path := filepath.Join(root, filepath.FromSlash(model))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".param", nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path+".bin", nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !isRealcuganRuntimeInstalled(binaryPath, root) {
		t.Fatal("expected installed after all model files exist")
	}
	if isRealcuganRuntimeInstalled(filepath.Join(root, "missing.exe"), root) {
		t.Fatal("missing executable should not count as installed")
	}
}
