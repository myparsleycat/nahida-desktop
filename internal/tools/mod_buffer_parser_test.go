package tools

import "testing"

func TestMatchIndexResources(t *testing.T) {
	t.Parallel()

	t.Run("matches LOD index resources to hash-named position resources", func(t *testing.T) {
		t.Parallel()
		positions := []modBufferResource{
			{Name: "beef0021Position", Filename: "beef0021-Position.buf", Stride: 40},
			{Name: "beef0022Position", Filename: "beef0022-Position.buf", Stride: 40},
		}
		indices := []modBufferResource{
			{Name: "_LOD0.beef0021_16590_0_Index", Filename: "LOD0.beef0021-16590-0-Index.buf", Format: "DXGI_FORMAT_R32_UINT"},
			{Name: "_LOD0.beef0022_57612_0_Index", Filename: "LOD0.beef0022-57612-0-Index.buf", Format: "DXGI_FORMAT_R32_UINT"},
		}
		matches := matchIndexResources(positions, indices, nil)
		assertIndexNames(t, matches["beef0021position"], "_LOD0.beef0021_16590_0_Index")
		assertIndexNames(t, matches["beef0022position"], "_LOD0.beef0022_57612_0_Index")
	})

	t.Run("matches index resources when named variants follow the buffer kind", func(t *testing.T) {
		t.Parallel()
		positions := []modBufferResource{
			{Name: "ZhaoBodyPosition_Default", Filename: `.\1Default\ZhaoBodyPosition.buf`, Stride: 40},
			{Name: "ZhaoBodyPosition_Bodysuit", Filename: `.\2Bodysuit\ZhaoBodyPosition.buf`, Stride: 40},
		}
		indices := []modBufferResource{
			{Name: "ZhaoBodyAIB_Default", Filename: `.\1Default\ZhaoBodyA.ib`, Format: "DXGI_FORMAT_R32_UINT"},
			{Name: "ZhaoBodyAIB_Bodysuit", Filename: `.\2Bodysuit\ZhaoBodyA.ib`, Format: "DXGI_FORMAT_R32_UINT"},
		}
		matches := matchIndexResources(positions, indices, nil)
		assertIndexNames(t, matches["zhaobodyposition_default"], "ZhaoBodyAIB_Default")
		assertIndexNames(t, matches["zhaobodyposition_bodysuit"], "ZhaoBodyAIB_Bodysuit")
	})

	t.Run("does not treat an earlier ib inside the base name as the index kind", func(t *testing.T) {
		t.Parallel()
		positions := []modBufferResource{
			{Name: "Rib_BodyPosition", Filename: "Rib_BodyPosition.buf", Stride: 40},
		}
		indices := []modBufferResource{
			{Name: "Rib_Body_Index", Filename: "Rib_Body_Index.buf", Format: "DXGI_FORMAT_R32_UINT"},
		}
		matches := matchIndexResources(positions, indices, nil)
		assertIndexNames(t, matches["rib_bodyposition"], "Rib_Body_Index")
	})
}

func assertIndexNames(t *testing.T, resources []modBufferResource, want ...string) {
	t.Helper()
	if len(resources) != len(want) {
		t.Fatalf("matches = %#v, want %v", resources, want)
	}
	for i, name := range want {
		if resources[i].Name != name {
			t.Fatalf("matches[%d] = %q, want %q", i, resources[i].Name, name)
		}
	}
}
