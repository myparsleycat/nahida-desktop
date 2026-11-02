package modelviewer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestListGenshinToonAssetsGroupsRampsAcrossSubfolders(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		"Avatar_Tex_MetalMap.png",
		"Avatar/avatar_loli_tex_facelightmap.png",
		"Avatar/Avatar_Tex_Face_Shadow.png",
		"Nahida/Avatar_Loli_Catalyst_Nahida_Tex_Body_Shadow_Ramp.png",
		"Nahida/Avatar_Loli_Catalyst_Nahida_Tex_Hair_Shadow_Ramp.png",
		"Nahida/Avatar_Loli_Catalyst_Nahida_Tex_Body_Diffuse.png",
		"Custom_Shadow_Ramp.png",
		"Deep/Nested/Avatar_Boy_Tex_FaceLightmap.png",
	}
	for _, name := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("png"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	assets, err := New().ListGenshinToonAssets(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(assets.MetalMapURL, "Avatar_Tex_MetalMap.png") ||
		!strings.Contains(assets.FaceShadowURL, "Avatar_Tex_Face_Shadow.png") {
		t.Fatalf("shared maps = %q, %q", assets.MetalMapURL, assets.FaceShadowURL)
	}
	if len(assets.FaceLightmaps) != 1 || assets.FaceLightmaps["loli"] == "" {
		t.Fatalf("face lightmaps = %v", assets.FaceLightmaps)
	}
	if len(assets.Ramps) != 2 {
		t.Fatalf("ramps = %#v", assets.Ramps)
	}
	for _, ramp := range assets.Ramps {
		switch ramp.Name {
		case "Avatar_Loli_Catalyst_Nahida":
			if !strings.Contains(ramp.BodyURL, "Body_Shadow_Ramp") ||
				!strings.Contains(ramp.HairURL, "Hair_Shadow_Ramp") {
				t.Fatalf("Nahida ramp = %#v", ramp)
			}
		case "Custom":
			if ramp.BodyURL == "" || ramp.HairURL != "" {
				t.Fatalf("custom ramp = %#v", ramp)
			}
		default:
			t.Fatalf("unexpected ramp %#v", ramp)
		}
	}
}
