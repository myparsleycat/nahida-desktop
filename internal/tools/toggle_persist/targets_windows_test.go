package togglepersist

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistWatcherSupportsJunctionModsRoot(t *testing.T) {
	t.Parallel()
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	mods := filepath.Join(filepath.Dir(harness.d3dxPath), "Mods")
	external := filepath.Join(t.TempDir(), "External Mods")
	if err := os.Rename(mods, external); err != nil {
		t.Fatal(err)
	}
	createPersistJunction(t, mods, external)
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.engine.Stop)

	harness.trigger([][2]string{{"Toggle", "2"}})
	harness.engine.Advance(3_000)
	raw, err := os.ReadFile(filepath.Join(external, "Example", "mod.ini"))
	if err != nil || !strings.Contains(string(raw), "$Toggle = 2") {
		t.Fatalf("junction target content = %q, %v; diagnostics = %v", raw, err, harness.errors)
	}
}

func TestPersistWatcherIgnoresNestedJunctions(t *testing.T) {
	t.Parallel()
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	mods := filepath.Join(filepath.Dir(harness.d3dxPath), "Mods")
	collection := filepath.Join(t.TempDir(), "External Mods")
	if err := os.Rename(mods, collection); err != nil {
		t.Fatal(err)
	}
	createPersistJunction(t, mods, collection)
	external := t.TempDir()
	path := filepath.Join(external, "mod.ini")
	input := "namespace = Shared\n[Constants]\nglobal persist $Toggle = 7\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	createPersistJunction(t, filepath.Join(collection, "escape"), external)
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.engine.Stop)

	triggerPersistContent(t, harness, `$\Shared\Toggle = 9`)
	harness.engine.Advance(3_000)
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != input {
		t.Fatalf("nested junction INI changed: %q, %v", raw, err)
	}
}

func createPersistJunction(t *testing.T, link, target string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction: %v (%s)", err, output)
	}
}
