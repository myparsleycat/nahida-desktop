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
	meshes := []modelViewerDirectMesh{
		{
			component:             "Component7",
			textureAssignments:    []modelViewerDirectTextureAssignment{mask},
			textureDefaultFile:    mask.file,
			slotTextureCandidates: []modelViewerDirectTextureAssignment{slot(1, "B1"), slot(3, "A1")},
		},
		{
			component:             "Component6",
			textureAssignments:    []modelViewerDirectTextureAssignment{mask},
			textureDefaultFile:    mask.file,
			slotTextureCandidates: []modelViewerDirectTextureAssignment{slot(1, "B1")},
		},
	}

	attachSlotBoundDiffuse(meshes, dir, false)

	swapped := meshes[0]
	if swapped.textureDefaultFile != "Textures/A1.dds" || len(swapped.textureAssignments) != 1 ||
		swapped.textureAssignments[0].file != "Textures/A1.dds" {
		t.Fatalf("swapped default = %q, assignments = %#v", swapped.textureDefaultFile, swapped.textureAssignments)
	}

	// A linear slot texture is no clearer diffuse than the linear ps-t0 file, so it stays.
	kept := meshes[1]
	if kept.textureDefaultFile != mask.file || len(kept.textureAssignments) != 1 ||
		kept.textureAssignments[0].file != mask.file {
		t.Fatalf("kept default = %q, assignments = %#v", kept.textureDefaultFile, kept.textureAssignments)
	}
}
