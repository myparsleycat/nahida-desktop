package xxmi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
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
	return f.disableDCRErr
}

func (f *fakeLaunch) disableSmoothMotion(context.Context, string) error {
	f.smoothDisabled++
	return f.disableSmoothErr
}

func TestCollectLaunchBlockers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		importer string
		exe      string
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
			got, err := collectLaunchBlockers(t.Context(), tc.importer, tc.exe, &fake)
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
	joined := errors.Join(errGimiDCREnabled, errSmoothMotionEnabled)
	annotated := infra.AnnotateError(joined, infra.Diagnostic{
		Severity: infra.DiagnosticWarn, Operation: "start-game", Stage: "launch-guard",
	})
	message := annotated.Error()
	if !strings.Contains(message, "GIMI_DCR_ENABLED") || !strings.Contains(message, "NVIDIA_SMOOTH_MOTION_ENABLED") {
		t.Fatalf("message = %q", message)
	}
	if !errors.Is(annotated, errGimiDCREnabled) || !errors.Is(annotated, errSmoothMotionEnabled) {
		t.Fatal("annotated error lost a sentinel")
	}
}

func TestCollectLaunchBlockersHonorsCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := collectLaunchBlockers(ctx, "GIMI", "GenshinImpact.exe", &fakeLaunch{dcr: true})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}
