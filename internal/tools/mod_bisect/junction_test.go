package modbisect

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBisectWorksInLinkedModsFolder(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	stored := filepath.Join(base, "Stored")
	for _, mod := range []string{"Enabled", "Interrupted"} {
		if err := os.MkdirAll(filepath.Join(stored, mod), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(stored, "Enabled", "mod.ini"), []byte("[Constants]"), 0o600); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(stored, "Interrupted", "mod.ini")
	if err := os.WriteFile(bisectDisabledPath(orphan), []byte("[Constants]"), 0o600); err != nil {
		t.Fatal(err)
	}
	mods := filepath.Join(base, "Mods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", mods, stored).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	relative, err := resolveBisectExclude(mods, filepath.Join(mods, "Enabled"))
	if err != nil || relative != "Enabled" {
		t.Fatalf("exclude = %q, %v", relative, err)
	}
	inis, err := scanEnabledINIs(mods)
	if want := filepath.Join(mods, "Enabled", "mod.ini"); err != nil || len(inis) != 1 || inis[0] != want {
		t.Fatalf("enabled INIs = %v, %v; want %q", inis, err, want)
	}
	orphans, err := listBisectOrphans(t.Context(), mods)
	if want := filepath.Join(mods, "Interrupted", "mod.ini"); err != nil || len(orphans) != 1 || orphans[0] != want {
		t.Fatalf("orphans = %v, %v; want %q", orphans, err, want)
	}
}

func TestResolveBisectExcludeAcceptsModLinkedIn(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	mods, elsewhere := filepath.Join(base, "Mods"), filepath.Join(base, "Elsewhere")
	for _, dir := range []string{mods, elsewhere} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(mods, "Linked")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, elsewhere).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	if relative, err := resolveBisectExclude(mods, link); err != nil || relative != "Linked" {
		t.Fatalf("exclude = %q, %v", relative, err)
	}
}
