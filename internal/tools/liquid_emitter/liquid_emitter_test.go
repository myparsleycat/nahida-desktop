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
	if again := text.drawSites(bodyDraw); again[0].pid != 7 || again[1].pid != 7 {
		t.Fatalf("existing hooks not detected: %+v", again)
	}

	if removed := text.removeHooks(func(pid int) bool { return pid == 7 }); removed != 2 {
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
	efmi := "EFMI"
	service := NewWithOptions(Options{
		GamePaths: fakeGamePaths{{Game: "endfield", ModFolderPath: modsRoot, Importer: &efmi}},
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
	// Picking another face of the same draw replaces the emitter instead of adding one.
	inspection, err = f.service.Apply(ctx, f.applyInput(1))
	if err != nil {
		t.Fatal(err)
	}
	if len(inspection.Emitters) != 1 || inspection.Emitters[0].Anchor.FaceIndex != 1 {
		t.Fatalf("second apply = %+v", inspection.Emitters)
	}
	patched, _ := os.ReadFile(f.iniPath)
	if strings.Count(string(patched), "y243 = 6") != 2 || strings.Contains(string(patched), "y243 = 3") {
		t.Fatalf("hooks do not point at the second face:\n%s", patched)
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

// LoadProfilesJSON reads a mod's look as six numbers after an emitter's 16,
// in this order, and a runtime from before looks rejects any record that is
// not 16 long; a mod that keeps the global settings must still write 16.
func TestServiceLookExtendsGPURecords(t *testing.T) {
	f := newServiceFixture(t)
	ctx := context.Background()
	look := Look{StrengthOffset: -2, CustomColor: true, Tint: [3]float64{0.2, 0.4, 1}, AbsorptionMM: 30}
	if _, err := f.service.SetLook(ctx, LookInput{ModPath: f.modRoot, Look: look}); !hasCode(err, ErrorNoEmitters) {
		t.Fatalf("look without emitters = %v", err)
	}
	if _, err := f.service.Apply(ctx, f.applyInput(0)); err != nil {
		t.Fatal(err)
	}
	invalid := look
	invalid.StrengthOffset = 8
	if _, err := f.service.SetLook(ctx, LookInput{ModPath: f.modRoot, Look: invalid}); !hasCode(err, ErrorInvalidLook) {
		t.Fatalf("out of range look = %v", err)
	}

	key, _ := readModMarker(f.modRoot)
	records := func() [][]float64 {
		t.Helper()
		var doc profileDocument
		if _, err := readJSON(filepath.Join(runtimeRootFor(f.modsRoot), profilesDir, key+".json"), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.GPUData
	}
	inspection, err := f.service.SetLook(ctx, LookInput{ModPath: f.modRoot, Look: look})
	if err != nil {
		t.Fatal(err)
	}
	// Moving the emitter keeps the mod's look.
	if inspection, err = f.service.Apply(ctx, f.applyInput(1)); err != nil {
		t.Fatal(err)
	}
	if inspection.Look != look {
		t.Fatalf("inspection look = %+v", inspection.Look)
	}
	got := records()
	if len(got) != 1 || len(got[0]) != 22 || !slices.Equal(got[0][16:], []float64{-2, 1, 0.2, 0.4, 1, 0.03}) {
		t.Fatalf("GPU records with a look = %v", got)
	}

	look.StrengthOffset, look.CustomColor = 0, false
	if _, err := f.service.SetLook(ctx, LookInput{ModPath: f.modRoot, Look: look}); err != nil {
		t.Fatal(err)
	}
	if got := records(); len(got) != 1 || len(got[0]) != 16 {
		t.Fatalf("GPU records with the global look = %v", got)
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
	copyRoot, removeRoot := f.modRoot+" - Copy", f.modRoot+" - Copy 2"
	copyMod(copyRoot)
	copyMod(removeRoot)

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
	copyKey, _ := readModMarker(copyRoot)
	if copyKey == originalKey || len(inspection.Emitters) != 1 || inspection.Emitters[0].PID == originalPID {
		t.Fatalf("copy shares the original's profile: key=%s emitters=%+v", copyKey, inspection.Emitters)
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
		"toggleKey": "ctrl VK_F7", "strengthUpKey": "VK_F8", "strengthDownKey": "VK_F9",
		"autoPulseKey": "VK_F11", "manualPulseKey": "VK_F12",
		"startEnabled": "0", "autoPulse": "1", "strength": "6", "slotCount": "6",
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
		"[KeyStrengthUp]\nkey = VK_F8\n",
		"[KeyStrengthDown]\nkey = VK_F9\n",
		"[KeyAutoPulse]\nkey = VK_F11\n",
		"[KeyManualPulse]\nkey = VK_F12\n",
		"global $auto_pulse = 1\n",
		"global $emitting = 0\n",
		"global $effect_requested = 0\n",
		"global $last_effect_request = 0\n",
		"global $strength_level = 6\n",
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
