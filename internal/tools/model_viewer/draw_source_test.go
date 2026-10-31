package modelviewer

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLinkDrawSourcesSeparatesRecoveredFaceMappings(t *testing.T) {
	transport := ModelViewerTransport{}
	sources := make(map[string]DrawSource)
	for _, frame := range []struct {
		id    string
		faces []uint32
	}{
		{id: "damaged", faces: []uint32{1, 2}},
		{id: "same", faces: []uint32{1, 2}},
		{id: "different", faces: []uint32{0, 2}},
		{id: "clean"},
	} {
		transport.Meshes = append(transport.Meshes, ModelViewerMeshTransport{
			ID: frame.id, Conditions: ModelViewerDNF{{{Var: "frame", Value: frame.id}}},
		})
		sources[frame.id] = DrawSource{
			INIPath: `C:\mod\mod.ini`, Section: "Body", IB: frame.id, IBPath: `C:\mod\body.ib`,
			IBFormat: "DXGI_FORMAT_R32_UINT", IndexCount: 9,
			vertexBuffer: frame.id + ".buf", sourceFaces: frame.faces,
		}
	}
	linkModelViewerDrawSources(&transport, sources)
	keys := make(map[string]string)
	for _, mesh := range transport.Meshes {
		keys[mesh.ID] = mesh.DrawKey
	}
	if keys["damaged"] == "" || keys["damaged"] != keys["same"] || keys["damaged"] == keys["different"] ||
		keys["damaged"] == keys["clean"] || keys["different"] == keys["clean"] {
		t.Fatalf("incompatible frame surfaces share draw keys: %v", keys)
	}
	for _, source := range sources {
		if !slices.Equal(source.IBAliases, []string{"clean", "damaged", "different", "same"}) {
			t.Fatalf("surface grouping changed game draw aliases: %v", source.IBAliases)
		}
	}
}

func TestDrawSourceMapsRecoveredFacesWithinDrawBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.ib")
	if err := os.WriteFile(path, modelViewerUint32Bytes([]uint32{99, 99, 99, 0, 1, 2, 3, 4, 5}), 0o600); err != nil {
		t.Fatal(err)
	}
	source := DrawSource{
		IBPath: path, IBFormat: "DXGI_FORMAT_R32_UINT", StartIndex: 3, IndexCount: 6, BaseVertex: 10,
		sourceFaces: []uint32{1}, Reversed: true,
	}
	if face, err := source.SourceFaceIndex(0); err != nil || face != 1 {
		t.Fatalf("source face=%d err=%v", face, err)
	}
	if vertices, err := source.TriangleVertices(0); err != nil || vertices != [3]uint32{13, 14, 15} {
		t.Fatalf("source vertices=%v err=%v", vertices, err)
	}
	for _, face := range []int{-1, 1} {
		if _, err := source.SourceFaceIndex(face); err == nil {
			t.Fatalf("accepted viewer face %d", face)
		}
	}
	source.sourceFaces = nil
	if face, err := source.SourceFaceIndex(1); err != nil || face != 1 {
		t.Fatalf("identity face=%d err=%v", face, err)
	}
	if _, err := source.SourceFaceIndex(2); err == nil {
		t.Fatal("accepted face beyond explicit draw")
	}
	source.Auto = true
	if _, err := source.TriangleVertices(2); err == nil {
		t.Fatal("accepted face beyond auto index buffer")
	}
}

func TestLinkDrawSourcesAliasesOnlyExclusiveFrames(t *testing.T) {
	draw := func(section, ib, vb string) DrawSource {
		return DrawSource{
			INIPath:      `C:\mod\mod.ini`,
			Section:      section,
			IB:           ib,
			IBPath:       `C:\mod\shared.ib`,
			IndexCount:   6,
			vertexBuffer: vb,
		}
	}
	frame := func(value string) ModelViewerDNF {
		return ModelViewerDNF{{{Var: "swapvar", Value: value}}}
	}
	static := draw("BodyA", "BodyAIB.0", "static.buf")
	static.StartIndex = 6
	sources := map[string]DrawSource{
		"a0": draw("BodyA", "BodyAIB.0", "frame0.buf"),
		"a1": draw("BodyA", "BodyAIB.1", "frame1.buf"),
		// A part beside the frames, drawn from another range under one of
		// their buffers in every frame.
		"a2": static,
		"b":  draw("BodyB", "BodyBIB", "b.buf"),
		// Two parts drawn one after the other from the same index file.
		"c0": draw("BodyC", "BodyCIB", "c.buf"),
		"c1": draw("BodyC", "BodyCAltIB", "c.buf"),
		// Two parts a toggle swaps over the same index range.
		"d0": draw("BodyD", "BodyDIB", "d0.buf"),
		"d1": draw("BodyD", "BodyDIB", "d1.buf"),
	}
	always := modelViewerDNFTrue()
	transport := ModelViewerTransport{Meshes: []ModelViewerMeshTransport{
		{ID: "a0", Conditions: frame("0")},
		{ID: "a1", Conditions: frame("1")},
		{ID: "a2", Conditions: always},
		{ID: "b", Conditions: frame("0")},
		{ID: "c0", Conditions: always},
		{ID: "c1", Conditions: always},
		{ID: "d0", Conditions: frame("0")},
		{ID: "d1", Conditions: frame("1")},
	}}

	linkModelViewerDrawSources(&transport, sources)

	if got := sources["a1"].IBAliases; !slices.Equal(got, []string{"BodyAIB.0", "BodyAIB.1"}) {
		t.Fatalf("frame aliases = %v", got)
	}
	if sources["a0"].MatchesIB("ResourceBodyBIB") {
		t.Fatal("a draw of one section matched the index buffer of another section")
	}
	if sources["c0"].MatchesIB("ResourceBodyCAltIB") || len(sources["c1"].IBAliases) != 0 {
		t.Fatalf("draws that run together were aliased: %v", sources["c1"].IBAliases)
	}
	keys := map[string]string{}
	for _, mesh := range transport.Meshes {
		keys[mesh.ID] = mesh.DrawKey
	}
	if keys["a0"] == "" || keys["a0"] != keys["a1"] || keys["a2"] == keys["a0"] || keys["b"] == keys["a0"] ||
		keys["c0"] == keys["c1"] || keys["d0"] == keys["d1"] {
		t.Fatalf("draw keys = %v", keys)
	}
}
