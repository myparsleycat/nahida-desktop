package reshade

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/github"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func effectArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for name, content := range files {
		entry, err := w.Create("package/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(entry, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func effectService(t *testing.T, data []byte) *ReShade {
	t.Helper()
	return effectServiceWithTransport(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK, Header: make(http.Header), Request: r,
			Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)),
		}, nil
	}))
}

func effectServiceWithTransport(t *testing.T, transport http.RoundTripper) *ReShade {
	t.Helper()
	client := infra.NewClientWithOptions(infra.ClientOptions{
		Status:     infra.BackendOnline,
		HTTPClient: &http.Client{Transport: transport},
	})
	download := infra.NewDownload()
	download.UseClient(client)
	queue := transfer.New()
	if err := queue.ServiceStartup(t.Context(), application.ServiceOptions{}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = queue.ServiceShutdown() })
	return New(Options{
		GitHub: github.New(github.Options{HTTP: client, Download: download}), Download: download,
		Archive: infra.NewArchive(), Transfer: queue,
	})
}

func TestEffectTransferCanRestartAfterInterruption(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"pause", "cancel"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			layout := layout{root: t.TempDir()}
			if err := layout.ensureShared(); err != nil {
				t.Fatal(err)
			}
			data := effectArchive(t, map[string]string{"Shaders/a.fx": "shader", "Textures/a.png": "texture"})
			started := make(chan struct{})
			var attempts atomic.Int32
			service := effectServiceWithTransport(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if attempts.Add(1) == 1 {
					close(started)
					<-r.Context().Done()
					return nil, r.Context().Err()
				}
				return &http.Response{
					StatusCode: http.StatusOK, Header: make(http.Header), Request: r,
					Body: io.NopCloser(bytes.NewReader(data)), ContentLength: int64(len(data)),
				}, nil
			}))
			completed := make(chan struct{}, 1)
			service.eventEmit = func(name string, _ ...any) {
				if name == "reshade:status" {
					completed <- struct{}{}
				}
			}
			pkg := EffectPackage{
				ID: "https://github.com/owner/repo/archive/main.zip", Name: "Example",
				installPath: layout.shaders(), textureInstallPath: layout.textures(),
			}
			result := make(chan error, 1)
			ctx, cancel := context.WithCancel(t.Context())
			callerDone := make(chan struct{})
			go func() {
				defer close(callerDone)
				result <- service.installEffectPackage(ctx, layout, pkg)
			}()
			t.Cleanup(func() {
				cancel()
				<-callerDone
			})
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("download did not start")
			}
			items := service.transfer.List()
			if len(items) != 1 || items[0].Type != "download" {
				t.Fatalf("transfers = %+v", items)
			}
			pid := items[0].PID
			if action == "pause" {
				if err := service.transfer.Pause(pid); err != nil {
					t.Fatal(err)
				}
			} else if err := service.transfer.Cancel(pid); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("interrupted install = %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("interrupted caller did not return")
			}
			if _, err := os.Stat(filepath.Join(layout.shaders(), "a.fx")); !os.IsNotExist(err) {
				t.Fatalf("interrupted download installed a file: %v", err)
			}
			if action == "pause" {
				if err := service.transfer.Resume(pid); err != nil {
					t.Fatal(err)
				}
			} else if err := service.transfer.Retry(pid); err != nil {
				t.Fatal(err)
			}
			select {
			case <-completed:
			case <-time.After(5 * time.Second):
				t.Fatal("restarted transfer did not complete")
			}
			item, ok := service.transfer.Get(pid)
			if !ok || item.Status != transfer.StatusCompleted || item.TransferredSize != int64(len(data)) ||
				attempts.Load() != 2 {
				t.Fatalf("restarted transfer = %+v, attempts = %d", item, attempts.Load())
			}
			if got, err := os.ReadFile(filepath.Join(layout.shaders(), "a.fx")); err != nil || string(got) != "shader" {
				t.Fatalf("restarted shader = %q, %v", got, err)
			}
			records, err := readEffectRecords(layout.record())
			if err != nil || len(records[pkg.ID].Files) != 2 {
				t.Fatalf("restarted records = %+v, %v", records, err)
			}
		})
	}
}

func TestEffectReinstallPreservesFilesAndRecordOnFailure(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"copy", "record", "success"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			layout := layout{root: t.TempDir()}
			if err := layout.ensureShared(); err != nil {
				t.Fatal(err)
			}
			pkg := EffectPackage{
				ID: "https://github.com/owner/repo/archive/main.zip", Name: "Example",
				installPath: layout.shaders(), textureInstallPath: layout.textures(),
			}
			old := map[string]string{"a.fx": "old shader", "obsolete.fx": "old obsolete", "shared.fx": "shared shader"}
			for name, content := range old {
				if err := os.WriteFile(filepath.Join(layout.shaders(), name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			records := map[string]effectRecord{
				pkg.ID: {
					Name:        pkg.Name,
					InstalledAt: "original",
					Files:       []string{`Shaders\a.fx`, `Shaders\obsolete.fx`, `Shaders\shared.fx`},
				},
				"other": {Name: "Other", Files: []string{`Shaders\shared.fx`}},
			}
			if err := writeEffectRecords(layout.record(), records); err != nil {
				t.Fatal(err)
			}
			switch failure {
			case "copy":
				if err := os.Mkdir(filepath.Join(layout.shaders(), "z.fx"), 0o700); err != nil {
					t.Fatal(err)
				}
			case "record":
				path, err := windows.UTF16PtrFromString(layout.record())
				if err != nil {
					t.Fatal(err)
				}
				handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
					windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = windows.CloseHandle(handle) }()
			}
			service := effectService(t, effectArchive(t, map[string]string{
				"Shaders/a.fx": "new shader", "Shaders/new.fx": "new file", "Shaders/z.fx": "last file",
				"Textures/example.png": "texture",
			}))
			err := service.installEffectPackage(t.Context(), layout, pkg)
			if failure == "success" {
				if err != nil {
					t.Fatal(err)
				}
				for name, content := range map[string]string{"a.fx": "new shader", "new.fx": "new file", "shared.fx": "shared shader"} {
					if got, err := os.ReadFile(
						filepath.Join(layout.shaders(), name),
					); err != nil ||
						string(got) != content {
						t.Errorf("installed %s = %q, %v; want %q", name, got, err, content)
					}
				}
				if _, err := os.Stat(filepath.Join(layout.shaders(), "obsolete.fx")); !os.IsNotExist(err) {
					t.Errorf("obsolete file survived successful installation: %v", err)
				}
				got, err := readEffectRecords(layout.record())
				if err != nil || len(got[pkg.ID].Files) != 4 || !reflect.DeepEqual(got["other"], records["other"]) {
					t.Errorf("installed record = %+v, %v", got, err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected installation failure")
			}
			for name, content := range old {
				if got, err := os.ReadFile(
					filepath.Join(layout.shaders(), name),
				); err != nil ||
					string(got) != content {
					t.Errorf("original %s = %q, %v; want %q", name, got, err, content)
				}
			}
			if _, err := os.Stat(filepath.Join(layout.shaders(), "new.fx")); !os.IsNotExist(err) {
				t.Errorf("new file survived failed installation: %v", err)
			}
			if got, err := readEffectRecords(layout.record()); err != nil || !reflect.DeepEqual(got, records) {
				t.Errorf("record = %+v, %v; want %+v", got, err, records)
			}
		})
	}
}

func TestBinaryDownloadIsTrackedUntilCacheVerified(t *testing.T) {
	t.Parallel()
	setup := setupFile(t, map[string][]byte{moduleName: peImage(t, 0x8664, 0x2000)})
	data, err := os.ReadFile(setup)
	if err != nil {
		t.Fatal(err)
	}
	service := effectService(t, data)
	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	service.UseAppData(store)
	service.fileVersion = func(string) (string, error) { return "6.8.0", nil }
	for range 2 {
		cache, err := service.ensureVersion(t.Context(), "6.8.0")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := verifyCache(cache, "6.8.0"); err != nil {
			t.Fatal(err)
		}
	}
	items := service.transfer.List()
	if len(items) != 1 || items[0].Status != transfer.StatusCompleted || items[0].TransferredSize != int64(len(data)) {
		t.Fatalf("binary transfers = %+v", items)
	}
}

func TestEffectPackageWithOnlyShadersPreservesNestedFolders(t *testing.T) {
	t.Parallel()
	layout := layout{root: t.TempDir()}
	if err := layout.ensureShared(); err != nil {
		t.Fatal(err)
	}
	service := effectService(t, effectArchive(t, map[string]string{"Shaders/Nested/a.fx": "shader"}))
	pkg := EffectPackage{
		ID: "https://github.com/owner/repo/archive/main.zip", Name: "Shaders only",
		installPath: layout.shaders(), textureInstallPath: layout.textures(),
	}
	if err := service.installEffectPackage(t.Context(), layout, pkg); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(
		filepath.Join(layout.shaders(), "Nested", "a.fx"),
	); err != nil ||
		string(got) != "shader" {
		t.Fatalf("nested shader = %q, %v", got, err)
	}
	records, err := readEffectRecords(layout.record())
	if err != nil || !reflect.DeepEqual(records[pkg.ID].Files, []string{`Shaders\Nested\a.fx`}) {
		t.Fatalf("nested shader record = %+v, %v", records, err)
	}
}
