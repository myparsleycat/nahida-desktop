package xxmi

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestRelocateUserDataPathLeavesPackagePathsAndUnrelatedFolders(t *testing.T) {
	source := filepath.Join(t.TempDir(), "GIMI")
	target := filepath.Join(t.TempDir(), "GIMI")
	moved := []ImportedImporter{{Key: "GIMI", PreviousFolder: source, ImporterFolder: target}}
	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{name: "mods", path: filepath.Join(source, "Mods"), want: filepath.Join(target, "Mods")},
		{name: "nested-mod", path: filepath.Join(source, "Mods", "Character"), want: filepath.Join(target, "Mods", "Character")},
		{name: "shader", path: filepath.Join(source, "ShaderFixes", "a.hlsl"), want: filepath.Join(target, "ShaderFixes", "a.hlsl")},
		{name: "user-ini", path: filepath.Join(source, "d3dx_user.ini"), want: filepath.Join(target, "d3dx_user.ini")},
		{name: "package", path: filepath.Join(source, "Core"), want: filepath.Join(source, "Core")},
		{name: "similar-prefix", path: filepath.Join(source, "Mods2"), want: filepath.Join(source, "Mods2")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := RelocateUserDataPath(test.path, moved); got != test.want {
				t.Fatalf("relocated path = %q, want %q", got, test.want)
			}
		})
	}
}

func TestImportExternalLauncherMaintenanceFailureLeavesSourceUntouched(t *testing.T) {
	ctx, _, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	failure := errors.New("watchers could not be paused")
	service.UseImporterMaintenance(func(context.Context) (func([]ImportedImporter) error, error) {
		return nil, failure
	})
	installs := fakeImportInstaller(t, service, nil)
	_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: t.TempDir(), UserData: ImportUserDataMove,
	})
	if !errors.Is(err, failure) || len(*installs) != 0 {
		t.Fatalf("error = %v, installs = %v", err, *installs)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
}

func TestImportExternalLauncherRejectsRunningGameBeforePausingWatchers(t *testing.T) {
	ctx, _, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	service.findProcess = func(context.Context, string) (int, error) { return 123, nil }
	service.UseImporterMaintenance(func(context.Context) (func([]ImportedImporter) error, error) {
		t.Fatal("watchers were paused while the game is running")
		return nil, nil
	})
	_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: t.TempDir(), UserData: ImportUserDataMove,
	})
	if err == nil || !strings.Contains(err.Error(), "XXMI_GAME_RUNNING") {
		t.Fatalf("error = %v, want running game rejection", err)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
}

func TestImportExternalLauncherLogsResumeFailureWithoutUndoingCommit(t *testing.T) {
	ctx, client, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	root := t.TempDir()
	var logs bytes.Buffer
	service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true})
	service.UseImporterMaintenance(func(context.Context) (func([]ImportedImporter) error, error) {
		return func([]ImportedImporter) error { return errors.New("watcher resume failed") }, nil
	})
	fakeImportInstaller(t, service, nil)
	if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	}); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "GIMI", "Mods", "user.ini"), "user mod")
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row == nil {
		t.Fatalf("committed importer = %+v, error = %v", row, err)
	}
	if !strings.Contains(logs.String(), "watcher resume failed") ||
		!strings.Contains(logs.String(), "resume-importer-watchers") {
		t.Fatalf("resume failure was not logged: %s", logs.String())
	}
}
