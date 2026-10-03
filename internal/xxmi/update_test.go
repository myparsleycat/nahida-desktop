package xxmi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
)

func TestUpdateAvailableDoesNotDowngradeImportedPackages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, latest, installed string
		want                    bool
	}{
		{name: "newer release", latest: "1.3.0", installed: "1.2.3", want: true},
		{name: "same release", latest: "1.2.3", installed: "1.2.3"},
		{name: "older stable release", latest: "1.9.0", installed: "2.0.0-beta.1"},
		{name: "stable replaces prerelease", latest: "2.0.0", installed: "2.0.0-beta.1", want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := updateAvailable(tc.latest, tc.installed, ""); got != tc.want {
				t.Fatalf("update available = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSkipVersionPreservesPinnedImporter(t *testing.T) {
	t.Parallel()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	ctx := context.Background()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.PackageVersion = VersionPin{Pinned: "1.0.0"}
	writeInstalledImporterPackage(t, "GIMI", cfg.ImporterFolder, "1.0.0")
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	latest := "1.1.0"
	if err := client.XXMIPackages.Upsert(
		ctx,
		db.XXMIPackageRow{Package: "importer:GIMI", LatestVersion: &latest},
	); err != nil {
		t.Fatal(err)
	}
	if !updateAvailable(latest, "1.0.0", "") {
		t.Fatal("pinned importer should still show the newer release")
	}
	if err := x.SkipVersion(ctx, "importer:GIMI", latest); err != nil {
		t.Fatal(err)
	}
	if updateAvailable(latest, "1.0.0", latest) {
		t.Fatal("skipped release should not be announced")
	}
	stored, err := x.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.PackageVersion.Pinned != "1.0.0" {
		t.Fatalf("pin changed: %+v, err = %v", stored.PackageVersion, err)
	}
}

func TestCheckUpdatesHonorsHourlyThrottleAndForce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	useBuiltinLauncher(t, x)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
		latest := "1.2.3"
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil || len(statuses) != 2 {
		t.Fatalf("cached update statuses = %+v, err = %v", statuses, err)
	}
	if _, err := x.CheckUpdates(ctx, true); err == nil {
		t.Fatal("force update check did not attempt a release refresh")
	}
}

func TestCheckUpdatesDoesNotReportLegacyRuntimeAsInstalledLibraries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	useBuiltinLauncher(t, x)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	cfg.Mode = RuntimeLegacy
	cfg.XXMIVersion = VersionPin{Pinned: "1.7.6"}
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	manifest, err := json.Marshal(runtimeManifest{Mode: RuntimeLegacy, Source: "legacy@123456789abc"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
		latest := "1.7.7"
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.Package == "xxmi-libs" {
			if status.Installed != "" || !status.Pinned {
				t.Fatalf("legacy runtime reported as XXMI libraries: %+v", status)
			}
			return
		}
	}
	t.Fatalf("XXMI libraries status missing: %+v", statuses)
}

func TestCheckUpdatesIncludesLegacyExtraDLLInjector(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	useBuiltinLauncher(t, x)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	cfg.Mode = RuntimeLegacy
	cfg.ExtraLibraries = ExtraLibraries{Enabled: true, Paths: []string{`C:\Mods\extra.dll`}}
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	cacheRoot, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(cacheRoot, "packages", "xxmi-libs", "1.7.6"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
		latest := "1.7.6"
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	statuses, err := x.CheckUpdates(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.Package == "xxmi-libs" {
			if status.Installed != "" || !status.Available || status.Pinned {
				t.Fatalf("legacy extra DLL injector update = %+v", status)
			}
			return
		}
	}
	t.Fatalf("XXMI libraries status missing: %+v", statuses)
}

func TestCheckUpdatesResolvesXXMILibrariesWithoutDeployedSource(t *testing.T) {
	for _, tc := range []struct {
		name      string
		manifest  *runtimeManifest
		installed string
		available bool
	}{
		{"not deployed with unverified cache", nil, "", true},
		{"adopted DLL with unverified cache", &runtimeManifest{Mode: RuntimeXXMI}, "", true},
		{"deployed source", &runtimeManifest{Mode: RuntimeXXMI, Source: "xxmi-libs@1.7.6"}, "1.7.6", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USERPROFILE", t.TempDir())
			ctx := context.Background()
			client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			if err := client.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			x := New()
			x.UseClient(client)
			useBuiltinLauncher(t, x)
			cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg.Enabled = true
			cfg.XXMIVersion = VersionPin{Pinned: "1.7.6"}
			if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
				t.Fatal(err)
			}

			// An unsigned cache folder must not count as installed libraries.
			cacheRoot, err := xxmiCacheRoot()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(cacheRoot, "packages", "xxmi-libs", "1.7.6"), 0o700); err != nil {
				t.Fatal(err)
			}
			if tc.manifest != nil {
				data, err := json.Marshal(tc.manifest)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(
					filepath.Join(cfg.ImporterFolder, runtimeManifestName),
					data,
					0o600,
				); err != nil {
					t.Fatal(err)
				}
			}
			for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
				latest := "1.7.6"
				if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
					Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
				}); err != nil {
					t.Fatal(err)
				}
			}

			statuses, err := x.CheckUpdates(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range statuses {
				if status.Package == "xxmi-libs" {
					if status.Installed != tc.installed || status.Available != tc.available {
						t.Fatalf("XXMI libraries status = %+v", status)
					}
					return
				}
			}
			t.Fatalf("XXMI libraries status missing: %+v", statuses)
		})
	}
}

func TestCheckUpdatesFollowsSharedAndImporterXXMILibrariesSelection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		shared    string
		pin       VersionPin
		installed string
		pinned    bool
		available bool
	}{
		{"shared latest", "", VersionPin{Follow: followShared}, "", false, true},
		{"shared version", "1.7.6", VersionPin{Follow: followShared}, "", true, true},
		{"own latest ignores shared version", "1.7.6", VersionPin{Follow: "latest"}, "", false, true},
		{"own version without notice", "", VersionPin{Pinned: "1.7.6"}, "", true, true},
		{"own version with notice", "", VersionPin{Pinned: "1.7.6", Notify: true}, "1.7.6", false, true},
		{"own version with notice at latest", "", VersionPin{Pinned: "1.7.7", Notify: true}, "1.7.7", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USERPROFILE", t.TempDir())
			ctx := context.Background()
			client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = client.Close() }()
			if err := client.Reconcile(ctx); err != nil {
				t.Fatal(err)
			}
			x := New()
			x.UseClient(client)
			useBuiltinLauncher(t, x)
			if tc.shared != "" {
				if err := client.Settings.Upsert(ctx, sharedLibsVersionKey, &tc.shared); err != nil {
					t.Fatal(err)
				}
			}
			cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg.Enabled = true
			cfg.XXMIVersion = tc.pin
			if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
				t.Fatal(err)
			}
			for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
				latest := "1.7.7"
				if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
					Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
				}); err != nil {
					t.Fatal(err)
				}
			}

			statuses, err := x.CheckUpdates(ctx, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range statuses {
				if status.Package == "xxmi-libs" {
					if status.Installed != tc.installed || status.Pinned != tc.pinned ||
						status.Available != tc.available || status.Shared != (tc.pin.Follow == followShared) {
						t.Fatalf("XXMI libraries status = %+v", status)
					}
					return
				}
			}
			t.Fatalf("XXMI libraries status missing: %+v", statuses)
		})
	}
}

func TestAutoUpdateLeavesNotifyingXXMILibrariesPinInPlace(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	useBuiltinLauncher(t, x)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	cfg.PackageVersion = VersionPin{Pinned: "1.0.0"}
	writeInstalledImporterPackage(t, "GIMI", cfg.ImporterFolder, "1.0.0")
	cfg.XXMIVersion = VersionPin{Pinned: "1.7.6", Notify: true}
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	for pkg, latest := range map[string]string{"importer:GIMI": "1.1.0", "xxmi-libs": "1.7.7"} {
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	if err := x.autoUpdateForLaunch(ctx, "GIMI"); err != nil {
		t.Fatalf("notifying pin triggered auto-install: %v", err)
	}
	stored, err := x.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.XXMIVersion != cfg.XXMIVersion {
		t.Fatalf("auto-update changed the pin: %+v, err = %v", stored.XXMIVersion, err)
	}
}

func TestSelectedLegacyInjectorVersionNormalizesPin(t *testing.T) {
	t.Parallel()
	if got := selectedLegacyInjectorVersion("v1.7.6"); got != "1.7.6" {
		t.Fatalf("selected legacy injector version = %q", got)
	}
}

func TestCachedLibsVersionClearsInstalledUpdateBeforeDeployment(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, deployed, skipped, installed string
		verified                           bool
		available                          bool
	}{
		{name: "downloaded update", deployed: "1.1.7", verified: true, installed: "1.2.0"},
		{name: "missing update", deployed: "1.1.7", installed: "1.1.7", available: true},
		{name: "skipped update", deployed: "1.1.7", skipped: "1.2.0", verified: true, installed: "1.1.7"},
		{name: "initial download", verified: true, installed: "1.2.0"},
		{name: "unverified initial cache", available: true},
		{name: "already deployed", deployed: "1.2.0", installed: "1.2.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			installed := cachedLibsVersion("1.2.0", tc.skipped, tc.deployed, func(version string) bool {
				if version != "1.2.0" {
					t.Fatalf("verified version = %q", version)
				}
				return tc.verified
			})
			if installed != tc.installed || updateAvailable("1.2.0", installed, tc.skipped) != tc.available {
				t.Fatalf("installed = %q, available = %v", installed, updateAvailable("1.2.0", installed, tc.skipped))
			}
		})
	}
}

func TestNewestCachedPackageVersionUsesVersionOrder(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	root, err := xxmiCacheRoot()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"xxmi-libs", "gi-fps-unlocker"} {
		parent := filepath.Join(root, "packages", pkg)
		for _, version := range []string{"1.7.6", "1.7.5", "1.7.7.tmp-abcd"} {
			if err := os.MkdirAll(filepath.Join(parent, version), 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if got := newestCachedPackageVersion(pkg); got != "1.7.6" {
			t.Fatalf("%s newest cached version = %q", pkg, got)
		}
	}
}

func TestAutoUpdateUsesEnabledDefaultBeforeSettingsPageOpens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	checks := 0
	x := NewWithOptions(Options{EventEmit: func(name string, _ ...any) {
		if name == "xxmi:updates" {
			checks++
		}
	}})
	x.UseClient(client)
	useBuiltinLauncher(t, x)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	for _, pkg := range []string{"importer:GIMI", "xxmi-libs"} {
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := x.autoUpdateForLaunch(ctx, "GIMI"); err != nil || checks != 1 {
		t.Fatalf("default auto-update: checks = %d, err = %v", checks, err)
	}
	disabled := "false"
	if err := client.Settings.Upsert(ctx, "xxmi_auto_update", &disabled); err != nil {
		t.Fatal(err)
	}
	if err := x.autoUpdateForLaunch(ctx, "GIMI"); err != nil || checks != 1 {
		t.Fatalf("disabled auto-update: checks = %d, err = %v", checks, err)
	}
	notify := "notify"
	if err := client.Settings.Upsert(ctx, "xxmi_auto_update", &notify); err != nil {
		t.Fatal(err)
	}
	if err := x.autoUpdateForLaunch(ctx, "GIMI"); err != nil || checks != 1 {
		t.Fatalf("asking auto-update: checks = %d, err = %v", checks, err)
	}
}

func TestLaunchUpdatesAsksOnlyForInstallablePackagesWhenNotifying(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	useBuiltinLauncher(t, x)
	for key, pin := range map[string]VersionPin{
		"GIMI": {Pinned: "1.7.6", Notify: true},
		"SRMI": {Pinned: "1.7.6"},
	} {
		cfg, err := DefaultImporterConfig(key, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		cfg.Enabled = true
		cfg.PackageVersion = VersionPin{Pinned: "1.0.0"}
		writeInstalledImporterPackage(t, key, cfg.ImporterFolder, "1.0.0")
		cfg.XXMIVersion = pin
		if err := x.SaveImporterConfig(ctx, key, cfg); err != nil {
			t.Fatal(err)
		}
	}
	for _, pkg := range []string{"importer:GIMI", "importer:SRMI", "xxmi-libs"} {
		latest := "1.7.7"
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// The default mode installs silently, so nothing waits for confirmation.
	if pending, err := x.LaunchUpdates(ctx, "GIMI"); err != nil || len(pending) != 0 {
		t.Fatalf("automatic mode asked for %+v, err = %v", pending, err)
	}
	notify := "notify"
	if err := client.Settings.Upsert(ctx, "xxmi_auto_update", &notify); err != nil {
		t.Fatal(err)
	}
	pending, err := x.LaunchUpdates(ctx, "GIMI")
	if err != nil || len(pending) != 1 || pending[0].Package != "xxmi-libs" || pending[0].Importer != "GIMI" {
		t.Fatalf("notifying pin asked for %+v, err = %v", pending, err)
	}
	if pending, err := x.LaunchUpdates(ctx, "SRMI"); err != nil || len(pending) != 0 {
		t.Fatalf("fixed versions asked for %+v, err = %v", pending, err)
	}
}

func TestAutoUpdateLeavesPinnedPackagesUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	client, err := db.New(filepath.Join(t.TempDir(), "data.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	if err := client.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	x := New()
	x.UseClient(client)
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	cfg.PackageVersion = VersionPin{Pinned: "1.0.0"}
	writeInstalledImporterPackage(t, "GIMI", cfg.ImporterFolder, "1.0.0")
	cfg.XXMIVersion = VersionPin{Pinned: "1.7.6"}
	if err := x.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	for pkg, latest := range map[string]string{"importer:GIMI": "1.1.0", "xxmi-libs": "1.7.7"} {
		if err := client.XXMIPackages.Upsert(ctx, db.XXMIPackageRow{
			Package: pkg, LatestVersion: &latest, UpdateCheckTime: time.Now().Unix(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := x.autoUpdateForLaunch(ctx, "GIMI"); err != nil {
		t.Fatalf("pinned packages triggered auto-install: %v", err)
	}
	stored, err := x.GetImporterConfig(ctx, "GIMI")
	if err != nil || stored.PackageVersion.Pinned != "1.0.0" || stored.XXMIVersion.Pinned != "1.7.6" {
		t.Fatalf("auto-update changed pins: %+v, err = %v", stored, err)
	}
}
