package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestPreviewExternalLauncherImportUsesInstalledVersionsAndReleasePolicy(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		prereleases bool
		failed      bool
		wantLatest  string
	}{
		{name: "stable releases", wantLatest: "1.3.0"},
		{name: "external prerelease preference", prereleases: true, wantLatest: "1.4.0-beta.1"},
		{name: "failed release check", failed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, client, _, external := newImportTest(t)
			writeImportSourceFiles(t, filepath.Join(external, "GIMI"))
			config := xxmiTestConfig()
			config["Launcher"].(map[string]any)["pre_release"] = tc.prereleases
			writeImportVersionConfig(t, external, config)
			writeImportLibsManifest(t, external, "1.7.6")
			httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				body := `[{"tag_name":"v1.8.0"}]`
				if strings.Contains(request.URL.Path, "GIMI-Package") {
					body = `[{"tag_name":"v1.2.0"},{"tag_name":"v1.4.0-beta.1","prerelease":true},{"tag_name":"v9.0.0","draft":true},{"tag_name":"v1.3.0"}]`
					if tc.failed {
						body = `[]`
					}
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Request: request,
					Body: io.NopCloser(strings.NewReader(body))}, nil
			})}
			service := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{
				HTTPClient: httpClient, Status: infra.BackendOnline,
			})})
			service.UseClient(client)

			versions, err := service.PreviewExternalLauncherImport(ctx, external)
			if err != nil || len(versions) != 2 {
				t.Fatalf("preview = %+v, err = %v", versions, err)
			}
			importer := versions[0]
			if importer.Package != "importer:GIMI" || importer.InstalledVersion != "1.2.3" ||
				importer.LatestVersion != tc.wantLatest || importer.CheckFailed != tc.failed ||
				importer.UpdateAvailable == tc.failed {
				t.Fatalf("importer preview = %+v", importer)
			}
			if versions[1].InstalledVersion != "1.7.6" || versions[1].LatestVersion != "1.8.0" {
				t.Fatalf("libraries preview = %+v", versions[1])
			}
			if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
				t.Fatalf("preview saved an importer: %+v, %v", row, err)
			}
			if value, err := client.Settings.GetValue(ctx, sharedLibsVersionKey); err != nil || value != nil {
				t.Fatalf("preview changed the libraries selection: %v, %v", value, err)
			}
		})
	}
}

func TestSelectImportVersionPreservesConfirmedVersions(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		mode      ImportVersionMode
		installed string
		snapshot  ImportPackageVersion
		want      string
		wantError string
	}{
		{name: "update", mode: ImportVersionLatest, installed: "1.7.6",
			snapshot: ImportPackageVersion{InstalledVersion: "1.7.6", LatestVersion: "1.8.0"}, want: "1.8.0"},
		{name: "pin after failed check", mode: ImportVersionPinned, installed: "1.7.6",
			snapshot: ImportPackageVersion{InstalledVersion: "1.7.6", CheckFailed: true}, want: "1.7.6"},
		{name: "no downgrade", mode: ImportVersionLatest, installed: "2.0.0-beta.1",
			snapshot: ImportPackageVersion{InstalledVersion: "2.0.0-beta.1", LatestVersion: "1.8.0"}, want: "2.0.0-beta.1"},
		{name: "source changed", mode: ImportVersionPinned, installed: "1.7.7",
			snapshot: ImportPackageVersion{InstalledVersion: "1.7.6"}, wantError: "XXMI_IMPORT_SOURCE_CHANGED"},
		{name: "cannot update without release", mode: ImportVersionLatest, installed: "1.7.6",
			snapshot: ImportPackageVersion{InstalledVersion: "1.7.6", CheckFailed: true}, wantError: "XXMI_IMPORT_PREVIEW_REQUIRED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.snapshot.Package = "xxmi-libs"
			version, err := selectImportVersion(ImportExternalLauncherInput{
				VersionMode: tc.mode, Versions: []ImportPackageVersion{tc.snapshot},
			}, "xxmi-libs", tc.installed)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("selection = %q, err = %v", version, err)
				}
				return
			}
			if err != nil || version != tc.want {
				t.Fatalf("selection = %q, err = %v; want %q", version, err, tc.want)
			}
		})
	}
}

func TestImportExternalLauncherAppliesConfirmedVersionPolicy(t *testing.T) {
	for _, mode := range []ImportVersionMode{ImportVersionLatest, ImportVersionPinned} {
		t.Run(string(mode), func(t *testing.T) {
			t.Setenv("USERPROFILE", t.TempDir())
			ctx, client, service, external := newImportTest(t)
			writeImportSourceFiles(t, filepath.Join(external, "GIMI"))
			config, _, err := readAndValidateConfig(filepath.Join(external, xxmiConfigName))
			if err != nil {
				t.Fatal(err)
			}
			packages := config["Packages"].(map[string]any)["packages"].(map[string]any)
			packages["GIMI"].(map[string]any)["skipped_version"] = "1.3.0"
			packages["XXMI"] = xxmiTestPackage("1.8.0")
			packages["XXMI"].(map[string]any)["skipped_version"] = "1.8.0"
			writeImportVersionConfig(t, external, config)
			writeImportLibsManifest(t, external, "1.7.6")
			installs := fakeImportInstaller(t, service, nil)
			wantImporter, wantLibs := "1.2.3", "1.7.6"
			if mode == ImportVersionLatest {
				wantImporter, wantLibs = "1.3.0", "1.8.0"
			}
			service.prepareImportLibraries = func(_ context.Context, path, installed, selected string) error {
				if path != external || installed != "1.7.6" || selected != wantLibs {
					t.Fatalf("library preparation = %q, %q, %q", path, installed, selected)
				}
				cacheRoot, err := xxmiCacheRoot()
				if err != nil {
					return err
				}
				folder := filepath.Join(cacheRoot, "packages", "xxmi-libs", selected)
				if err := os.MkdirAll(folder, 0o700); err != nil {
					return err
				}
				for _, name := range xxmiLibraryFiles {
					if err := os.WriteFile(filepath.Join(folder, name), []byte("verified fixture"), 0o600); err != nil {
						return err
					}
				}
				return nil
			}

			root := t.TempDir()
			_, err = service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
				Path: external, Root: root, UserData: ImportUserDataKeep, VersionMode: mode,
				Versions: []ImportPackageVersion{
					{Package: "importer:GIMI", InstalledVersion: "1.2.3", LatestVersion: "1.3.0"},
					{Package: "xxmi-libs", InstalledVersion: "1.7.6", LatestVersion: "1.8.0"},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(*installs, []string{"GIMI@" + wantImporter}) {
				t.Fatalf("installs = %q", *installs)
			}
			cfg, err := service.GetImporterConfig(ctx, "GIMI")
			if err != nil {
				t.Fatal(err)
			}
			wantPin := VersionPin{Pinned: "1.2.3"}
			wantShared := "1.7.6"
			if mode == ImportVersionLatest {
				wantPin, wantShared = VersionPin{Follow: "latest"}, ""
			}
			if cfg.PackageVersion != wantPin {
				t.Fatalf("package selection = %+v", cfg.PackageVersion)
			}
			if shared, err := client.Settings.GetValue(
				ctx,
				sharedLibsVersionKey,
			); err != nil || shared == nil ||
				*shared != wantShared {
				t.Fatalf("shared selection = %v, err = %v", shared, err)
			}
			if deployed, ok := deployedLibsVersion(cfg.ImporterFolder); !ok || deployed != wantLibs {
				t.Fatalf("initial libraries deployment = %q, %v", deployed, ok)
			}
			for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
				state, err := client.XXMIPackages.Get(ctx, pkg)
				if err != nil || state == nil || (state.SkippedVersion == nil) != (mode == ImportVersionLatest) {
					t.Fatalf("package state = %+v, err = %v", state, err)
				}
			}
			if setting, err := client.Settings.GetValue(
				ctx,
				"xxmi_auto_update",
			); err != nil || setting == nil ||
				*setting != "false" {
				t.Fatalf("launch update setting = %v, err = %v", setting, err)
			}
		})
	}
}

func TestImportExternalLauncherPreparesLibrariesBeforeMovingUserData(t *testing.T) {
	t.Parallel()
	ctx, client, service, external := newImportTest(t)
	source := filepath.Join(external, "GIMI")
	writeImportSourceFiles(t, source)
	writeImportLibsManifest(t, external, "1.7.6")
	installs := fakeImportInstaller(t, service, nil)
	service.prepareImportLibraries = func(context.Context, string, string, string) error {
		return errors.New("download failed")
	}
	_, err := service.ImportExternalLauncher(ctx, ImportExternalLauncherInput{
		Path: external, Root: t.TempDir(), UserData: ImportUserDataMove, VersionMode: ImportVersionLatest,
		Versions: []ImportPackageVersion{
			{Package: "importer:GIMI", InstalledVersion: "1.2.3", LatestVersion: "1.3.0"},
			{Package: "xxmi-libs", InstalledVersion: "1.7.6", LatestVersion: "1.8.0"},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "download failed") || len(*installs) != 0 {
		t.Fatalf("failed preparation = %v, installs = %q", err, *installs)
	}
	assertFile(t, filepath.Join(source, "Mods", "user.ini"), "user mod")
	if row, err := client.XXMIImporters.Get(ctx, "GIMI"); err != nil || row != nil {
		t.Fatalf("failed preparation saved an importer: %+v, %v", row, err)
	}
}

func TestSnapshotImportRuntimeRestoresReusedFolder(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	manifest, err := json.Marshal(runtimeManifest{
		Mode: RuntimeLegacy, Files: map[string]string{"3DMigoto Loader.exe": "old hash"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		runtimeManifestName: manifest, "d3d11.dll": []byte("old dll"), "3DMigoto Loader.exe": []byte("old loader"),
	} {
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	undo, err := snapshotImportRuntime(folder)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{runtimeManifestName, "d3d11.dll", "d3dcompiler_47.dll"} {
		if err := os.WriteFile(filepath.Join(folder, name), []byte("new runtime"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(folder, "3DMigoto Loader.exe")); err != nil {
		t.Fatal(err)
	}

	if err := undo(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(folder, runtimeManifestName), string(manifest))
	assertFile(t, filepath.Join(folder, "d3d11.dll"), "old dll")
	assertFile(t, filepath.Join(folder, "3DMigoto Loader.exe"), "old loader")
	if _, err := os.Stat(filepath.Join(folder, "d3dcompiler_47.dll")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new runtime file survived rollback: %v", err)
	}
}

func writeImportVersionConfig(t *testing.T, external string, config map[string]any) {
	t.Helper()
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(external, xxmiConfigName), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeImportLibsManifest(t *testing.T, external, version string) {
	t.Helper()
	folder := filepath.Join(external, "Resources", "Packages", "XXMI")
	if err := os.MkdirAll(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(xxmiLibraryManifest{Version: version})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "Manifest.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
