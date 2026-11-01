package bodyshape

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/platform"
)

func TestLoadBodyShapeModBelowLinkedModsFolder(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	stored := filepath.Join(base, "Stored", "Mod")
	if err := os.MkdirAll(stored, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTestPositions(t, filepath.Join(stored, "Position.buf"), []float32{0, 0, 0}, 12)
	ini := "[ResourcePositionBuffer]\nstride = 12\nfilename = Position.buf\n"
	if err := os.WriteFile(filepath.Join(stored, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(base, "Mods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", mods, filepath.Dir(stored)).
		CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	loaded, err := loadBodyShapeMod(filepath.Join(mods, "Mod"), func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Meshes) != 1 || !platform.SameOrChildPath(loaded.ModRoot, loaded.Meshes[0].PositionPath) {
		t.Fatalf("modRoot = %q, meshes = %#v", loaded.ModRoot, loaded.Meshes)
	}
}
