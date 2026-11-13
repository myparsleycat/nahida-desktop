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
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

func TestRuntimeBodyResourceCapacities(t *testing.T) {
	files, err := runtimeFiles("wwmi")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := fs.ReadFile(files, "Probe.ini")
	if err != nil {
		t.Fatal(err)
	}
	arrays := map[string]int{}
	section := ""
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") {
			section = strings.Trim(line, "[]")
		} else if value, ok := strings.CutPrefix(line, "array = "); ok {
			arrays[section], err = strconv.Atoi(value)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	for resource, want := range map[string]int{"ResourceSurfaceStats": 10, "ResourceSurfaceProfiles": 4096 * 8, "ResourceSlotState": 8 * 18, "ResourceEmitterState": 8 * 16, "ResourceYogurtBindings": 2048 * 3, "ResourceYogurtCounter": 3} {
		if arrays[resource] != want {
			t.Errorf("%s has %d rows, need %d for its shader contract", resource, arrays[resource], want)
		}
	}
}

type surfaceViewer struct{ fakeViewer }

func (v surfaceViewer) SurfaceGeometry(_ context.Context, session, mesh string) (modelviewer.SurfaceGeometry, error) {
	source, ok := v.DrawSource(session, mesh)
	if !ok {
		return modelviewer.SurfaceGeometry{}, errors.New("missing fixture mesh")
	}
	return modelviewer.SurfaceGeometry{
		Draw:        source,
		Fingerprint: "fixture",
		Positions:   []float32{0, 0, 1, 1, 0, 1, 0, 1, 1, 1, 1, 1},
		VertexIDs:   []uint32{0, 1, 2, 3},
		Indices:     []uint32{0, 1, 2, 2, 1, 3},
	}, nil
}

func TestTrackedApplyAndYogurtAcrossGames(t *testing.T) {
	for _, game := range games {
		t.Run(game.importer, func(t *testing.T) {
			f := newGameServiceFixture(t, game.importer)
			f.service.viewer = surfaceViewer{f.service.viewer.(fakeViewer)}
			ctx := context.Background()
			input := f.applyInput(0)
			look := defaultLook()
			look.Nozzle = NozzleYogurt
			input.Look = &look
			result, err := f.service.Apply(ctx, input)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Emitters) != 1 || !result.Emitters[0].SurfaceTracked ||
				result.Emitters[0].Look.Nozzle != NozzleYogurt {
				t.Fatalf("untracked result: %+v", result)
			}
			input = f.applyInput(1)
			if _, err = f.service.Apply(ctx, input); err != nil {
				t.Fatal(err)
			}
			root := runtimeRootFor(f.modsRoot)
			entries, err := os.ReadDir(filepath.Join(root, "bodymesh", "data"))
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 2 {
				t.Fatalf("duplicated shared topology: %v", entries)
			}
			if _, err = f.service.Remove(ctx, RemoveInput{ModPath: f.modRoot}); err != nil {
				t.Fatal(err)
			}
			entries, err = os.ReadDir(filepath.Join(root, "bodymesh", "data"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("resources left after removal: %v, %v", entries, err)
			}
		})
	}
}

func TestTrackedApplyRollsBackMeshFiles(t *testing.T) {
	f := newServiceFixture(t)
	f.service.viewer = surfaceViewer{f.service.viewer.(fakeViewer)}
	ctx := context.Background()
	if _, err := f.service.InstallRuntime(ctx, f.modRoot); err != nil {
		t.Fatal(err)
	}
	root := runtimeRootFor(f.modsRoot)
	generated := filepath.Join(root, "bodymesh", "Generated.ini")
	original, err := os.ReadFile(generated)
	if err != nil {
		t.Fatal(err)
	}
	failed := errors.New("fixture write failed")
	f.service.writeFile = func(name string, data []byte) error {
		if filepath.Ext(name) == ".topology" {
			return failed
		}
		return writeAtomic(name, data)
	}
	if _, err = f.service.Apply(ctx, f.applyInput(0)); !errors.Is(err, failed) {
		t.Fatalf("write error = %v", err)
	}
	current, err := os.ReadFile(f.iniPath)
	if err != nil || string(current) != characterINI {
		t.Fatal("INI did not roll back")
	}
	current, err = os.ReadFile(generated)
	if err != nil || !bytes.Equal(current, original) {
		t.Fatal("generated loader did not roll back")
	}
	entries, err := os.ReadDir(filepath.Join(root, "bodymesh", "data"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("uncommitted mesh files: %v, %v", entries, err)
	}
}

func TestBodyTopologyPreservesIndicesAndSeams(t *testing.T) {
	for _, test := range []struct {
		name      string
		gap       float32
		extra     bool
		connected bool
	}{
		{name: "exact split", connected: true},
		{name: "separate layers", gap: .0001},
		{name: "ambiguous seam", extra: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			g := modelviewer.SurfaceGeometry{
				Draw: modelviewer.DrawSource{INIPath: filepath.Join(t.TempDir(), "mod.ini"), Section: "Body"},
				Positions: []float32{
					0,
					0,
					0,
					1,
					0,
					0,
					0,
					1,
					0,
					1,
					0,
					test.gap,
					1,
					1,
					test.gap,
					0,
					1,
					test.gap,
				},
				VertexIDs: []uint32{0, 1, 2, 3, 4, 5},
				Indices:   []uint32{0, 1, 2, 3, 4, 5},
			}
			if test.extra {
				g.Indices = append(g.Indices, 3, 4, 5)
			}
			mesh, index, topology, err := buildBody(g)
			if err != nil {
				t.Fatal(err)
			}
			if mesh.Faces != len(g.Indices)/3 || len(index) != len(g.Indices)*4 {
				t.Fatalf("lost triangles: %+v", mesh)
			}
			links := 0
			for at := 16; at < len(topology); at += 16 {
				for k := range 3 {
					if math.Float32frombits(binary.LittleEndian.Uint32(topology[at+k*4:])) >= 0 {
						links++
					}
				}
			}
			if (links > 0) != test.connected {
				t.Fatalf("adjacency links = %d", links)
			}
			other := g
			other.Draw.Section = "Other"
			second, raw, _, err := buildBody(other)
			if err != nil {
				t.Fatal(err)
			}
			if second.Identity == mesh.Identity || second.Key != mesh.Key || !bytes.Equal(raw, index) {
				t.Fatal("draw identity and reusable topology were conflated")
			}
		})
	}
}

func TestBodyDataSharesReferencesAndCleansLastOwner(t *testing.T) {
	root := t.TempDir()
	layout := runtimeLayout{root: root}
	mesh := &surfaceMesh{Key: strings.Repeat("a", 32), Identity: "draw", Faces: 1, Generation: 1}
	if err := os.MkdirAll(filepath.Dir(bodyPath(layout, mesh.Key, ".ib")), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".ib", ".topology"} {
		if err := os.WriteFile(bodyPath(layout, mesh.Key, suffix), []byte("owned"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	index := profileIndex{Owners: map[string][]int{"owner": {1, 2}}}
	doc := profileDocument{Enabled: true, Emitters: []profileEmitter{{PID: 1, Surface: mesh}, {PID: 2, Surface: mesh}}}
	changes, err := bodyChanges(layout, index, "owner", doc)
	if err != nil {
		t.Fatal(err)
	}
	generated := string(changes[0].data)
	if strings.Count(generated, "[ResourceIB_") != 1 || strings.Count(generated, "if x243 ==") != 2 {
		t.Fatal(generated)
	}
	copyDoc := profileDocument{Enabled: true, Emitters: []profileEmitter{{PID: 3, Surface: mesh}}}
	encoded, err := encodeProfile(copyDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Dir(layout.profilePath("copy")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(layout.profilePath("copy"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	copied, err := bodyChanges(
		layout,
		profileIndex{Owners: map[string][]int{"owner": {1, 2}, "copy": {3}}},
		"owner",
		doc,
	)
	if err != nil {
		t.Fatal(err)
	}
	models := copied[2].data
	if binary.LittleEndian.Uint32(models[4:]) == binary.LittleEndian.Uint32(models[12:]) {
		t.Fatal("copied mods share a captured actor identity")
	}
	doc.Enabled = false
	changes, err = bodyChanges(layout, index, "owner", doc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(changes[0].data), "[ResourceIB_") {
		t.Fatal("inactive profile still binds body resources")
	}
	for _, c := range changes {
		if c.remove && strings.Contains(c.path, "data") {
			t.Fatal("deleted an inactive profile's mesh")
		}
	}
	copyDoc.Enabled = false
	encoded, err = encodeProfile(copyDoc)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(layout.profilePath("copy"), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	changes, err = bodyChanges(layout, profileIndex{Owners: map[string][]int{"copy": {3}}}, "", profileDocument{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.remove && strings.Contains(c.path, "data") {
			t.Fatal("deleted another inactive profile's mesh")
		}
	}
	doc.Enabled = true
	doc.Emitters = doc.Emitters[:1]
	changes, err = bodyChanges(layout, index, "owner", doc)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range changes {
		if c.remove && strings.Contains(c.path, "data") {
			t.Fatal("deleted a referenced mesh")
		}
	}
	doc.Emitters = nil
	changes, err = bodyChanges(layout, index, "owner", doc)
	if err != nil {
		t.Fatal(err)
	}
	removed := 0
	for _, c := range changes {
		if c.remove && strings.Contains(c.path, "data") {
			removed++
		}
	}
	if removed != 2 {
		t.Fatalf("removed %d files, want both mesh files", removed)
	}
}

func TestBodyCleanupOnlyOwnedRegularFiles(t *testing.T) {
	t.Parallel()
	layout := runtimeLayout{root: t.TempDir()}
	key := strings.Repeat("b", 32)
	dir := filepath.Join(layout.root, "bodymesh", "data")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{key + ".ib", key + ".topology", key + ".notes", key + ".ib.backup", "custom.ib"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("fixture"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, strings.Repeat("c", 32)+".ib"), 0o700); err != nil {
		t.Fatal(err)
	}
	changes, err := bodyChanges(layout, profileIndex{}, "", profileDocument{})
	if err != nil {
		t.Fatal(err)
	}
	removed := map[string]bool{}
	for _, change := range changes {
		if change.remove {
			removed[filepath.Base(change.path)] = true
		}
	}
	if len(removed) != 2 || !removed[key+".ib"] || !removed[key+".topology"] {
		t.Fatalf("cleanup scheduled non-owned entries: %v", removed)
	}
}

func TestBodyChangesRejectEscapingJunctions(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		link string
		file string
	}{
		{name: "missing body files", link: "bodymesh"},
		{name: "existing dimensions", link: "bodymesh", file: "Dimensions.hlsl"},
		{name: "mesh cleanup", link: "bodymesh/data", file: strings.Repeat("b", 32) + ".ib"},
		{name: "shader rewrite", link: "fluid", file: "BodyAcquire.hlsl"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			root, outside := t.TempDir(), t.TempDir()
			link := filepath.Join(root, filepath.FromSlash(test.link))
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if test.file != "" {
				if err := os.WriteFile(filepath.Join(outside, test.file), []byte("external"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
				t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
			}
			changes, err := bodyChanges(runtimeLayout{root: root}, profileIndex{}, "", profileDocument{})
			if err == nil || len(changes) != 0 {
				t.Fatalf("accepted runtime-root escape: err=%v, changes=%d", err, len(changes))
			}
		})
	}
}

func TestEmissionSettingsValidationAndLegacyRecord(t *testing.T) {
	look := defaultLook()
	legacy := lookRecord(&look)
	look.Emission = DefaultEmissionSettings()
	if !validLook(look) || len(lookRecord(&look)) != 20 {
		t.Fatal("extended emission record rejected")
	}
	for _, value := range []float64{0, math.NaN(), math.Inf(1), 5.1} {
		look.Emission.Interval = value
		if validLook(look) {
			t.Fatalf("accepted interval %v", value)
		}
	}
	look.Emission.Enabled = false
	if !bytes.Equal(floatRecordBytes(lookRecord(&look)), floatRecordBytes(legacy)) {
		t.Fatal("disabled extension changed legacy record")
	}
}

func floatRecordBytes(values []float64) []byte {
	raw := make([]byte, len(values)*8)
	for i, v := range values {
		binary.LittleEndian.PutUint64(raw[i*8:], math.Float64bits(v))
	}
	return raw
}
