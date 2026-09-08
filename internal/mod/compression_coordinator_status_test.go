package mod

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"nahida.live/desktop/internal/setting"
)

func TestStartCompressionOmitsBusyStatusWithoutWork(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		forbidden string
	}{
		{name: "disabled restore with unowned files", enabled: false, forbidden: "decompressing"},
		{name: "enabled compress", enabled: true, forbidden: "compressing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			settings, err := setting.Open(ctx, filepath.Join(t.TempDir(), "compression.db"))
			if err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			mods := filepath.Join(root, "Mods")
			if err := os.MkdirAll(mods, 0o755); err != nil {
				t.Fatal(err)
			}
			if !test.enabled {
				if err := os.WriteFile(filepath.Join(mods, "payload.bin"), []byte("unowned"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if test.enabled {
				if err := settings.SetCompressionEnabled(ctx, true); err != nil {
					t.Fatal(err)
				}
			}
			emit, statuses := compressionStatusCollector()
			m := NewWithOptions(Options{
				Settings:  settings,
				XXMI:      compressionImporterSource{{Key: "A", ImporterFolder: root}},
				EventEmit: emit,
			})
			m.UseClient(settings.Client())
			t.Cleanup(func() {
				_ = m.ServiceShutdown()
				_ = settings.Close()
			})
			if err := m.StartCompression(ctx); err != nil {
				t.Fatal(err)
			}
			waitForCompression(t, m.compression)
			got := statuses()
			for _, status := range got {
				if status == test.forbidden {
					t.Fatalf("published statuses = %v", got)
				}
			}
			if state := m.compression.snapshot(); state.Status != "idle" || state.Enabled != test.enabled {
				t.Fatalf("state = %+v", state)
			}
		})
	}
}

func TestStartCompressionPublishesTargetRestoreBeforeCompress(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	importer := filepath.Join(base, "Importer")
	enabled := filepath.Join(importer, "Mods", "Sample")
	disabled := filepath.Join(importer, "Mods", "DISABLED Sample")
	if err := os.MkdirAll(enabled, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(disabled, 0o755); err != nil {
		t.Fatal(err)
	}
	restorePath := filepath.Join(enabled, "restore.bin")
	compressPath := filepath.Join(disabled, "compress.bin")
	restoreData := bytes.Repeat([]byte("restore-me"), 1024)
	if err := os.WriteFile(restorePath, restoreData, 0o644); err != nil {
		t.Fatal(err)
	}
	compressData := bytes.Repeat([]byte("compress-me"), 128*1024)
	if err := os.WriteFile(compressPath, compressData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(ctx, restorePath, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionConfig(ctx, "zstd", 1); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	emit, statuses := compressionStatusCollector()
	m := NewWithOptions(Options{
		Settings:  settings,
		XXMI:      compressionImporterSource{{Key: "A", ImporterFolder: importer}},
		EventEmit: emit,
	})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})
	if err := m.StartCompression(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	got := statuses()
	restoreIndex, compressIndex := -1, -1
	for index, status := range got {
		if restoreIndex < 0 && status == "decompressing" {
			restoreIndex = index
		}
		if compressIndex < 0 && status == "compressing" {
			compressIndex = index
		}
		if restoreIndex >= 0 && compressIndex >= 0 {
			break
		}
	}
	if restoreIndex < 0 || compressIndex < 0 || restoreIndex >= compressIndex {
		t.Fatalf("published statuses = %v", got)
	}
	restored, err := os.ReadFile(restorePath)
	if err != nil || !bytes.Equal(restored, restoreData) {
		t.Fatalf("restored %s: %v", restorePath, err)
	}
	if _, err := os.Stat(restorePath + managedZstdExtension); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("archive remains")
	}
	if _, err := os.Stat(compressPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("compress input was not archived")
	}
	if _, err := os.Stat(compressPath + managedZstdExtension); err != nil {
		t.Fatalf("compress archive missing: %v", err)
	}
}

func TestStartCompressionPublishesDecompressingForLeftoverZstd(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	settings, err := setting.Open(ctx, filepath.Join(base, "compression.db"))
	if err != nil {
		t.Fatal(err)
	}
	importer := filepath.Join(base, "Importer")
	root := filepath.Join(importer, "Mods")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("leftover-zstd-restore"), 32*1024)
	path := filepath.Join(root, "payload.bin")
	if err := os.WriteFile(path, want, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := compressZstdFile(ctx, path, ignoreCompressionMutations); err != nil {
		t.Fatal(err)
	}
	if err := settings.SetCompressionConfig(ctx, "zstd", 1); err != nil {
		t.Fatal(err)
	}
	emit, statuses := compressionStatusCollector()
	m := NewWithOptions(Options{
		Settings:  settings,
		XXMI:      compressionImporterSource{{Key: "A", ImporterFolder: importer}},
		EventEmit: emit,
	})
	m.UseClient(settings.Client())
	t.Cleanup(func() {
		_ = m.ServiceShutdown()
		_ = settings.Close()
	})
	if err := m.StartCompression(ctx); err != nil {
		t.Fatal(err)
	}
	waitForCompression(t, m.compression)
	got := statuses()
	found := false
	for _, status := range got {
		if status == "decompressing" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("published statuses = %v, want decompressing", got)
	}
	restored, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(restored, want) {
		t.Fatalf("restored %s: %v", path, err)
	}
	if _, err := os.Stat(path + managedZstdExtension); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("archive remains")
	}
}

func TestAddWorkTotalsSetsBusyStatusOnlyWhenWorkExists(t *testing.T) {
	tests := []struct {
		name   string
		add    func(*compressionCoordinator, int, int64)
		status string
		files  int
		bytes  int64
	}{
		{name: "compress files", add: (*compressionCoordinator).addCompressTotals, status: "compressing", files: 2, bytes: 40},
		{name: "compress bytes only", add: (*compressionCoordinator).addCompressTotals, status: "compressing"},
		{name: "restore files", add: (*compressionCoordinator).addRestoreTotals, status: "decompressing", files: 2, bytes: 40},
		{name: "restore bytes only", add: (*compressionCoordinator).addRestoreTotals, status: "decompressing"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coordinator := newCompressionCoordinator(New())
			coordinator.state.Status = "checking"
			test.add(coordinator, 0, 0)
			if state := coordinator.snapshot(); state.Status != "checking" || state.TotalFiles != 0 {
				t.Fatalf("empty totals = %+v", state)
			}
			files, bytes := test.files, test.bytes
			if files == 0 && bytes == 0 {
				bytes = 40
			}
			test.add(coordinator, files, bytes)
			if state := coordinator.snapshot(); state.Status != test.status || state.TotalFiles != files || state.TotalBytes != bytes {
				t.Fatalf("work totals = %+v", state)
			}
		})
	}
}

func compressionStatusCollector() (func(string, ...any), func() []string) {
	var mu sync.Mutex
	var statuses []string
	return func(name string, data ...any) {
			if name == compressionEvent {
				mu.Lock()
				statuses = append(statuses, data[0].(CompressionState).Status)
				mu.Unlock()
			}
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), statuses...)
		}
}
