package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestParseModelViewerINIMatchesElectronCommentsAndDuplicateSections(t *testing.T) {
	parsed := parseModelViewerINI("\ufeff[KeyToggle]\nkey = no_ctrl K ; keep\n$Mode = 0, 1 ; trim\n[KeyToggle]\nback = L ; keep\n$Reset = 0 ; trim\n", "mod.ini")
	if len(parsed.Sections) != 1 {
		t.Fatalf("sections = %#v", parsed.Sections)
	}
	lines := parsed.Sections[0].Lines
	want := []string{"key = no_ctrl K ; keep", "$Mode = 0, 1", "back = L ; keep", "$Reset = 0"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %#v", lines)
	}
	for index := range want {
		if lines[index] != want[index] {
			t.Fatalf("line %d = %q, want %q", index, lines[index], want[index])
		}
	}
	meta := parsed.Lines["KeyToggle"]
	if len(meta) != 4 || meta[0].INIPath != "mod.ini" || meta[0].LineNo != 2 || meta[0].Section != "KeyToggle" {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestDiscoverModelViewerINIsMatchesElectronOrderDepthAndLimit(t *testing.T) {
	root := t.TempDir()
	writeINI := func(relative string) {
		t.Helper()
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("[Constants]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeINI("b.ini")
	writeINI("a.ini")
	writeINI("DISABLEDskip.ini")
	for _, name := range []string{"c.ini", "d.ini", "e.ini", "f.ini", "g.ini", "h.ini", "i.ini", "j.ini", "k.ini"} {
		writeINI(filepath.Join("one", name))
	}
	writeINI(filepath.Join("one", "two", "kept.ini"))
	writeINI(filepath.Join("one", "two", "three", "ignored.ini"))
	paths, err := discoverModelViewerActiveINIs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != maxModelViewerINIFiles {
		t.Fatalf("paths = %#v", paths)
	}
	if filepath.Base(paths[0]) != "a.ini" || filepath.Base(paths[1]) != "b.ini" || filepath.Base(paths[9]) != "j.ini" {
		t.Fatalf("order = %#v", paths)
	}
	for _, path := range paths {
		if filepath.Base(path) == "ignored.ini" || filepath.Base(path) == "DISABLEDskip.ini" {
			t.Fatalf("unexpected path %q", path)
		}
	}
}

func TestDiscoverModelViewerINIsKeepsAllRootFilesAtThreshold(t *testing.T) {
	root := t.TempDir()
	for index := range maxModelViewerINIFiles + 1 {
		path := filepath.Join(root, fmt.Sprintf("%02d.ini", index))
		if err := os.WriteFile(path, []byte("[Constants]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := discoverModelViewerActiveINIs(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != maxModelViewerINIFiles+1 {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestLoadModViewerScopesMultipleINIsAndRebasesResources(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "part")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	ini := func(variable string) string {
		return `[Constants]
global persist $` + variable + ` = 0
[KeyToggle]
type = cycle
$` + variable + ` = 0, 1
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
if $` + variable + ` == 1
drawindexed = 3, 0, 0
endif
` + viewerBodyResources
	}
	if err := os.WriteFile(filepath.Join(root, "A.ini"), []byte(ini("Mode")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "B.ini"), []byte(ini("variant")), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, root)
	writeViewerGeometry(t, sub)
	fixture := loadViewerDir(t, root)
	if len(fixture.result.Meshes) != 2 {
		t.Fatalf("meshes = %#v", fixture.result.Meshes)
	}
	if findViewerVariable(fixture.result, "A::Mode") == nil || findViewerVariable(fixture.result, "B::variant") == nil {
		t.Fatalf("variables = %#v", fixture.result.Variables)
	}
	if fixture.result.DefaultState["A::Mode"] != "0" || fixture.result.DefaultState["B::variant"] != "0" {
		t.Fatalf("defaultState = %#v", fixture.result.DefaultState)
	}
}

func TestModelViewerUnusedResourceDoesNotConsumeLoadBudget(t *testing.T) {
	root := t.TempDir()
	budget, err := newModelViewerLoadBudget(root)
	if err != nil {
		t.Fatal(err)
	}
	resources := []modelViewerResource{
		{Name: "Used", Filename: "missing.buf", Stride: 40},
		{Name: "Unused", Filename: `..\..\escape.buf`, Stride: 40},
	}
	if err := budget.validateReferencedResources(root, resources, map[string]bool{"used": true}); err != nil {
		t.Fatalf("unused resource affected budget: %v", err)
	}
}

func TestLoadModViewerKeepsMenuVariablesWithoutMeshGating(t *testing.T) {
	root := t.TempDir()
	fixture := loadViewerFixture(t, root, `[Constants]
global persist $menu = 0
global persist $other = 0
global $clickedSlot
[CommandListClickedSlot]
if $clickedSlot == 1
    $menu = 1 - $menu
elif $clickedSlot == 2
    $other = 1 - $other
endif
[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
`+viewerBodyResources)
	if findViewerVariable(fixture.result, "menu") == nil || findViewerVariable(fixture.result, "other") == nil {
		t.Fatalf("variables = %#v", fixture.result.Variables)
	}
}

func TestLoadModViewerUsesElectronGeometryErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "empty.ini"), []byte("[Constants]\nglobal $unused = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Protocol: infra.NewProtocol()})
	_, err := service.LoadModViewer(context.Background(), root)
	if err == nil || err.Error() != "No mesh geometry found across 1 ini file(s)." {
		t.Fatalf("err = %v", err)
	}

	root = t.TempDir()
	ini := `[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
drawindexed = 3, 0, 0
` + viewerBodyResources
	if err := os.WriteFile(filepath.Join(root, "missing.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	service = NewWithOptions(Options{Protocol: infra.NewProtocol()})
	_, err = service.LoadModViewer(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "No mesh data could be extracted") {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadModViewerLogsElectronLoadMessages(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	log := infra.NewLogWithOptions(infra.LogOptions{Writer: &buf, DisableFile: true})
	log.SetLevel("info")
	service := NewWithOptions(Options{Protocol: infra.NewProtocol(), Log: log})
	service.UseClient(openToolsTestDB(t))
	if err := os.WriteFile(filepath.Join(root, "empty.ini"), []byte("[Constants]\nglobal $unused = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := service.LoadModViewer(context.Background(), root)
	if err == nil {
		t.Fatal("expected geometry error")
	}
	failed := buf.String()
	if !strings.Contains(failed, "Starting model viewer load") || !strings.Contains(failed, "Model viewer load failed after") || !strings.Contains(failed, "No mesh geometry found") {
		t.Fatalf("failure logs = %q", failed)
	}
	if !strings.Contains(failed, "StaticGlb.loadForViewer") {
		t.Fatalf("failure where = %q", failed)
	}

	buf.Reset()
	writeTextureFile(t, root, "diffuse.png", encodeTinyPNG())
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte(`[TextureOverrideBody]
ib = ResourceBodyIB
vb0 = ResourcePos
vb1 = ResourceTc
Resource\GIMI\Diffuse = ref ResourceDiffuse
drawindexed = 3, 0, 0
`+viewerBodyResources+`
[ResourceDiffuse]
filename = diffuse.png
`), 0o600); err != nil {
		t.Fatal(err)
	}
	writeViewerGeometry(t, root)
	result, err := service.LoadModViewer(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = service.CleanupModelViewer(context.Background(), result.MemorySessionID)
	})
	ok := buf.String()
	if !strings.Contains(ok, "Starting model viewer load") || !strings.Contains(ok, "Texture encoding completed in") || !strings.Contains(ok, "textures=1") || !strings.Contains(ok, "Completed model viewer load in") || !strings.Contains(ok, "meshes=1") {
		t.Fatalf("success logs = %q", ok)
	}
}
