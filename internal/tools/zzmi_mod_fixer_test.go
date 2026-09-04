package tools

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	zzmiengine "nahida.live/desktop/internal/tools/zzmi"
)

func TestZZMIFixerRunBackupConflictAndRestore(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	target := filepath.Join(root, "Jane")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := `[TextureOverrideJane]
hash = 33a09cfe
vb2 = ResourceHairBlend

[ResourceHairBlend]
type = Buffer
stride = 32
filename = hair.buf
`
	if err := os.WriteFile(filepath.Join(target, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	bufferPath := filepath.Join(target, "hair.buf")
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], 26)
	if err := os.WriteFile(bufferPath, buffer, 0o644); err != nil {
		t.Fatal(err)
	}

	service := New()
	service.UseClient(openToolsTestDB(t))
	useToolsTestAppData(t, service, t.TempDir())
	importer := "ZZMI"
	if err := service.client.GamePaths.Insert(ctx, db.GamePathRow{Game: "ZZZ", ModFolderPath: root, Importer: &importer}); err != nil {
		t.Fatal(err)
	}

	result, err := service.ZZMIFixerRun(ctx, ZZMIFixerRunInput{Path: target, Tool: "jane"})
	if err != nil {
		t.Fatal(err)
	}
	if result.SessionID == nil || result.ChangedBUF != 1 {
		t.Fatalf("unexpected run result: %+v", result)
	}
	fixed, err := os.ReadFile(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(fixed[16:]); got != 4 {
		t.Fatalf("expected fixed index 4, got %d", got)
	}
	sessions, err := service.ZZMIFixerListBackups(ctx, target)
	if err != nil || len(sessions) != 1 || len(sessions[0].Entries) != 1 {
		t.Fatalf("unexpected backups: %+v, %v", sessions, err)
	}

	binary.LittleEndian.PutUint32(fixed[16:], 99)
	if err := os.WriteFile(bufferPath, fixed, 0o644); err != nil {
		t.Fatal(err)
	}
	restore, err := service.ZZMIFixerRestore(ctx, ZZMIFixerRestoreInput{Path: target, SessionID: *result.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if len(restore.Conflicts) != 1 || restore.Restored != 0 {
		t.Fatalf("expected one restore conflict: %+v", restore)
	}
	restore, err = service.ZZMIFixerRestore(ctx, ZZMIFixerRestoreInput{Path: target, SessionID: *result.SessionID, Force: true})
	if err != nil || restore.Restored != 1 {
		t.Fatalf("force restore = %+v, %v", restore, err)
	}
	restored, err := os.ReadFile(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(restored[16:]); got != 26 {
		t.Fatalf("expected original index 26, got %d", got)
	}
	if sessions, err := service.ZZMIFixerListBackups(ctx, target); err != nil || len(sessions) != 0 {
		t.Fatalf("restored session was not removed: %+v, %v", sessions, err)
	}
}

func TestZZMIFixerRequiresZZMIImporter(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	service := New()
	service.UseClient(openToolsTestDB(t))
	useToolsTestAppData(t, service, t.TempDir())
	importer := "WWMI"
	if err := service.client.GamePaths.Insert(ctx, db.GamePathRow{Game: "WW", ModFolderPath: root, Importer: &importer}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ZZMIFixerPrepare(ctx, root, false); err == nil || err.Error() != "Path is outside the managed ZZMI mod folder" {
		t.Fatalf("unexpected importer validation: %v", err)
	}
}

func TestValidateZZMIZipballURL(t *testing.T) {
	t.Parallel()
	valid := "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/v1"
	if err := validateZZMIZipballURL(valid); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{
		"http://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/v1",
		"https://example.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/v1",
		"https://api.github.com/repos/other/project/zipball/v1",
	} {
		if err := validateZZMIZipballURL(invalid); err == nil {
			t.Fatalf("accepted unsafe URL %s", invalid)
		}
	}
}

func TestZZMICleanupMarksInterruptedSessionPartial(t *testing.T) {
	t.Parallel()
	service := New()
	useToolsTestAppData(t, service, t.TempDir())
	target := t.TempDir()
	session := ZZMIBackupSession{
		SchemaVersion: 1,
		ID:            "0f25e321-3e5d-4d15-a1c8-41e457af0c33",
		TargetPath:    target,
		Status:        "preparing",
	}
	dir, err := service.zzmiSessionDir(target, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "staging"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeZZMISession(dir, session); err != nil {
		t.Fatal(err)
	}
	if err := service.zzmiCleanupAbandonedStaging(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "staging")); !os.IsNotExist(err) {
		t.Fatalf("staging directory still exists: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "partial"`) {
		t.Fatalf("session was not marked partial: %s", data)
	}
}

func TestZZMIActivePackRequiresMatchingManifestDigest(t *testing.T) {
	t.Parallel()
	service := New()
	useToolsTestAppData(t, service, t.TempDir())
	pack, err := zzmiengine.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	data, err := zzmiengine.EncodePack(*pack)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.zzmiStorePack(data, *pack); err != nil {
		t.Fatal(err)
	}
	if _, source, err := service.zzmiLoadActivePack(); err != nil || source != "cached" {
		t.Fatalf("valid cached pack was not loaded: %s, %v", source, err)
	}
	dir, err := service.appDataPath(filepath.Join("tools", zzmiFixerDirName))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256Hex(data)
	if err := os.WriteFile(filepath.Join(dir, zzmiRulesCacheDirName, digest, zzmiRulesFileName), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, source, err := service.zzmiLoadActivePack()
	if err != nil || source != "embedded" || loaded.CommitSHA != zzmiengine.EmbeddedCommit {
		t.Fatalf("tampered cached pack did not fall back: %s, %v", source, err)
	}
}

func TestZZMIInactivePackDoesNotReplaceActivePack(t *testing.T) {
	t.Parallel()
	service := New()
	useToolsTestAppData(t, service, t.TempDir())
	pack, err := zzmiengine.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	activeData, err := zzmiengine.EncodePack(*pack)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.zzmiStorePack(activeData, *pack); err != nil {
		t.Fatal(err)
	}

	inactive := *pack
	inactive.GeneratedAt = "interrupted-promotion"
	inactiveData, err := zzmiengine.EncodePack(inactive)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := service.appDataPath(filepath.Join("tools", zzmiFixerDirName))
	if err != nil {
		t.Fatal(err)
	}
	inactiveDir := filepath.Join(dir, zzmiRulesCacheDirName, sha256Hex(inactiveData))
	if err := os.MkdirAll(inactiveDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inactiveDir, zzmiRulesFileName), inactiveData, 0o644); err != nil {
		t.Fatal(err)
	}

	loaded, source, err := service.zzmiLoadActivePack()
	if err != nil || source != "cached" || loaded.GeneratedAt != pack.GeneratedAt {
		t.Fatalf("incomplete promotion replaced the active pack: source=%s pack=%+v err=%v", source, loaded, err)
	}
}

func TestZZMIFixerPrepareUsesAppStateCache(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := insertZZMITestTarget(t, client)
	cached := sampleZZMILatestRelease(time.Now().UTC())
	seedZZMILatestRelease(t, client, cached)

	hits := 0
	service := newZZMIFixerTestService(t, client, func(*http.Request) (int, string, error) {
		hits++
		return 0, "", fmt.Errorf("unexpected GitHub request")
	})

	result, err := service.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatalf("ZZMIFixerPrepare failed: %v", err)
	}
	if hits != 0 {
		t.Fatalf("fresh cache still hit GitHub %d time(s)", hits)
	}
	if result.Rules.CheckedRemotely {
		t.Fatal("expected CheckedRemotely to be false when app_state cache is valid")
	}
	if result.Rules.LatestTag == nil || *result.Rules.LatestTag != cached.Tag {
		t.Fatalf("unexpected LatestTag: %v", result.Rules.LatestTag)
	}
	if result.Rules.LatestCommit == nil || *result.Rules.LatestCommit != cached.Commit {
		t.Fatalf("unexpected LatestCommit: %v", result.Rules.LatestCommit)
	}

	service2 := newZZMIFixerTestService(t, client, func(*http.Request) (int, string, error) {
		hits++
		return 0, "", fmt.Errorf("unexpected GitHub request")
	})
	result2, err := service2.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatalf("ZZMIFixerPrepare on fresh service failed: %v", err)
	}
	if hits != 0 {
		t.Fatalf("second service hit GitHub %d time(s)", hits)
	}
	if result2.Rules.CheckedRemotely || result2.Rules.LatestTag == nil || *result2.Rules.LatestTag != cached.Tag {
		t.Fatalf("fresh service did not reuse app_state cache: %+v", result2.Rules)
	}
}

func TestZZMIFixerPreparePersistsRemoteRelease(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := insertZZMITestTarget(t, client)
	remote := sampleZZMILatestRelease(time.Time{})
	service := newZZMIFixerTestService(t, client, zzmiRemoteReleaseHandler(remote.Tag, remote.Commit, nil))

	result, err := service.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Rules.CheckedRemotely || result.Rules.LatestTag == nil || *result.Rules.LatestTag != remote.Tag {
		t.Fatalf("unexpected remote prepare: %+v", result.Rules)
	}

	stored := readZZMILatestRelease(t, client)
	if stored.Tag != remote.Tag || stored.Commit != remote.Commit || stored.Zipball != remote.Zipball || stored.CheckedAt == "" {
		t.Fatalf("persisted release = %#v", stored)
	}

	hits := 0
	service2 := newZZMIFixerTestService(t, client, func(*http.Request) (int, string, error) {
		hits++
		return 0, "", fmt.Errorf("unexpected GitHub request")
	})
	result2, err := service2.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 || result2.Rules.CheckedRemotely || result2.Rules.LatestTag == nil || *result2.Rules.LatestTag != remote.Tag {
		t.Fatalf("persisted cache was not reused: hits=%d rules=%+v", hits, result2.Rules)
	}
}

func TestZZMIFixerPrepareExpiredCacheHitsRemote(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := insertZZMITestTarget(t, client)
	seedZZMILatestRelease(t, client, sampleZZMILatestRelease(time.Now().UTC().Add(-2*time.Hour)))
	remote := zzmiLatestRelease{
		Tag:     "v9.9.9",
		Commit:  "abcdef0123456789abcdef0123456789abcdef01",
		Zipball: "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/v9.9.9",
	}
	service := newZZMIFixerTestService(t, client, zzmiRemoteReleaseHandler(remote.Tag, remote.Commit, nil))

	result, err := service.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Rules.CheckedRemotely || result.Rules.LatestTag == nil || *result.Rules.LatestTag != remote.Tag {
		t.Fatalf("expired cache did not refresh: %+v", result.Rules)
	}
}

func TestZZMIFixerPrepareForceRefreshBypassesCache(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := insertZZMITestTarget(t, client)
	seedZZMILatestRelease(t, client, sampleZZMILatestRelease(time.Now().UTC()))
	remote := zzmiLatestRelease{
		Tag:     "v2.0.0",
		Commit:  "fedcba9876543210fedcba9876543210fedcba98",
		Zipball: "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/v2.0.0",
	}
	hits := 0
	service := newZZMIFixerTestService(t, client, zzmiRemoteReleaseHandler(remote.Tag, remote.Commit, func() { hits++ }))

	result, err := service.ZZMIFixerPrepare(ctx, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if hits == 0 {
		t.Fatal("force refresh did not contact GitHub")
	}
	if !result.Rules.CheckedRemotely || result.Rules.LatestTag == nil || *result.Rules.LatestTag != remote.Tag {
		t.Fatalf("force refresh did not use remote release: %+v", result.Rules)
	}
}

func TestZZMIFixerPrepareFallsBackAndRefreshesCooldown(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := insertZZMITestTarget(t, client)
	staleCheckedAt := time.Now().UTC().Add(-2 * time.Hour)
	cached := sampleZZMILatestRelease(staleCheckedAt)
	seedZZMILatestRelease(t, client, cached)

	hits := 0
	service := newZZMIFixerTestService(t, client, func(*http.Request) (int, string, error) {
		hits++
		return http.StatusServiceUnavailable, "unavailable", nil
	})

	result, err := service.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if hits == 0 {
		t.Fatal("expired cache did not attempt a remote check")
	}
	if result.Rules.CheckedRemotely || result.Rules.IncompatibilityReason != nil {
		t.Fatalf("fallback should stay silent: %+v", result.Rules)
	}
	if result.Rules.LatestTag == nil || *result.Rules.LatestTag != cached.Tag {
		t.Fatalf("fallback lost cached tag: %v", result.Rules.LatestTag)
	}

	stored := readZZMILatestRelease(t, client)
	if parseRFC3339(stored.CheckedAt).Equal(staleCheckedAt) || time.Since(parseRFC3339(stored.CheckedAt)) > time.Minute {
		t.Fatalf("fallback did not refresh CheckedAt: %q", stored.CheckedAt)
	}

	hits = 0
	result2, err := service.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 0 || result2.Rules.CheckedRemotely || result2.Rules.LatestTag == nil || *result2.Rules.LatestTag != cached.Tag {
		t.Fatalf("refreshed cooldown still contacted GitHub: hits=%d rules=%+v", hits, result2.Rules)
	}
}

func TestZZMIFixerPrepareTreatsInvalidOrFutureCheckedAtAsMiss(t *testing.T) {
	cases := []struct {
		name      string
		checkedAt string
	}{
		{name: "invalid", checkedAt: "not-a-timestamp"},
		{name: "future", checkedAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			client := openToolsTestDB(t)
			root := insertZZMITestTarget(t, client)
			cached := sampleZZMILatestRelease(time.Time{})
			cached.CheckedAt = test.checkedAt
			seedZZMILatestRelease(t, client, cached)
			remote := zzmiLatestRelease{
				Tag:     "v9.9.9",
				Commit:  "abcdef0123456789abcdef0123456789abcdef01",
				Zipball: "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/v9.9.9",
			}
			service := newZZMIFixerTestService(t, client, zzmiRemoteReleaseHandler(remote.Tag, remote.Commit, nil))

			result, err := service.ZZMIFixerPrepare(ctx, root, false)
			if err != nil {
				t.Fatal(err)
			}
			if !result.Rules.CheckedRemotely || result.Rules.LatestTag == nil || *result.Rules.LatestTag != remote.Tag {
				t.Fatalf("%s timestamp did not refresh: %+v", test.name, result.Rules)
			}
		})
	}
}

func TestZZMIFixerPrepareTreatsBrokenCacheAsMiss(t *testing.T) {
	ctx := context.Background()
	client := openToolsTestDB(t)
	root := insertZZMITestTarget(t, client)
	if err := client.AppState.Upsert(ctx, zzmiLatestReleaseKey, "{not-json", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	remote := sampleZZMILatestRelease(time.Time{})
	service := newZZMIFixerTestService(t, client, zzmiRemoteReleaseHandler(remote.Tag, remote.Commit, nil))

	result, err := service.ZZMIFixerPrepare(ctx, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Rules.CheckedRemotely || result.Rules.LatestTag == nil || *result.Rules.LatestTag != remote.Tag {
		t.Fatalf("broken cache was not treated as a miss: %+v", result.Rules)
	}
}

func insertZZMITestTarget(t *testing.T, client *db.Client) string {
	t.Helper()
	root := t.TempDir()
	importer := "ZZMI"
	if err := client.GamePaths.Insert(context.Background(), db.GamePathRow{Game: "ZZZ", ModFolderPath: root, Importer: &importer}); err != nil {
		t.Fatal(err)
	}
	return root
}

func sampleZZMILatestRelease(checkedAt time.Time) zzmiLatestRelease {
	release := zzmiLatestRelease{
		Tag:       "v1.2.3",
		Commit:    "0123456789abcdef0123456789abcdef01234567",
		Zipball:   "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/v1.2.3",
		Published: "2026-09-03T12:00:00Z",
		Blobs:     map[string]string{"Source Codes/Jane.remapper.py": "abc123"},
	}
	if !checkedAt.IsZero() {
		release.CheckedAt = checkedAt.Format(time.RFC3339Nano)
	}
	return release
}

func seedZZMILatestRelease(t *testing.T, client *db.Client, release zzmiLatestRelease) {
	t.Helper()
	raw, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	updatedAt := release.CheckedAt
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err := client.AppState.Upsert(context.Background(), zzmiLatestReleaseKey, string(raw), updatedAt); err != nil {
		t.Fatal(err)
	}
}

func readZZMILatestRelease(t *testing.T, client *db.Client) zzmiLatestRelease {
	t.Helper()
	raw, err := client.AppState.GetValue(context.Background(), zzmiLatestReleaseKey)
	if err != nil || raw == nil {
		t.Fatalf("stored release = %v, %v", raw, err)
	}
	var release zzmiLatestRelease
	if err := json.Unmarshal([]byte(*raw), &release); err != nil {
		t.Fatal(err)
	}
	return release
}

func newZZMIFixerTestService(t *testing.T, client *db.Client, handle func(*http.Request) (int, string, error)) *Tools {
	t.Helper()
	service := NewWithOptions(Options{
		HTTP: infra.NewClientWithOptions(infra.ClientOptions{
			HTTPClient: &http.Client{Transport: toolsRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				status, body, err := handle(request)
				if err != nil {
					return nil, err
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})},
		}),
	})
	service.UseClient(client)
	useToolsTestAppData(t, service, t.TempDir())
	return service
}

func zzmiRemoteReleaseHandler(tag, commit string, onHit func()) func(*http.Request) (int, string, error) {
	zipball := "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/zipball/" + tag
	refURL := "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/git/ref/tags/" + tag
	treeURL := "https://api.github.com/repos/Vonksdesu/ZZZ-Mod-Fixer/git/trees/" + commit + "?recursive=1"
	return func(request *http.Request) (int, string, error) {
		if onHit != nil {
			onHit()
		}
		switch request.URL.String() {
		case zzmiLatestReleaseURL:
			return http.StatusOK, fmt.Sprintf(`{"tag_name":%q,"zipball_url":%q,"published_at":"2026-09-03T12:00:00Z"}`, tag, zipball), nil
		case refURL:
			return http.StatusOK, fmt.Sprintf(`{"object":{"type":"commit","sha":%q}}`, commit), nil
		case treeURL:
			return http.StatusOK, `{"truncated":false,"tree":[{"path":"Source Codes/Jane.remapper.py","type":"blob","sha":"abc123"}]}`, nil
		default:
			return 0, "", fmt.Errorf("unexpected request: %s", request.URL)
		}
	}
}
