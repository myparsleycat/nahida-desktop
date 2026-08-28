package tools

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/pepad"
	"nahida.live/desktop/internal/platform"
)

type toolsRoundTripFunc func(*http.Request) (*http.Response, error)

func (f toolsRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestPEDiversifierInProcessContract(t *testing.T) {
	root := t.TempDir()
	input, output := filepath.Join(root, "input.dll"), filepath.Join(root, "output.dll")
	if err := os.WriteFile(input, pepad.MinimalDLL(pepad.RetThenInt3Padding(64)), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	report, err := service.runPEDiversifier(context.Background(), input, output)
	if err != nil {
		t.Fatal(err)
	}
	if report.DiscoveredRegions == 0 || report.ModifiedRegions == 0 || len(report.Patches) == 0 || report.OutputSHA256 == nil {
		t.Fatalf("report = %#v", report)
	}
	outputHash, err := hashFile(output)
	if err != nil || outputHash != *report.OutputSHA256 || outputHash == report.InputSHA256 {
		t.Fatalf("output hash/report = %q, %#v, %v", outputHash, report, err)
	}
}

func TestFourThousandOneFixerReleaseCacheUsesProcessLifetimeAndRefreshCooldown(t *testing.T) {
	var requests atomic.Int32
	httpClient := &http.Client{Transport: toolsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests.Add(1)
		if request.URL.Path != "/repos/SpectrumQT/XXMI-Libs-Package/releases" {
			t.Fatalf("URL = %s", request.URL)
		}
		if !strings.Contains(request.Header.Get("User-Agent"), "Chrome/138.0.0.0") {
			t.Fatalf("User-Agent = %q", request.Header.Get("User-Agent"))
		}
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`[{"tag_name":"v2"},{"tag_name":"main"},{"tag_name":"v1"}]`)), Request: request,
		}, nil
	})}
	service := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})})
	for range 2 {
		got, err := service.FourThousandOneFixerGetProviderReleases(context.Background(), "SpectrumQT")
		if err != nil || strings.Join(got, ",") != "v2,v1" {
			t.Fatalf("releases = %v, %v", got, err)
		}
	}
	service.fixerMu.Lock()
	entry := service.releaseCache["SpectrumQT"]
	entry.fetched = time.Now().Add(-2 * time.Minute)
	service.releaseCache["SpectrumQT"] = entry
	service.fixerMu.Unlock()
	if _, err := service.FourThousandOneFixerGetProviderReleases(context.Background(), "SpectrumQT"); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 {
		t.Fatalf("get refetched process cache: %d", requests.Load())
	}
	if err := service.FourThousandOneFixerUpdateReleases(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("refresh after cooldown requests = %d, want 2", requests.Load())
	}
	if err := service.FourThousandOneFixerUpdateReleases(context.Background()); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatalf("refresh ignored cooldown: %d", requests.Load())
	}
}

func TestFourThousandOneFixerReleaseCacheDeduplicatesInFlightFetch(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var requests atomic.Int32
	httpClient := &http.Client{Transport: toolsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		if requests.Add(1) == 1 {
			close(started)
		}
		<-release
		return &http.Response{
			StatusCode: http.StatusOK, Status: "200 OK", Header: make(http.Header),
			Body: io.NopCloser(strings.NewReader(`[{"tag_name":"v1"}]`)), Request: request,
		}, nil
	})}
	service := NewWithOptions(Options{HTTP: infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: httpClient, Status: infra.BackendOnline})})
	results := make(chan error, 2)
	go func() {
		_, err := service.FourThousandOneFixerGetProviderReleases(context.Background(), "SpectrumQT")
		results <- err
	}()
	<-started
	go func() {
		_, err := service.FourThousandOneFixerGetProviderReleases(context.Background(), "SpectrumQT")
		results <- err
	}()
	close(release)
	for range 2 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if requests.Load() != 1 {
		t.Fatalf("requests = %d, want 1", requests.Load())
	}
}

func TestFourThousandOneFixerInstallErrorClassification(t *testing.T) {
	service := New()
	service.failed4001Install(syscall.Errno(32), filepath.Join(t.TempDir(), targetD3D11DLL), "XXMI_ERR_BUILD_FAILED")
	if got := service.FourThousandOneFixerGetState().Progress; got != "XXMI_ERR_DLL_IN_USE" {
		t.Fatalf("sharing violation code = %q", got)
	}
	service.failed4001Install(elevatedFileCopyError{err: errors.New("elevated failed")}, "target", "XXMI_ERR_BUILD_FAILED")
	if got := service.FourThousandOneFixerGetState().Progress; got != "XXMI_ERR_ELEVATION_FAILED" {
		t.Fatalf("elevated code = %q", got)
	}
	service.failed4001Install(errors.New("copy failed"), "target", "XXMI_ERR_BUILD_FAILED")
	if got := service.FourThousandOneFixerGetState().Progress; got != "XXMI_ERR_BUILD_FAILED" {
		t.Fatalf("fallback code = %q", got)
	}
}

type fakePEDiversifier struct {
	noCandidates bool
}

func (f fakePEDiversifier) Diversify(_ context.Context, input, output string) (PEDiversificationReport, error) {
	before, err := os.ReadFile(input)
	if err != nil {
		return PEDiversificationReport{}, err
	}
	inputHash, err := hashFile(input)
	if err != nil {
		return PEDiversificationReport{}, err
	}
	if f.noCandidates {
		return PEDiversificationReport{InputSHA256: inputHash}, nil
	}
	after := append(append([]byte(nil), before...), byte(0xcc))
	if err := os.WriteFile(output, after, 0o600); err != nil {
		return PEDiversificationReport{}, err
	}
	outputHash, err := hashFile(output)
	if err != nil {
		return PEDiversificationReport{}, err
	}
	return PEDiversificationReport{
		DiscoveredRegions: 1, ModifiedRegions: 1, InputSHA256: inputHash,
		OutputSHA256: stringPointer(outputHash), Patches: []PEDiversifierPatch{{CandidateID: 0}},
	}, nil
}

func TestFourThousandOneFixerDiversifiesAndRestoresThroughNativeBoundary(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	importerPath := t.TempDir()
	target := filepath.Join(importerPath, targetD3D11DLL)
	original := []byte("original PE fixture")
	if err := os.WriteFile(target, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{PEDiversifier: fakePEDiversifier{}})
	service.UseClient(client)
	result := service.FourThousandOneFixerDiversifyDllPadding(ctx, Fixer4001ImporterInput{ImporterKey: "GIMI", ImporterPath: &importerPath})
	if !result.Success || result.BackupPath == nil {
		t.Fatalf("diversify result = %#v", result)
	}
	diversified, err := os.ReadFile(target)
	if err != nil || string(diversified) != string(append(append([]byte(nil), original...), 0xcc)) {
		t.Fatalf("diversified target = %q, %v", diversified, err)
	}
	backup, err := os.ReadFile(*result.BackupPath)
	if err != nil || string(backup) != string(original) {
		t.Fatalf("backup = %q, %v", backup, err)
	}
	state, err := service.FourThousandOneFixerGetDiversificationState(Fixer4001PathInput{ImporterPath: &importerPath})
	if err != nil || !state.HasBackup || state.BackupPath == nil || *state.BackupPath != *result.BackupPath {
		t.Fatalf("diversification state = %#v, %v", state, err)
	}
	restored := service.FourThousandOneFixerRestoreDiversifiedDll(ctx, Fixer4001PathInput{ImporterPath: &importerPath})
	if !restored.Success {
		t.Fatalf("restore result = %#v", restored)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(original) {
		t.Fatalf("restored target = %q, %v", got, err)
	}
}

func TestFourThousandOneFixerDiversifierNoCandidates(t *testing.T) {
	t.Parallel()
	importerPath := t.TempDir()
	if err := os.WriteFile(filepath.Join(importerPath, targetD3D11DLL), []byte("PE"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{PEDiversifier: fakePEDiversifier{noCandidates: true}})
	result := service.FourThousandOneFixerDiversifyDllPadding(context.Background(), Fixer4001ImporterInput{ImporterPath: &importerPath})
	if result.Success || service.FourThousandOneFixerGetState().Progress != "XXMI_ERR_OBFUSCATE_NO_CANDIDATES" {
		t.Fatalf("result/state = %#v / %#v", result, service.FourThousandOneFixerGetState())
	}
}

func TestFourThousandOneFixerDiversifyRejectsExistingBackup(t *testing.T) {
	importerPath := t.TempDir()
	target := filepath.Join(importerPath, targetD3D11DLL)
	current := []byte("already diversified")
	if err := os.WriteFile(target, current, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := hashFile(target)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(importerPath, diversifierBackupPre+hash[:7]+"-1234.bak")
	if err := os.WriteFile(backup, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}

	service := NewWithOptions(Options{PEDiversifier: fakePEDiversifier{}})
	result := service.FourThousandOneFixerDiversifyDllPadding(context.Background(), Fixer4001ImporterInput{ImporterPath: &importerPath})
	if result.Success || result.BackupPath == nil || *result.BackupPath != backup {
		t.Fatalf("diversify result = %#v", result)
	}
	if state := service.FourThousandOneFixerGetState(); state.Progress != "XXMI_OBFUSCATE_BACKUP_EXISTS" {
		t.Fatalf("diversify state = %#v", state)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(current) {
		t.Fatalf("target = %q, %v", got, err)
	}
	entries, err := filepath.Glob(filepath.Join(importerPath, diversifierBackupPre+"*.bak"))
	if err != nil || len(entries) != 1 || entries[0] != backup {
		t.Fatalf("backups = %#v, %v", entries, err)
	}
}

func TestFourThousandOneFixerBackupStateAndRestore(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	importerPath := t.TempDir()
	target := filepath.Join(importerPath, targetD3D11DLL)
	diversified := []byte("diversified-dll")
	original := []byte("original-dll")
	if err := os.WriteFile(target, diversified, 0o600); err != nil {
		t.Fatal(err)
	}
	hash, err := hashFile(target)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(importerPath, diversifierBackupPre+hash[:7]+"-1234.bak")
	if err := os.WriteFile(backup, original, 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var progress []Fixer4001ProgressEvent
	service := NewWithOptions(Options{
		FS: platform.NewFS(),
		EventEmit: func(name string, data ...any) {
			if name == fixer4001Event && len(data) == 1 {
				if event, ok := data[0].(Fixer4001ProgressEvent); ok {
					mu.Lock()
					progress = append(progress, event)
					mu.Unlock()
				}
			}
		},
	})
	service.UseClient(client)
	state, err := service.FourThousandOneFixerGetDiversificationState(Fixer4001PathInput{ImporterPath: &importerPath})
	if err != nil || !state.HasBackup || state.BackupPath == nil || *state.BackupPath != backup {
		t.Fatalf("diversification state = %#v, %v", state, err)
	}
	result := service.FourThousandOneFixerRestoreDiversifiedDll(ctx, Fixer4001PathInput{ImporterPath: &importerPath})
	if !result.Success || result.BackupPath == nil || *result.BackupPath != backup {
		t.Fatalf("restore result = %#v", result)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != string(original) {
		t.Fatalf("restored target = %q, %v", got, err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("backup remains after restore: %v", err)
	}
	stateAfter := service.FourThousandOneFixerGetState()
	if stateAfter.IsBuilding || stateAfter.ActiveTask != nil || stateAfter.Progress != "XXMI_RESTORE_SUCCESS" {
		t.Fatalf("state after restore = %#v", stateAfter)
	}
	if len(progress) < 3 || progress[len(progress)-1].Code != "XXMI_RESTORE_SUCCESS" {
		t.Fatalf("progress = %#v", progress)
	}
}

func TestFindDiversifierBackupRemovesStaleHash(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, targetD3D11DLL)
	if err := os.WriteFile(target, []byte("current"), 0o600); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(root, diversifierBackupPre+"1234567-1.bak")
	if err := os.WriteFile(stale, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	backup, err := service.findDiversifierBackup(root)
	if err != nil || backup != nil {
		t.Fatalf("findDiversifierBackup = %v, %v", backup, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale backup remains: %v", err)
	}
}

func TestCleanupStaleD3DBuildsUsesValidatedStateKey(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	service := New()
	service.UseClient(client)
	validID := "valid_build-1"
	validDir := filepath.Join(os.TempDir(), d3dBuildTempDirName, validID)
	if err := os.MkdirAll(validDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(validDir) })
	for _, key := range []string{d3dBuildStatePrefix + validID, d3dBuildStatePrefix + "../outside"} {
		if err := client.AppState.Upsert(ctx, key, `{}`, time.Now().UTC().Format(time.RFC3339)); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.CleanupStaleD3DBuilds(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(validDir); !os.IsNotExist(err) {
		t.Fatalf("valid stale directory remains: %v", err)
	}
	states, err := client.AppState.ListByPrefix(ctx, d3dBuildStatePrefix)
	if err != nil || len(states) != 0 {
		t.Fatalf("stale states = %#v, %v", states, err)
	}
}
