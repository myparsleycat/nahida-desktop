package modelviewer

import "testing"

// A texture choice fills only the slots it has, so a mesh keeps its own normal map under a choice without one, and
// returns to its own textures without a choice.
func TestApplyTextureChoice(t *testing.T) {
	key := func(s string) *string { return &s }
	mesh := ModelViewerMeshTransport{NormalMapKey: key("own-normal")}
	base := TextureChoiceOf(&mesh)
	a := ModelViewerTextureChoice{Source: "A", TexKey: key("a"), NormalMapKey: key("a-normal")}
	b := ModelViewerTextureChoice{Source: "B", TexKey: key("b")}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}

	for _, step := range []struct {
		choice          *ModelViewerTextureChoice
		diffuse, normal string
	}{
		{&a, "a", "a-normal"},
		{&b, "b", "own-normal"},
		{nil, "", "own-normal"},
	} {
		ApplyTextureChoice(&mesh, base, step.choice)
		source := "none"
		if step.choice != nil {
			source = step.choice.Source
		}
		if got := [2]string{deref(mesh.TexKey), deref(mesh.NormalMapKey)}; got != [2]string{step.diffuse, step.normal} {
			t.Fatalf("after choice %s: diffuse, normal = %q, want %q, %q", source, got, step.diffuse, step.normal)
		}
	}
}
