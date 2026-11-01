package touchprofile

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestTouchProfilePrepareBelowLinkedModsFolder(t *testing.T) {
	base := t.TempDir()
	stored := filepath.Join(base, "Stored")
	writeTouchTestMod(t, filepath.Join(stored, "Hero"))
	mods := filepath.Join(base, "Mods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", mods, stored).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	service := NewWithOptions(Options{Mod: touchTestMod{}})
	useTouchTestAppData(t, service, filepath.Join(base, "user-data"))

	inspection, err := service.TouchProfilePrepare(
		context.Background(),
		TouchProfileLoadInput{ModPath: filepath.Join(mods, "Hero")},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Components) != 1 {
		t.Fatalf("components = %#v", inspection.Components)
	}
}
