package mod

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func writeModFile(t *testing.T, root, relative, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWalkModCollectsSizeMtimeIniAndPreviewInOnePass(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	group := filepath.Join(root, "CharF")
	modPath := filepath.Join(group, "Costume")
	preview := writeModFile(t, modPath, "preview.png", "image")
	writeModFile(t, modPath, "mod.ini", "[KeyToggle]\nkey = F1\n$swap = 0, 1\n")
	writeModFile(t, modPath, "nested/buffer.buf", "xxxxx")

	walked := walkMod(group, modPath)
	if walked == nil || walked.info == nil {
		t.Fatal("walkMod returned nil")
	}
	if walked.info.Size == 0 || walked.info.Mtime == 0 {
		t.Fatalf("size/mtime not collected: %#v", walked.info)
	}
	if walked.info.Preview == nil || *walked.info.Preview != preview {
		t.Fatalf("preview = %v, want %q", walked.info.Preview, preview)
	}
	if len(walked.iniPaths) != 1 || filepath.Base(walked.iniPaths[0]) != "mod.ini" {
		t.Fatalf("ini paths = %#v", walked.iniPaths)
	}

	info := scanMod(group, modPath)
	if info == nil || len(info.Inis) != 1 || !info.Inis[0].HasToggleKey {
		t.Fatalf("scanMod inis = %#v", info)
	}
}

func TestOrdinaryScannerExcludesOGGWhileNTEPreviewFinderAcceptsIt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	group := filepath.Join(root, "Group")
	modPath := filepath.Join(group, "Mod")
	ogg := writeModFile(t, modPath, "preview.ogg", "audio")
	writeModFile(t, modPath, "mod.ini", "[Constants]\n")
	walked := walkMod(group, modPath)
	if walked == nil || walked.info == nil {
		t.Fatal("ordinary scanner returned nil")
	}
	if walked.info.Preview != nil {
		t.Fatalf("ordinary scanner preview = %q, want nil", *walked.info.Preview)
	}
	previewEqual(t, findPreview(modPath, false), ogg)
}

func TestScanGroupScansModsInParallel(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, name := range []string{"Mod B", "Mod A", "DISABLED Mod C"} {
		writeModFile(t, root, filepath.Join(name, "mod.ini"), "[KeyToggle]\nkey = F1\n$swap = 0, 1\n")
		writeModFile(t, root, filepath.Join(name, "preview.png"), "image")
	}
	if err := os.MkdirAll(filepath.Join(root, "Empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	group := scanGroup(root)
	if group.ModCount != 3 || group.EnabledModCount != 2 {
		t.Fatalf("counts = %+v", group)
	}
	if len(group.Mods) != 3 || group.Mods[0].Name != "DISABLED Mod C" || group.Mods[1].Name != "Mod A" {
		t.Fatalf("sorted mods = %#v", group.Mods)
	}
	for _, info := range group.Mods {
		if info.Preview == nil || len(info.Inis) != 1 {
			t.Fatalf("mod missing preview/ini: %#v", info)
		}
	}
}

func TestScannerAndNteUseTheirSeparateElectronCollations(t *testing.T) {
	t.Parallel()

	ordinaryRoot := filepath.Join(t.TempDir(), "ordinary")
	for _, name := range []string{"Mod10", "Mod2"} {
		writeModFile(t, ordinaryRoot, filepath.Join(name, "mod.ini"), "[Constants]\n")
	}
	ordinary := scanGroup(ordinaryRoot)
	if len(ordinary.Mods) != 2 || ordinary.Mods[0].Name != "Mod2" || ordinary.Mods[1].Name != "Mod10" {
		t.Fatalf("ordinary scanner order = %#v, want natural Mod2, Mod10", modNames(ordinary))
	}

	nteRoot := filepath.Join(t.TempDir(), "nte")
	nteGroup := filepath.Join(nteRoot, "Character", "Group")
	for _, name := range []string{"Mod10", "Mod2"} {
		writePak(t, filepath.Join(nteGroup, name), "mod.pak")
	}
	nte := nteScanGroup(testNteRoots(nteRoot), nteGroup, false)
	if len(nte.Mods) != 2 || nte.Mods[0].Name != "Mod10" || nte.Mods[1].Name != "Mod2" {
		t.Fatalf("NTE order = %#v, want locale Mod10, Mod2", modNames(nte))
	}
}

func TestGetModsLightDefersIniParse(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{preview: true})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "CharF")
	modPath := filepath.Join(group, "Costume")
	preview := writeModFile(t, modPath, "preview.png", "image")
	writeModFile(t, modPath, "mod.ini", "[KeyToggle]\nkey = F1\n$swap = 0, 1\n")
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	light, err := service.GetModsLight(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if light.ModCount != 1 || !light.Mods[0].IsEnabled || len(light.Mods[0].Inis) != 0 {
		t.Fatalf("light = %#v", light)
	}
	if light.Mods[0].Preview == nil || *light.Mods[0].Preview != preview {
		t.Fatalf("light preview = %v, want %q", light.Mods[0].Preview, preview)
	}
	if light.Mods[0].Size != 0 || light.Mods[0].Mtime != 0 {
		t.Fatalf("light should omit size/mtime: %#v", light.Mods[0])
	}

	full, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if len(full.Mods[0].Inis) != 1 || !full.Mods[0].Inis[0].HasToggleKey {
		t.Fatalf("full inis = %#v", full.Mods[0].Inis)
	}
	if full.Mods[0].Size == 0 {
		t.Fatal("full scan missing size")
	}
}

func TestGetModsReparsesIniPathsWithoutScanCache(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{preview: true})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "CharF")
	modPath := filepath.Join(group, "Costume")
	iniPath := writeModFile(t, modPath, "CC.ini", "[KeyToggle]\nkey = F1\n$swap = 0, 1\n")
	faceDir := filepath.Join(modPath, "DD_face")
	faceINI := writeModFile(t, faceDir, "face.ini", "[KeyToggle]\nkey = F3\n$swap = 0\n")
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	first, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if cached, err := service.client.ModScanCache.Get(ctx, first.Mods[0].Path); err != nil || cached != nil {
		t.Fatalf("first scan should not write cache: %+v %v", cached, err)
	}
	if len(first.Mods[0].Inis) != 2 {
		t.Fatalf("first scan inis = %#v", first.Mods[0].Inis)
	}

	renamedINI := filepath.Join(modPath, "CC88.ini")
	if err := os.Rename(iniPath, renamedINI); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(renamedINI, []byte("[KeyToggle]\nkey = F2\n$swap = 0, 1, 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	renamedFaceDir := filepath.Join(modPath, "DD_face8")
	if err := os.Rename(faceDir, renamedFaceDir); err != nil {
		t.Fatal(err)
	}
	renamedFaceINI := filepath.Join(renamedFaceDir, "face.ini")
	if _, err := os.Stat(renamedFaceINI); err != nil {
		t.Fatal(err)
	}

	second, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	if cached, err := service.client.ModScanCache.Get(ctx, second.Mods[0].Path); err != nil || cached != nil {
		t.Fatalf("second scan should not write cache: %+v %v", cached, err)
	}
	if len(second.Mods[0].Inis) != 2 {
		t.Fatalf("second scan inis = %#v", second.Mods[0].Inis)
	}
	paths := map[string]IniResult{}
	for _, ini := range second.Mods[0].Inis {
		paths[ini.Path] = ini
	}
	if got, ok := paths[renamedINI]; !ok || got.Name != "CC88.ini" || got.ToggleKeys[0].Key == nil || *got.ToggleKeys[0].Key != "F2" || len(got.ToggleKeys[0].Values) != 3 {
		t.Fatalf("renamed ini not reparsed: %#v", second.Mods[0].Inis)
	}
	if _, ok := paths[renamedFaceINI]; !ok {
		t.Fatalf("renamed nested ini missing: %#v", second.Mods[0].Inis)
	}
	if _, err := os.Stat(faceINI); !os.IsNotExist(err) {
		t.Fatalf("old nested ini still present: %v", err)
	}
}
