package efmioutline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
)

const serviceFixture = "[CommandList_Draw_Component0]\r\n" +
	"if vs == 202 || vs == 203\r\n" +
	"    if $a\r\n" +
	"        drawindexed = 3, 0, 0\r\n" +
	"    endif\r\n" +
	"    if $b\n" +
	"        drawindexed = 3, 3, 0\n" +
	"    endif\r\n" +
	"endif\r\n"

type fakeGamePaths []db.GamePathRow

func (f fakeGamePaths) List(context.Context) ([]db.GamePathRow, error) { return f, nil }

func newTestMod(t *testing.T, importer string) (*Service, string, string) {
	t.Helper()
	modsRoot := filepath.Join(t.TempDir(), "Mods")
	modDir := filepath.Join(modsRoot, "Character", "Zhuang")
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "mod.ini"), []byte(serviceFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{GamePaths: fakeGamePaths{
		{Game: "Endfield", ModFolderPath: modsRoot, Importer: &importer},
	}})
	return service, modsRoot, modDir
}

func partIDs(t *testing.T, inspection OutlineInspection) map[string]string {
	t.Helper()
	if len(inspection.Files) != 1 {
		t.Fatalf("files = %+v, want one", inspection.Files)
	}
	ids := map[string]string{}
	for _, p := range inspection.Files[0].Parts {
		ids[p.Label] = p.ID
	}
	return ids
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func expectContract(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || !strings.HasPrefix(err.Error(), code+":") {
		t.Fatalf("error = %v, want %s", err, code)
	}
}

func TestApplyRecomputesFromBackupAndRevertRestores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, _, modDir := newTestMod(t, "EFMI")
	iniPath := filepath.Join(modDir, "mod.ini")
	backupPath := iniPath + backupSuffix

	inspection, err := service.Inspect(ctx, modDir)
	if err != nil {
		t.Fatal(err)
	}
	ids := partIDs(t, inspection)
	apply := func(labels ...string) OutlineInspection {
		t.Helper()
		selection := OutlineSelection{Path: "mod.ini", PartIDs: []string{}}
		for _, label := range labels {
			selection.PartIDs = append(selection.PartIDs, ids[label])
		}
		result, err := service.Apply(ctx, OutlineApplyInput{ModPath: modDir, Files: []OutlineSelection{selection}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}

	onlyA := strings.Replace(serviceFixture, "if $a\r\n", "if $a && vs != 203\r\n", 1)
	result := apply("$a")
	if got := readFile(t, iniPath); got != onlyA {
		t.Fatalf("after applying a:\n%q", got)
	}
	if got := readFile(t, backupPath); got != serviceFixture {
		t.Fatalf("backup = %q, want the original", got)
	}
	if file := result.Files[0]; file.State != StateApplied || !file.HasBackup {
		t.Fatalf("file = %+v, want applied with backup", file)
	}

	apply("$a", "$b")
	apply("$a")
	if got := readFile(t, iniPath); got != onlyA {
		t.Fatalf("reapplying a after a and b accumulated edits:\n%q", got)
	}
	if got := readFile(t, backupPath); got != serviceFixture {
		t.Fatalf("backup changed to %q", got)
	}

	if result = apply(); result.Files[0].State != StateOriginal {
		t.Fatalf("empty selection left state %q", result.Files[0].State)
	}
	if got := readFile(t, iniPath); got != serviceFixture {
		t.Fatalf("empty selection did not restore the original:\n%q", got)
	}

	apply("$b")
	if _, err := service.Revert(ctx, modDir, "mod.ini"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, iniPath); got != serviceFixture {
		t.Fatalf("revert did not restore the original:\n%q", got)
	}
	if _, err := os.Stat(backupPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup still present after revert: %v", err)
	}
}

func TestUserEditAfterApplyIsNeverOverwritten(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, _, modDir := newTestMod(t, "EFMI")
	iniPath := filepath.Join(modDir, "mod.ini")

	inspection, err := service.Inspect(ctx, modDir)
	if err != nil {
		t.Fatal(err)
	}
	ids := partIDs(t, inspection)
	selection := []OutlineSelection{{Path: "mod.ini", PartIDs: []string{ids["$a"]}}}
	if _, err := service.Apply(ctx, OutlineApplyInput{ModPath: modDir, Files: selection}); err != nil {
		t.Fatal(err)
	}
	edited := readFile(t, iniPath) + "; tuned by hand\r\n"
	if err := os.WriteFile(iniPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	if inspection, err = service.Inspect(ctx, modDir); err != nil {
		t.Fatal(err)
	}
	if state := inspection.Files[0].State; state != StateModified {
		t.Fatalf("state = %q, want modified", state)
	}
	selection[0].PartIDs = []string{ids["$a"], ids["$b"]}
	_, err = service.Apply(ctx, OutlineApplyInput{ModPath: modDir, Files: selection})
	expectContract(t, err, ErrorModified)
	_, err = service.Revert(ctx, modDir, "mod.ini")
	expectContract(t, err, ErrorModified)
	if got := readFile(t, iniPath); got != edited {
		t.Fatalf("edited file was overwritten:\n%q", got)
	}

	if inspection, err = service.Adopt(ctx, modDir, "mod.ini"); err != nil {
		t.Fatal(err)
	}
	file := inspection.Files[0]
	if file.State != StateOriginal || file.HasBackup {
		t.Fatalf("after adopt file = %+v, want original without backup", file)
	}
	if got := readFile(t, iniPath); got != edited {
		t.Fatalf("adopt changed the file:\n%q", got)
	}
}

func TestRejectsModsOutsideAnEFMIFolder(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, modsRoot, modDir := newTestMod(t, "GIMI")
	iniPath := filepath.Join(modDir, "mod.ini")

	_, err := service.Inspect(ctx, modDir)
	expectContract(t, err, ErrorNotEFMI)
	_, err = service.Apply(ctx, OutlineApplyInput{
		ModPath: modDir,
		Files:   []OutlineSelection{{Path: "mod.ini", PartIDs: []string{}}},
	})
	expectContract(t, err, ErrorNotEFMI)
	if got := readFile(t, iniPath); got != serviceFixture {
		t.Fatalf("rejected apply changed the file:\n%q", got)
	}

	efmi := NewWithOptions(Options{GamePaths: fakeGamePaths{
		{Game: "Endfield", ModFolderPath: modsRoot, Importer: new("EFMI")},
	}})
	_, err = efmi.Inspect(ctx, modsRoot)
	expectContract(t, err, ErrorNotEFMI)
}

// EFMI's Mods folder is commonly a junction to another drive, so a mod opened
// through the link must match a game folder recorded by its physical path.
func TestAcceptsAModReachedThroughAJunction(t *testing.T) {
	t.Parallel()
	service, modsRoot, _ := newTestMod(t, "EFMI")
	link := filepath.Join(t.TempDir(), "LinkedMods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, modsRoot).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	inspection, err := service.Inspect(context.Background(), filepath.Join(link, "Character", "Zhuang"))
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Files) != 1 || len(inspection.Files[0].Parts) != 2 {
		t.Fatalf("inspection = %+v, want one file with two parts", inspection)
	}
}

// A read-only destination makes the second replace fail after the first file
// was already written; the first file must not stay half applied.
func TestApplyRollsBackEarlierFilesWhenALaterWriteFails(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, _, modDir := newTestMod(t, "EFMI")
	first := filepath.Join(modDir, "mod.ini")
	second := filepath.Join(modDir, "zz.ini")
	if err := os.WriteFile(second, []byte(serviceFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	inspection, err := service.Inspect(ctx, modDir)
	if err != nil {
		t.Fatal(err)
	}
	var selections []OutlineSelection
	for _, file := range inspection.Files {
		selection := OutlineSelection{Path: file.Path}
		for _, p := range file.Parts {
			selection.PartIDs = append(selection.PartIDs, p.ID)
		}
		selections = append(selections, selection)
	}
	if len(selections) != 2 || selections[0].Path != "mod.ini" {
		t.Fatalf("selections = %+v, want mod.ini then zz.ini", selections)
	}

	if err := os.Chmod(second, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(second, 0o644) })
	if _, err := service.Apply(ctx, OutlineApplyInput{ModPath: modDir, Files: selections}); err == nil {
		t.Fatal("apply over a read-only file succeeded")
	}

	for _, path := range []string{first, second} {
		if got := readFile(t, path); got != serviceFixture {
			t.Fatalf("%s left changed after the failed apply:\n%q", filepath.Base(path), got)
		}
		if _, err := os.Stat(path + backupSuffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s backup left behind: %v", filepath.Base(path), err)
		}
	}
}
