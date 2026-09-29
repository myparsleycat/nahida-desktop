package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

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
