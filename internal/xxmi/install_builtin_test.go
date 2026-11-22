package xxmi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/watcher"
)

func TestInstallBuiltinImporterPreservesModsAndExternalConfig(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	writeXXMITestConfig(t, root)
	externalConfig, err := os.ReadFile(filepath.Join(root, xxmiConfigName))
	if err != nil {
		t.Fatal(err)
	}
	importerFolder := filepath.Join(root, "GIMI")
	if err := os.MkdirAll(filepath.Join(importerFolder, "Mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	mainINI := filepath.Join(importerFolder, "Core", "GIMI", "main.ini")
	if err := os.MkdirAll(filepath.Dir(mainINI), 0o755); err != nil {
		t.Fatal(err)
	}
	const oldMain = "global $version = 1.22\n"
	for name, content := range map[string]string{
		"d3dx.ini": "user ini", "Mods/user.ini": "user mod", "Core/GIMI/main.ini": oldMain,
	} {
		if err := os.WriteFile(filepath.Join(importerFolder, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	zipBody := writeXXMITestZip(t, map[string]string{
		"Core/GIMI/main.ini": "global $version = 1.23\n", "d3dx.ini": "package ini",
		"Mods/package.ini": "package mod", "ShaderFixes/new.hlsl": "shader",
	})
	digest := sha256.Sum256(zipBody)
	assetURL := "https://github.com/SilentNightSound/GIMI-Package/releases/download/v1.2.3/GIMI-PACKAGE-v1.2.3.zip"
	releases := fmt.Sprintf(
		`[ {"tag_name":"v1.2.3","body":"## Changes\nunsigned", "assets":[{"name":"GIMI-PACKAGE-v1.2.3.zip","browser_download_url":%q,"digest":"sha256:%s"}]} ]`,
		assetURL,
		hex.EncodeToString(digest[:]),
	)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var body []byte
		switch request.URL.String() {
		case "https://api.github.com/repos/SilentNightSound/GIMI-Package/releases":
			body = []byte(releases)
		case assetURL:
			body = zipBody
		case "https://api.github.com/repos/SpectrumQT/XXMI-Libs-Package/releases":
			body = []byte(`[]`)
		default:
			t.Fatalf("unexpected request %s", request.URL)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(infraClient)
	var lookup func(context.Context, string) []platform.ProcessInfo
	var logOutput bytes.Buffer
	service := NewWithOptions(Options{
		HTTP: infraClient, Download: download, Archive: infra.NewArchive(),
		Log: infra.NewLogWithOptions(infra.LogOptions{Writer: &logOutput, DisableFile: true}),
		FindImporterLockingProcesses: func(ctx context.Context, path string) []platform.ProcessInfo {
			if lookup == nil {
				return nil
			}
			return lookup(ctx, path)
		},
	})
	useTestAppData(t, service)
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ImporterFolder = importerFolder
	cfg.OverwriteINI = false
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	cfg.PackageVersion = VersionPin{Pinned: "1.2.3"}
	cfg.ProcessTimeout = 40
	input := InstallImporterPackageInput{Importer: "GIMI", Version: "v1.2.3", Config: &cfg}
	watch, err := watcher.WatchTree([]string{filepath.Join(importerFolder, "Mods")},
		watcher.TreeConfig{Depth: -1, Ops: watcher.All}, func(watcher.Event) {})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = watch.Close() })
	service.UseImporterMaintenance(func(context.Context) (func([]ImportedImporter) error, error) {
		if err := watch.Close(); err != nil {
			return nil, err
		}
		return func([]ImportedImporter) error {
			var err error
			watch, err = watcher.WatchTree([]string{filepath.Join(importerFolder, "Mods")},
				watcher.TreeConfig{Depth: -1, Ops: watcher.All}, func(watcher.Event) {})
			return err
		}, nil
	})
	if err := service.InstallImporterPackage(
		ctx,
		input,
	); err == nil ||
		!strings.Contains(err.Error(), "XXMI_UNSIGNED_RELEASE") {
		t.Fatalf("unsigned release accepted without confirmation: %v", err)
	}
	stored, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.PackageVersion.Pinned != "" || stored.ProcessTimeout == 40 {
		t.Fatalf("failed install persisted the draft: %+v, err = %v", stored, err)
	}
	input.AllowUnsigned = true
	for _, outcome := range []string{"found", "empty", "cancelled"} {
		t.Run(outcome, func(t *testing.T) {
			held, err := os.Open(mainINI)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = held.Close() }()
			installCtx, cancel := context.WithCancel(t.Context())
			defer cancel()

			lookups := 0
			holder := platform.ProcessInfo{Name: "Game", PID: 1234}
			lookup = func(_ context.Context, path string) []platform.ProcessInfo {
				lookups++
				if !platform.SamePathFold(path, importerFolder) {
					t.Errorf("lookup folder = %q, want %q", path, importerFolder)
				}
				if entries, globErr := filepath.Glob(filepath.Join(root, ".nahida-gimi-install.*")); globErr != nil ||
					len(entries) != 0 {
					t.Errorf("lookup ran before cleanup: %v, err = %v", entries, globErr)
				}
				if outcome == "cancelled" {
					cancel()
				}
				if outcome == "empty" {
					return nil
				}
				return []platform.ProcessInfo{holder}
			}
			t.Cleanup(func() { lookup = nil })
			logOutput.Reset()
			err = service.InstallImporterPackage(installCtx, input)
			inUse, ok := errors.AsType[*ImporterFolderInUseError](err)
			if !ok || !strings.Contains(inUse.Error(), "XXMI_IMPORTER_FOLDER_IN_USE:") {
				t.Fatalf("blocked install returned %v, want importer folder in use", err)
			}
			if !errors.Is(err, windows.ERROR_ACCESS_DENIED) && !errors.Is(err, windows.ERROR_SHARING_VIOLATION) {
				t.Fatalf("original Windows error missing: %v", err)
			}
			if lookups != 1 {
				t.Fatalf("lock lookups = %d, want one", lookups)
			}
			if outcome == "found" {
				if len(inUse.Processes) != 1 || inUse.Processes[0] != holder {
					t.Fatalf("locking processes = %v, want %v", inUse.Processes, holder)
				}
				logged := logOutput.String()
				if !strings.Contains(logged, `"locking_processes":[{"name":"Game","pid":1234}]`) ||
					!strings.Contains(logged, `"rollback":"rolled-back"`) {
					t.Fatalf("lock or rollback diagnostic missing: %s", logged)
				}
			} else if len(inUse.Processes) != 0 {
				t.Fatalf("%s lookup returned processes: %v", outcome, inUse.Processes)
			}
			assertFile(t, mainINI, oldMain)
			assertFile(t, filepath.Join(importerFolder, "Mods", "user.ini"), "user mod")
			assertFile(t, filepath.Join(importerFolder, "d3dx.ini"), "user ini")
			stored, err := service.GetImporterConfig(t.Context(), "GIMI")
			if err != nil || stored.PackageVersion.Pinned != "" || stored.ProcessTimeout == 40 {
				t.Fatalf("blocked install persisted the draft: %+v, err = %v", stored, err)
			}
		})
	}
	cacheRoot, err := service.cacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	backupPattern := filepath.Join(cacheRoot, "backups", "GIMI *", "d3dx.ini")
	failedInstallBackups, err := filepath.Glob(backupPattern)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.InstallImporterPackage(ctx, input); err != nil {
		t.Fatal(err)
	}
	assertFile(t, mainINI, "global $version = 1.23\n")
	stored, err = service.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.PackageVersion != cfg.PackageVersion || stored.ProcessTimeout != 40 {
		t.Fatalf("successful install did not persist the draft: %+v, err = %v", stored, err)
	}
	assertFile(t, filepath.Join(importerFolder, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(importerFolder, "d3dx.ini"), "user ini")
	assertFile(t, filepath.Join(importerFolder, "ShaderFixes", "new.hlsl"), "shader")
	if _, err := os.Stat(filepath.Join(importerFolder, "Mods", "package.ini")); !os.IsNotExist(err) {
		t.Fatalf("package mod copied: %v", err)
	}
	if err := service.InstallImporterPackage(ctx, input); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(importerFolder, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(importerFolder, "d3dx.ini"), "user ini")
	backups, err := filepath.Glob(backupPattern)
	if err != nil || len(backups)-len(failedInstallBackups) != 2 {
		t.Fatalf("consecutive INI backups = %v, err = %v", backups, err)
	}
	for _, backup := range backups {
		assertFile(t, backup, "user ini")
	}
	gotConfig, err := os.ReadFile(filepath.Join(root, xxmiConfigName))
	if err != nil || !bytes.Equal(gotConfig, externalConfig) {
		t.Fatalf("external config changed: %v", err)
	}

	// Importer installation already owns the package lock when preparing pinned libraries.
	cfg.XXMIVersion = VersionPin{Pinned: "1.7.6"}
	if err := service.InstallImporterPackage(ctx, input); err == nil ||
		!strings.Contains(err.Error(), "XXMI libraries release 1.7.6 not found") {
		t.Fatalf("library preparation failure was not returned: %v", err)
	}
	stored, err = service.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.XXMIVersion.Pinned != "" {
		t.Fatalf("library failure persisted the draft: %+v, err = %v", stored, err)
	}
	cfg.XXMIVersion = stored.XXMIVersion

	// A settings write failure must restore the installed tree and keep the previous settings.
	if _, err := client.SQL().ExecContext(ctx, `CREATE TRIGGER reject_importer_update
		BEFORE UPDATE ON xxmi_importers BEGIN SELECT RAISE(ABORT, 'settings write failed'); END`); err != nil {
		t.Fatal(err)
	}
	shader := filepath.Join(importerFolder, "ShaderFixes", "new.hlsl")
	if err := os.WriteFile(shader, []byte("previous shader"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg.ProcessTimeout = 41
	if err := service.InstallImporterPackage(
		ctx,
		input,
	); err == nil ||
		!strings.Contains(err.Error(), "settings write failed") {
		t.Fatalf("settings failure was not returned: %v", err)
	}
	assertFile(t, shader, "previous shader")
	assertFile(t, filepath.Join(importerFolder, "Mods", "user.ini"), "user mod")
	stored, err = service.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.ProcessTimeout != 40 {
		t.Fatalf("settings failure changed the draft: %+v, err = %v", stored, err)
	}
}

func TestInstallBuiltinImporterKeepsLinkedUserData(t *testing.T) {
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	writeImportSourceFiles(t, external)
	importerFolder := filepath.Join(t.TempDir(), "GIMI")
	if err := os.MkdirAll(importerFolder, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Mods", "ShaderFixes"} {
		if err := createJunction(filepath.Join(importerFolder, name), filepath.Join(external, name)); err != nil {
			t.Fatal(err)
		}
	}
	zipBody := writeXXMITestZip(t, map[string]string{
		"Core/GIMI/main.ini": "global $version = 1.23\n", "d3dx.ini": "package ini",
		"ShaderFixes/new.hlsl": "package shader",
	})
	assetURL := "https://github.com/SilentNightSound/GIMI-Package/releases/download/v1.2.3/GIMI-PACKAGE-v1.2.3.zip"
	releases := fmt.Sprintf(
		`[ {"tag_name":"v1.2.3","body":"unsigned","assets":[{"name":"GIMI-PACKAGE-v1.2.3.zip","browser_download_url":%q}]} ]`,
		assetURL,
	)
	httpClient := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := zipBody
		if request.URL.String() == "https://api.github.com/repos/SilentNightSound/GIMI-Package/releases" {
			body = []byte(releases)
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(bytes.NewReader(body)), Request: request}, nil
	})}
	infraClient := infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})
	download := infra.NewDownload()
	download.UseClient(infraClient)
	service := NewWithOptions(Options{HTTP: infraClient, Download: download, Archive: infra.NewArchive()})
	useTestAppData(t, service)
	service.UseClient(client)
	useBuiltinLauncher(t, service)
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ImporterFolder = importerFolder
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	if err := service.InstallImporterPackage(ctx, InstallImporterPackageInput{
		Importer: "GIMI", Version: "v1.2.3", AllowUnsigned: true,
	}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Mods", "ShaderFixes"} {
		info, err := os.Lstat(filepath.Join(importerFolder, name))
		if err != nil || !isInstallReparsePoint(info) {
			t.Fatalf("%s link was replaced: info = %v, err = %v", name, info, err)
		}
	}
	assertFile(t, filepath.Join(external, "Mods", "user.ini"), "user mod")
	assertFile(t, filepath.Join(external, "ShaderFixes", "fix.hlsl"), "user shader")
	assertFile(t, filepath.Join(external, "ShaderFixes", "new.hlsl"), "package shader")
}
