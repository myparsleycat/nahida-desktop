package ui

import "testing"

// The GIMI shader reads the face light map's and the face shadow's alpha, so a bundled file whose alpha was lost
// would light every face fully.
func TestBundledToonMapsKeepSizeAndAlpha(t *testing.T) {
	alphaVaries := func(texture *viewerTextureData) bool {
		pixels := texture.mips[0]
		for i := 3; i < len(pixels); i += 4 {
			if pixels[i] != pixels[3] {
				return true
			}
		}
		return false
	}

	for _, faceType := range genshinFaceTypes {
		data, err := readBundledToonMap(bundledFaceLightmapURL(faceType))
		if err != nil {
			t.Fatal(err)
		}
		texture, err := decodeViewerImage(data)
		if err != nil {
			t.Fatalf("%s: %v", faceType, err)
		}
		if texture.width != 1024 || texture.height != 1024 || !alphaVaries(texture) {
			t.Errorf("%s face light map is %dx%d, alpha varies %v; want 1024x1024 with an SDF in alpha",
				faceType, texture.width, texture.height, alphaVaries(texture))
		}
	}

	maps, err := bundledToonMaps()
	if err != nil {
		t.Fatal(err)
	}
	if metal := maps[0]; metal.width != 256 || metal.height != 256 {
		t.Errorf("metal map is %dx%d, want 256x256", metal.width, metal.height)
	}
	if shadow := maps[1]; shadow.width != 512 || shadow.height != 512 || !alphaVaries(shadow) {
		t.Errorf("face shadow is %dx%d, alpha varies %v; want 512x512 with a mask in alpha",
			shadow.width, shadow.height, alphaVaries(shadow))
	}
}
