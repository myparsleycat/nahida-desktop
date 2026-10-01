package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/watcher"
)

func TestImportExternalLauncherSuspendsWatchersUntilCommitOrRollback(t *testing.T) {
	for _, test := range []struct {
		name   string
		mode   ImportUserDataMode
		fail   bool
		cancel bool
	}{
		{name: "move", mode: ImportUserDataMove},
		{name: "keep", mode: ImportUserDataKeep},
		{name: "rollback", mode: ImportUserDataMove, fail: true},
		{name: "cancel", mode: ImportUserDataMove, cancel: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, _, service, external := newImportTest(t)
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			source := filepath.Join(external, "GIMI")
			writeImportSourceFiles(t, source)
			root := t.TempDir()
			target := filepath.Join(root, "GIMI")
			watch, err := watcher.WatchTree([]string{filepath.Join(source, "Mods")},
				watcher.TreeConfig{Depth: -1, Ops: watcher.All}, func(watcher.Event) {})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = watch.Close() })
			failure := errors.New("package installation failed")
			service.installImporter = func(
				ctx context.Context, _ importerPackageSpec, cfg ImporterConfig, _ InstallImporterPackageInput,
			) error {
				transaction, err := beginImporterInstallTransaction(ctx, cfg.ImporterFolder, "", "GIMI")
				if err != nil {
					return err
				}
				defer func() { _ = transaction.Close() }()
				stage, err := transaction.prepare(ctx)
				if err != nil {
					return errors.Join(err, transaction.rollback())
				}
				if err := stage.Close(); err != nil {
					return errors.Join(err, transaction.rollback())
				}
				if _, _, err := transaction.commit(ctx, nil); err != nil {
					return errors.Join(err, transaction.rollback())
				}
				if err := transaction.finish(); err != nil {
					return err
				}
				if test.cancel {
					cancel()
					return ctx.Err()
				}
				if test.fail {
					return failure
				}
				return nil
			}
			resumed := false
			service.UseImporterMaintenance(func(context.Context) (func([]ImportedImporter) error, error) {
				if err := watch.Close(); err != nil {
					return nil, err
				}
				return func(moved []ImportedImporter) error {
					resumed = true
					path := source
					if test.mode == ImportUserDataMove && !test.fail && !test.cancel {
						if len(moved) != 1 || moved[0].PreviousFolder != source || moved[0].ImporterFolder != target {
							t.Fatalf("committed moves = %+v", moved)
						}
						path = target
					} else if len(moved) != 0 {
						t.Fatalf("uncommitted moves = %+v", moved)
					}
					// Rollback must have put user data back before a watcher is reopened.
					assertFile(t, filepath.Join(path, "Mods", "user.ini"), "user mod")
					var err error
					watch, err = watcher.WatchTree([]string{filepath.Join(path, "Mods")},
						watcher.TreeConfig{Depth: -1, Ops: watcher.All}, func(watcher.Event) {})
					return err
				}, nil
			})
			_, err = service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
				Path: external, Root: root, UserData: test.mode,
			})
			if test.fail && !errors.Is(err, failure) || test.cancel && !errors.Is(err, context.Canceled) ||
				!test.fail && !test.cancel && err != nil {
				t.Fatalf("import error = %v", err)
			}
			if !resumed {
				t.Fatal("watcher was not resumed")
			}
		})
	}
}

func TestImportExternalLauncherMoveWithActiveWatcherFailsWithoutMaintenance(t *testing.T) {
	ctx, _, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	watch, err := watcher.WatchTree([]string{filepath.Join(source, "Mods")},
		watcher.TreeConfig{Depth: -1, Ops: watcher.All}, func(watcher.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watch.Close() })
	root := t.TempDir()
	service.installImporter = func(
		_ context.Context, _ importerPackageSpec, _ ImporterConfig, _ InstallImporterPackageInput,
	) error {
		parent, err := os.OpenRoot(root)
		if err != nil {
			return err
		}
		defer func() { _ = parent.Close() }()
		return parent.Rename("GIMI", ".nahida-gimi-install.backup")
	}
	_, err = service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	})
	if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
		t.Fatalf("import error = %v, want access denied from the open Mods handle", err)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
}

func TestImportExternalLauncherMovesUserData(t *testing.T) {
	t.Parallel()
	ctx, client, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	root := t.TempDir()
	target := filepath.Join(root, "GIMI")
	fakeImportInstaller(t, service, nil)

	if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range importerUserData {
		if _, err := os.Lstat(filepath.Join(source, name)); !os.IsNotExist(err) {
			t.Fatalf("%s left in the external folder: %v", name, err)
		}
		info, err := os.Lstat(filepath.Join(target, name))
		if err != nil || isInstallReparsePoint(info) {
			t.Fatalf("%s was not moved: info = %v, err = %v", name, info, err)
		}
	}
	assertFile(t, filepath.Join(target, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(target, "d3dx_user.ini"), "user state")
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row == nil {
		t.Fatalf("imported row = %+v, err = %v", row, err)
	}
}

func TestImportExternalLauncherRollsBackFailedInstall(t *testing.T) {
	t.Parallel()
	for _, mode := range []ImportUserDataMode{ImportUserDataKeep, ImportUserDataMove} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			ctx, client, service, external := newImportTest(t)
			source := filepath.Join(external, "GIMI")
			writeImportSourceFiles(t, source)
			root := t.TempDir()
			fakeImportInstaller(t, service, errors.New("install failed"))

			_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
				Path: external, Root: root, UserData: mode,
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
		})
	}
}

func TestImportExternalLauncherInstallsLatestWhenVersionUnreadable(t *testing.T) {
	t.Parallel()
	ctx, _, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	if err := os.Remove(filepath.Join(source, "Core", "GIMI", "main.ini")); err != nil {
		t.Fatal(err)
	}
	installs := fakeImportInstaller(t, service, nil)

	if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: t.TempDir(), UserData: ImportUserDataKeep,
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(*installs, ",") != "GIMI@1" {
		t.Fatalf("installs = %v, want the launcher's latest GIMI release", *installs)
	}
}

func TestImportExternalLauncherRejectsUnknownImporterVersion(t *testing.T) {
	t.Parallel()
	ctx, client, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	if err := os.Remove(filepath.Join(source, "Core", "GIMI", "main.ini")); err != nil {
		t.Fatal(err)
	}
	config := xxmiTestConfig()
	config["Packages"] = map[string]any{"packages": map[string]any{}}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, xxmiConfigName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	installs := fakeImportInstaller(t, service, nil)

	_, err = service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	})
	if !errors.Is(err, errImportVersionUnknown) {
		t.Fatalf("import result = %v, want %v", err, errImportVersionUnknown)
	}
	if len(*installs) != 0 {
		t.Fatalf("installs = %v, want none", *installs)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
	if _, err := os.Lstat(filepath.Join(root, "GIMI")); !os.IsNotExist(err) {
		t.Fatalf("importer folder created for a rejected import: %v", err)
	}
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
		t.Fatalf("row saved after rejected import: row = %+v, err = %v", row, err)
	}
}

func TestImportExternalLauncherReusesResetImporterFolder(t *testing.T) {
	t.Parallel()
	for _, mode := range []ImportUserDataMode{ImportUserDataKeep, ImportUserDataMove} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			ctx, client, service, external := newImportTest(t)
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
			if err := service.ResetBuiltinRuntime(ctx); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				filepath.Join(target, "d3dx_user.ini"),
				[]byte("built-in state"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			if _, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
				Path: external, Root: root, UserData: mode,
			}); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(filepath.Join(target, "Mods"))
			if err != nil || isInstallReparsePoint(info) != (mode == ImportUserDataKeep) {
				t.Fatalf("Mods after %s reimport: info = %v, err = %v", mode, info, err)
			}
			assertFile(t, filepath.Join(target, "Mods", "user.ini"), "user mod")
			assertFile(t, filepath.Join(target, "d3dx_user.ini"), "user state")
			if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row == nil {
				t.Fatalf("reimported row = %+v, err = %v", row, err)
			}
		})
	}
}

func TestImportExternalLauncherRestoresReusedFolderOnFailure(t *testing.T) {
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

	// An empty folder in place of the ShaderFixes link, as the built-in launcher leaves after the link is removed.
	if err := os.Remove(filepath.Join(target, "ShaderFixes")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(target, "ShaderFixes"), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeImportInstaller(t, service, errors.New("install failed"))

	_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: root, UserData: ImportUserDataMove,
	})
	if err == nil || !strings.Contains(err.Error(), "install failed") {
		t.Fatalf("failed reimport result = %v", err)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(source, "ShaderFixes", "fix.hlsl"), "user shader")
	assertFile(t, filepath.Join(source, "d3dx_user.ini"), "user state")
	if info, err := os.Lstat(filepath.Join(target, "Mods")); err != nil || !isInstallReparsePoint(info) {
		t.Fatalf("Mods link not restored: info = %v, err = %v", info, err)
	}
	assertFile(t, filepath.Join(target, "Mods", "user.ini"), "user mod")
	if entries, err := os.ReadDir(filepath.Join(target, "ShaderFixes")); err != nil || len(entries) != 0 {
		t.Fatalf("empty ShaderFixes not restored: entries = %v, err = %v", entries, err)
	}
	assertFile(t, filepath.Join(target, "d3dx_user.ini"), "built-in state")
}

func TestImportExternalLauncherRejectsUnsafeTargets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		root func(t *testing.T, external string) string
		want error
	}{
		{
			name: "target with its own mods",
			root: func(t *testing.T, _ string) string {
				t.Helper()
				root := t.TempDir()
				if err := os.MkdirAll(filepath.Join(root, "GIMI", "Mods"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "GIMI", "Mods", "own.ini"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return root
			},
			want: errImportTargetNotEmpty,
		},
		{
			name: "target linking elsewhere",
			root: func(t *testing.T, _ string) string {
				t.Helper()
				root := t.TempDir()
				if err := os.MkdirAll(filepath.Join(root, "GIMI"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := createJunction(filepath.Join(root, "GIMI", "Mods"), t.TempDir()); err != nil {
					t.Fatal(err)
				}
				return root
			},
			want: errImportTargetNotEmpty,
		},
		{
			name: "root is the external launcher",
			root: func(_ *testing.T, external string) string { return external },
			want: errImportFolderConflict,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, client, service, external := newImportTest(t)
			source := filepath.Join(external, "GIMI")
			writeImportSourceFiles(t, source)
			installs := fakeImportInstaller(t, service, nil)

			_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
				Path: external, Root: test.root(t, external), UserData: ImportUserDataMove,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("unsafe target result = %v, want %v", err, test.want)
			}
			if len(*installs) != 0 {
				t.Fatalf("installed before rejecting the target: %q", *installs)
			}
			assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
			if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
				t.Fatalf("row saved for rejected import: row = %+v, err = %v", row, err)
			}
		})
	}
}

func newImportTest(t *testing.T) (context.Context, *db.Client, *XXMI, string) {
	t.Helper()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	config := xxmiTestConfig()
	config["Importers"].(map[string]any)["GIMI"].(map[string]any)["Importer"].(map[string]any)["process_start_method"] =
		"Native"
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, xxmiConfigName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.findProcess = func(context.Context, string) (int, error) { return 0, nil }
	service.UseClient(client)
	return ctx, client, service, external
}

func writeImportSourceFiles(t *testing.T, source string) {
	t.Helper()
	for name, content := range map[string]string{
		filepath.Join("Core", "GIMI", "main.ini"): "global $version = 1.23\n",
		"d3dx.ini":                               "user ini",
		"d3dx_user.ini":                          "user state",
		filepath.Join("Mods", "user.ini"):        "user mod",
		filepath.Join("ShaderFixes", "fix.hlsl"): "user shader",
	} {
		path := filepath.Join(source, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// fakeImportInstaller replaces the package installer, which needs signed GitHub releases, with one that records
// each install and writes the package version file.
func fakeImportInstaller(t *testing.T, service *XXMI, result error) *[]string {
	t.Helper()
	service.findProcess = func(context.Context, string) (int, error) { return 0, nil }
	installs := []string{}
	service.installImporter = func(
		_ context.Context, spec importerPackageSpec, cfg ImporterConfig, input InstallImporterPackageInput,
	) error {
		installs = append(installs, spec.key+"@"+input.Version)
		if result != nil {
			return result
		}
		path := filepath.Join(cfg.ImporterFolder, spec.versionFile)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, []byte("global $version = "+input.Version+"\n"), 0o600)
	}
	return &installs
}
