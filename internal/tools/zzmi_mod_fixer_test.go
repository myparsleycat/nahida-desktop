package tools

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
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
