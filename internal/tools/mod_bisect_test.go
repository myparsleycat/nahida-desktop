package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
)

type bisectTestSettings struct {
	preserve bool
	style    string
}

func (s bisectTestSettings) GetBisectPreserveD3dx(context.Context) (bool, error) {
	return s.preserve, nil
}

func (s bisectTestSettings) GetDisabledPrefixStyle(context.Context) (string, error) {
	if s.style == "" {
		return "space", nil
	}
	return s.style, nil
}

func newBisectTestService(t *testing.T, root string) (*Tools, *db.Client, *[]BisectSnapshot) {
	t.Helper()
	client := openToolsTestDB(t)
	var eventMu sync.Mutex
	events := []BisectSnapshot{}
	service := NewWithOptions(Options{
		Settings: bisectTestSettings{style: "underscore"},
		EventEmit: func(name string, data ...any) {
			if name != bisectStateEvent || len(data) != 1 {
				return
			}
			snapshot, ok := data[0].(BisectSnapshot)
			if !ok {
				return
			}
			eventMu.Lock()
			events = append(events, snapshot)
			eventMu.Unlock()
		},
	})
	service.UseClient(client)
	useToolsTestAppData(t, service, t.TempDir())
	if err := client.GamePaths.Insert(context.Background(), db.GamePathRow{Game: "test", ModFolderPath: root}); err != nil {
		t.Fatalf("insert game: %v", err)
	}
	return service, client, &events
}

func writeBisectINIs(t *testing.T, root string, count int) []string {
	t.Helper()
	paths := make([]string, count)
	for i := range count {
		dir := filepath.Join(root, string(rune('a'+i)))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		paths[i] = filepath.Join(dir, "mod.ini")
		if err := os.WriteFile(paths[i], []byte("[TextureOverride]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return paths
}

func TestBisectFindsAndKeepsCulpritDisabled(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	paths := writeBisectINIs(t, root, 4)
	service, _, events := newBisectTestService(t, root)

	first, err := service.BisectStart(ctx, "test", nil)
	if err != nil {
		t.Fatalf("BisectStart: %v", err)
	}
	if first.Status != BisectRound || first.Round != 1 || len(first.CurrentBatch) != 2 {
		t.Fatalf("first snapshot = %#v", first)
	}
	for _, path := range first.CurrentBatch {
		if _, err := os.Stat(bisectDisabledPath(path)); err != nil {
			t.Fatalf("batch file was not disabled: %v", err)
		}
	}

	second, err := service.BisectRespond(ctx, true)
	if err != nil {
		t.Fatalf("first respond: %v", err)
	}
	if second.Round != 2 || len(second.CurrentBatch) != 1 {
		t.Fatalf("second snapshot = %#v", second)
	}
	done, err := service.BisectRespond(ctx, true)
	if err != nil {
		t.Fatalf("second respond: %v", err)
	}
	if done.Status != BisectDone || done.FinalBadPath == nil || *done.FinalBadPath != paths[0] {
		t.Fatalf("done snapshot = %#v", done)
	}

	idle, err := service.BisectFinalize(ctx, []string{*done.FinalBadPath})
	if err != nil {
		t.Fatalf("BisectFinalize: %v", err)
	}
	if idle.Status != BisectIdle || service.BisectGetState() != nil {
		t.Fatalf("idle snapshot/state = %#v / %#v", idle, service.BisectGetState())
	}
	kept := filepath.Join(filepath.Dir(paths[0]), "DISABLED_"+filepath.Base(paths[0]))
	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("culprit was not kept disabled: %v", err)
	}
	for _, path := range paths[1:] {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("non-culprit was not restored: %s: %v", path, err)
		}
	}
	if len(*events) < 6 || (*events)[len(*events)-1].Status != BisectIdle {
		t.Fatalf("bisect events = %#v", *events)
	}
}

func TestBisectFinalizeAcceptsExistingKeepDisabledTarget(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBisectINIs(t, root, 2)
	service, _, _ := newBisectTestService(t, root)

	if _, err := service.BisectStart(ctx, "test", nil); err != nil {
		t.Fatalf("BisectStart: %v", err)
	}
	done, err := service.BisectRespond(ctx, true)
	if err != nil {
		t.Fatalf("BisectRespond: %v", err)
	}
	if done.Status != BisectDone || done.FinalBadPath == nil {
		t.Fatalf("done snapshot = %#v", done)
	}

	target := filepath.Join(filepath.Dir(*done.FinalBadPath), "DISABLED_"+filepath.Base(*done.FinalBadPath))
	const existing = "pre-existing disabled ini"
	if err := os.WriteFile(target, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	idle, err := service.BisectFinalize(ctx, []string{*done.FinalBadPath})
	if err != nil {
		t.Fatalf("BisectFinalize: %v", err)
	}
	if idle.Status != BisectIdle || service.BisectGetState() != nil {
		t.Fatalf("idle snapshot/state = %#v / %#v", idle, service.BisectGetState())
	}
	contents, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != existing {
		t.Fatalf("existing target was overwritten: %q", contents)
	}
}

func TestBisectCancelAndRecoverOrphans(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	paths := writeBisectINIs(t, root, 3)
	service, _, _ := newBisectTestService(t, root)

	if _, err := service.BisectStart(ctx, "test", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BisectCancel(ctx); err != nil {
		t.Fatal(err)
	}
	for _, path := range paths {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("cancel did not restore %s: %v", path, err)
		}
	}

	orphan := bisectDisabledPath(paths[0])
	if err := os.Rename(paths[0], orphan); err != nil {
		t.Fatal(err)
	}
	recovered, err := service.BisectRecover(ctx, "test")
	if err != nil || recovered != 1 {
		t.Fatalf("BisectRecover = %d, %v", recovered, err)
	}
	if _, err := os.Stat(paths[0]); err != nil {
		t.Fatalf("orphan not restored: %v", err)
	}

	if err := os.WriteFile(orphan, []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, err = service.BisectRecover(ctx, "test")
	if err != nil || recovered != 1 {
		t.Fatalf("stale BisectRecover = %d, %v", recovered, err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatalf("stale disabled duplicate remains: %v", err)
	}
}

func TestBisectExcludeValidationAndDisabledScanning(t *testing.T) {
	root := t.TempDir()
	paths := writeBisectINIs(t, root, 2)
	disabledDir := filepath.Join(root, "disabled_Mod")
	if err := os.MkdirAll(disabledDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(disabledDir, "ignored.ini"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "DISABLEDfoo.ini"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	service, _, _ := newBisectTestService(t, root)

	relative, err := service.BisectValidateExcludePath(context.Background(), "test", filepath.Dir(paths[0]))
	if err != nil || filepath.ToSlash(relative) != "a" {
		t.Fatalf("BisectValidateExcludePath = %q, %v", relative, err)
	}
	if _, err := service.BisectValidateExcludePath(context.Background(), "test", ".."); err == nil || !strings.Contains(err.Error(), bisectExcludeOutside) {
		t.Fatalf("outside validation error = %v", err)
	}
	snapshot, err := service.BisectStart(context.Background(), "test", []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Candidates) != 1 || snapshot.Candidates[0] != paths[1] {
		t.Fatalf("filtered candidates = %#v", snapshot.Candidates)
	}
	_, _ = service.BisectCancel(context.Background())
}

func TestResolveBisectExcludeRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := resolveBisectExclude(root, link); err == nil || !strings.Contains(err.Error(), bisectExcludeOutside) {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestBisectRestartCompletedSessionBroadcastsCancelLifecycle(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBisectINIs(t, root, 2)
	service, _, events := newBisectTestService(t, root)
	if _, err := service.BisectStart(ctx, "test", nil); err != nil {
		t.Fatal(err)
	}
	if done, err := service.BisectRespond(ctx, true); err != nil || done.Status != BisectDone {
		t.Fatalf("done = %#v, %v", done, err)
	}
	before := len(*events)
	if _, err := service.BisectStart(ctx, "test", nil); err != nil {
		t.Fatal(err)
	}
	want := []BisectStatus{BisectCancelled, BisectReverting, BisectIdle, BisectScanning, BisectRound}
	got := (*events)[before:]
	if len(got) != len(want) {
		t.Fatalf("events = %#v", got)
	}
	for index, status := range want {
		if got[index].Status != status {
			t.Fatalf("events = %#v", got)
		}
	}
	_, _ = service.BisectCancel(ctx)
}

func TestBisectContractErrorMessagesMatchElectron(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	writeBisectINIs(t, root, 2)
	service, client, _ := newBisectTestService(t, root)

	if _, err := service.BisectStart(ctx, "test", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BisectStart(ctx, "test", nil); err == nil || err.Error() != "A bisect session is already running. Cancel it first." {
		t.Fatalf("already running error = %v", err)
	}
	if _, err := service.BisectUndoLastRound(ctx); err == nil || err.Error() != "Nothing to undo." {
		t.Fatalf("undo error = %v", err)
	}
	if _, err := service.BisectRecover(ctx, "test"); err == nil || err.Error() != "Cannot recover while a bisect session is active." {
		t.Fatalf("recover error = %v", err)
	}
	if done, err := service.BisectRespond(ctx, true); err != nil || done.Status != BisectDone {
		t.Fatalf("done = %#v, %v", done, err)
	}
	if _, err := service.BisectRespond(ctx, true); err == nil || err.Error() != "No active batch to respond to." {
		t.Fatalf("respond error = %v", err)
	}
	_, _ = service.BisectCancel(ctx)
	if _, err := service.BisectRespond(ctx, true); err == nil || err.Error() != "No active bisect session." {
		t.Fatalf("session error = %v", err)
	}
	if _, err := service.requireBisectGame(ctx, "missing"); err == nil || err.Error() != "Game not found: missing" {
		t.Fatalf("game error = %v", err)
	}
	empty := "empty"
	if err := client.GamePaths.Insert(ctx, db.GamePathRow{Game: empty}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.requireBisectGame(ctx, empty); err == nil || err.Error() != "Mod folder path is not configured for empty." {
		t.Fatalf("mod path error = %v", err)
	}
}

func TestRequireBisectGameDoesNotPreflightConfiguredFolder(t *testing.T) {
	root := t.TempDir()
	service, client, _ := newBisectTestService(t, root)
	missing := filepath.Join(root, "not-created")
	if err := client.GamePaths.Insert(context.Background(), db.GamePathRow{Game: "configured", ModFolderPath: missing}); err != nil {
		t.Fatal(err)
	}
	row, err := service.requireBisectGame(context.Background(), "configured")
	if err != nil || row.ModFolderPath != missing {
		t.Fatalf("row = %#v, err = %v", row, err)
	}
}

func TestScanEnabledINIsSkipsDotPathsAndIncludesFileSymlinks(t *testing.T) {
	root := t.TempDir()
	visible := filepath.Join(root, "visible.ini")
	if err := os.WriteFile(visible, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(root, ".backup", "hidden.ini")
	if err := os.MkdirAll(filepath.Dir(hidden), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hidden, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked.ini")
	linkCreated := os.Symlink(visible, link) == nil
	paths, err := scanEnabledINIs(root)
	if err != nil {
		t.Fatal(err)
	}
	found := lowerPathSet(paths)
	if _, ok := found[strings.ToLower(visible)]; !ok {
		t.Fatalf("paths = %#v", paths)
	}
	if _, ok := found[strings.ToLower(hidden)]; ok {
		t.Fatalf("hidden path included: %#v", paths)
	}
	if linkCreated {
		if _, ok := found[strings.ToLower(link)]; !ok {
			t.Fatalf("symlink missing: %#v", paths)
		}
	}
}

func TestD3dxRestoreQueueDoesNotBlockWatcherDispatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d3dx_user.ini")
	initial := []byte("initial")
	if err := os.WriteFile(path, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	guard := &d3dxGuard{path: path, initial: initial, cancel: cancel, restoreWake: make(chan struct{}, 1), restoreDone: make(chan struct{})}
	service := &Tools{}
	go service.runD3dxRestoreWorker(ctx, guard)
	started := time.Now()
	queueD3dxRestore(guard)
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("queue blocked for %s", elapsed)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(path)
		if err == nil && string(raw) == string(initial) {
			cancel()
			<-guard.restoreDone
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-guard.restoreDone
	t.Fatal("d3dx file was not restored")
}
