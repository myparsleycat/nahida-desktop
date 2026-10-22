package modelviewer

import (
	"slices"
	"testing"
)

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
