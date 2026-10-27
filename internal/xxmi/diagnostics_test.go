package xxmi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func newDiagnosticsTestService(t *testing.T, ini string) (*XXMI, ImporterConfig) {
	t.Helper()
	return newImporterDiagnosticsTestService(t, "GIMI", ini, func(*ImporterConfig) {})
}

func newImporterDiagnosticsTestService(
	t *testing.T,
	key, ini string,
	edit func(*ImporterConfig),
) (*XXMI, ImporterConfig) {
	t.Helper()
	t.Setenv("USERPROFILE", t.TempDir())
	ctx := context.Background()
	service := NewWithOptions(Options{Elevated: stubLaunchHelper{}})
	service.UseClient(newXXMITestClient(t))
	service.processSnapshot = func(context.Context) (map[string]bool, error) { return map[string]bool{}, nil }
	service.crashEvents = func(context.Context, time.Time, []string) (string, int, error) { return "", 0, nil }
	useBuiltinLauncher(t, service)

	cfg, err := DefaultImporterConfig(key, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Enabled = true
	edit(&cfg)
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, "d3dx.ini"), []byte(ini))
	manifest, err := json.Marshal(runtimeManifest{
		Mode: RuntimeXXMI, Source: "xxmi-libs@1.2.2", Provider: "xxmi-libs-myparsleycat@1.2.2-nhd.13",
	})
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, runtimeManifestName), manifest)
	if err := service.SaveImporterConfig(ctx, key, cfg); err != nil {
		t.Fatal(err)
	}
	return service, cfg
}

func readTestINI(t *testing.T, cfg ImporterConfig) *iniDocument {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, "d3dx.ini"))
	if err != nil {
		t.Fatal(err)
	}
	return parseINI(data)
}

func assertLoggingOptions(t *testing.T, step string, doc *iniDocument, want map[string]string) {
	t.Helper()
	assertINIOptions(t, step, doc, "Logging", want)
}

func assertINIOptions(t *testing.T, step string, doc *iniDocument, section string, want map[string]string) {
	t.Helper()
	for option, value := range want {
		got, ok := doc.Option(section, option)
		switch {
		case value == "" && ok:
			t.Errorf("%s: [%s] %s = %q, want it absent", step, section, option, got)
		case value != "" && got != value:
			t.Errorf("%s: [%s] %s = %q (set %t), want %q", step, section, option, got, ok, value)
		}
	}
}

func TestDiagnosticsSessionSurvivesSavesAndLaunchesAndRestoresTheINI(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t,
		"[Logging]\r\nlog_level = disabled\r\nunbuffered = 0\r\nshow_warnings = 1\r\n")

	if err := service.StartDiagnostics(ctx, "gimi", DiagnosticsOptions{
		Level: DiagnosticsDebug, DebugLocks: true, FrameTimes: true,
	}); err != nil {
		t.Fatal(err)
	}
	session := map[string]string{
		"log_level": "debug", "unbuffered": "1", "crash": "1", "debug_locks": "1",
		"spike_log_threshold": "0", "spike_log_relative": "2",
	}
	assertLoggingOptions(t, "start", readTestINI(t, cfg), session)

	// A settings save and a launch rebuilding the file from the provider's d3dx.ini both write the
	// [Logging] options, and neither may switch the session off.
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	assertLoggingOptions(t, "settings save", readTestINI(t, cfg), session)
	provider := []byte(
		"[Logging]\nlog_level = disabled\nunbuffered = 0\ncrash = 0\ndebug_locks = 0\nshow_warnings = 0\n",
	)
	if err := service.updateLaunchINI(ctx, "GIMI", cfg, "GenshinImpact.exe", provider); err != nil {
		t.Fatal(err)
	}
	assertLoggingOptions(t, "launch", readTestINI(t, cfg), session)

	if err := service.EndDiagnostics(ctx, "GIMI"); err != nil {
		t.Fatal(err)
	}
	assertLoggingOptions(t, "end", readTestINI(t, cfg), map[string]string{
		"log_level": "disabled", "unbuffered": "0", "crash": "", "debug_locks": "",
		"spike_log_threshold": "", "spike_log_relative": "",
	})
	if session, err := service.GetDiagnostics(ctx, "GIMI"); err != nil || session != nil {
		t.Fatalf("session after end = %+v, err = %v", session, err)
	}
}

// A session stored by a version that did not restore the frame time options reads them when it is changed to
// set them, so its end does not leave the game logging frame times.
func TestDiagnosticsSessionReadsOptionsItsStoredRecordLacks(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\nspike_log_relative = 3\r\n")
	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsInfo}); err != nil {
		t.Fatal(err)
	}
	records, err := service.loadDiagnostics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	delete(records["GIMI"].Restore, "spike_log_threshold")
	delete(records["GIMI"].Restore, "spike_log_relative")
	if err := service.storeDiagnostics(ctx, records); err != nil {
		t.Fatal(err)
	}

	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{
		Level: DiagnosticsInfo, FrameTimes: true,
	}); err != nil {
		t.Fatal(err)
	}
	assertLoggingOptions(t, "frame times on", readTestINI(t, cfg), map[string]string{
		"spike_log_threshold": "0", "spike_log_relative": "2",
	})
	if err := service.EndDiagnostics(ctx, "GIMI"); err != nil {
		t.Fatal(err)
	}
	assertLoggingOptions(t, "end", readTestINI(t, cfg), map[string]string{
		"spike_log_threshold": "", "spike_log_relative": "3",
	})
}

func TestDiagnosticsSessionSkipsTheLoggingQuestion(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	cfg.Migoto.LogLevel = "Info"
	if err := service.SaveImporterConfig(ctx, "GIMI", cfg); err != nil {
		t.Fatal(err)
	}
	if err := service.rejectLogging(ctx, "GIMI", false); !errors.Is(err, errLoggingEnabled) {
		t.Fatalf("before the session = %v, want the logging question", err)
	}

	// Turning logging off from the question would leave the session's log level in d3dx.ini.
	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsDebug}); err != nil {
		t.Fatal(err)
	}
	if err := service.rejectLogging(ctx, "GIMI", false); err != nil {
		t.Fatalf("during the session = %v", err)
	}
}

func TestDiagnosticsSessionRestoresTheFolderItStartedOnWhenTheImporterMoves(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\nunbuffered = 0\r\n")
	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsInfo}); err != nil {
		t.Fatal(err)
	}

	moved := cfg
	moved.ImporterFolder = filepath.Join(t.TempDir(), "GIMI")
	writeTestFile(t, filepath.Join(moved.ImporterFolder, "d3dx.ini"),
		[]byte("[Logging]\r\nlog_level = disabled\r\nunbuffered = 7\r\n"))
	if err := service.SaveImporterConfig(ctx, "GIMI", moved); err != nil {
		t.Fatal(err)
	}

	assertLoggingOptions(t, "previous folder", readTestINI(t, cfg), map[string]string{
		"log_level": "disabled", "unbuffered": "0", "crash": "",
	})
	assertLoggingOptions(t, "moved folder", readTestINI(t, moved), map[string]string{
		"log_level": "disabled", "unbuffered": "7", "crash": "",
	})
	if session, err := service.GetDiagnostics(ctx, "GIMI"); err != nil || session != nil {
		t.Fatalf("session after the move = %+v, err = %v", session, err)
	}
}

func TestCollectDiagnosticsBundlesOnlyTheSessionFiles(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	gameFolder := t.TempDir()
	old := filepath.Join(gameFolder, "3DM-20200101000000.dmp")
	writeTestFile(t, old, []byte("old dump"))
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	staleLog := filepath.Join(cfg.ImporterFolder, diagnosticsLogName)
	writeTestFile(t, staleLog, []byte("log of an earlier launch\n"))
	if err := os.Chtimes(staleLog, past, past); err != nil {
		t.Fatal(err)
	}
	service.crashEvents = func(context.Context, time.Time, []string) (string, int, error) {
		return "Event[0]\r\n  Faulting application name: GenshinImpact.exe\r\n", 1, nil
	}

	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{
		Level: DiagnosticsInfo, FrameTimes: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CollectDiagnostics(ctx, "GIMI", 100<<20); err == nil ||
		!strings.Contains(err.Error(), errDiagnosticsEmpty) {
		t.Fatalf("collect without session files: err = %v, want %s", err, errDiagnosticsEmpty)
	}

	if err := service.recordDiagnosticsLaunch(ctx, "GIMI", gameFolder); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, staleLog, []byte("D3D11 DLL starting init\nsession line\n"))
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, diagnosticsSpikeLogName), []byte("frame 1: 40.00ms\n"))
	writeTestFile(t, filepath.Join(gameFolder, "3DM-20991231000000.dmp"), []byte("new dump"))
	bundle, err := service.CollectDiagnostics(ctx, "GIMI", 100<<20)
	if err != nil {
		t.Fatal(err)
	}

	archive, err := zip.OpenReader(bundle.Path)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	want := []string{
		"d3d11_log.txt", "d3d11_spike_log.txt", "d3dx.ini", "dumps/3DM-20991231000000.dmp", "session.json",
		"windows-events.txt",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("bundle entries = %q, want %q", names, want)
	}
	if bundle.Dumps != 1 || bundle.CrashEvents != 1 || bundle.LogTruncated {
		t.Fatalf("bundle = %+v", bundle)
	}

	// The logs are in the bundle, so the game stops logging even if the report is never sent.
	assertLoggingOptions(t, "collect", readTestINI(t, cfg), map[string]string{"log_level": "disabled", "crash": ""})
	session, err := service.GetDiagnostics(ctx, "GIMI")
	if err != nil || session == nil || session.State != DiagnosticsCollected || session.Bundle.Path != bundle.Path {
		t.Fatalf("session after collect = %+v, err = %v", session, err)
	}
	again, err := service.CollectDiagnostics(ctx, "GIMI", 100<<20)
	if err != nil || again != bundle {
		t.Fatalf("collect again = %+v, err = %v, want the same bundle", again, err)
	}

	if err := service.EndDiagnostics(ctx, "GIMI"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundle.Path); !os.IsNotExist(err) {
		t.Fatalf("bundle after end: stat err = %v, want it removed", err)
	}
}

func TestDiagnosticsHashDumpsFollowTheRenderer(t *testing.T) {
	ctx := context.Background()
	service, cfg := newImporterDiagnosticsTestService(t, "ZZMI",
		"[Logging]\r\nlog_level = disabled\r\n[Hunting]\r\nvk_dump_hashes = 0\r\n",
		func(cfg *ImporterConfig) { cfg.SkipD3D11Switch = true })
	support, err := service.DiagnosticsSupport(ctx, "ZZMI")
	if err != nil || !support.Supported || !support.HashDumps {
		t.Fatalf("support with DirectX 12 = %+v, err = %v", support, err)
	}
	if err := service.StartDiagnostics(ctx, "ZZMI", DiagnosticsOptions{
		Level: DiagnosticsInfo, HashDumps: true,
	}); err != nil {
		t.Fatal(err)
	}
	assertINIOptions(t, "start", readTestINI(t, cfg), "Hunting", map[string]string{
		"dx12_dump_hashes": "1", "vk_dump_hashes": "1",
	})

	// A game switched to DirectX 11 has nothing to dump, so applying the session again takes the options out.
	cfg.SkipD3D11Switch = false
	if err := service.SaveImporterConfig(ctx, "ZZMI", cfg); err != nil {
		t.Fatal(err)
	}
	support, err = service.DiagnosticsSupport(ctx, "ZZMI")
	if err != nil || !support.Supported || support.HashDumps {
		t.Fatalf("support with DirectX 11 = %+v, err = %v", support, err)
	}
	if err := service.StartDiagnostics(ctx, "ZZMI", DiagnosticsOptions{
		Level: DiagnosticsInfo, HashDumps: true,
	}); err != nil {
		t.Fatal(err)
	}
	assertINIOptions(t, "DirectX 11", readTestINI(t, cfg), "Hunting", map[string]string{
		"dx12_dump_hashes": "", "vk_dump_hashes": "0",
	})
	if session, err := service.GetDiagnostics(ctx, "ZZMI"); err != nil || session == nil || session.HashDumps {
		t.Fatalf("session with DirectX 11 = %+v, err = %v", session, err)
	}
}

func TestCollectDiagnosticsBundlesSessionHashDumps(t *testing.T) {
	ctx := context.Background()
	service, cfg := newImporterDiagnosticsTestService(t, "ZZMI", "[Logging]\r\nlog_level = disabled\r\n",
		func(cfg *ImporterConfig) { cfg.SkipD3D11Switch = true })
	stale := filepath.Join(cfg.ImporterFolder, "dx12_report.txt")
	writeTestFile(t, stale, []byte("report of an earlier launch\n"))
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}

	if err := service.StartDiagnostics(ctx, "ZZMI", DiagnosticsOptions{
		Level: DiagnosticsInfo, HashDumps: true,
	}); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, diagnosticsLogName), []byte("session line\n"))
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, "vk_draws.csv"), []byte("frame,cmd\n1,2\n"))
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, diagnosticsVKShadersFolder, "0123456789abcdef-vs.spv"),
		[]byte{0x03, 0x02, 0x23, 0x07})
	bundle, err := service.CollectDiagnostics(ctx, "ZZMI", 100<<20)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"d3d11_log.txt", "d3dx.ini", "hash-dumps/vk_draws.csv", "hash-dumps/vk_shaders/0123456789abcdef-vs.spv",
		"session.json",
	}
	if names := diagnosticsBundleEntries(t, bundle.Path); !slices.Equal(names, want) {
		t.Fatalf("bundle entries = %q, want %q", names, want)
	}

	// A bundle over its bound leaves out the shaders before it cuts the log.
	folder := t.TempDir()
	logPath := filepath.Join(folder, diagnosticsLogName)
	writeTestFile(
		t,
		logPath,
		bytes.Repeat([]byte("draw call line\n"), (diagnosticsLogHead+diagnosticsLogMinTail)/15+1024),
	)
	csvPath := filepath.Join(folder, "vk_draws.csv")
	writeTestFile(t, csvPath, []byte("frame,cmd\n1,2\n"))
	shaderPath := filepath.Join(folder, diagnosticsVKShadersFolder, "fedcba9876543210-ps.spv")
	noise := make([]byte, 2<<20)
	_, _ = rand.Read(noise)
	writeTestFile(t, shaderPath, noise)
	file := func(path string) diagnosticsFile {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return diagnosticsFile{path: path, size: info.Size(), modified: info.ModTime()}
	}
	log := file(logPath)
	bound, err := writeDiagnosticsBundle(ctx, filepath.Join(t.TempDir(), "bundle.zip"), "ZZMI",
		diagnosticsRecord{Level: DiagnosticsInfo, HashDumps: true}, diagnosticsSources{
			log: &log, hashDumps: []diagnosticsFile{file(csvPath)}, vkShaders: []diagnosticsFile{file(shaderPath)},
			collectedAt: time.Now(),
		}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if bound.LogTruncated {
		t.Fatalf("bundle over its bound = %+v, want the log whole", bound)
	}
	want = []string{"d3d11_log.txt", "hash-dumps/vk_draws.csv", "session.json"}
	if names := diagnosticsBundleEntries(t, bound.Path); !slices.Equal(names, want) {
		t.Fatalf("bundle over its bound entries = %q, want %q", names, want)
	}
}

func diagnosticsBundleEntries(t *testing.T, path string) []string {
	t.Helper()
	archive, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = archive.Close() }()
	names := make([]string, 0, len(archive.File))
	for _, file := range archive.File {
		names = append(names, file.Name)
	}
	slices.Sort(names)
	return names
}

func TestCollectDiagnosticsLeavesOtherSessionsReadable(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsInfo}); err != nil {
		t.Fatal(err)
	}
	if err := service.recordDiagnosticsLaunch(ctx, "GIMI", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, diagnosticsLogName), []byte("D3D11 DLL starting init\n"))

	// The crash record query stands in for any long step of a collection.
	entered, release := make(chan struct{}), make(chan struct{})
	service.crashEvents = func(context.Context, time.Time, []string) (string, int, error) {
		close(entered)
		<-release
		return "", 0, nil
	}
	collected := make(chan error, 1)
	go func() {
		_, err := service.CollectDiagnostics(ctx, "GIMI", 100<<20)
		collected <- err
	}()
	<-entered

	read := make(chan error, 1)
	go func() {
		_, err := service.GetDiagnostics(ctx, "SRMI")
		read <- err
	}()
	select {
	case err := <-read:
		close(release)
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		close(release)
		t.Fatal("another importer's session read waited for the collection")
	}
	if err := <-collected; err != nil {
		t.Fatal(err)
	}
	if session, err := service.GetDiagnostics(ctx, "GIMI"); err != nil || session == nil ||
		session.State != DiagnosticsCollected {
		t.Fatalf("session after collect = %+v, err = %v", session, err)
	}
}

func TestRestartThatFailsKeepsTheCollectedSession(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsInfo}); err != nil {
		t.Fatal(err)
	}
	if err := service.recordDiagnosticsLaunch(ctx, "GIMI", t.TempDir()); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, diagnosticsLogName), []byte("D3D11 DLL starting init\n"))
	bundle, err := service.CollectDiagnostics(ctx, "GIMI", 100<<20)
	if err != nil {
		t.Fatal(err)
	}

	unlock := lockTestINI(t, cfg)
	err = service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsDebug})
	unlock()
	if err == nil {
		t.Fatal("restart over a locked d3dx.ini succeeded")
	}

	if _, err := os.Stat(bundle.Path); err != nil {
		t.Fatalf("bundle after the failed restart: %v", err)
	}
	session, err := service.GetDiagnostics(ctx, "GIMI")
	if err != nil || session == nil || session.State != DiagnosticsCollected || session.Bundle.Path != bundle.Path {
		t.Fatalf("session after the failed restart = %+v, err = %v", session, err)
	}
}

func TestOptionsChangeThatFailsKeepsTheRunningOptions(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsInfo}); err != nil {
		t.Fatal(err)
	}

	unlock := lockTestINI(t, cfg)
	err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{Level: DiagnosticsDebug, DebugLocks: true})
	unlock()
	if err == nil {
		t.Fatal("options change over a locked d3dx.ini succeeded")
	}

	session, err := service.GetDiagnostics(ctx, "GIMI")
	if err != nil || session == nil || session.Level != DiagnosticsInfo || session.DebugLocks {
		t.Fatalf("session after the failed change = %+v, err = %v", session, err)
	}
	assertLoggingOptions(t, "failed change", readTestINI(t, cfg), map[string]string{
		"log_level": "info", "debug_locks": "",
	})
}

func TestResetBuiltinRuntimeEndsTheDiagnosticsSession(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\nunbuffered = 0\r\n")
	if err := service.StartDiagnostics(ctx, "GIMI", DiagnosticsOptions{
		Level: DiagnosticsDebug, DebugLocks: true,
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.ResetBuiltinRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	assertLoggingOptions(t, "reset", readTestINI(t, cfg), map[string]string{
		"log_level": "disabled", "unbuffered": "0", "crash": "", "debug_locks": "",
	})
	if session, err := service.GetDiagnostics(ctx, "GIMI"); err != nil || session != nil {
		t.Fatalf("session after reset = %+v, err = %v", session, err)
	}
}

func TestListDiagnosticsModsGivesTheFolderBehindAJunction(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	target := filepath.Join(t.TempDir(), "Linked")
	writeTestFile(t, filepath.Join(target, "mod.ini"), []byte("[TextureOverride]\n"))
	if err := os.MkdirAll(filepath.Join(cfg.ImporterFolder, "Mods"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := createJunction(filepath.Join(cfg.ImporterFolder, "Mods", "Linked"), target); err != nil {
		t.Skipf("filesystem cannot create a junction: %v", err)
	}

	mods, err := service.ListDiagnosticsMods(ctx, "GIMI", "")
	if err != nil || len(mods) != 1 || mods[0].Name != "Linked" {
		t.Fatalf("mods = %+v, err = %v", mods, err)
	}
	// A report walks the listed folder, and a walk does not step into a junction at its root.
	var files int
	err = filepath.WalkDir(mods[0].Path, func(_ string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			files++
		}
		return err
	})
	if err != nil || files != 1 {
		t.Fatalf("walk of %s found %d files, err = %v", mods[0].Path, files, err)
	}
}

func TestListDiagnosticsModsMarksFoldersBehindANestedJunction(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	group := filepath.Join(cfg.ImporterFolder, "Mods", "Group")
	target := filepath.Join(t.TempDir(), "Target")
	writeTestFile(t, filepath.Join(group, "Plain", "mod.ini"), nil)
	writeTestFile(t, filepath.Join(target, "Sub", "mod.ini"), nil)
	if err := createJunction(filepath.Join(group, "Linked"), target); err != nil {
		t.Skipf("filesystem cannot create a junction: %v", err)
	}

	// A report attaching Group walks past the junction, so the renderer must not show Linked as carried by it.
	children, err := service.ListDiagnosticsMods(ctx, "GIMI", "Group")
	if err != nil || len(children) != 2 {
		t.Fatalf("children = %+v, err = %v", children, err)
	}
	linkedAt := map[string]string{children[0].Name: children[0].LinkedAt, children[1].Name: children[1].LinkedAt}
	if linkedAt["Linked"] != "Group/Linked" || linkedAt["Plain"] != "" {
		t.Fatalf("linkedAt = %q", linkedAt)
	}
	behind, err := service.ListDiagnosticsMods(ctx, "GIMI", "Group/Linked")
	if err != nil || len(behind) != 1 || behind[0].LinkedAt != "Group/Linked" {
		t.Fatalf("behind = %+v, err = %v", behind, err)
	}
}

func TestListDiagnosticsModsListsSubfoldersBelowTheModsFolder(t *testing.T) {
	ctx := context.Background()
	service, cfg := newDiagnosticsTestService(t, "[Logging]\r\nlog_level = disabled\r\n")
	writeTestFile(t, filepath.Join(cfg.ImporterFolder, "Mods", "DISABLED a", "b", "c", "mod.ini"), nil)

	top, err := service.ListDiagnosticsMods(ctx, "GIMI", "")
	if err != nil || len(top) != 1 || !top[0].HasChildren {
		t.Fatalf("top = %+v, err = %v", top, err)
	}
	// XXMI skips everything below a disabled folder, so a nested folder is disabled too.
	nested, err := service.ListDiagnosticsMods(ctx, "GIMI", "DISABLED a/b")
	if err != nil || len(nested) != 1 {
		t.Fatalf("nested = %+v, err = %v", nested, err)
	}
	if got := nested[0]; got.Relative != "DISABLED a/b/c" || !got.Disabled || got.HasChildren {
		t.Fatalf("nested[0] = %+v", got)
	}

	for _, parent := range []string{"../Mods", "DISABLED a/../../x", filepath.Dir(cfg.ImporterFolder)} {
		if mods, err := service.ListDiagnosticsMods(ctx, "GIMI", parent); err == nil {
			t.Errorf("parent %q listed %+v outside the Mods folder", parent, mods)
		}
	}
}

// lockTestINI keeps d3dx.ini from being replaced until the returned function runs: the handle does not share
// deletion.
func lockTestINI(t *testing.T, cfg ImporterConfig) func() {
	t.Helper()
	ini, err := windows.UTF16PtrFromString(filepath.Join(cfg.ImporterFolder, "d3dx.ini"))
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(ini, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil,
		windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := windows.CloseHandle(handle); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCopyLogWindowKeepsWholeLinesAtBothEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), diagnosticsLogName)
	var log strings.Builder
	for index := range 100 {
		log.WriteString(strings.Repeat("x", 9) + string(rune('0'+index%10)) + "\n")
	}
	writeTestFile(t, path, []byte(log.String()))

	var out bytes.Buffer
	written, omitted, err := copyLogWindow(context.Background(), &out, path, 25, 25)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) < 4 || lines[0] != "xxxxxxxxx0" || lines[1] != "xxxxxxxxx1" ||
		lines[len(lines)-1] != "xxxxxxxxx9" || lines[len(lines)-2] != "xxxxxxxxx8" {
		t.Fatalf("window = %q", out.String())
	}
	if !strings.Contains(out.String(), "bytes of the log left out") || written+omitted != int64(log.Len()) {
		t.Fatalf("written %d + omitted %d != %d: %q", written, omitted, log.Len(), out.String())
	}
}
