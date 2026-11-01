package zzmifixer

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestZZMIFixerRunBelowLinkedModsFolder(t *testing.T) {
	ctx := context.Background()
	base := t.TempDir()
	stored := filepath.Join(base, "Stored")
	if err := os.MkdirAll(filepath.Join(stored, "Jane"), 0o755); err != nil {
		t.Fatal(err)
	}
	ini := "[TextureOverrideJane]\nhash = 33a09cfe\nvb2 = ResourceHairBlend\n\n" +
		"[ResourceHairBlend]\ntype = Buffer\nstride = 32\nfilename = hair.buf\n"
	if err := os.WriteFile(filepath.Join(stored, "Jane", "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	bufferPath := filepath.Join(stored, "Jane", "hair.buf")
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], 26)
	if err := os.WriteFile(bufferPath, buffer, 0o644); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(base, "Mods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", mods, stored).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	service := New()
	service.UseClient(openToolsTestDB(t))
	useToolsTestAppData(t, service, t.TempDir())
	importer := "ZZMI"
	if err := service.client.GamePaths.Insert(
		ctx,
		db.GamePathRow{Game: "ZZZ", ModFolderPath: mods, Importer: &importer},
	); err != nil {
		t.Fatal(err)
	}

	result, err := service.ZZMIFixerRun(ctx, ZZMIFixerRunInput{Path: filepath.Join(mods, "Jane"), Tool: "jane"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedBUF != 1 {
		t.Fatalf("unexpected run result: %+v", result)
	}
	fixed, err := os.ReadFile(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(fixed[16:]); got != 4 {
		t.Fatalf("expected fixed index 4, got %d", got)
	}
}
