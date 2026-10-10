package xxmi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeLaunch struct {
	dcr              bool
	dcrErr           error
	smooth           bool
	smoothErr        error
	disableDCRErr    error
	disableSmoothErr error
	dcrReads         int
	smoothReads      int
	dcrDisabled      int
	smoothDisabled   int
}

func (f *fakeLaunch) gimiDCREnabled(context.Context) (bool, error) {
	f.dcrReads++
	return f.dcr, f.dcrErr
}

func (f *fakeLaunch) smoothMotionEnabled(context.Context, string) (bool, error) {
	f.smoothReads++
	return f.smooth, f.smoothErr
}

func (f *fakeLaunch) disableGIMIDCR(context.Context) error {
	f.dcrDisabled++
	if f.disableDCRErr == nil {
		f.dcr = false
	}
	return f.disableDCRErr
}

func (f *fakeLaunch) disableSmoothMotion(context.Context, string) error {
	f.smoothDisabled++
	return f.disableSmoothErr
}

func TestRejectLaunchBlockersIgnoresUnreadableSmoothMotion(t *testing.T) {
	t.Parallel()
	service := NewWithOptions(Options{})
	unreadable := errors.New("nvapi failed")

	err := service.rejectLaunchBlockersFrom(
		t.Context(), "WWMI", "Client-Win64-Shipping.exe", true, &fakeLaunch{smoothErr: unreadable},
	)
	if err != nil {
		t.Fatalf("error = %v, want an unreadable smooth motion setting to allow the launch", err)
	}

	err = service.rejectLaunchBlockersFrom(
		t.Context(), "GIMI", "GenshinImpact.exe", true, &fakeLaunch{dcr: true, smoothErr: unreadable},
	)
	if !errors.Is(err, errGimiDCREnabled) {
		t.Fatalf("error = %v, want the DCR blocker kept", err)
	}
}

func TestRejectLaunchBlockersAllowsUnreadableDCR(t *testing.T) {
	t.Parallel()
	var payload map[string]any
	service := NewWithOptions(Options{EventEmit: func(name string, data ...any) {
		if name != "xxmi:launch-progress" || len(data) == 0 {
			return
		}
		if event, ok := data[0].(map[string]any); ok {
			payload = event
		}
	}})

	err := service.rejectLaunchBlockersFrom(
		t.Context(), "GIMI", "GenshinImpact.exe", true,
		&fakeLaunch{dcrErr: errors.New("genshin impact registry key is not found")},
	)
	if err != nil {
		t.Fatalf("error = %v, want an unreadable DCR setting to allow the launch", err)
	}
	if payload["warningCode"] != launchWarningGimiDCRUnreadable || payload["importer"] != "GIMI" {
		t.Fatalf("warning payload = %v", payload)
	}
	if _, ok := payload["stage"]; ok {
		t.Fatalf("warning payload must not report a progress stage: %v", payload)
	}
}

func TestCollectLaunchBlockers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		importer     string
		exe          string
		skipDCR      bool
		fake         fakeLaunch
		want         []error
		wantWarnings []string
		wantErr      string
		dcrReads     int
		smooth       int
	}{
		{
			name:     "gimi with both settings on",
			importer: "GIMI",
			exe:      "GenshinImpact.exe",
			fake:     fakeLaunch{dcr: true, smooth: true},
			want:     []error{errGimiDCREnabled, errSmoothMotionEnabled},
			dcrReads: 1,
			smooth:   1,
		},
		{
			name:     "gimi dcr only",
			importer: "gimi",
			exe:      "GenshinImpact.exe",
			fake:     fakeLaunch{dcr: true},
			want:     []error{errGimiDCREnabled},
			dcrReads: 1,
			smooth:   1,
		},
		{
			name:     "gimi without dcr check",
			importer: "GIMI",
			exe:      "GenshinImpact.exe",
			skipDCR:  true,
			fake:     fakeLaunch{dcr: true, dcrErr: errors.New("registry missing")},
			smooth:   1,
		},
		{
			name:     "other importer ignores dcr",
			importer: "WWMI",
			exe:      "Client-Win64-Shipping.exe",
			fake:     fakeLaunch{dcr: true, smooth: true},
			want:     []error{errSmoothMotionEnabled},
			smooth:   1,
		},
		{
			name:     "explicit off is not a blocker",
			importer: "SRMI",
			exe:      "StarRail.exe",
			dcrReads: 0,
			smooth:   1,
		},
		{
			name:     "missing executable skips smooth motion",
			importer: "ZZMI",
			dcrReads: 0,
		},
		{
			name:         "dcr read failure warns and keeps the smooth motion blocker",
			importer:     "GIMI",
			exe:          "GenshinImpact.exe",
			fake:         fakeLaunch{dcrErr: errors.New("registry missing"), smooth: true},
			want:         []error{errSmoothMotionEnabled},
			wantWarnings: []string{launchWarningGimiDCRUnreadable},
			dcrReads:     1,
			smooth:       1,
		},
		{
			name:     "smooth motion read failure",
			importer: "WWMI",
			exe:      "Client-Win64-Shipping.exe",
			fake:     fakeLaunch{smoothErr: errors.New("nvapi failed")},
			wantErr:  "smooth motion",
			smooth:   1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := tc.fake
			got, warnings, err := collectLaunchBlockers(t.Context(), tc.importer, tc.exe, !tc.skipDCR, &fake)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tc.wantErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("blockers = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if !errors.Is(got[i], tc.want[i]) {
					t.Fatalf("blocker %d = %v, want %v", i, got[i], tc.want[i])
				}
			}
			if len(warnings) != len(tc.wantWarnings) {
				t.Fatalf("warnings = %v, want %v", warnings, tc.wantWarnings)
			}
			for i := range tc.wantWarnings {
				if warnings[i].code != tc.wantWarnings[i] {
					t.Fatalf("warning %d = %q, want %q", i, warnings[i].code, tc.wantWarnings[i])
				}
			}
			if fake.dcrReads != tc.dcrReads {
				t.Fatalf("dcr reads = %d, want %d", fake.dcrReads, tc.dcrReads)
			}
			if fake.smoothReads != tc.smooth {
				t.Fatalf("smooth motion reads = %d, want %d", fake.smoothReads, tc.smooth)
			}
		})
	}
}

func TestApplyLaunchFixesClearsOnlyActiveBlockers(t *testing.T) {
	t.Parallel()
	fake := &fakeLaunch{dcr: true, smooth: true}
	if err := applyLaunchFixes(t.Context(), "GIMI", "GenshinImpact.exe", fake); err != nil {
		t.Fatal(err)
	}
	if fake.dcrDisabled != 1 || fake.smoothDisabled != 1 {
		t.Fatalf("disabled dcr=%d smooth=%d", fake.dcrDisabled, fake.smoothDisabled)
	}

	smoothOnly := &fakeLaunch{dcr: true, smooth: true}
	if err := applyLaunchFixes(t.Context(), "WWMI", "Client-Win64-Shipping.exe", smoothOnly); err != nil {
		t.Fatal(err)
	}
	if smoothOnly.dcrDisabled != 0 || smoothOnly.smoothDisabled != 1 {
		t.Fatalf("wwmi disabled dcr=%d smooth=%d", smoothOnly.dcrDisabled, smoothOnly.smoothDisabled)
	}

	unreadable := &fakeLaunch{dcrErr: errors.New("registry missing"), smooth: true}
	if err := applyLaunchFixes(t.Context(), "GIMI", "GenshinImpact.exe", unreadable); err != nil {
		t.Fatal(err)
	}
	if unreadable.dcrDisabled != 0 || unreadable.smoothDisabled != 1 {
		t.Fatalf("unreadable dcr disabled dcr=%d smooth=%d", unreadable.dcrDisabled, unreadable.smoothDisabled)
	}

	idle := &fakeLaunch{}
	if err := applyLaunchFixes(t.Context(), "GIMI", "GenshinImpact.exe", idle); err != nil {
		t.Fatal(err)
	}
	if idle.dcrDisabled != 0 || idle.smoothDisabled != 0 {
		t.Fatalf("idle disabled dcr=%d smooth=%d", idle.dcrDisabled, idle.smoothDisabled)
	}
}

func TestApplyLaunchFixesStopsAfterDisableError(t *testing.T) {
	t.Parallel()
	fake := &fakeLaunch{dcr: true, smooth: true, disableDCRErr: errors.New("registry write failed")}
	err := applyLaunchFixes(t.Context(), "GIMI", "GenshinImpact.exe", fake)
	if err == nil || !strings.Contains(err.Error(), "registry write failed") {
		t.Fatalf("error = %v", err)
	}
	if fake.smoothDisabled != 0 {
		t.Fatalf("smooth motion disable ran after dcr failure")
	}
}

func TestLaunchBlockerErrorTextKeepsBothCodes(t *testing.T) {
	t.Parallel()
	err := NewWithOptions(Options{}).rejectLaunchBlockersFrom(
		t.Context(), "GIMI", "GenshinImpact.exe", true, &fakeLaunch{dcr: true, smooth: true},
	)
	if err == nil {
		t.Fatal("launch with both blockers active was allowed")
	}
	message := err.Error()
	if !strings.Contains(message, "GIMI_DCR_ENABLED") || !strings.Contains(message, "NVIDIA_SMOOTH_MOTION_ENABLED") {
		t.Fatalf("message = %q", message)
	}
	if !errors.Is(err, errGimiDCREnabled) || !errors.Is(err, errSmoothMotionEnabled) {
		t.Fatal("rejection lost a sentinel")
	}
}

func TestExternalLoggingEnabled(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		migoto map[string]any
		want   bool
	}{
		{"missing section", nil, false},
		{"level disabled", map[string]any{"log_level": "DISABLED"}, false},
		{"level warning", map[string]any{"log_level": "WARNING"}, false},
		{"level info", map[string]any{"log_level": "INFO"}, true},
		{"level debug", map[string]any{"log_level": "debug"}, true},
		{"level wins over old switches", map[string]any{"log_level": "DISABLED", "debug_logging": true}, false},
		{"old switches off", map[string]any{"calls_logging": false, "debug_logging": false}, false},
		{"old calls switch", map[string]any{"calls_logging": true, "debug_logging": false}, true},
		{"old debug switch", map[string]any{"calls_logging": false, "debug_logging": true}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := externalLoggingEnabled(tc.migoto); got != tc.want {
				t.Fatalf("enabled = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestBuiltinLaunchAsksAboutLoggingUntilDisabled(t *testing.T) {
	ctx := t.Context()
	client := newXXMITestClient(t)
	service := New()
	service.UseClient(client)
	// The stored configs have no mode, so saving one counts as a mode change and looks for the game.
	service.findProcess = noGameProcess
	useBuiltinLauncher(t, service)
	root := t.TempDir()
	if err := client.Settings.Upsert(ctx, "xxmi_root", &root); err != nil {
		t.Fatal(err)
	}
	store := func(config string) {
		t.Helper()
		if err := client.XXMIImporters.Upsert(ctx, "GIMI", config); err != nil {
			t.Fatal(err)
		}
	}

	// An importer that is not set up fails the launch on its own, without the logging question.
	store(`{"schemaVersion":2,"enabled":false,"migoto":{"logLevel":"Debug"}}`)
	if err := service.rejectLogging(ctx, "GIMI", false); err != nil {
		t.Fatalf("disabled importer = %v", err)
	}
	// Logging settings do not reach the game when the XXMI DLL is left out.
	store(`{"schemaVersion":2,"enabled":true,"xxmiDLLInjectMode":"Bypass","migoto":{"logLevel":"Debug"}}`)
	if err := service.rejectLogging(ctx, "GIMI", false); err != nil {
		t.Fatalf("launch without the XXMI DLL = %v", err)
	}

	// A launch that will stop at the missing importer files must not offer to save the config first.
	store(`{"schemaVersion":2,"enabled":true,"migoto":{"logLevel":"Debug"}}`)
	if err := service.rejectLogging(ctx, "GIMI", false); err != nil {
		t.Fatalf("importer without its files = %v", err)
	}
	cfg, err := service.GetImporterConfig(ctx, "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cfg.ImporterFolder, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.ImporterFolder, "d3dx.ini"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	store(`{"schemaVersion":2,"enabled":true,"migoto":{"logLevel":"Warning"}}`)
	if err := service.rejectLogging(ctx, "GIMI", false); err != nil {
		t.Fatalf("warnings only = %v", err)
	}

	store(`{"schemaVersion":2,"enabled":true,"migoto":{"logLevel":"Info"}}`)
	err = service.rejectLogging(ctx, "GIMI", false)
	if !errors.Is(err, errLoggingEnabled) || !strings.Contains(err.Error(), "XXMI_LOGGING_ENABLED") {
		t.Fatalf("error = %v, want the logging question", err)
	}
	if err := service.DisableLogging(ctx, " gimi "); err != nil {
		t.Fatal(err)
	}
	cfg, err = service.GetImporterConfig(ctx, "GIMI")
	if err != nil || cfg.Migoto.LogLevel != "Disabled" || !cfg.Enabled {
		t.Fatalf("config after disabling = %+v, %v", cfg.Migoto, err)
	}
	if err := service.rejectLogging(ctx, "GIMI", false); err != nil {
		t.Fatalf("after disabling = %v", err)
	}
}

func TestExternalLaunchAsksAboutLoggingUntilDisabled(t *testing.T) {
	ctx := t.Context()
	for _, tc := range []struct {
		name  string
		apply func(migoto map[string]any)
		check func(migoto map[string]any) bool
	}{
		{
			name:  "log level",
			apply: func(migoto map[string]any) { migoto["log_level"] = "INFO" },
			check: func(migoto map[string]any) bool { return migoto["log_level"] == "DISABLED" },
		},
		{
			name:  "old switches",
			apply: func(migoto map[string]any) { migoto["debug_logging"] = true },
			check: func(migoto map[string]any) bool {
				_, modern := migoto["log_level"]
				return !modern && migoto["calls_logging"] == false && migoto["debug_logging"] == false
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			config := xxmiTestConfig()
			migoto := config["Importers"].(map[string]any)["GIMI"].(map[string]any)["Migoto"].(map[string]any)
			tc.apply(migoto)
			if _, modern := migoto["log_level"]; modern {
				delete(migoto, "calls_logging")
				delete(migoto, "debug_logging")
			}
			if err := writeXXMIConfig(filepath.Join(root, xxmiConfigName), config); err != nil {
				t.Fatal(err)
			}
			client := newXXMITestClient(t)
			service := New()
			service.UseClient(client)
			useExternalLauncher(t, service, root)
			service.launchSettings = &fakeLaunch{}

			if err := service.rejectLogging(ctx, "SRMI", true); err != nil {
				t.Fatalf("importer without logging = %v", err)
			}
			if err := service.StartGame(ctx, "GIMI"); !errors.Is(err, errLoggingEnabled) {
				t.Fatalf("start error = %v, want the logging question", err)
			}
			// With its guard off the launch goes on to the launcher, which this fixture does not have.
			for _, tc := range []struct {
				guard string
				asked bool
			}{{"false", false}, {"true", true}} {
				if err := client.Settings.Upsert(ctx, launchGuardLoggingKey, &tc.guard); err != nil {
					t.Fatal(err)
				}
				err := service.StartGame(ctx, "GIMI")
				if err == nil || errors.Is(err, errLoggingEnabled) != tc.asked {
					t.Fatalf("logging guard %s: start error = %v", tc.guard, err)
				}
			}
			if err := service.DisableLogging(ctx, "gimi"); err != nil {
				t.Fatal(err)
			}
			launcher, err := service.requireExternalLauncher(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if stored := launcher.migoto("GIMI"); !tc.check(stored) {
				t.Fatalf("stored Migoto section = %v", stored)
			}
			if err := service.rejectLogging(ctx, "GIMI", true); err != nil {
				t.Fatalf("after disabling = %v", err)
			}
		})
	}
}

func TestDisabledLaunchGuardsLeaveSettingsAlone(t *testing.T) {
	ctx := t.Context()
	client := newXXMITestClient(t)
	service := New()
	service.UseClient(client)
	store := func(key, value string) {
		t.Helper()
		if err := client.Settings.Upsert(ctx, key, &value); err != nil {
			t.Fatal(err)
		}
	}

	guards, err := service.launchGuards(ctx)
	if err != nil || guards != (launchGuards{dcr: true, smoothMotion: true, logging: true}) {
		t.Fatalf("default guards = %+v, %v", guards, err)
	}

	store(launchGuardTexturesKey, "true")
	store(launchGuardDCRKey, "false")
	guards, err = service.launchGuards(ctx)
	if err != nil || guards != (launchGuards{smoothMotion: true, logging: true, textures: true}) {
		t.Fatalf("guards without DCR = %+v, %v", guards, err)
	}
	fake := &fakeLaunch{dcr: true, smooth: true}
	if err := applyLaunchFixes(ctx, "GIMI", "GenshinImpact.exe", guards.settings(fake)); err != nil {
		t.Fatal(err)
	}
	if fake.dcrReads != 0 || fake.dcrDisabled != 0 || fake.smoothDisabled != 1 {
		t.Fatalf("settings touched without the DCR guard: %+v", fake)
	}

	store(launchGuardKey, "false")
	guards, err = service.launchGuards(ctx)
	if err != nil || guards != (launchGuards{}) {
		t.Fatalf("guards with the launch guard off = %+v, %v", guards, err)
	}
	fake = &fakeLaunch{dcr: true, smooth: true}
	err = service.rejectLaunchBlockersFrom(ctx, "GIMI", "GenshinImpact.exe", true, guards.settings(fake))
	if err != nil || fake.dcrReads != 0 || fake.smoothReads != 0 {
		t.Fatalf("launch with the launch guard off = %v, settings %+v", err, fake)
	}
}

func TestCollectLaunchBlockersHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, _, err := collectLaunchBlockers(ctx, "GIMI", "GenshinImpact.exe", true, &fakeLaunch{dcr: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestBuiltinLaunchAllowsSmoothMotionWithSupportingProviderDLL(t *testing.T) {
	ctx := context.Background()
	supporting, older := testCustomDLLImage("1.2.2-nhd.3"), testCustomDLLImage("1.2.2-nhd.2")
	service, _, _ := newProviderTestService(t, []providerTestRelease{
		{tag: "v1.2.2-nhd.3", data: supporting, digest: providerTestDigest(supporting)},
		{tag: "v1.2.2-nhd.2", data: older, digest: providerTestDigest(older)},
	})
	service.launchSettings = &fakeLaunch{smooth: true}
	cfg, err := DefaultImporterConfig("WWMI", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true

	for _, tc := range []struct {
		name          string
		provider      string
		pin           VersionPin
		migotoDLLUsed bool
		blocked       bool
	}{
		{"first supporting release", "myparsleycat", VersionPin{Pinned: "1.2.2-nhd.3"}, true, false},
		{"latest release", "myparsleycat", VersionPin{Follow: "latest"}, true, false},
		{"release before support", "myparsleycat", VersionPin{Pinned: "1.2.2-nhd.2"}, true, true},
		{"signed libraries", defaultLibsProvider, VersionPin{Follow: "latest"}, true, true},
		{"launch without the XXMI DLL", "myparsleycat", VersionPin{Follow: "latest"}, false, true},
	} {
		cfg.LibsProvider, cfg.XXMIVersion = tc.provider, tc.pin
		settings := service.builtinLaunchSettings(ctx, cfg, tc.migotoDLLUsed)
		err := service.rejectLaunchBlockersFrom(ctx, "WWMI", "Client-Win64-Shipping.exe", false, settings)
		if blocked := errors.Is(err, errSmoothMotionEnabled); blocked != tc.blocked || err != nil && !blocked {
			t.Fatalf("%s: error = %v, want blocked = %t", tc.name, err, tc.blocked)
		}
	}

	// Unsafe mode keeps a DLL found in a folder nothing was deployed to yet, so the supporting release the
	// importer is set to is not the one the game loads.
	cfg.LibsProvider, cfg.XXMIVersion = "myparsleycat", VersionPin{Pinned: "1.2.2-nhd.3"}
	cfg.Migoto.UnsafeMode = true
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, customDLLName), older)
	settings := service.builtinLaunchSettings(ctx, cfg, true)
	err = service.rejectLaunchBlockersFrom(ctx, "WWMI", "Client-Win64-Shipping.exe", false, settings)
	if !errors.Is(err, errSmoothMotionEnabled) {
		t.Fatalf("kept DLL without a manifest: error = %v, want the smooth motion blocker", err)
	}
}
