package xxmi

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestDefaultImporterSettings(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "xxmi")
	for _, key := range []string{"GIMI", "SRMI", "ZZMI", "WWMI", "HIMI", "EFMI"} {
		cfg, err := DefaultImporterConfig(key, root)
		if err != nil {
			t.Fatalf("%s default: %v", key, err)
		}
		if cfg.PackageVersion.Follow != "latest" || cfg.XXMIVersion.Follow != followShared {
			t.Fatalf("%s pins = %+v, %+v", key, cfg.PackageVersion, cfg.XXMIVersion)
		}
		if err := ValidateImporterSettings(key, cfg); err != nil {
			t.Fatalf("%s validation: %v", key, err)
		}
		if cfg.InjectionMethod != "Default" {
			t.Fatalf("%s changed default injection method: %q", key, cfg.InjectionMethod)
		}
	}
}

func TestImporterSettingsRejectNestedGameFolderAndInvalidPin(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "xxmi")
	cfg, err := DefaultImporterConfig("GIMI", root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.GameFolder = filepath.Join(cfg.ImporterFolder, "game")
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("nested game folder accepted")
	}
	cfg.GameFolder = ""
	cfg.XXMIVersion = VersionPin{Follow: "latest", Pinned: "1.7.6"}
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("ambiguous version pin accepted")
	}
	for _, version := range []string{".", "..", "v..", " "} {
		cfg.XXMIVersion = VersionPin{Pinned: version}
		if err := ValidateImporterSettings("GIMI", cfg); err == nil {
			t.Fatalf("dot version pin %q accepted", version)
		}
	}

	for _, pin := range []VersionPin{{Follow: "latest"}, {Pinned: "1.7.6"}, {Pinned: "1.7.6", Notify: true}} {
		cfg.XXMIVersion = pin
		if err := ValidateImporterSettings("GIMI", cfg); err != nil {
			t.Fatalf("XXMI version %+v rejected: %v", pin, err)
		}
	}
	cfg.XXMIVersion = VersionPin{Follow: "latest", Notify: true}
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("update notice without a pinned version accepted")
	}
	cfg.XXMIVersion = VersionPin{Follow: followShared}
	cfg.PackageVersion = VersionPin{Follow: followShared}
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("importer package following the shared libraries version accepted")
	}
}

func TestPinnedVersionsSurviveReload(t *testing.T) {
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
	service := New()
	service.UseClient(client)

	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.PackageVersion = VersionPin{Pinned: "1.2.3"}
	writeInstalledImporterPackage(t, "GIMI", cfg.ImporterFolder, "1.2.3")
	cfg.XXMIVersion = VersionPin{Pinned: "1.1.7"}
	cfg.InjectionMethod = "Native"
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}

	stored, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if stored.PackageVersion != cfg.PackageVersion || stored.XXMIVersion != cfg.XXMIVersion {
		t.Fatalf("reloaded pins = %+v, %+v", stored.PackageVersion, stored.XXMIVersion)
	}
	if err := ValidateImporterSettings("GIMI", stored); err != nil {
		t.Fatalf("reloaded config validation: %v", err)
	}
	if stored.InjectionMethod != "Native" {
		t.Fatalf("reloaded injection method = %q", stored.InjectionMethod)
	}
}

func TestImporterInjectionMethodValidation(t *testing.T) {
	t.Parallel()
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{"", "Default", "Native", "unknown"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			cfg := cfg
			cfg.InjectionMethod = method
			err := ValidateImporterSettings("GIMI", cfg)
			if (err != nil) != (method == "unknown") {
				t.Fatalf("method %q: %v", method, err)
			}
		})
	}
}

func TestImporterSettingsRejectInvalidWindowMode(t *testing.T) {
	t.Parallel()
	cfg, err := DefaultImporterConfig("GIMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"Windowed", "Borderless", "Fullscreen", "Exclusive Fullscreen"} {
		cfg.WindowMode = mode
		if err := ValidateImporterSettings("GIMI", cfg); err != nil {
			t.Fatalf("valid window mode %q rejected: %v", mode, err)
		}
	}
	cfg.WindowMode = "other"
	if err := ValidateImporterSettings("GIMI", cfg); err == nil {
		t.Fatal("invalid window mode accepted")
	}
}

func TestImporterConfigIsStoredUnderCanonicalKey(t *testing.T) {
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
	service := New()
	service.UseClient(client)
	cfg, err := DefaultImporterConfig("EFMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	if err := service.SaveImporterConfig(ctx, " efmi ", cfg); err != nil {
		t.Fatal(err)
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil || len(rows) != 1 || rows[0].Key != "EFMI" {
		t.Fatalf("stored rows = %+v, err = %v", rows, err)
	}
	stored, err := service.GetImporterConfig(ctx, "efmi")
	if err != nil || !stored.Enabled {
		t.Fatalf("config read through a non-canonical key = %+v, err = %v", stored, err)
	}
}

func TestModeChangeRejectsLaunchInProgress(t *testing.T) {
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
	service := New()
	service.UseClient(client)
	service.findProcess = func(context.Context, string) (int, error) { return 0, nil }
	cfg, err := DefaultImporterConfig("EFMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := service.SaveImporterConfig(ctx, "EFMI", cfg); err != nil {
		t.Fatal(err)
	}
	if !service.acquireImporter("EFMI") {
		t.Fatal("failed to start importer launch")
	}
	err = service.SetImporterMode(ctx, "EFMI", RuntimeLegacy)
	if err == nil || !strings.Contains(err.Error(), "XXMI_GAME_RUNNING") {
		t.Fatalf("mode change during launch = %v", err)
	}
	service.releaseImporter("EFMI")
	stored, err := service.GetImporterConfig(ctx, "EFMI")
	if err != nil || stored.Mode != RuntimeXXMI {
		t.Fatalf("saved mode after rejected change = %q, err = %v", stored.Mode, err)
	}
	if err := service.SetImporterMode(ctx, "EFMI", RuntimeLegacy); err != nil {
		t.Fatal(err)
	}
}

func TestStoredSchema1ConfigUpgrades(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, tc := range []struct {
		name   string
		key    string
		stored string
		check  func(ImporterConfig) bool
	}{
		{
			name: "custom launch carries its injection mode",
			key:  "GIMI",
			stored: `{"schemaVersion":1,"processStartMethod":"Native",` +
				`"customLaunch":{"enabled":true,"command":"start game","injectMode":"Bypass"},` +
				`"migoto":{"enforceRendering":true,"callsLogging":true,"debugLogging":true}}`,
			check: func(cfg ImporterConfig) bool {
				return cfg.GameLaunch == "Custom" && cfg.CustomLaunch.Command == "start game" &&
					cfg.XXMIDLLInjectMode == "Bypass" && cfg.ProcessStartMethod == "Native" &&
					cfg.Migoto.LogLevel == "Debug"
			},
		},
		{
			name: "disabled custom launch keeps the importer default",
			key:  "WWMI",
			stored: `{"schemaVersion":1,"processStartMethod":"Shell",` +
				`"customLaunch":{"enabled":false,"command":"start game","injectMode":"Hook"},` +
				`"migoto":{"callsLogging":true},` +
				`"wwmi":{"unlockFPS":true,"applyPerfTweaks":true,"meshLODDistanceBaseFOV":170}}`,
			check: func(cfg ImporterConfig) bool {
				return cfg.GameLaunch == "Direct" && cfg.XXMIDLLInjectMode == "Inject" &&
					cfg.ProcessStartMethod == "Shell" && cfg.Migoto.LogLevel == "Info" &&
					*cfg.WWMI == WWMIOptions{
						UnlockFPS: true, MeshLODDistanceBaseFOV: 170, ResourceTier: "HD",
						RetiredEngineOptionsPending: true,
					}
			},
		},
		{
			name: "removed performance tweaks are not removed twice",
			key:  "WWMI",
			stored: `{"schemaVersion":1,"processStartMethod":"Native",` +
				`"wwmi":{"applyPerfTweaks":true,"retiredEngineOptionsPending":false}}`,
			check: func(cfg ImporterConfig) bool { return !cfg.WWMI.RetiredEngineOptionsPending },
		},
		{
			name:   "unused performance tweaks leave nothing to remove",
			key:    "WWMI",
			stored: `{"schemaVersion":1,"processStartMethod":"Native","wwmi":{"applyPerfTweaks":false}}`,
			check:  func(cfg ImporterConfig) bool { return !cfg.WWMI.RetiredEngineOptionsPending },
		},
		{
			name:   "manual start becomes a manual launch",
			key:    "GIMI",
			stored: `{"schemaVersion":1,"processStartMethod":"Manual","customLaunch":{"injectMode":"Hook"}}`,
			check: func(cfg ImporterConfig) bool {
				return cfg.GameLaunch == "Manual" && cfg.ProcessStartMethod == "Shell" &&
					cfg.XXMIDLLInjectMode == "Hook" && cfg.Migoto.LogLevel == "Disabled"
			},
		},
		{
			name: "imported overrides win",
			key:  "GIMI",
			stored: `{"schemaVersion":1,"processStartMethod":"Native","xxmiDLLInjectMode":"Inject",` +
				`"customLaunch":{"enabled":true,"command":"","injectMode":"Hook"},"migoto":{"logLevel":"WARNING"}}`,
			check: func(cfg ImporterConfig) bool {
				return cfg.GameLaunch == "Direct" && cfg.XXMIDLLInjectMode == "Inject" &&
					cfg.Migoto.LogLevel == "Warning"
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := decodeImporterConfig(tc.key, root, tc.stored)
			if err != nil || cfg.SchemaVersion != importerConfigSchema || !tc.check(cfg) {
				t.Fatalf("upgraded config = %+v, %v", cfg, err)
			}
			if !cfg.Migoto.Input || !cfg.Migoto.ClearUnknownSettings || cfg.Migoto.InputDisableMode != "Mods" {
				t.Fatalf("upgraded config lost the new defaults: %+v", cfg.Migoto)
			}
			if err := ValidateImporterSettings(tc.key, cfg); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestImporterSettingsRejectInvalidLaunchFields(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*ImporterConfig){
		"unknown game launch":           func(cfg *ImporterConfig) { cfg.GameLaunch = "Portal" },
		"manual start method":           func(cfg *ImporterConfig) { cfg.ProcessStartMethod = "Manual" },
		"missing inject mode":           func(cfg *ImporterConfig) { cfg.XXMIDLLInjectMode = "" },
		"upper-case log level":          func(cfg *ImporterConfig) { cfg.Migoto.LogLevel = "DEBUG" },
		"unknown input mode":            func(cfg *ImporterConfig) { cfg.Migoto.InputDisableMode = "None" },
		"custom launch without command": func(cfg *ImporterConfig) { cfg.GameLaunch = "Custom" },
		"process path":                  func(cfg *ImporterConfig) { cfg.GameProcessExe = `folder\game.exe` },
		"multi-line hotkey":             func(cfg *ImporterConfig) { cfg.Migoto.ToggleInput = "VK_F9\nhunting = 2" },
		"unknown resource tier":         func(cfg *ImporterConfig) { cfg.WWMI.ResourceTier = "4K" },
		"old schema":                    func(cfg *ImporterConfig) { cfg.SchemaVersion = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg, err := DefaultImporterConfig("WWMI", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			mutate(&cfg)
			if err := ValidateImporterSettings("WWMI", cfg); err == nil {
				t.Fatal("invalid config was accepted")
			}
		})
	}
}
