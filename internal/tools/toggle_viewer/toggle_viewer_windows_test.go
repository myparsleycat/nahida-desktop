//go:build windows

package toggleviewer

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestWriteArtifactKeepsFileOpenWithoutDeleteSharing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, artifactININame)
	writeTestFile(t, path, "old")
	openWithoutDeleteSharing(t, path)

	if err := writeArtifactIfChanged(path, "new"); err == nil {
		t.Fatal("replacement succeeded while the game holds the artifact")
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "old" {
		t.Fatalf("content = %q, %v", content, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries = %v, %v", entries, err)
	}
}

func TestScanRetriesStaleCleanupWhileArtifactIsLocked(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	client := openTestDB(t)
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(client)
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	if err := os.Remove(filepath.Join(modRoot, "mod.ini")); err != nil {
		t.Fatal(err)
	}
	openWithoutDeleteSharing(t, filepath.Join(modRoot, artifactININame))
	retry, err := service.scanAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !retry {
		t.Fatal("locked stale artifact did not request a retry")
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 1 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestBatchDeleteKeepsRecordsWhileMovedArtifactIsUnreadable(t *testing.T) {
	ctx := context.Background()
	service, client, moved := generateAndMoveMod(t)
	var unlocks []func()
	for _, name := range []string{artifactININame, artifactTXTName} {
		unlocks = append(unlocks, lockFile(t, filepath.Join(moved, name), 0))
	}

	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 1 {
		t.Fatalf("records while locked = %#v, %v", records, err)
	}

	for _, unlock := range unlocks {
		unlock()
	}
	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(moved, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains after unlock: %v", name, err)
		}
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 0 {
		t.Fatalf("records after unlock = %#v, %v", records, err)
	}
}

func TestDeferredRenameKeepsArtifactsTracked(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	client := openTestDB(t)
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(client)
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	// The key list names the source INI, so the rename must replace the locked TXT.
	unlock := openWithoutDeleteSharing(t, filepath.Join(modRoot, artifactTXTName))
	if err := os.Rename(filepath.Join(modRoot, "mod.ini"), filepath.Join(modRoot, "renamed.ini")); err != nil {
		t.Fatal(err)
	}
	retry, err := service.scanAll(ctx)
	if err != nil || !retry {
		t.Fatalf("deferred scan = %v, %v", retry, err)
	}
	unlock()

	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(modRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains after batch delete: %v", name, err)
		}
	}
}

func TestUnreadableSourceKeepsArtifacts(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	client := openTestDB(t)
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(client)
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	lockFile(t, filepath.Join(modRoot, "mod.ini"), 0)
	retry, err := service.scanAll(ctx)
	if err != nil || !retry {
		t.Fatalf("scan with unreadable source = %v, %v", retry, err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(modRoot, name)); err != nil {
			t.Fatalf("%s removed while its source was unreadable: %v", name, err)
		}
	}
	if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 1 {
		t.Fatalf("records = %#v, %v", records, err)
	}
}

func TestPartialArtifactWriteIsTracked(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	writeTestFile(t, filepath.Join(modRoot, artifactTXTName), "untracked")
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(openTestDB(t))
	t.Cleanup(func() { _ = service.Shutdown() })

	unlock := openWithoutDeleteSharing(t, filepath.Join(modRoot, artifactTXTName))
	retry, err := service.scanAll(ctx)
	if err != nil || !retry {
		t.Fatalf("scan with locked TXT = %v, %v", retry, err)
	}
	if _, err := os.Stat(filepath.Join(modRoot, artifactININame)); err != nil {
		t.Fatalf("INI beside the locked TXT was not written: %v", err)
	}
	unlock()

	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(modRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains after batch delete: %v", name, err)
		}
	}
}

// A TXT that could not be replaced keeps its old content, so its record must keep identifying that
// content for a batch delete after the folder moves.
func TestBatchDeleteRemovesKeptArtifactOfMovedFolder(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(openTestDB(t))
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), strings.Replace(cycleModINI, "ctrl H", "alt K", 1))
	unlock := openWithoutDeleteSharing(t, filepath.Join(modRoot, artifactTXTName))
	retry, err := service.scanAll(ctx)
	if err != nil || !retry {
		t.Fatalf("scan with locked TXT = %v, %v", retry, err)
	}
	unlock()

	moved := filepath.Join(importer, "Mods", "DISABLED Character")
	if err := os.Rename(modRoot, moved); err != nil {
		t.Fatal(err)
	}
	if err := service.RunBatchDelete(ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{artifactININame, artifactTXTName} {
		if _, err := os.Stat(filepath.Join(moved, name)); !os.IsNotExist(err) {
			t.Fatalf("%s remains after batch delete: %v", name, err)
		}
	}
}

// Moved folders whose generated INIs are identical cannot be told apart by hash, so rewriting one
// must not move the record of a locked twin away from the content the twin still holds, whether the
// twin could still be read or not.
func TestApplyHotkeyKeepsLockedTwinOfMovedFolderTracked(t *testing.T) {
	for lock, share := range map[string]uint32{
		"replace blocked": windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE,
		"read blocked":    0,
	} {
		t.Run(lock, func(t *testing.T) {
			ctx := context.Background()
			importer := t.TempDir()
			mods := filepath.Join(importer, "Mods")
			for _, name := range []string{"A", "B"} {
				writeTestFile(t, filepath.Join(mods, name, "mod.ini"), cycleModINI)
			}
			client := openTestDB(t)
			service := NewWithOptions(Options{
				Settings:  &fakeSettings{},
				Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
			})
			service.UseClient(client)
			t.Cleanup(func() { _ = service.Shutdown() })
			if err := service.RunBatchGenerate(ctx); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"A", "B"} {
				if err := os.Rename(filepath.Join(mods, name), filepath.Join(mods, "DISABLED "+name)); err != nil {
					t.Fatal(err)
				}
			}

			lockedINI := filepath.Join(mods, "DISABLED B", artifactININame)
			unlock := lockFile(t, lockedINI, share)
			if err := service.ApplyHotkey(ctx, "alt J"); err != nil {
				t.Fatal(err)
			}
			unlock()
			if err := service.ApplyHotkey(ctx, "alt J"); err != nil {
				t.Fatal(err)
			}
			if content, err := os.ReadFile(lockedINI); err != nil || !strings.Contains(string(content), "key = alt J") {
				t.Fatalf("released twin = %q, %v", content, err)
			}

			if err := service.RunBatchDelete(ctx); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"A", "B"} {
				if _, err := os.Stat(filepath.Join(mods, "DISABLED "+name, artifactININame)); !os.IsNotExist(err) {
					t.Fatalf("artifact of %s remains: %v", name, err)
				}
			}
			if records, err := client.ToggleViewerArtifacts.List(ctx); err != nil || len(records) != 0 {
				t.Fatalf("records = %#v, %v", records, err)
			}
		})
	}
}

func TestHotkeyRetriesLockedArtifactWithoutWatcher(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	modRoot := filepath.Join(importer, "Mods", "Character")
	writeTestFile(t, filepath.Join(modRoot, "mod.ini"), cycleModINI)
	settings := &fakeSettings{hotkey: "ctrl H"}
	service := NewWithOptions(Options{
		Settings:  settings,
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.retryDelay = 10 * time.Millisecond
	service.UseClient(openTestDB(t))
	t.Cleanup(func() { _ = service.Shutdown() })
	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}

	toggleINIPath := filepath.Join(finalPath(t, modRoot), artifactININame)
	unlock := openWithoutDeleteSharing(t, toggleINIPath)
	settings.setHotkey("alt J")
	if err := service.ApplyHotkey(ctx, "alt J"); err != nil {
		t.Fatal(err)
	}
	unlock()

	deadline := time.Now().Add(5 * time.Second)
	for {
		content, err := os.ReadFile(toggleINIPath)
		if err == nil && strings.Contains(string(content), "key = alt J") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("hotkey was not retried after the game released the artifact: %q, %v", content, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBatchGenerateFollowsLinkedModsFolder(t *testing.T) {
	ctx := context.Background()
	importer := t.TempDir()
	library := t.TempDir()
	writeTestFile(t, filepath.Join(library, "Character", "mod.ini"), cycleModINI)
	link := filepath.Join(importer, "Mods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, library).CombinedOutput(); err != nil {
		t.Fatalf("create junction: %v (%s)", err, output)
	}
	service := NewWithOptions(Options{
		Settings:  &fakeSettings{},
		Importers: fakeImporters{{Key: "GIMI", ImporterFolder: importer}},
	})
	service.UseClient(openTestDB(t))
	t.Cleanup(func() { _ = service.Shutdown() })

	if err := service.RunBatchGenerate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(library, "Character", artifactININame)); err != nil {
		t.Fatalf("artifact in linked mods folder: %v", err)
	}
}

// openWithoutDeleteSharing opens path the way XXMI holds loaded INI files until the returned
// function or the test cleanup closes it.
func openWithoutDeleteSharing(t *testing.T, path string) func() {
	t.Helper()
	return lockFile(t, path, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE)
}

func lockFile(t *testing.T, path string, share uint32) func() {
	t.Helper()
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ,
		share,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	closeHandle := func() {
		once.Do(func() {
			if err := windows.CloseHandle(handle); err != nil {
				t.Errorf("close shared file: %v", err)
			}
		})
	}
	t.Cleanup(closeHandle)
	return closeHandle
}
