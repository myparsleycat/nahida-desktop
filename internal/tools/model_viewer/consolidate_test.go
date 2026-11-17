package modelviewer

import (
	"context"
	"testing"
)

func consolidateFixture(
	conditions ...ModelViewerDNF,
) (*ModelViewerTransport, []modelViewerMeshPayload, map[string]DrawSource) {
	transport := &ModelViewerTransport{
		Animations: []ModelViewerAnimationClip{{ID: "frame", VariableIDs: []string{"frame"}}},
	}
	var payloads []modelViewerMeshPayload
	sources := map[string]DrawSource{}
	for index, condition := range conditions {
		id := "mesh" + string(rune('a'+index))
		key := "body.png"
		transport.Meshes = append(transport.Meshes, ModelViewerMeshTransport{
			ID: id, Component: "Body", Conditions: condition, TexKey: &key,
		})
		payloads = append(payloads, modelViewerMeshPayload{
			Positions: []float32{float32(index), 0, 0, 1, 0, 0, 0, 1, 0},
			UVs:       []float32{0, 0, 1, 0, 0, 1},
			Indices:   []uint32{0, 1, 2},
		})
		sources[id] = DrawSource{INIPath: "mod.ini", Section: "TextureOverrideBody", IBPath: "body.ib", IndexCount: 3}
	}
	return transport, payloads, sources
}

func frameCondition(frame string, extra ...ModelViewerDNFClause) ModelViewerDNF {
	return ModelViewerDNF{append([]ModelViewerDNFClause{{Var: "frame", Value: frame}}, extra...)}
}

// Frames of one draw that differ only in their positions fold into one mesh with a variant per frame; a frame
// with its own texture keeps it under that frame's condition.
func TestConsolidateFoldsPositionOnlyFrames(t *testing.T) {
	transport, payloads, sources := consolidateFixture(frameCondition("0"), frameCondition("1"), frameCondition("2"))
	other := "other.png"
	transport.Meshes[2].TexKey = &other

	payloads, aliases, err := consolidateModelViewerMeshes(context.Background(), transport, payloads, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.Meshes) != 1 || len(payloads) != 1 || len(payloads[0].Frames) != 3 ||
		len(transport.Meshes[0].PositionVariants) != 3 || aliases["meshc"] != "mesha" {
		t.Fatalf("meshes = %#v, aliases = %v", transport.Meshes, aliases)
	}
	if payloads[0].Frames[2].Positions[0] != 2 {
		t.Fatalf("frame 2 positions = %v", payloads[0].Frames[2].Positions)
	}
	for frame, want := range []string{"body.png", "body.png", "other.png"} {
		_, states := EvaluateViewerState(*transport, map[string]any{"frame": float64(frame)}, "")
		if !states[0].Visible || states[0].PositionVariant != frame || states[0].TexKey != want {
			t.Fatalf("frame %d state = %#v, want texture %s", frame, states[0], want)
		}
	}
}

// Meshes an outfit toggle tells apart, or that can draw together, are not frames and stay separate.
func TestConsolidateKeepsMeshesThatAreNotFrames(t *testing.T) {
	for name, conditions := range map[string][]ModelViewerDNF{
		"toggle": {
			frameCondition("0", ModelViewerDNFClause{Var: "dress", Value: "0"}),
			frameCondition("1", ModelViewerDNFClause{Var: "dress", Value: "1"}),
		},
		"overlapping": {frameCondition("0"), {{{Var: "dress", Value: "1"}}}},
	} {
		transport, payloads, sources := consolidateFixture(conditions...)
		if _, aliases, err := consolidateModelViewerMeshes(
			context.Background(),
			transport,
			payloads,
			sources,
		); err != nil ||
			len(transport.Meshes) != 2 ||
			len(aliases) != 0 {
			t.Fatalf("%s: meshes = %d, aliases = %v, err = %v", name, len(transport.Meshes), aliases, err)
		}
	}
}

// The same draw repeated under another override hash draws once; its ID still finds the mesh that draws it.
func TestConsolidateRemovesHashCopies(t *testing.T) {
	always := ModelViewerDNF{{}}
	transport, payloads, sources := consolidateFixture(always, always)
	payloads[1].Positions = payloads[0].Positions
	copySource := sources["meshb"]
	copySource.Section = "TextureOverrideBody1"
	sources["meshb"] = copySource
	transport.Meshes[1].Component = "Body1"

	if _, aliases, err := consolidateModelViewerMeshes(
		context.Background(),
		transport,
		payloads,
		sources,
	); err != nil ||
		len(transport.Meshes) != 1 || aliases["meshb"] != "mesha" ||
		transport.Meshes[0].MergedIDs[0] != "meshb" {
		t.Fatalf("meshes = %#v, aliases = %v, err = %v", transport.Meshes, aliases, err)
	}
}
