//go:build windows

package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// crossVolumeRename fails like a rename between volumes, so one volume is enough to exercise the copy.
func crossVolumeRename(from, to string) error {
	return &os.LinkError{Op: "rename", Old: from, New: to, Err: windows.ERROR_NOT_SAME_DEVICE}
}

func TestImportExternalLauncherCopiesUserDataAcrossVolumes(t *testing.T) {
	t.Parallel()
	ctx, client, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	nested := filepath.Join(source, "Mods", "Character", "skin.ini")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("nested mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	if err := os.WriteFile(filepath.Join(linked, "linked.ini"), []byte("linked mod"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := createJunction(filepath.Join(source, "Mods", "Linked"), linked); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	target := filepath.Join(root, "GIMI")
	fakeImportInstaller(t, service, nil)
	service.renameUserData = crossVolumeRename
	events := []map[string]any{}
	service.eventEmit = func(name string, data ...any) {
		if name == "xxmi:import-progress" {
			events = append(events, data[0].(map[string]any))
		}
	}

	if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
	assertFile(t, nested, "nested mod")
	assertFile(t, filepath.Join(source, "ShaderFixes", "fix.hlsl"), "user shader")
	assertFile(t, filepath.Join(source, "d3dx_user.ini"), "user state")
	assertFile(t, filepath.Join(target, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(target, "Mods", "Character", "skin.ini"), "nested mod")
	assertFile(t, filepath.Join(target, "d3dx_user.ini"), "user state")
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row == nil {
		t.Fatalf("imported row = %+v, err = %v", row, err)
	}

	// The link is recreated rather than followed, and the linked data stays in its existing location.
	link := filepath.Join(target, "Mods", "Linked")
	if info, err := os.Lstat(link); err != nil || !isInstallReparsePoint(info) || !linksTo(link, linked) {
		t.Fatalf("junction was not recreated: info = %v, err = %v", info, err)
	}
	assertFile(t, filepath.Join(linked, "linked.ini"), "linked mod")

	copied := map[string]bool{}
	for _, event := range events[:len(events)-1] {
		if event["stage"] != "copy" || event["importer"] != "GIMI" {
			t.Fatalf("progress event = %v", event)
		}
		copied[event["name"].(string)] = event["copied"] == event["total"]
	}
	if !copied["Mods"] || !copied["d3dx_user.ini"] {
		t.Fatalf("progress never completed for every entry: %v", events)
	}
	if events[len(events)-1]["stage"] != "done" {
		t.Fatalf("last progress event = %v, want done", events[len(events)-1])
	}
}

func TestImportExternalLauncherPreservesSourceChangesAfterCrossVolumeCopy(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		failure error
	}{
		{name: "success"},
		{name: "rollback", failure: errors.New("install failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, _, service, external := newImportTest(t)
			source := filepath.Join(external, "GIMI")
			writeImportSourceFiles(t, source)
			root := t.TempDir()
			fakeImportInstaller(t, service, test.failure)
			install := service.installImporter
			service.installImporter = func(
				ctx context.Context, spec importerPackageSpec, cfg ImporterConfig, input InstallImporterPackageInput,
			) error {
				// Simulate external edits after copying, without relying on concurrent timing.
				if err := os.WriteFile(
					filepath.Join(source, "Mods", "user.ini"),
					[]byte("edited mod"),
					0o600,
				); err != nil {
					return err
				}
				if err := os.WriteFile(filepath.Join(source, "Mods", "new.ini"), []byte("new mod"), 0o600); err != nil {
					return err
				}
				return install(ctx, spec, cfg, input)
			}
			service.renameUserData = crossVolumeRename

			_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
				Path: external, Root: root, UserData: ImportUserDataMove,
			})
			if !errors.Is(err, test.failure) {
				t.Fatalf("import error = %v, want %v", err, test.failure)
			}
			assertFile(t, filepath.Join(source, "Mods", "user.ini"), "edited mod")
			assertFile(t, filepath.Join(source, "Mods", "new.ini"), "new mod")
			if test.failure != nil {
				if _, err := os.Lstat(filepath.Join(root, "GIMI")); !os.IsNotExist(err) {
					t.Fatalf("importer folder left after rollback: %v", err)
				}
				return
			}
			assertFile(t, filepath.Join(root, "GIMI", "Mods", "user.ini"), "user mod")
			if _, err := os.Lstat(filepath.Join(root, "GIMI", "Mods", "new.ini")); !os.IsNotExist(err) {
				t.Fatalf("source changes unexpectedly synced to the copy: %v", err)
			}
		})
	}
}

func TestImportExternalLauncherRollsBackCrossVolumeCopy(t *testing.T) {
	t.Parallel()
	ctx, client, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	root := t.TempDir()
	fakeImportInstaller(t, service, errors.New("install failed"))
	service.renameUserData = crossVolumeRename

	_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	})
	if err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("failed install result = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "GIMI")); !os.IsNotExist(err) {
		t.Fatalf("importer folder left after rollback: %v", err)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(source, "ShaderFixes", "fix.hlsl"), "user shader")
	assertFile(t, filepath.Join(source, "d3dx_user.ini"), "user state")
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
		t.Fatalf("row saved after failed import: row = %+v, err = %v", row, err)
	}
}

func TestImportExternalLauncherRestoresReusedFolderAfterCrossVolumeCopy(t *testing.T) {
	t.Parallel()
	ctx, _, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	root := t.TempDir()
	target := filepath.Join(root, "GIMI")
	fakeImportInstaller(t, service, nil)
	if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataKeep,
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "d3dx_user.ini"), []byte("built-in state"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeImportInstaller(t, service, errors.New("install failed"))
	service.renameUserData = crossVolumeRename

	_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	})
	if err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("failed reimport result = %v", err)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(source, "d3dx_user.ini"), "user state")
	if info, err := os.Lstat(filepath.Join(target, "Mods")); err != nil || !isInstallReparsePoint(info) {
		t.Fatalf("Mods link not restored: info = %v, err = %v", info, err)
	}
	assertFile(t, filepath.Join(target, "d3dx_user.ini"), "built-in state")
}

func TestImporterFolderMigrationRestoresDisplacedFilesAfterCopyCleanupFails(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		share        uint32
		restoreFails bool
	}{
		{name: "restoration succeeds", share: windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE},
		{name: "restoration fails", share: windows.FILE_SHARE_READ, restoreFails: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			source := t.TempDir()
			target := t.TempDir()
			for _, name := range []string{"d3dx.ini", "d3dx_user.ini"} {
				if err := os.WriteFile(filepath.Join(source, name), []byte("external state"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(target, name), []byte("built-in state"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			migration := importerFolderMigration{mode: ImportUserDataMove, rename: crossVolumeRename}
			if err := migration.prepare(t.Context(), "GIMI", source, target, false); err != nil {
				t.Fatal(err)
			}

			// Deny deletion without relying on timing; one case still permits restoring the file in place.
			path := filepath.Join(target, "d3dx_user.ini")
			widePath, err := windows.UTF16PtrFromString(path)
			if err != nil {
				t.Fatal(err)
			}
			handle, err := windows.CreateFile(
				widePath,
				windows.GENERIC_READ,
				test.share,
				nil,
				windows.OPEN_EXISTING,
				windows.FILE_ATTRIBUTE_NORMAL,
				0,
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := windows.CloseHandle(handle); err != nil {
					t.Error(err)
				}
			})

			err = migration.rollback()
			if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) || !strings.Contains(err.Error(), "remove copied ") {
				t.Fatalf("rollback error = %v, want copy cleanup failure", err)
			}
			if strings.Contains(err.Error(), "restore ") != test.restoreFails {
				t.Fatalf("rollback error = %v, want restoration failure = %v", err, test.restoreFails)
			}
			assertFile(t, filepath.Join(target, "d3dx.ini"), "built-in state")
			if test.restoreFails {
				assertFile(t, path, "external state")
			} else {
				assertFile(t, path, "built-in state")
			}
			assertFile(t, filepath.Join(source, "d3dx_user.ini"), "external state")
		})
	}
}

// TestImportExternalLauncherCopiesUserDataBetweenRealVolumes covers the real rename failure, which the injected one
// only imitates. It needs a second writable volume and is skipped on machines and runners that have just one.
func TestImportExternalLauncherCopiesUserDataBetweenRealVolumes(t *testing.T) {
	t.Parallel()
	ctx, _, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	root := otherVolumeTempDir(t, external)
	target := filepath.Join(root, "GIMI")
	fakeImportInstaller(t, service, nil)

	if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(source, "ShaderFixes", "fix.hlsl"), "user shader")
	assertFile(t, filepath.Join(source, "d3dx_user.ini"), "user state")
	assertFile(t, filepath.Join(target, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(target, "d3dx_user.ini"), "user state")
}

// otherVolumeTempDir creates a temporary directory on a fixed volume other than the one holding path.
func otherVolumeTempDir(t *testing.T, path string) string {
	t.Helper()
	for letter := 'C'; letter <= 'Z'; letter++ {
		volume := string(letter) + ":"
		if strings.EqualFold(volume, filepath.VolumeName(path)) {
			continue
		}
		drive, err := windows.UTF16PtrFromString(volume + `\`)
		if err != nil {
			t.Fatal(err)
		}
		if windows.GetDriveType(drive) != windows.DRIVE_FIXED {
			continue
		}
		dir, err := os.MkdirTemp(volume+`\`, "nahida-xxmi-import-test-")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	t.Skip("no second writable fixed volume to move user data to")
	return ""
}
