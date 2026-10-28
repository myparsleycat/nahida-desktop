package modelviewer

import (
	"encoding/binary"
	"testing"
)

func encodeDX10HeaderDDS(width, height, dxgi uint32) []byte {
	header := make([]byte, 148)
	copy(header[:4], "DDS ")
	binary.LittleEndian.PutUint32(header[4:8], 124)
	binary.LittleEndian.PutUint32(header[12:16], height)
	binary.LittleEndian.PutUint32(header[16:20], width)
	copy(header[84:88], "DX10")
	binary.LittleEndian.PutUint32(header[128:132], dxgi)
	return header
}

func TestAttachSlotBoundDiffuseReplacesLinearPsT0WithSRGBSlot(t *testing.T) {
	const (
		bc7Linear = 98
		bc7SRGB   = 99
	)
	dir := t.TempDir()
	writeTextureFile(t, dir, "Textures/C1.dds", encodeDX10HeaderDDS(8192, 8192, bc7Linear))
	writeTextureFile(t, dir, "Textures/B1.dds", encodeDX10HeaderDDS(8192, 8192, bc7Linear))
	writeTextureFile(t, dir, "Textures/A1.dds", encodeDX10HeaderDDS(8192, 8192, bc7SRGB))
	writeTextureFile(t, dir, "Textures/D1.dds", encodeDX10HeaderDDS(8192, 8192, bc7SRGB))

	mask := modelViewerDirectTextureAssignment{
		role: "diffuse", resource: "ResourceC1", file: "Textures/C1.dds", conditions: modelViewerDNFTrue(),
	}
	slot := func(slot int, name string) modelViewerDirectTextureAssignment {
		return modelViewerDirectTextureAssignment{
			role:       modelViewerSlotTextureRole(slot),
			resource:   "Resource" + name,
			file:       "Textures/" + name + ".dds",
			conditions: modelViewerDNFTrue(),
		}
	}
	for _, tt := range []struct {
		name       string
		component  string
		wwmi       bool
		authored   bool
		linearOnly bool
		want       string
	}{
		{name: "component", component: "Component7", want: "A1"},
		{name: "underscore component", component: "_Component7", want: "A1"},
		{name: "normal component", component: "NormalComponent3", wwmi: true, want: "A1"},
		{name: "UL component", component: "ULComponent3", wwmi: true, want: "A1"},
		{name: "non-WWMI prefix", component: "NormalComponent3", want: "C1"},
		{name: "authored diffuse", component: "NormalComponent3", wwmi: true, authored: true, want: "C1"},
		{name: "linear candidates", component: "NormalComponent3", wwmi: true, linearOnly: true, want: "C1"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			candidates := []modelViewerDirectTextureAssignment{slot(1, "B1")}
			if !tt.linearOnly {
				candidates = append(candidates, slot(3, "D1"), slot(2, "A1"))
			}
			meshes := []modelViewerDirectMesh{{
				component:             tt.component,
				textureAuthored:       tt.authored,
				textureAssignments:    []modelViewerDirectTextureAssignment{mask},
				textureDefaultFile:    mask.file,
				slotTextureCandidates: candidates,
			}}

			attachSlotBoundDiffuse(meshes, dir, tt.wwmi, false)

			mesh := meshes[0]
			want := "Textures/" + tt.want + ".dds"
			if mesh.textureDefaultFile != want || len(mesh.textureAssignments) != 1 ||
				mesh.textureAssignments[0].file != want {
				t.Fatalf(
					"default = %q, assignments = %#v, want %s",
					mesh.textureDefaultFile,
					mesh.textureAssignments,
					want,
				)
			}
		})
	}
}
