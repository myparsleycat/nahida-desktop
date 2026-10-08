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

func TestCollectLaunchBlockers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		importer string
		exe      string
		skipDCR  bool
		fake     fakeLaunch
		want     []error
		wantErr  string
		dcrReads int
		smooth   int
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
			name:     "dcr read failure",
			importer: "GIMI",
			exe:      "GenshinImpact.exe",
			fake:     fakeLaunch{dcrErr: errors.New("registry missing")},
			wantErr:  "dynamic character resolution",
			dcrReads: 1,
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
			got, err := collectLaunchBlockers(t.Context(), tc.importer, tc.exe, !tc.skipDCR, &fake)
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
			service := New()
			service.UseClient(newXXMITestClient(t))
			useExternalLauncher(t, service, root)

			if err := service.rejectLogging(ctx, "SRMI", true); err != nil {
				t.Fatalf("importer without logging = %v", err)
			}
			if err := service.StartGame(ctx, "GIMI"); !errors.Is(err, errLoggingEnabled) {
				t.Fatalf("start error = %v, want the logging question", err)
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

func TestCollectLaunchBlockersHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := collectLaunchBlockers(ctx, "GIMI", "GenshinImpact.exe", true, &fakeLaunch{dcr: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
