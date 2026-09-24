package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/drive"
	"nahida.live/desktop/internal/setting"
)

func TestIncludedTargets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mods := filepath.Join(root, "Mods")
	nested := filepath.Join(mods, "Nahida")
	other := filepath.Join(root, "Other")
	for _, dir := range []string{nested, other} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	game := "GI"
	targets := []Target{
		{ID: "game:GI", Kind: TargetKindGame, Game: &game, Label: "GI", Path: mods},
		{ID: "nested", Kind: TargetKindCustom, Label: "Nahida", Path: nested},
		{ID: "same", Kind: TargetKindCustom, Label: "Mods again", Path: strings.ToUpper(mods)},
		{ID: "excluded", Kind: TargetKindCustom, Label: "Other", Path: other, Excluded: true},
		{ID: "missing", Kind: TargetKindCustom, Label: "Gone", Path: filepath.Join(root, "gone"), Missing: true},
	}
	included := includedTargets(targets)
	if len(included) != 1 || included[0].ID != "game:GI" {
		t.Fatalf("included = %+v", included)
	}
}

func TestScanTargetsFiltersAndCountsSkipped(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "merged.ini"), "ini")
	writeFile(t, filepath.Join(root, "tool.exe"), "exe")
	writeFile(t, filepath.Join(root, "Thumbs.db"), "db")
	writeFile(t, filepath.Join(root, "Nahida", "skin.dds"), "dds")

	filter := func(name string, _ int64) string {
		switch {
		case strings.EqualFold(name, "thumbs.db"):
			return "system_file"
		case strings.HasSuffix(name, ".exe"):
			return "denied_file_type"
		}
		return ""
	}
	result, err := scanTargets(t.Context(), []Target{{Path: root}}, filter)
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]bool{}
	for _, file := range result.files {
		paths[file.relPath] = true
	}
	if len(paths) != 2 || !paths["merged.ini"] || !paths["Nahida/skin.dds"] {
		t.Fatalf("scanned = %v", paths)
	}
	if result.skipped["denied_file_type"] != 1 || result.skipped["system_file"] != 1 {
		t.Fatalf("skipped = %v", result.skipped)
	}
}

func TestHashFilesReusesTheCache(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	path := filepath.Join(root, "a.ini")
	writeFile(t, path, "content")

	scan := func() []scannedFile {
		result, err := scanTargets(t.Context(), []Target{{Path: root}}, func(string, int64) string { return "" })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := hashFiles(t.Context(), client.BackupFileCache, result.files, 2, nil); err != nil {
			t.Fatal(err)
		}
		return result.files
	}

	first := scan()
	if first[0].sha256 == "" {
		t.Fatal("the file was not hashed")
	}
	fake := strings.Repeat("f", 64)
	if err := client.BackupFileCache.UpsertMany(t.Context(), []db.BackupFileCacheRow{{
		Path: first[0].fullPath, Size: first[0].size, Mtime: first[0].modified.UnixNano(), SHA256: fake,
	}}); err != nil {
		t.Fatal(err)
	}
	if again := scan(); again[0].sha256 != fake {
		t.Fatalf("an unchanged file was hashed again: %s", again[0].sha256)
	}

	later := time.Now().Add(time.Minute)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if touched := scan(); touched[0].sha256 != first[0].sha256 {
		t.Fatalf("a touched file kept the cached hash: %s", touched[0].sha256)
	}
}

func TestManifestDigestIgnoresOrder(t *testing.T) {
	t.Parallel()

	game := "GI"
	targets := []Target{{Kind: TargetKindGame, Game: &game, Path: `D:\GI`}, {Kind: TargetKindCustom, Path: `D:\Extra`}}
	files := []scannedFile{
		{target: 0, relPath: "a.ini", sha256: "1"},
		{target: 1, relPath: "b.ini", sha256: "2"},
	}
	reversed := []scannedFile{files[1], files[0]}
	if manifestDigest(targets, files) != manifestDigest(targets, reversed) {
		t.Fatal("the digest depends on the walk order")
	}
	changed := []scannedFile{files[0], {target: 1, relPath: "b.ini", sha256: "3"}}
	if manifestDigest(targets, files) == manifestDigest(targets, changed) {
		t.Fatal("the digest ignores a content change")
	}
}

func TestRestoreFolders(t *testing.T) {
	t.Parallel()

	folders := restoreFolders([]ManifestTarget{
		{ID: "a", Label: "GI"},
		{ID: "b", Label: "gi"},
		{ID: "c", Label: `D:\Mods`},
		{ID: "d", Label: "skipped"},
	}, []string{"a", "b", "c"})
	if folders["a"] != "GI" || folders["b"] != "gi (2)" || folders["c"] != "D__Mods" || len(folders) != 3 {
		t.Fatalf("folders = %v", folders)
	}
}

func TestRunDropsAFewMissingFiles(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ini"), "a")
	writeFile(t, filepath.Join(root, "b.ini"), "b")
	if err := client.GamePaths.Insert(t.Context(), db.GamePathRow{Game: "GI", ModFolderPath: root}); err != nil {
		t.Fatal(err)
	}

	remote := &fakeRemote{missingOnce: 1}
	backup := testBackup(t, client, remote)
	result := backup.run(t.Context(), client, "manual")
	if result.Outcome != OutcomeCompleted || result.Error != "" {
		t.Fatalf("result = %+v", result)
	}
	if len(remote.uploaded) != 2 {
		t.Fatalf("uploaded %d files", len(remote.uploaded))
	}
	if len(remote.commits) != 2 || len(remote.commits[1].Failed) != 1 ||
		remote.commits[1].Failed[0].Reason != "upload_failed" {
		t.Fatalf("commits = %+v", remote.commits)
	}
	if remote.aborted {
		t.Fatal("a committed snapshot was aborted")
	}

	// The dropped file is not remembered, so the next run sends it again.
	requireCommitted(t, client, backup, remote)
	remote.reset()
	if result := backup.run(t.Context(), client, "manual"); result.Outcome != OutcomeCompleted {
		t.Fatalf("next run = %+v", result)
	}
	if len(remote.uploaded) != 1 {
		t.Fatalf("next run uploaded %d files, want the dropped one", len(remote.uploaded))
	}
}

func TestRunAbortsWhenPlanningStopsAfterSomeFiles(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ini"), "a")
	writeFile(t, filepath.Join(root, "b.ini"), "b")
	if err := client.GamePaths.Insert(t.Context(), db.GamePathRow{Game: "GI", ModFolderPath: root}); err != nil {
		t.Fatal(err)
	}

	remote := &fakeRemote{planningErr: errors.New("second plan page failed")}
	backup := testBackup(t, client, remote)
	result := backup.run(t.Context(), client, "manual")
	if result.Outcome != OutcomeFailed || !remote.aborted || len(remote.commits) != 0 {
		t.Fatalf(
			"a partly planned snapshot was committed: result=%+v aborted=%v commits=%+v",
			result,
			remote.aborted,
			remote.commits,
		)
	}
}

func TestRunAbortsWhenMostContentIsMissing(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	for index := range missingToleranceFiles + 5 {
		writeFile(t, filepath.Join(root, fmt.Sprintf("%02d.ini", index)), fmt.Sprint(index))
	}
	if err := client.BackupCustomPaths.Insert(
		t.Context(),
		db.BackupCustomPathRow{ID: "c", Path: root, Label: "Extra"},
	); err != nil {
		t.Fatal(err)
	}

	remote := &fakeRemote{missingOnce: missingToleranceFiles + 5}
	result := testBackup(t, client, remote).run(t.Context(), client, "schedule")
	if result.Outcome != OutcomeFailed || !remote.aborted {
		t.Fatalf("result = %+v, aborted = %v", result, remote.aborted)
	}
}

func TestRunStopsWhenTheManifestIsUnchanged(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ini"), "a")
	if err := client.GamePaths.Insert(t.Context(), db.GamePathRow{Game: "GI", ModFolderPath: root}); err != nil {
		t.Fatal(err)
	}

	remote := &fakeRemote{unchanged: true}
	result := testBackup(t, client, remote).run(t.Context(), client, "watch")
	if result.Outcome != OutcomeUnchanged || len(remote.uploaded) != 0 || len(remote.commits) != 0 {
		t.Fatalf("result = %+v, remote = %+v", result, remote)
	}
}

func TestRunSendsOnlyWhatChanged(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ini"), "a")
	writeFile(t, filepath.Join(root, "b.ini"), "b")
	if err := client.GamePaths.Insert(t.Context(), db.GamePathRow{Game: "GI", ModFolderPath: root}); err != nil {
		t.Fatal(err)
	}

	remote := &fakeRemote{}
	backup := testBackup(t, client, remote)
	if result := backup.run(t.Context(), client, "manual"); result.Outcome != OutcomeCompleted {
		t.Fatalf("first run = %+v", result)
	}
	if len(remote.uploaded) != 2 || len(remote.deleted) != 0 {
		t.Fatalf("first run sent %d files and %d removals", len(remote.uploaded), len(remote.deleted))
	}

	remote.reset()
	writeFile(t, filepath.Join(root, "a.ini"), "a, changed")
	writeFile(t, filepath.Join(root, "c.ini"), "c")
	if err := os.Remove(filepath.Join(root, "b.ini")); err != nil {
		t.Fatal(err)
	}
	result := backup.run(t.Context(), client, "watch")
	if result.Outcome != OutcomeCompleted || result.FileCount != 2 {
		t.Fatalf("second run = %+v", result)
	}
	sent := lo.Map(remote.uploaded, func(file drive.BackupUploadFile, _ int) string { return file.RelPath })
	slices.Sort(sent)
	if !slices.Equal(sent, []string{"a.ini", "c.ini"}) || len(remote.deleted) != 1 ||
		remote.deleted[0].RelPath != "b.ini" {
		t.Fatalf("second run sent %v and removed %+v", sent, remote.deleted)
	}
	if remote.manifests != 0 {
		t.Fatalf("a run on a known base read the server's list %d times", remote.manifests)
	}

	requireCommitted(t, client, backup, remote)
}

func TestRunTakesTheBaseFromTheServer(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ini"), "a")
	writeFile(t, filepath.Join(root, "b.ini"), "b")
	if err := client.GamePaths.Insert(t.Context(), db.GamePathRow{Game: "GI", ModFolderPath: root}); err != nil {
		t.Fatal(err)
	}

	// Another install of this PC already backed up a.ini; this one remembers
	// nothing.
	scan, err := scanTargets(t.Context(), []Target{{Path: root}}, func(string, int64) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hashFiles(t.Context(), client.BackupFileCache, scan.files, 2, nil); err != nil {
		t.Fatal(err)
	}
	a, _ := lo.Find(scan.files, func(file scannedFile) bool { return file.relPath == "a.ini" })
	remote := &fakeRemote{latest: "snapshot-7", snapshots: 7, files: map[string]map[string]string{
		"game:GI": {"a.ini": a.sha256, "gone.ini": strings.Repeat("0", 64)},
	}}

	backup := testBackup(t, client, remote)
	if result := backup.run(t.Context(), client, "startup"); result.Outcome != OutcomeCompleted {
		t.Fatalf("result = %+v", result)
	}
	if remote.creates != 2 || remote.manifests != 1 {
		t.Fatalf("creates = %d, manifests = %d", remote.creates, remote.manifests)
	}
	if len(remote.uploaded) != 1 || remote.uploaded[0].RelPath != "b.ini" ||
		len(remote.deleted) != 1 || remote.deleted[0].RelPath != "gone.ini" {
		t.Fatalf("sent %+v and removed %+v", remote.uploaded, remote.deleted)
	}
	requireCommitted(t, client, backup, remote)
}

func TestRunForgetsATargetLeftOut(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	mods, extra := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(mods, "a.ini"), "a")
	writeFile(t, filepath.Join(extra, "b.ini"), "b")
	if err := client.GamePaths.Insert(t.Context(), db.GamePathRow{Game: "GI", ModFolderPath: mods}); err != nil {
		t.Fatal(err)
	}
	if err := client.BackupCustomPaths.Insert(
		t.Context(),
		db.BackupCustomPathRow{ID: "c", Path: extra, Label: "Extra"},
	); err != nil {
		t.Fatal(err)
	}

	remote := &fakeRemote{}
	backup := testBackup(t, client, remote)
	if result := backup.run(t.Context(), client, "manual"); result.Outcome != OutcomeCompleted {
		t.Fatalf("first run = %+v", result)
	}
	if err := client.BackupCustomPaths.Delete(t.Context(), "c"); err != nil {
		t.Fatal(err)
	}
	remote.reset()
	if result := backup.run(t.Context(), client, "manual"); result.Outcome != OutcomeCompleted {
		t.Fatalf("second run = %+v", result)
	}
	if len(remote.uploaded) != 0 || len(remote.deleted) != 0 {
		t.Fatalf("leaving a folder out sent %+v and removed %+v", remote.uploaded, remote.deleted)
	}
	requireCommitted(t, client, backup, remote)
}

// requireCommitted checks that this PC remembers exactly the server's newest
// snapshot.
func requireCommitted(t *testing.T, client *db.Client, backup *Backup, remote *fakeRemote) {
	t.Helper()
	base, err := backup.getState(t.Context(), client, stateBaseSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if base != remote.latest {
		t.Fatalf("base = %q, server's newest = %q", base, remote.latest)
	}
	rows, err := client.BackupCommitted.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]map[string]string{}
	for _, row := range rows {
		if got[row.TargetKey] == nil {
			got[row.TargetKey] = map[string]string{}
		}
		got[row.TargetKey][row.RelPath] = row.SHA256
	}
	want := map[string]map[string]string{}
	for key, paths := range remote.files {
		if len(paths) > 0 {
			want[key] = paths
		}
	}
	if !maps.EqualFunc(got, want, maps.Equal) {
		t.Fatalf("remembered %v, server has %v", got, want)
	}
}

func TestDueTrigger(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	backup := testBackup(t, client, &fakeRemote{})
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	backup.now = func() time.Time { return now }
	backup.startedAt = now.Add(-time.Minute)
	ctx := t.Context()

	due := func(startup bool) string {
		trigger, err := backup.dueTrigger(ctx, client, startup)
		if err != nil {
			t.Fatal(err)
		}
		return trigger
	}
	setSuccess := func(at time.Time) {
		if err := backup.putState(ctx, client, stateLastSuccessAt, at.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
	}

	setSuccess(now.Add(-25 * time.Hour))
	if got := due(true); got != "startup" {
		t.Fatalf("startup catch-up = %q", got)
	}
	if err := backup.opts.Settings.Set(ctx, setting.KeyBackupOnStartup, false); err != nil {
		t.Fatal(err)
	}
	if got := due(true); got != "" {
		t.Fatalf("catch-up without the setting = %q", got)
	}
	if got := due(false); got != "" {
		t.Fatalf("time the app was closed counted = %q", got)
	}
	backup.startedAt = now.Add(-25 * time.Hour)
	if got := due(false); got != "schedule" {
		t.Fatalf("an interval that passed while running = %q", got)
	}

	setSuccess(now.Add(-2 * time.Hour))
	backup.changed = now.Add(-11 * time.Minute)
	if got := due(false); got != "watch" {
		t.Fatalf("a settled change = %q", got)
	}
	backup.changed = now.Add(-time.Minute)
	if got := due(false); got != "" {
		t.Fatalf("a change still settling = %q", got)
	}

	backup.changed = time.Time{}
	setSuccess(now.Add(-25 * time.Hour))
	for _, outcome := range []string{OutcomeFailed, OutcomeCancelled} {
		raw, _ := json.Marshal(RunResult{At: now.Add(-10 * time.Minute), Outcome: outcome})
		if err := backup.putState(ctx, client, stateLastRun, string(raw)); err != nil {
			t.Fatal(err)
		}
		if got := due(false); got != "" {
			t.Fatalf("a run right after a %s one = %q", outcome, got)
		}
	}
}

func TestScanTargetsWalksALinkedRoot(t *testing.T) {
	t.Parallel()

	real := t.TempDir()
	writeFile(t, filepath.Join(real, "Nahida", "merged.ini"), "ini")
	link := filepath.Join(t.TempDir(), "Mods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
		t.Skipf("cannot create a junction: %v %s", err, output)
	}

	result, err := scanTargets(t.Context(), []Target{{Path: link}}, func(string, int64) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if len(result.files) != 1 || result.files[0].relPath != "Nahida/merged.ini" {
		t.Fatalf("scanned = %+v, skipped = %v", result.files, result.skipped)
	}
}

func TestHashFilesLeavesOutAVanishedFile(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ini"), "a")
	writeFile(t, filepath.Join(root, "b.ini"), "b")
	result, err := scanTargets(t.Context(), []Target{{Path: root}}, func(string, int64) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	gone := slices.IndexFunc(result.files, func(file scannedFile) bool { return file.relPath == "b.ini" })
	if err := os.Remove(result.files[gone].fullPath); err != nil {
		t.Fatal(err)
	}

	unreadable, err := hashFiles(t.Context(), client.BackupFileCache, result.files, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(unreadable, []int{gone}) || result.files[1-gone].sha256 == "" {
		t.Fatalf("unreadable = %v, files = %+v", unreadable, result.files)
	}
}

func TestShutdownWaitsForTheRunToRecordItsResult(t *testing.T) {
	t.Parallel()

	client := testClient(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.ini"), "a")
	if err := client.GamePaths.Insert(t.Context(), db.GamePathRow{Game: "GI", ModFolderPath: root}); err != nil {
		t.Fatal(err)
	}

	remote := &fakeRemote{uploading: make(chan struct{})}
	backup := New(Options{Remote: remote, Settings: setting.New(client), Session: loggedIn{}})
	backup.UseClient(client)
	if err := backup.RunNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-remote.uploading
	if err := backup.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}

	last, err := backup.lastRun(t.Context(), client)
	if err != nil {
		t.Fatal(err)
	}
	if last == nil || last.Outcome != OutcomeCancelled {
		t.Fatalf("last run = %+v", last)
	}
	if !remote.aborted {
		t.Fatal("the cancelled snapshot was not abandoned")
	}
}

type loggedIn struct{}

func (loggedIn) IsLoggedIn(context.Context) (bool, error) { return true, nil }

// fakeRemote answers the backup routes the way the server does: it keeps the
// files of the newest snapshot, refuses a snapshot taken against another base,
// and applies what a commit changes.
type fakeRemote struct {
	mu          sync.Mutex
	unchanged   bool
	missingOnce int
	planningErr error
	uploaded    []drive.BackupUploadFile
	deleted     []drive.BackupDeletedFile
	commits     []commitBody
	creates     int
	manifests   int
	aborted     bool
	// uploading, when set, is closed when an upload starts, and the upload
	// then waits for its context to end.
	uploading chan struct{}

	// latest is the newest committed snapshot, and files its content by
	// target key and path.
	latest    string
	snapshots int
	files     map[string]map[string]string
	pending   fakePending
}

// fakePending is the snapshot a run is writing.
type fakePending struct {
	targets []string
	upserts []drive.BackupUploadFile
	deletes []drive.BackupDeletedFile
}

type commitBody struct {
	Failed []struct {
		TargetID string `json:"targetId"`
		Path     string `json:"path"`
		Reason   string `json:"reason"`
	} `json:"failed"`
}

func fakeTargetID(key string) string { return "target:" + key }

func fakeTargetKey(id string) string { return strings.TrimPrefix(id, "target:") }

func (f *fakeRemote) BackupJSON(_ context.Context, method, route string, body, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var answer any
	switch {
	case method == http.MethodPut && strings.HasPrefix(route, "/backup/devices/"):
		answer = map[string]any{"id": "device-1"}
	case method == http.MethodPost && route == "/backup/snapshots":
		f.creates++
		if f.unchanged {
			answer = map[string]any{"unchanged": true}
			break
		}
		raw, _ := json.Marshal(body)
		var request struct {
			BaseSnapshotID *string `json:"baseSnapshotId"`
			Targets        []struct {
				Key string `json:"key"`
			} `json:"targets"`
		}
		_ = json.Unmarshal(raw, &request)
		if lo.FromPtr(request.BaseSnapshotID) != f.latest {
			return &drive.BackupAPIError{Status: http.StatusConflict, Code: "base_mismatch", LatestSnapshotID: f.latest}
		}
		f.pending = fakePending{}
		ids := make([]string, len(request.Targets))
		for index, target := range request.Targets {
			f.pending.targets = append(f.pending.targets, target.Key)
			ids[index] = fakeTargetID(target.Key)
		}
		answer = map[string]any{"snapshotId": fmt.Sprintf("snapshot-%d", f.snapshots+1), "targetIds": ids}
	case method == http.MethodGet && strings.HasSuffix(route, "/manifest"):
		f.manifests++
		targets := []map[string]string{}
		files := []map[string]any{}
		for key, paths := range f.files {
			targets = append(targets, map[string]string{"id": fakeTargetID(key), "key": key})
			for path, sha := range paths {
				files = append(
					files,
					map[string]any{"id": key + path, "targetId": fakeTargetID(key), "path": path, "sha256": sha},
				)
			}
		}
		answer = map[string]any{"snapshot": map[string]any{"targets": targets}, "files": files}
	case strings.HasSuffix(route, "/commit"):
		raw, _ := json.Marshal(body)
		var commit commitBody
		_ = json.Unmarshal(raw, &commit)
		f.commits = append(f.commits, commit)
		if len(f.commits) == 1 && f.missingOnce > 0 {
			missing := make([]string, 0, f.missingOnce)
			for _, file := range f.pending.upserts[:min(f.missingOnce, len(f.pending.upserts))] {
				missing = append(missing, file.SHA256)
			}
			return &drive.BackupAPIError{Status: http.StatusConflict, Code: "backup_incomplete", Missing: missing}
		}
		answer = f.commit(commit)
	case strings.HasSuffix(route, "/abort"):
		f.aborted = true
	default:
		return fmt.Errorf("unexpected %s %s", method, route)
	}
	if out == nil || answer == nil {
		return nil
	}
	raw, _ := json.Marshal(answer)
	return json.Unmarshal(raw, out)
}

// commit applies the pending changes, less the failed files, the way the
// server turns them into file versions.
func (f *fakeRemote) commit(commit commitBody) map[string]any {
	failed := lo.Keyify(lo.Map(commit.Failed, func(file struct {
		TargetID string `json:"targetId"`
		Path     string `json:"path"`
		Reason   string `json:"reason"`
	}, _ int) string {
		return file.TargetID + "\x00" + file.Path
	}))
	next := map[string]map[string]string{}
	for _, key := range f.pending.targets {
		next[key] = maps.Clone(f.files[key])
		if next[key] == nil {
			next[key] = map[string]string{}
		}
	}
	for _, file := range f.pending.deletes {
		delete(next[fakeTargetKey(file.TargetID)], file.RelPath)
	}
	for _, file := range f.pending.upserts {
		if _, gone := failed[file.TargetID+"\x00"+file.RelPath]; !gone {
			next[fakeTargetKey(file.TargetID)][file.RelPath] = file.SHA256
		}
	}
	f.files = next
	f.snapshots++
	f.latest = fmt.Sprintf("snapshot-%d", f.snapshots)
	count := lo.SumBy(lo.Values(next), func(paths map[string]string) int { return len(paths) })
	return map[string]any{"id": f.latest, "state": "COMPLETED", "fileCount": count}
}

func (f *fakeRemote) BackupFilter(context.Context) (func(string, int64) string, error) {
	return func(string, int64) string { return "" }, nil
}

func (f *fakeRemote) UploadBackupFiles(
	ctx context.Context,
	_ string,
	files []drive.BackupUploadFile,
	deleted []drive.BackupDeletedFile,
	_ func(int64),
) ([]string, error) {
	if f.uploading != nil {
		close(f.uploading)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploaded = append(f.uploaded, files...)
	f.deleted = append(f.deleted, deleted...)
	f.pending.upserts = append(f.pending.upserts, files...)
	f.pending.deletes = append(f.pending.deletes, deleted...)
	if f.planningErr != nil {
		return nil, errors.Join(drive.ErrBackupPlanning, f.planningErr)
	}
	return nil, nil
}

func (f *fakeRemote) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploaded, f.deleted, f.commits, f.creates, f.manifests = nil, nil, nil, 0, 0
}

func (f *fakeRemote) DownloadBackupFile(context.Context, string, drive.BackupDownload, string, func(int64)) error {
	return nil
}

func testClient(t *testing.T) *db.Client {
	t.Helper()
	client, err := db.New(filepath.Join(t.TempDir(), "backup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	if err := client.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	return client
}

func testBackup(t *testing.T, client *db.Client, remote Remote) *Backup {
	t.Helper()
	backup := New(Options{Remote: remote, Settings: setting.New(client)})
	backup.UseClient(client)
	return backup
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
