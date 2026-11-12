package togglepersist

import (
	"os"
	"path/filepath"
	"testing"
)

// d3dx_user.ini holds the saved values of every mod, so a preset may change only the entries it owns.
func TestUpdateUserINIRewritesOnlyExistingEntriesOfTheMod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d3dx_user.ini")
	original := "\xEF\xBB\xBF; saved by 3DMigoto\r\n" +
		"[Constants]\r\n" +
		"$\\mods\\baz\\baz.ini\\outfit = 3\r\n" +
		"$\\Custom\\Foo\\Outfit = 1\r\n" +
		"$\\mods\\foo\\foo.ini\\outfit=1\r\n" +
		"$\\custom\\foo\\glow = 0.5\r\n" +
		"[Present]\r\n" +
		"$\\custom\\foo\\outfit = 9\r\n"
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		`$\custom\foo\outfit`:       "2",
		`$\mods\foo\foo.ini\outfit`: "2",
		`$\custom\foo\glow`:         "0.5",
		`$\custom\foo\hat`:          "1",
	}

	changed, err := updateUserINI(path, values)
	if err != nil {
		t.Fatal(err)
	}
	want := "\xEF\xBB\xBF; saved by 3DMigoto\r\n" +
		"[Constants]\r\n" +
		"$\\mods\\baz\\baz.ini\\outfit = 3\r\n" +
		"$\\Custom\\Foo\\Outfit = 2\r\n" +
		"$\\mods\\foo\\foo.ini\\outfit = 2\r\n" +
		"$\\custom\\foo\\glow = 0.5\r\n" +
		"[Present]\r\n" +
		"$\\custom\\foo\\outfit = 9\r\n"
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if changed != 2 || string(got) != want {
		t.Fatalf("updateUserINI changed %d lines:\n%q\nwant 2 lines:\n%q", changed, got, want)
	}

	if changed, err := updateUserINI(path, values); err != nil || changed != 0 {
		t.Fatalf("second update changed %d lines, error %v; want none", changed, err)
	}
	missing := filepath.Join(t.TempDir(), "d3dx_user.ini")
	if changed, err := updateUserINI(missing, values); err != nil || changed != 0 {
		t.Fatalf("missing file: changed %d, error %v", changed, err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("missing d3dx_user.ini was created: %v", err)
	}
}
