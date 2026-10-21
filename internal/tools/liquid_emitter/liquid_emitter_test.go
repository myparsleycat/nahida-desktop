package liquidemitter

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

type fakeGamePaths []db.GamePathRow

func (f fakeGamePaths) List(context.Context) ([]db.GamePathRow, error) { return f, nil }

type fakeViewer map[string]modelviewer.DrawSource

func (f fakeViewer) DrawSource(_, meshID string) (modelviewer.DrawSource, bool) {
	source, ok := f[meshID]
	return source, ok
}

func TestProfileFollowsIndexBufferOrder(t *testing.T) {
	// The viewer lists the corners as (a, c, b); the index buffer holds (a, b, c).
	input := ApplyInput{
		FaceVertices:      []uint32{10, 12, 11},
		Barycentric:       []float64{.2, .5, .3},
		TrianglePositions: []float64{0, 0, 0, 0, 1, 0, 1, 0, 0},
	}
	triangle, err := orderTriangle(input, [3]uint32{10, 11, 12})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := buildSurfaceProfile(triangle, vec3{1, 0, 0}, vec3{.01, .02, .03}, -1)
	if err != nil {
		t.Fatal(err)
	}

	// Expected values follow AttachmentEditor apply_anchor for this frame:
	// up=(-1,0,0), right=(0,0,1), forward=(0,-1,0); tangent=X, bitangent=Y, normal=Z.
	want := [12]float64{-1, .2, .3, .5, 0, -1, 0, 0, -1, 0, 0, 0}
	for index := range want {
		if math.Abs(profile.row[index]-want[index]) > 1e-9 {
			t.Fatalf("row = %v, want %v", profile.row, want)
		}
	}
	if offset := (vec3{.03, -.01, -.02}); profile.offset.sub(offset).dot(profile.offset.sub(offset)) > 1e-18 {
		t.Fatalf("local offset = %v, want %v", profile.offset, offset)
	}

	if _, err := orderTriangle(input, [3]uint32{10, 11, 13}); !hasCode(err, ErrorDrawMismatch) {
		t.Fatalf("mismatched face error = %v", err)
	}
	if _, err := buildSurfaceProfile(triangle, vec3{0, 0, -1}, vec3{.2, .2, 0}, -1); !hasCode(err, ErrorOffsetLimit) {
		t.Fatalf("offset beyond 25 cm error = %v", err)
	}
}

const characterINI = "\xef\xbb\xbf[TextureOverrideBody]\r\n" +
	"hash = 1234\r\n" +
	"run = CommandListBody\r\n" +
	"\r\n" +
	"[CommandListBody]\r\n" +
	"if $outfit == 0\r\n" +
	"\tdrawindexed = 6, 3, 0\r\n" +
	"else\r\n" +
	"\tdrawindexed = 6, 3, 0 ; same part\r\n" +
	"\tdrawindexed = $count, 3, 0\r\n" +
	"endif\r\n" +
	"drawindexed = 9, 3, 0\r\n"

var bodyDraw = modelviewer.DrawSource{Section: "Body", StartIndex: 3, IndexCount: 6}

func TestINIHooksRoundTrip(t *testing.T) {
	text, err := parseINIText([]byte(characterINI))
	if err != nil {
		t.Fatal(err)
	}
	sites := text.drawSites(bodyDraw)
	if len(sites) != 2 {
		t.Fatalf("sites = %+v, want both literal 6,3,0 draws", sites)
	}
	text.insertHooks(sites, 7, 6, 0)
	patched := string(text.bytes())
	if strings.Count(patched, "\t; nahida-liquid 7 begin\r\n\tx243 = 7\r\n\ty243 = 6\r\n\tz243 = 0\r\n\t"+
		captureCommand+"\r\n\t; nahida-liquid 7 end\r\n\tdrawindexed = 6, 3, 0") != 2 {
		t.Fatalf("hooks not placed above both draws:\n%s", patched)
	}
	if again := text.drawSites(
		bodyDraw,
	); !slices.Equal(again[0].pids, []int{7}) ||
		!slices.Equal(again[1].pids, []int{7}) {
		t.Fatalf("existing hooks not detected: %+v", again)
	}

	// Another emitter on the same draw stacks its block under the first, and
	// each comes off on its own.
	text.insertHooks(text.drawSites(bodyDraw), 8, 9, 0)
	if again := text.drawSites(bodyDraw); !slices.Equal(again[0].pids, []int{8, 7}) ||
		!slices.Equal(again[1].pids, []int{8, 7}) {
		t.Fatalf("stacked hooks not detected: %+v", again)
	}
	if removed := text.removeHooks(func(pid int) bool { return pid == 7 }); removed != 2 {
		t.Fatalf("removed %d blocks", removed)
	}
	if again := text.drawSites(bodyDraw); !slices.Equal(again[0].pids, []int{8}) {
		t.Fatalf("hook left on the draw = %+v", again)
	}
	if removed := text.removeHooks(func(pid int) bool { return pid == 8 }); removed != 2 {
		t.Fatalf("removed %d blocks", removed)
	}
	if !bytes.Equal(text.bytes(), []byte(characterINI)) {
		t.Fatalf("removal did not restore the original:\n%q", text.bytes())
	}

	// The command list is reached second but sits above the override in the file.
	const listFirst = "[CommandListBody]\ndrawindexed = 6, 3, 0\n\n" +
		"[TextureOverrideBody]\nrun = CommandListBody\ndrawindexed = 6, 3, 0\n"
	text, err = parseINIText([]byte(listFirst))
	if err != nil {
		t.Fatal(err)
	}
	text.insertHooks(text.drawSites(bodyDraw), 7, 6, 0)
	if patched := string(text.bytes()); strings.Count(patched, "; nahida-liquid 7 end\ndrawindexed = 6, 3, 0") != 2 {
		t.Fatalf("hooks not placed above both draws:\n%s", patched)
	}

	// EFMI exports name the instance arguments instead of writing numbers.
	const efmi = "[TextureOverride_Component0]\nrun = CommandList_Draw_Component0\n\n" +
		"[CommandList_Draw_Component0]\ndrawindexedinstanced = 6, INSTANCE_COUNT, 3, 0, FIRST_INSTANCE\n" +
		"drawindexedinstanced = 9, INSTANCE_COUNT, 3, 0, FIRST_INSTANCE\n"
	if text, err = parseINIText([]byte(efmi)); err != nil {
		t.Fatal(err)
	}
	component := modelviewer.DrawSource{Section: "_Component0", StartIndex: 3, IndexCount: 6}
	if sites := text.drawSites(component); len(sites) != 1 || sites[0].line != 4 {
		t.Fatalf("EFMI draw sites = %+v", sites)
	}

	// Draws with the same arguments over different index buffers are separate
	// parts. The buffer bound before a run carries into the command list, and
	// each branch of an if starts from the buffer bound before it.
	for _, sample := range []struct {
		ini   string
		lines map[string]int
	}{
		{
			ini: "[TextureOverrideBody]\nib = ResourceBodyIB\ndrawindexed = 6, 3, 0\n" +
				"ib = ref ResourceArmIB\nrun = CommandListArm\n\n[CommandListArm]\ndrawindexed = 6, 3, 0\n",
			lines: map[string]int{"BodyIB": 2, "ArmIB": 7},
		},
		{
			ini: "[TextureOverrideBody]\nif $outfit == 0\n\tib = ResourceBodyIB\n\tdrawindexed = 6, 3, 0\n" +
				"else\n\tib = ResourceArmIB\n\tdrawindexed = 6, 3, 0\nendif\n",
			lines: map[string]int{"BodyIB": 3, "ArmIB": 6},
		},
	} {
		if text, err = parseINIText([]byte(sample.ini)); err != nil {
			t.Fatal(err)
		}
		for ib, line := range sample.lines {
			draw := bodyDraw
			draw.IB = ib
			sites := text.drawSites(draw)
			if len(sites) != 1 || sites[0].line != line {
				t.Fatalf("draw sites over %s = %+v, want line %d in\n%s", ib, sites, line, sample.ini)
			}
			if text.sharedDraw(draw, sites) {
				t.Fatalf("draw over %s reported as shared in\n%s", ib, sample.ini)
			}
		}
	}

	// A command list run under two buffers would fire one hook for both parts.
	const shared = "[TextureOverrideBody]\nib = ResourceBodyIB\nrun = CommandListShared\n\n" +
		"[TextureOverrideArm]\nib = ResourceArmIB\nrun = CommandListShared\n\n" +
		"[CommandListShared]\ndrawindexed = 6, 3, 0\n"
	if text, err = parseINIText([]byte(shared)); err != nil {
		t.Fatal(err)
	}
	sharedBody := bodyDraw
	sharedBody.IB = "BodyIB"
	if sites := text.drawSites(sharedBody); len(sites) != 1 || !text.sharedDraw(sharedBody, sites) {
		t.Fatalf("draw in a command list shared with another buffer = %+v", sites)
	}

	// One CRLF line in an LF file must not merge the hook lines.
	const mixed = "[TextureOverrideBody]\r\nrun = CommandListBody\n\n[CommandListBody]\n"
	hooked := mixed + "; nahida-liquid 7 begin\nx243 = 7\ny243 = 6\nz243 = 0\n" + captureCommand +
		"\n; nahida-liquid 7 end\ndrawindexed = 6, 3, 0\n"
	if text, err = parseINIText([]byte(hooked)); err != nil {
		t.Fatal(err)
	}
	if removed := text.removeHooks(func(pid int) bool { return pid == 7 }); removed != 1 ||
		string(text.bytes()) != mixed+"drawindexed = 6, 3, 0\n" {
		t.Fatalf("removed %d blocks from a mixed line-ending file:\n%q", removed, text.bytes())
	}
}

func TestServiceRemoveRefusesDamagedHook(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if _, err := f.service.Apply(ctx, f.applyInput(0)); err != nil {
		t.Fatal(err)
	}
	patched, _ := os.ReadFile(f.iniPath)
	damaged := strings.Replace(string(patched), "; nahida-liquid 0 end", "; edited", 1)
	if err := os.WriteFile(f.iniPath, []byte(damaged), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: f.modRoot}); !hasCode(err, ErrorDamagedHook) {
		t.Fatalf("remove with a missing end marker = %v", err)
	}
	if current, _ := os.ReadFile(f.iniPath); string(current) != damaged {
		t.Fatalf("damaged INI was rewritten:\n%s", current)
	}
	if backup, err := os.ReadFile(f.iniPath + backupSuffix); err != nil || string(backup) != characterINI {
		t.Fatalf("backup lost (err=%v)", err)
	}
}

func TestServiceKeepsSlotsOfHooksAfterRuntimeLoss(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if _, err := f.service.Apply(ctx, f.applyInput(0)); err != nil {
		t.Fatal(err)
	}
	copyRoot := f.modRoot + " - Copy"
	if err := os.MkdirAll(copyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(f.modRoot)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(f.modRoot, entry.Name()))
		if err := os.WriteFile(filepath.Join(copyRoot, entry.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.RemoveAll(filepath.Join(f.modsRoot, modruntime.GroupDir)); err != nil {
		t.Fatal(err)
	}

	inspection, err := f.service.Inspect(ctx, f.modRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Emitters) != 1 || inspection.Emitters[0].PID != 0 || !inspection.Emitters[0].Hooked {
		t.Fatalf("left-over hook not listed: %+v", inspection.Emitters)
	}
	// Without a profile only the copied marker tells the copy apart; removing
	// from it must keep the original's slot reserved.
	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: copyRoot, PIDs: []int{0}}); err != nil {
		t.Fatal(err)
	}
	if copied, _ := os.ReadFile(filepath.Join(copyRoot, "mod.ini")); string(copied) != characterINI {
		t.Fatalf("left-over hook in the copy was not removed:\n%s", copied)
	}

	other := filepath.Join(f.modsRoot, "Chen", "Chen Other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mod.ini", "body.ib"} {
		data, _ := os.ReadFile(filepath.Join(filepath.Dir(f.iniPath), name))
		if name == "mod.ini" {
			data = []byte(characterINI)
		}
		if err := os.WriteFile(filepath.Join(other, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	viewer := f.service.viewer.(fakeViewer)
	source := viewer["mod:body:0"]
	source.INIPath, source.IBPath = filepath.Join(other, "mod.ini"), filepath.Join(other, "body.ib")
	viewer["other:body:0"] = source
	input := f.applyInput(0)
	input.ModPath, input.MeshID = other, "other:body:0"
	applied, err := f.service.Apply(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Emitters) != 1 || applied.Emitters[0].PID == 0 {
		t.Fatalf("slot still hooked by another mod was reassigned: %+v", applied.Emitters)
	}

	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: f.modRoot}); err != nil {
		t.Fatal(err)
	}
	if restored, _ := os.ReadFile(f.iniPath); string(restored) != characterINI {
		t.Fatalf("left-over hook was not removed:\n%s", restored)
	}
}

// EFMI's Mods folder is often a junction, which filepath.WalkDir does not
// enter, and a mod can be opened through either spelling.
func TestServiceFollowsJunctionedModsRoot(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	link := filepath.Join(t.TempDir(), "LinkedMods")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, f.modsRoot).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	efmi := "EFMI"
	f.service.games = fakeGamePaths{{Game: "endfield", ModFolderPath: link, Importer: &efmi}}

	if _, err := f.service.Apply(ctx, f.applyInput(0)); err != nil {
		t.Fatal(err)
	}
	key, _ := readModMarker(f.modRoot)

	// The same mod through the junction is not a copy of itself.
	viewer := f.service.viewer.(fakeViewer)
	aliasRoot := filepath.Join(link, "Chen", "Chen Swimsuit")
	source := viewer["mod:body:0"]
	source.INIPath, source.IBPath = filepath.Join(aliasRoot, "mod.ini"), filepath.Join(aliasRoot, "body.ib")
	viewer["alias:body:0"] = source
	input := f.applyInput(1)
	input.ModPath, input.MeshID = aliasRoot, "alias:body:0"
	if _, err := f.service.Apply(ctx, input); err != nil {
		t.Fatal(err)
	}
	if again, _ := readModMarker(f.modRoot); again != key {
		t.Fatalf("mod opened through the junction was split from itself: %s -> %s", key, again)
	}

	// A real copy is still found below the junctioned root.
	copyRoot := f.modRoot + " - Copy"
	if err := os.MkdirAll(copyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(f.modRoot)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(f.modRoot, entry.Name()))
		if err := os.WriteFile(filepath.Join(copyRoot, entry.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: copyRoot}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(runtimeRootFor(f.modsRoot), profilesDir, key+".json")); err != nil {
		t.Fatalf("removing from the copy deleted the original's profile: %v", err)
	}

	foreign := filepath.Join(f.modsRoot, "Effects", "LiquidGravityProbe")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(foreign, "Probe.ini"),
		[]byte("namespace = LiquidGravityProbe\r\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if inspection, err := f.service.Inspect(ctx, aliasRoot); err != nil ||
		inspection.Runtime.State != modruntime.StateForeign {
		t.Fatalf("second install below the junction = %+v (err=%v)", inspection.Runtime, err)
	}
	if _, err := f.service.Apply(ctx, input); !hasCode(err, ErrorRuntimeForeign) {
		t.Fatalf("apply with a second install = %v", err)
	}
}

type serviceFixture struct {
	service  *Service
	modsRoot string
	modRoot  string
	iniPath  string
}

func newServiceFixture(t *testing.T) serviceFixture {
	t.Helper()
	return newGameServiceFixture(t, "EFMI")
}

func newGameServiceFixture(t *testing.T, importer string) serviceFixture {
	t.Helper()
	modsRoot := t.TempDir()
	modRoot := filepath.Join(modsRoot, "Chen", "Chen Swimsuit")
	if err := os.MkdirAll(modRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	iniPath := filepath.Join(modRoot, "mod.ini")
	if err := os.WriteFile(iniPath, []byte(characterINI), 0o644); err != nil {
		t.Fatal(err)
	}
	ib := make([]byte, 0, 4*12)
	for _, index := range []uint32{0, 0, 0, 0, 1, 2, 2, 1, 3, 9, 9, 9} {
		ib = binary.LittleEndian.AppendUint32(ib, index)
	}
	ibPath := filepath.Join(modRoot, "body.ib")
	if err := os.WriteFile(ibPath, ib, 0o644); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{
		GamePaths: fakeGamePaths{{Game: strings.ToLower(importer), ModFolderPath: modsRoot, Importer: &importer}},
		Viewer: fakeViewer{"mod:body:0": {
			INIPath: iniPath, Section: "Body", IBPath: ibPath, IBFormat: "DXGI_FORMAT_R32_UINT",
			StartIndex: 3, IndexCount: 6, BaseVertex: 0,
		}},
	})
	return serviceFixture{service: service, modsRoot: modsRoot, modRoot: modRoot, iniPath: iniPath}
}

func (f serviceFixture) applyInput(face int) ApplyInput {
	vertices := [][]uint32{{0, 1, 2}, {2, 1, 3}}[face]
	return ApplyInput{
		ModPath: f.modRoot, ViewerSessionID: "session", MeshID: "mod:body:0", FaceIndex: face,
		FaceVertices:      vertices,
		Barycentric:       []float64{.25, .25, .5},
		TrianglePositions: []float64{0, 0, 1, 1, 0, 1, 0, 1, 1},
		Point:             []float64{.2, .2, 1},
		Offset:            []float64{0, 0, .01},
		Direction:         []float64{0, 0, -1},
	}
}

func TestServiceApplyAndRemoveRestoreMod(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()

	inspection, err := f.service.Apply(ctx, f.applyInput(0))
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Runtime.State != modruntime.StateCurrent || len(inspection.Emitters) != 1 ||
		!inspection.Emitters[0].Hooked {
		t.Fatalf("inspection after apply = %+v", inspection)
	}
	first := inspection.Emitters[0].PID
	// Another face of the same draw adds an emitter whose hooks stack on the first's.
	inspection, err = f.service.Apply(ctx, f.applyInput(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Emitters) != 2 {
		t.Fatalf("second apply = %+v", inspection.Emitters)
	}
	patched, _ := os.ReadFile(f.iniPath)
	if strings.Count(string(patched), "y243 = 3") != 2 || strings.Count(string(patched), "y243 = 6") != 2 {
		t.Fatalf("hooks of both emitters not placed:\n%s", patched)
	}
	// Naming an emitter moves it and keeps its slot.
	moved := f.applyInput(1)
	moved.PID = &first
	if inspection, err = f.service.Apply(ctx, moved); err != nil {
		t.Fatal(err)
	}
	if len(inspection.Emitters) != 2 || !slices.ContainsFunc(inspection.Emitters, func(e Emitter) bool {
		return e.PID == first && e.Anchor.FaceIndex == 1
	}) {
		t.Fatalf("moved emitter = %+v", inspection.Emitters)
	}
	if patched, _ = os.ReadFile(f.iniPath); strings.Count(string(patched), "y243 = 6") != 4 ||
		strings.Contains(string(patched), "y243 = 3") {
		t.Fatalf("hooks do not follow the moved emitter:\n%s", patched)
	}

	// Moving an emitter out of another INI takes its hooks and backup with it.
	otherINI := filepath.Join(f.modRoot, "other.ini")
	if err := os.WriteFile(otherINI, []byte(characterINI), 0o644); err != nil {
		t.Fatal(err)
	}
	viewer := f.service.viewer.(fakeViewer)
	otherSource := viewer["mod:body:0"]
	otherSource.INIPath = otherINI
	viewer["mod:other:0"] = otherSource
	inOther := f.applyInput(0)
	inOther.MeshID = "mod:other:0"
	if inspection, err = f.service.Apply(ctx, inOther); err != nil {
		t.Fatal(err)
	}
	third := slices.IndexFunc(inspection.Emitters, func(e Emitter) bool { return e.INIPath == "other.ini" })
	if third < 0 {
		t.Fatalf("emitter in other.ini missing: %+v", inspection.Emitters)
	}
	back := f.applyInput(0)
	back.PID = &inspection.Emitters[third].PID
	if _, err := f.service.Apply(ctx, back); err != nil {
		t.Fatal(err)
	}
	if other, _ := os.ReadFile(otherINI); string(other) != characterINI {
		t.Fatalf("hook left in the INI the emitter moved out of:\n%s", other)
	}
	if _, err := os.Stat(otherINI + backupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup of the INI without hooks kept (err=%v)", err)
	}

	if inspection, err = f.service.Apply(ctx, f.applyInput(0)); err != nil || len(inspection.Emitters) != maxEmitters {
		t.Fatalf("fourth emitter = %+v (err=%v)", inspection.Emitters, err)
	}
	if _, err := f.service.Apply(ctx, f.applyInput(0)); !hasCode(err, ErrorEmitterLimit) {
		t.Fatalf("emitter over the limit = %v", err)
	}
	missing := f.applyInput(0)
	missing.PID = new(int)
	*missing.PID = 4095
	if _, err := f.service.Apply(ctx, missing); !hasCode(err, ErrorEmitterNotFound) {
		t.Fatalf("moving an emitter the mod lacks = %v", err)
	}

	runtimeRoot := runtimeRootFor(f.modsRoot)
	key, err := readModMarker(f.modRoot)
	if err != nil || key == "" {
		t.Fatalf("marker key=%q err=%v", key, err)
	}
	profile, err := os.ReadFile(filepath.Join(runtimeRoot, profilesDir, key+".json"))
	if err != nil {
		t.Fatal(err)
	}
	head := string(profile[:min(len(profile), 2048)])
	if !strings.Contains(head, `"enabled": true`) || !strings.Contains(head, `"gpu_data": [`) {
		t.Fatalf("GPU keys are not in the first 2048 bytes:\n%s", head)
	}
	loader, _ := os.ReadFile(filepath.Join(runtimeRoot, filepath.FromSlash(profileLoaderFile)))
	if !strings.Contains(string(loader), "filename = ../Profiles/"+key+".json") {
		t.Fatalf("loader does not bind the profile:\n%s", loader)
	}

	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: f.modRoot}); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(f.iniPath)
	if !bytes.Equal(restored, []byte(characterINI)) {
		t.Fatalf("remove did not restore the INI:\n%q", restored)
	}
	for _, leftover := range []string{
		f.iniPath + backupSuffix,
		filepath.Join(f.modRoot, modMarkerFile),
		filepath.Join(runtimeRoot, profilesDir, key+".json"),
	} {
		if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists (err=%v)", leftover, err)
		}
	}
}

// Wuthering Waves meshes are in centimeters, while the runtime and its 25 cm
// offset limit are in meters.
func TestServiceScalesOffsetToMeters(t *testing.T) {
	f := newGameServiceFixture(t, "WWMI")
	input := f.applyInput(0)
	input.Offset = []float64{0, 0, 20}
	inspection, err := f.service.Apply(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.MeshUnit != .01 {
		t.Fatalf("mesh unit = %v", inspection.MeshUnit)
	}
	key, err := readModMarker(f.modRoot)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := readProfile(runtimeLayout{root: runtimeRootFor(f.modsRoot)}, key)
	if err != nil || len(doc.Emitters) != 1 {
		t.Fatalf("profile = %+v (err=%v)", doc, err)
	}
	offset := vec3(doc.Emitters[0].Offset)
	if length := math.Sqrt(offset.dot(offset)); math.Abs(length-.2) > 1e-9 {
		t.Fatalf("runtime offset = %v, want 0.2 m", offset)
	}
}

// LoadProfilesJSON reads an emitter's look as ten numbers after its 16, in
// this order, and a runtime from before looks rejects any record that is not
// 16 long; an emitter that keeps the global settings must still write 16.
func TestServiceLookExtendsGPURecords(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	look := Look{
		Force:        2,
		Amount:       0.5,
		CustomColor:  true,
		Tint:         [3]float64{0.2, 0.4, 1},
		AbsorptionMM: 30,
		Viscosity:    4,
		Nozzle:       NozzleSpray,
	}
	if _, err := f.service.SetLook(ctx, LookInput{ModPath: f.modRoot, Look: look}); !hasCode(err, ErrorNoEmitters) {
		t.Fatalf("look without emitters = %v", err)
	}
	applied, err := f.service.Apply(ctx, f.applyInput(0))
	if err != nil {
		t.Fatal(err)
	}
	pid := applied.Emitters[0].PID
	setLook := func(look Look) (Inspection, error) {
		return f.service.SetLook(ctx, LookInput{ModPath: f.modRoot, PID: pid, Look: look})
	}
	for _, change := range []func(*Look){
		func(l *Look) { l.Force = 3 },
		func(l *Look) { l.Amount = 0.01 },
		func(l *Look) { l.Viscosity = 20 },
		func(l *Look) { l.Nozzle = "fog" },
		func(l *Look) { l.Scattering = -0.1 },
		func(l *Look) { l.Roughness = 1.5 },
		func(l *Look) { l.Scattering = math.NaN() },
		func(l *Look) { l.Roughness = math.Inf(1) },
	} {
		invalid := look
		change(&invalid)
		if _, err := setLook(invalid); !hasCode(err, ErrorInvalidLook) {
			t.Fatalf("out of range look %+v = %v", invalid, err)
		}
	}
	if _, err := f.service.SetLook(ctx, LookInput{ModPath: f.modRoot, PID: pid + 1, Look: look}); !hasCode(
		err,
		ErrorEmitterNotFound,
	) {
		t.Fatalf("look of an emitter the mod lacks = %v", err)
	}

	key, _ := readModMarker(f.modRoot)
	profilePath := filepath.Join(runtimeRootFor(f.modsRoot), profilesDir, key+".json")
	records := func() [][]float64 {
		t.Helper()
		var doc profileDocument
		if _, err := readJSON(profilePath, &doc); err != nil {
			t.Fatal(err)
		}
		return doc.GPUData
	}
	if _, err := setLook(look); err != nil {
		t.Fatal(err)
	}
	// Moving the emitter keeps its look, and another emitter starts from the
	// runtime's.
	moved := f.applyInput(1)
	moved.PID = &pid
	if _, err := f.service.Apply(ctx, moved); err != nil {
		t.Fatal(err)
	}
	inspection, err := f.service.Apply(ctx, f.applyInput(0))
	if err != nil {
		t.Fatal(err)
	}
	for _, emitter := range inspection.Emitters {
		want := defaultLook()
		if emitter.PID == pid {
			want = look
		}
		if emitter.Look != want {
			t.Fatalf("look of emitter %d = %+v", emitter.PID, emitter.Look)
		}
	}
	got := records()
	if len(got) != 2 || len(got[0]) != 26 ||
		!slices.Equal(got[0][16:], []float64{2, 1, 0.2, 0.4, 1, 0.03, 0, 4, 0.5, 0}) || len(got[1]) != 16 {
		t.Fatalf("GPU records with one emitter's look = %v", got)
	}

	// Each scale alone is a look of the emitter's own.
	look.Force, look.Amount, look.CustomColor, look.Viscosity = 1, 1, false, 0.25
	if _, err := setLook(look); err != nil {
		t.Fatal(err)
	}
	if got := records(); len(got[0]) != 26 || got[0][23] != 0.25 || got[0][24] != 1 {
		t.Fatalf("GPU records with a viscosity = %v", got)
	}
	look.Viscosity, look.Amount = 1, 1.5
	if _, err := setLook(look); err != nil {
		t.Fatal(err)
	}
	if got := records(); len(got[0]) != 26 || got[0][16] != 1 || got[0][24] != 1.5 {
		t.Fatalf("GPU records with an amount = %v", got)
	}
	look.Amount = 1
	for nozzle, code := range map[string]float64{NozzleJet: 1, NozzleOoze: 2} {
		look.Nozzle = nozzle
		if _, err := setLook(look); err != nil {
			t.Fatal(err)
		}
		if got := records(); len(got[0]) != 26 || got[0][16] != 1 || got[0][25] != code {
			t.Fatalf("GPU records with nozzle %s = %v", nozzle, got)
		}
	}

	// Scattering and roughness extend the record only when either is set, and
	// then always as a pair.
	look.Nozzle, look.Roughness = NozzleSpray, 0.2
	if _, err := setLook(look); err != nil {
		t.Fatal(err)
	}
	if got := records(); len(got[0]) != 28 || !slices.Equal(got[0][25:], []float64{0, 0, 0.2}) {
		t.Fatalf("GPU records with a roughness = %v", got)
	}
	look.Nozzle, look.Scattering, look.Roughness = NozzleJet, 0.85, 0.35
	if _, err := setLook(look); err != nil {
		t.Fatal(err)
	}
	if got := records(); len(got[0]) != 28 || !slices.Equal(got[0][25:], []float64{1, 0.85, 0.35}) {
		t.Fatalf("GPU records with a material = %v", got)
	}

	look.Nozzle, look.Scattering, look.Roughness = NozzleSpray, 0, 0
	if _, err := setLook(look); err != nil {
		t.Fatal(err)
	}
	if got := records(); len(got[0]) != 16 {
		t.Fatalf("GPU records with the global look = %v", got)
	}

	// A profile from before each emitter had its own look shared one look,
	// saved before the force, amount and viscosity settings, among all of them;
	// its strength level offset and stream width have nothing to become.
	var raw map[string]any
	if _, err := readJSON(profilePath, &raw); err != nil {
		t.Fatal(err)
	}
	emitters, ok := raw["emitters"].([]any)
	if !ok || len(emitters) != 2 {
		t.Fatalf("profile emitters = %v", raw["emitters"])
	}
	for _, emitter := range emitters {
		delete(emitter.(map[string]any), "look")
	}
	raw["look"] = map[string]any{
		"strengthOffset": 2, "streamWidth": 0.5, "tint": []float64{0.9, 0.75, 0.03}, "absorptionMm": 15,
	}
	data, err := encodeJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(profilePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if inspection, err = f.service.Inspect(ctx, f.modRoot); err != nil {
		t.Fatal(err)
	}
	for _, emitter := range inspection.Emitters {
		if look := emitter.Look; look.Force != 1 || look.Amount != 1 || look.Viscosity != 1 ||
			look.Nozzle != NozzleSpray || look.Scattering != 0 || look.Roughness != 0 || lookRecord(&look) != nil {
			t.Fatalf("legacy look of emitter %d = %+v", emitter.PID, emitter.Look)
		}
	}
}

// The renderer saves an anchor and its look with one Apply, so a look given
// there must reach a new emitter and replace the one a moved emitter keeps.
func TestServiceApplyStoresGivenLook(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	look := Look{Force: 2, Amount: 0.5, AbsorptionMM: 30, Viscosity: 4, Nozzle: NozzleJet}

	invalid := f.applyInput(0)
	invalid.Look = &Look{Force: 3, Amount: 1, AbsorptionMM: 30, Viscosity: 1}
	if _, err := f.service.Apply(ctx, invalid); !hasCode(err, ErrorInvalidLook) {
		t.Fatalf("apply with an out of range look = %v", err)
	}

	added := f.applyInput(0)
	added.Look = &look
	inspection, err := f.service.Apply(ctx, added)
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Emitters) != 1 || inspection.Emitters[0].Look != look {
		t.Fatalf("emitters after adding with a look = %+v", inspection.Emitters)
	}

	pid := inspection.Emitters[0].PID
	moved := f.applyInput(1)
	moved.PID = &pid
	look.Nozzle, look.Viscosity = NozzleOoze, 0.5
	moved.Look = &look
	if inspection, err = f.service.Apply(ctx, moved); err != nil {
		t.Fatal(err)
	}
	if len(inspection.Emitters) != 1 || inspection.Emitters[0].Look != look {
		t.Fatalf("emitters after moving with a look = %+v", inspection.Emitters)
	}
}

// Changing the original must not release or reuse a slot an unsplit copy
// still hooks; the copy is split off first and keeps its emitter.
func TestServiceOriginalChangeSplitsCopies(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if _, err := f.service.Apply(ctx, f.applyInput(0)); err != nil {
		t.Fatal(err)
	}
	originalKey, _ := readModMarker(f.modRoot)
	copyRoot := f.modRoot + " - Copy"
	if err := os.MkdirAll(copyRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(f.modRoot)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(f.modRoot, entry.Name()))
		if err := os.WriteFile(filepath.Join(copyRoot, entry.Name()), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: f.modRoot}); err != nil {
		t.Fatal(err)
	}
	copyKey, _ := readModMarker(copyRoot)
	inspection, err := f.service.Inspect(ctx, copyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if copyKey == "" || copyKey == originalKey || len(inspection.Emitters) != 1 ||
		!inspection.Emitters[0].Hooked || inspection.Emitters[0].Anchor.MeshID == "" {
		t.Fatalf("copy after removing from the original: key=%q emitters=%+v", copyKey, inspection.Emitters)
	}
}

func TestServiceCopiedModGetsOwnProfile(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	if _, err := f.service.Apply(ctx, f.applyInput(0)); err != nil {
		t.Fatal(err)
	}
	originalKey, _ := readModMarker(f.modRoot)
	originalProfile := filepath.Join(runtimeRootFor(f.modsRoot), profilesDir, originalKey+".json")
	profileBefore, _ := os.ReadFile(originalProfile)
	iniBefore, _ := os.ReadFile(f.iniPath)

	copyMod := func(copyRoot string) {
		t.Helper()
		if err := os.MkdirAll(copyRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(f.modRoot)
		for _, entry := range entries {
			data, _ := os.ReadFile(filepath.Join(f.modRoot, entry.Name()))
			if err := os.WriteFile(filepath.Join(copyRoot, entry.Name()), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	copyRoot, removeRoot, lookRoot := f.modRoot+" - Copy", f.modRoot+" - Copy 2", f.modRoot+" - Copy 3"
	copyMod(copyRoot)
	copyMod(removeRoot)
	copyMod(lookRoot)

	// The renderer lists an unsplit copy's emitters by the slots it shares.
	originalPID := 0
	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: removeRoot, PIDs: []int{originalPID}}); err != nil {
		t.Fatal(err)
	}
	if removed, _ := os.ReadFile(filepath.Join(removeRoot, "mod.ini")); !bytes.Equal(removed, []byte(characterINI)) {
		t.Fatalf("removing a shared slot from the copy left its hooks:\n%s", removed)
	}

	viewer := f.service.viewer.(fakeViewer)
	source := viewer["mod:body:0"]
	source.INIPath, source.IBPath = filepath.Join(copyRoot, "mod.ini"), filepath.Join(copyRoot, "body.ib")
	viewer["copy:body:0"] = source
	input := f.applyInput(1)
	input.ModPath, input.MeshID = copyRoot, "copy:body:0"

	inspection, err := f.service.Apply(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	// The copy keeps the emitter it was copied with, on a slot of its own, and adds one.
	copyKey, _ := readModMarker(copyRoot)
	if copyKey == originalKey || len(inspection.Emitters) != 2 ||
		slices.ContainsFunc(inspection.Emitters, func(e Emitter) bool { return e.PID == originalPID }) {
		t.Fatalf("copy shares the original's profile: key=%s emitters=%+v", copyKey, inspection.Emitters)
	}
	// The renderer follows its selection through the slots the call reports,
	// since the emitter it named by the shared slot now has another.
	renumbered, applied := inspection.Renumbered, inspection.Applied
	if len(renumbered) != 1 || renumbered[0].From != originalPID || applied == nil || *applied == renumbered[0].To ||
		!slices.ContainsFunc(inspection.Emitters, func(e Emitter) bool {
			return e.PID == *applied && e.Anchor.FaceIndex == 1
		}) {
		t.Fatalf("slots reported by the copy's first apply: renumbered=%+v applied=%v", renumbered, applied)
	}
	look := defaultLook()
	look.Force = 1.5
	looked, err := f.service.SetLook(ctx, LookInput{ModPath: lookRoot, PID: originalPID, Look: look})
	if err != nil {
		t.Fatal(err)
	}
	if looked.Applied == nil || len(looked.Renumbered) != 1 ||
		looked.Renumbered[0] != (PIDChange{From: originalPID, To: *looked.Applied}) ||
		len(looked.Emitters) != 1 || looked.Emitters[0].PID != *looked.Applied || looked.Emitters[0].Look != look {
		t.Fatalf("copy's first look: renumbered=%+v applied=%v emitters=%+v",
			looked.Renumbered, looked.Applied, looked.Emitters)
	}
	if _, err := f.service.Remove(ctx, RemoveInput{ModPath: copyRoot}); err != nil {
		t.Fatal(err)
	}

	profileAfter, _ := os.ReadFile(originalProfile)
	iniAfter, _ := os.ReadFile(f.iniPath)
	if !bytes.Equal(profileAfter, profileBefore) || !bytes.Equal(iniAfter, iniBefore) {
		t.Fatalf("editing the copy changed the original:\n%s\n%s", profileAfter, iniAfter)
	}
	copyINI, _ := os.ReadFile(filepath.Join(copyRoot, "mod.ini"))
	if !bytes.Equal(copyINI, []byte(characterINI)) {
		t.Fatalf("removing from the copy left hooks:\n%s", copyINI)
	}
}

type renamingMods struct{}

func (renamingMods) Disable(_ context.Context, path string) (string, error) {
	disabled := filepath.Join(filepath.Dir(path), "DISABLED "+filepath.Base(path))
	return disabled, os.Rename(path, disabled)
}

func (renamingMods) Enable(_ context.Context, path string) (string, error) {
	enabled := filepath.Join(filepath.Dir(path), strings.TrimPrefix(filepath.Base(path), "DISABLED "))
	return enabled, os.Rename(path, enabled)
}

// compressingMods compresses a folder as soon as it is disabled, which the
// mod service schedules to happen at any time after, and restores it when
// enabled.
type compressingMods struct{ renamingMods }

func (m compressingMods) Disable(ctx context.Context, path string) (string, error) {
	disabled, err := m.renamingMods.Disable(ctx, path)
	if err != nil {
		return disabled, err
	}
	return disabled, os.Rename(filepath.Join(disabled, "body.ib"), filepath.Join(disabled, "body.ib.nzst"))
}

func (m compressingMods) Enable(ctx context.Context, path string) (string, error) {
	enabled, err := m.renamingMods.Enable(ctx, path)
	if err != nil {
		return enabled, err
	}
	return enabled, os.Rename(filepath.Join(enabled, "body.ib.nzst"), filepath.Join(enabled, "body.ib"))
}

type failingEnable struct{ compressingMods }

func (failingEnable) Enable(context.Context, string) (string, error) {
	return "", errors.New("folder in use")
}

func TestServiceNewModLeavesOriginalAndRestores(t *testing.T) {
	f := newServiceFixture(t)
	f.service.mod = compressingMods{}
	ctx := context.Background()
	disabled := filepath.Join(filepath.Dir(f.modRoot), "DISABLED "+filepath.Base(f.modRoot))
	input := f.applyInput(0)
	input.Mode = ApplyNewMod

	// A failed apply deletes the copy and leaves the original enabled.
	failing := errors.New("disk full")
	f.service.writeFile = func(path string, data []byte) error {
		if filepath.Base(path) == profileIndexFile {
			return failing
		}
		return writeAtomic(path, data)
	}
	if _, err := f.service.Apply(ctx, input); !errors.Is(err, failing) {
		t.Fatalf("failing new-mod apply = %v", err)
	}
	if _, err := os.Stat(f.modRoot + generatedSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("copy left behind (err=%v)", err)
	}
	if current, err := os.ReadFile(f.iniPath); err != nil || string(current) != characterINI {
		t.Fatalf("original not restored (err=%v)", err)
	}
	f.service.writeFile = writeAtomic

	inspection, err := f.service.Apply(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	target := f.modRoot + generatedSuffix
	if !samePhysicalPath(inspection.ModPath, target) || !samePhysicalPath(inspection.SourcePath, disabled) ||
		len(inspection.Emitters) != 1 || !inspection.Emitters[0].Hooked {
		t.Fatalf("new-mod inspection = %+v", inspection)
	}
	if _, err := os.Stat(filepath.Join(target, "body.ib")); err != nil {
		t.Fatalf("copy is missing a file compressed in the original: %v", err)
	}
	if original, err := os.ReadFile(
		filepath.Join(disabled, "mod.ini"),
	); err != nil ||
		string(original) != characterINI {
		t.Fatalf("original INI changed (err=%v)", err)
	}
	viewer := f.service.viewer.(fakeViewer)
	source := viewer["mod:body:0"]
	source.INIPath, source.IBPath = filepath.Join(target, "mod.ini"), filepath.Join(target, "body.ib")
	viewer["target:body:0"] = source
	again := input
	again.ModPath, again.MeshID = target, "target:body:0"
	if _, err := f.service.Apply(ctx, again); !hasCode(err, ErrorGeneratedMod) {
		t.Fatalf("new-mod apply on a generated mod = %v", err)
	}
	key, _ := readModMarker(target)

	// Without emitters the generated mod still knows its original.
	emptied, err := f.service.Remove(ctx, RemoveInput{ModPath: target})
	if err != nil || emptied.SourcePath == "" || len(emptied.Emitters) != 0 {
		t.Fatalf("remove all from a generated mod = %+v (err=%v)", emptied, err)
	}

	// A file locked in the copy, or an original that cannot be enabled, stops
	// the restore with the copy whole and the original disabled under the name
	// the marker records, so a retry can finish.
	untouched := func(step string) {
		t.Helper()
		marker, err := readMarker(target)
		if err != nil || !samePhysicalPath(marker.Source, disabled) || !marker.ReenableSource {
			t.Fatalf("copy marker after %s = %+v (err=%v)", step, marker, err)
		}
		if _, err := os.Stat(disabled); err != nil {
			t.Fatalf("original after %s: %v", step, err)
		}
	}
	locked, err := os.Open(filepath.Join(target, "body.ib"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.service.RestoreSource(ctx, target)
	_ = locked.Close()
	if err == nil {
		t.Fatal("restore with a locked file in the copy succeeded")
	}
	untouched("a locked copy")
	f.service.mod = failingEnable{compressingMods{}}
	if _, err := f.service.RestoreSource(ctx, target); err == nil {
		t.Fatal("restore with a failing enable succeeded")
	}
	untouched("a failed enable")
	f.service.mod = compressingMods{}

	restored, err := f.service.RestoreSource(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if !samePhysicalPath(restored.ModPath, f.modRoot) || len(restored.Emitters) != 0 {
		t.Fatalf("restored inspection = %+v", restored)
	}
	for _, gone := range []string{target, disabled, filepath.Join(runtimeRootFor(f.modsRoot), profilesDir, key+".json")} {
		if _, err := os.Stat(gone); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists (err=%v)", gone, err)
		}
	}
}

func TestServiceApplyRollsBackFailedWrite(t *testing.T) {
	f := newServiceFixture(t)
	if _, err := f.service.InstallRuntime(context.Background(), f.modRoot); err != nil {
		t.Fatal(err)
	}
	failing := errors.New("disk full")
	f.service.writeFile = func(path string, data []byte) error {
		if filepath.Base(path) == profileIndexFile {
			return failing
		}
		return writeAtomic(path, data)
	}

	if _, err := f.service.Apply(context.Background(), f.applyInput(0)); !errors.Is(err, failing) {
		t.Fatalf("apply error = %v", err)
	}
	current, _ := os.ReadFile(f.iniPath)
	if !bytes.Equal(current, []byte(characterINI)) {
		t.Fatalf("INI kept a hook without its profile:\n%s", current)
	}
	entries, _ := os.ReadDir(filepath.Join(runtimeRootFor(f.modsRoot), profilesDir))
	for _, entry := range entries {
		t.Fatalf("profile file %s left behind", entry.Name())
	}
	if _, err := os.Stat(f.iniPath + backupSuffix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup left behind (err=%v)", err)
	}
}

func runtimeRootFor(modsRoot string) string {
	return filepath.Join(modsRoot, modruntime.GroupDir, runtimeName)
}

func hasCode(err error, code string) bool {
	var contract infra.ContractError
	return errors.As(err, &contract) && strings.HasPrefix(string(contract), code)
}

// Every setting must find its line in the bundled Probe.ini with the same
// default, or a runtime update would fail the install or change behavior.
func TestRuntimeSettingsMatchBundle(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	efmi := "EFMI"
	manager := modruntime.New(modruntime.Options{
		GamePaths: fakeGamePaths{{Game: "endfield", ModFolderPath: f.modsRoot, Importer: &efmi}},
		Specs:     RuntimeSpecs(),
	})
	if _, err := manager.Install(ctx, efmi, runtimeName); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(runtimeRootFor(f.modsRoot), "Probe.ini")
	bundled, _ := fs.ReadFile(RuntimeSpecs()[0].Files, probeFile)
	if installed, _ := os.ReadFile(probe); !bytes.Equal(installed, bundled) {
		t.Fatal("default settings changed the bundled Probe.ini")
	}

	status, err := manager.SetSettings(ctx, efmi, runtimeName, map[string]string{
		"toggleKey": "ctrl VK_F7", "autoPulseKey": "VK_F11", "manualPulseKey": "VK_F12",
		"startEnabled": "0", "autoPulse": "1", "slotCount": "6",
		"tint": "0.5 0.25 1", "refraction": "0.03",
	})
	if err != nil || status.State != modruntime.StateCurrent {
		t.Fatalf("set settings = %+v (err=%v)", status, err)
	}
	written, _ := os.ReadFile(probe)

	// .gitattributes stores the bundle with LF, but a checkout can still carry
	// CRLF; settings must keep whichever line ending the bundle uses.
	eol := "\n"
	if bytes.Contains(bundled, []byte("\r\n")) {
		eol = "\r\n"
	}
	for _, line := range []string{
		"[KeyToggle]\nkey = ctrl VK_F7\n",
		"[KeyAutoPulse]\nkey = VK_F11\n",
		"[KeyManualPulse]\nkey = VK_F12\n",
		"global $auto_pulse = 1\n",
		"global $emitting = 0\n",
		"global $effect_requested = 0\n",
		"global $last_effect_request = 0\n",
		"global $slot_count = 6\n",
		"global $liquid_tint_r = 0.5\n",
		"global $liquid_tint_g = 0.25\n",
		"global $liquid_tint_b = 1\n",
		"global $liquid_refraction = 0.03\n",
	} {
		line = strings.ReplaceAll(line, "\n", eol)
		if !strings.Contains(string(written), line) {
			t.Fatalf("Probe.ini lacks %q", line)
		}
	}
}

// Unreal rewrites the view constants WWMI refers to before the injection, so
// its camera must be built when the main view is seen and not again later;
// the other games keep building it at the injection.
func TestRuntimeBuildsWWMICameraAtMainView(t *testing.T) {
	section := func(ini, name string) string {
		start := strings.Index(ini, "["+name+"]\n")
		if start < 0 {
			return ""
		}
		body := ini[start+len(name)+3:]
		if end := strings.Index(body, "\n["); end >= 0 {
			body = body[:end]
		}
		return body
	}
	for _, spec := range RuntimeSpecs() {
		bundled, err := fs.ReadFile(spec.Files, probeFile)
		if err != nil {
			t.Fatal(err)
		}
		ini := strings.ReplaceAll(string(bundled), "\r\n", "\n")
		atInjection := strings.Contains(section(ini, "CommandListInjectWorld"), "run = CommandListBuildCamera")
		atMainView := strings.Contains(section(ini, "CommandListMainView"), "run = CommandListBuildCamera")
		wwmi := slices.Contains(spec.Importers, "WWMI")
		if atInjection == wwmi || atMainView != wwmi {
			t.Errorf("%v: camera built at injection %v, at main view %v", spec.Importers, atInjection, atMainView)
		}
	}
}

// The passes the injection runs leave their slots empty, while the game may
// still expect its own resources there, so CommandListInjectWorld must save
// and restore every shader resource and UAV slot they bind.
func TestRuntimeRestoresSlotsItBinds(t *testing.T) {
	for _, spec := range RuntimeSpecs() {
		sections := map[string][]string{}
		err := fs.WalkDir(spec.Files, ".", func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || path.Ext(name) != ".ini" {
				return err
			}
			data, err := fs.ReadFile(spec.Files, name)
			if err != nil {
				return err
			}
			current := ""
			for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
					current = line[1 : len(line)-1]
					continue
				}
				sections[current] = append(sections[current], line)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}

		// A namespaced reference such as CommandList\Namespace\Name names the
		// section CommandListName of that namespace's INI.
		resolve := func(target string) string {
			if first, last := strings.Index(target, `\`), strings.LastIndex(target, `\`); first >= 0 {
				return target[:first] + target[last+1:]
			}
			return target
		}
		inject := sections["CommandListInjectWorld"]
		seen := map[string]bool{}
		queue := []string{"CommandListInjectWorld"}
		checked := 0
		for len(queue) > 0 {
			name := queue[0]
			queue = queue[1:]
			if seen[name] {
				continue
			}
			seen[name] = true
			for _, line := range sections[name] {
				key, value, ok := strings.Cut(line, "=")
				key, value = strings.TrimSpace(key), strings.TrimSpace(value)
				if !ok {
					continue
				}
				if key == "run" && !strings.HasPrefix(value, "BuiltIn") {
					queue = append(queue, resolve(value))
					continue
				}
				stage, slot, bound := strings.Cut(key, "-")
				if name == "CommandListInjectWorld" || !bound || (stage != "cs" && stage != "vs" && stage != "ps") ||
					len(slot) < 2 || (slot[0] != 't' && slot[0] != 'u') {
					continue
				}
				checked++
				saved := "ResourceSaved" + strings.ToUpper(stage[:1]) + stage[1:] + strings.ToUpper(slot)
				if !slices.Contains(inject, saved+" = ref "+key) || !slices.Contains(inject, key+" = ref "+saved) {
					t.Errorf("%v: %s binds %s, which the injection does not restore", spec.Importers, name, key)
				}
			}
		}
		if checked < 50 {
			t.Fatalf("%v: found only %d bindings", spec.Importers, checked)
		}
	}
}

// The enhanced runtime's Composite.ini replaces the core one, so each of its
// composite passes must bind the scattering and roughness as the core's does,
// or the composite reads them as zero there.
func TestRuntimeCompositesBindMaterial(t *testing.T) {
	for _, spec := range RuntimeSpecs() {
		composites := 0
		err := fs.WalkDir(spec.Files, ".", func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || path.Ext(name) != ".ini" {
				return err
			}
			data, err := fs.ReadFile(spec.Files, name)
			if err != nil {
				return err
			}
			for _, section := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n[") {
				lines := strings.Split(section, "\n")
				if !slices.ContainsFunc(lines, func(line string) bool {
					return strings.HasPrefix(line, "ps = FluidComposite")
				}) {
					continue
				}
				composites++
				if !slices.Contains(lines, `ps-t107 = Resource\LiquidGravityProbe\FluidMaterial`) {
					t.Errorf("%v: %s [%s does not bind the material", spec.Importers, name, lines[0])
				}
			}
			return nil
		})
		if err != nil || composites == 0 {
			t.Fatalf("%v: found %d composite passes (err=%v)", spec.Importers, composites, err)
		}
	}
}

// Every shader the bundled INI files of each game name must ship in its
// runtime, or the pass fails only inside the game, as an overlay error nothing
// here reports. Shader paths are relative to their INI; a variant INI installs
// at its path below the variant directory.
func TestRuntimeINIsReferenceBundledShaders(t *testing.T) {
	for _, spec := range RuntimeSpecs() {
		var checked int
		err := fs.WalkDir(spec.Files, ".", func(name string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() || path.Ext(name) != ".ini" {
				return err
			}
			ini, err := fs.ReadFile(spec.Files, name)
			if err != nil {
				return err
			}
			checked++
			if missing := missingShaderReferences(spec.Files, name, ini); len(missing) != 0 {
				t.Errorf("%v: %s references shaders missing from the bundle: %v", spec.Importers, name, missing)
			}
			return nil
		})
		if err != nil || checked < 2 {
			t.Fatalf("%v: walked %d INI files (err=%v)", spec.Importers, checked, err)
		}
	}
}

func missingShaderReferences(files fs.FS, iniPath string, ini []byte) []string {
	installedDir := strings.TrimPrefix(path.Dir(iniPath), runtimeEnhancedDir)
	installedDir = strings.TrimPrefix(installedDir, "/")
	var missing []string
	for _, line := range strings.Split(string(ini), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || !strings.HasSuffix(value, ".hlsl") {
			continue
		}
		switch key {
		case "vs", "ps", "cs", "hs", "ds", "gs":
		default:
			continue
		}
		shader := path.Join(installedDir, value)
		_, baseErr := fs.Stat(files, shader)
		_, variantErr := fs.Stat(files, path.Join(runtimeEnhancedDir, shader))
		if baseErr != nil && variantErr != nil {
			missing = append(missing, value)
		}
	}
	return missing
}
