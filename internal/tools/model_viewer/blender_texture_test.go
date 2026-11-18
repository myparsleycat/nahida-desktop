package modelviewer

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/blenderlink"
	"nahida.live/desktop/internal/infra"
)

// A session keeps the texture keys of a Blender scene while the content behind them changes: new pixels and a
// rewritten file arrive as live textures, and only a change of role opens the session again.
func TestBlenderLinkReplacesTexturesInPlace(t *testing.T) {
	root := filepath.Join("testdata", "blender_live", "xxmi", "gimi")
	protocol := infra.NewProtocol()
	var updates []ModelViewerLiveUpdate
	service := NewWithOptions(Options{Protocol: protocol, EventEmit: func(name string, data ...any) {
		if update, ok := data[0].(ModelViewerLiveUpdate); ok && name == EventLiveMeshes {
			updates = append(updates, update)
		}
	}})
	ctx := context.Background()
	link, client := service.BlenderLink(), blenderlink.Link{Session: 1}

	normalPath := filepath.Join(t.TempDir(), "Normal.png")
	writeBlenderTestPNG(t, normalPath, 4, color.NRGBA{R: 128, G: 128, B: 255, A: 255})
	textures := func(paintRole, normalVersion string) []blenderTexture {
		return []blenderTexture{
			{Name: "BodyPaint", Role: paintRole, Source: blenderTexturePixels},
			{Name: "Extra", Role: paintRole, Source: blenderTexturePixels},
			{
				Name:    "Normal.png",
				Role:    "normal_map",
				Source:  blenderTextureFile,
				Path:    normalPath,
				Version: normalVersion,
			},
		}
	}
	sendScene := func(rev uint32, list []blenderTexture) blenderlink.Result {
		t.Helper()
		scene, objects := blenderLinkCapture(t, filepath.Join(root, "capture.json"), rev)
		scene = withBlenderTextures(t, scene, list)
		result, err := link.Scene(ctx, client, scene)
		if err != nil {
			t.Fatalf("scene %d: %v", rev, err)
		}
		if rev == 1 {
			for _, object := range objects {
				if _, err := link.Object(ctx, client, object); err != nil {
					t.Fatalf("object %s: %v", object.ObjectKey, err)
				}
			}
		}
		return result
	}
	sendScene(1, textures("diffuse", "1"))

	opened, err := link.OpenScene(ctx, client.Session, "")
	if err != nil {
		t.Fatal(err)
	}
	paintKey, normalKey := blenderTextureKey("diffuse", "BodyPaint"), blenderTextureKey("normal_map", "Normal.png")
	mesh := opened.Meshes[0]
	if mesh.TexKey == nil || *mesh.TexKey != paintKey || mesh.NormalMapKey == nil || *mesh.NormalMapKey != normalKey {
		t.Fatalf("mesh texture keys %v, %v; want %s, %s", mesh.TexKey, mesh.NormalMapKey, paintKey, normalKey)
	}
	if _, ok := opened.Textures[paintKey]; ok {
		t.Fatal("pixels that have not arrived have a texture")
	}
	if _, ok := opened.Textures[normalKey]; !ok {
		t.Fatal("the normal map file has no texture")
	}

	pixels := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255}
	result, err := link.Texture(ctx, client, blenderlink.TextureUpdate{
		SceneRev: 1, Image: "BodyPaint", Version: 1, Width: 2, Height: 2, Pixels: pixels,
	})
	if err != nil || result.Status != blenderlink.AckApplied {
		t.Fatalf("texture: %+v, %v", result, err)
	}
	live := lastBlenderLiveTextures(t, updates, opened.MemorySessionID)
	if len(live) != 1 || live[0].Key != paintKey || live[0].Texture.Encoding != "rgba8" ||
		live[0].Texture.Width != 2 || live[0].Texture.Height != 2 {
		t.Fatalf("live textures after pixels %+v", live)
	}
	if got := readModelViewerProtocolBytes(t, protocol, live[0].Texture.URL); !bytes.Equal(got, pixels) {
		t.Fatalf("live pixels %v, want %v", got, pixels)
	}

	// The add-on stops sending an image no slot shows until the next scene.
	updates = nil
	result, err = link.Texture(ctx, client, blenderlink.TextureUpdate{
		SceneRev: 1, Image: "Extra", Version: 1, Width: 2, Height: 2, Pixels: pixels,
	})
	if err != nil || result.Status != blenderlink.AckUnmatched || len(updates) != 0 {
		t.Fatalf("texture of an image no slot shows: %+v, %v, updates %+v", result, err, updates)
	}

	// Painted-out alpha that the loader would invert on its own keeps the image's first decision.
	erased := bytes.Repeat([]byte{200, 200, 200, 0}, 4)
	updates = nil
	if _, err := link.Texture(ctx, client, blenderlink.TextureUpdate{
		SceneRev: 1, Image: "BodyPaint", Version: 2, Width: 2, Height: 2, Pixels: erased,
	}); err != nil {
		t.Fatal(err)
	}
	live = lastBlenderLiveTextures(t, updates, opened.MemorySessionID)
	if len(live) != 1 {
		t.Fatalf("live textures after painting alpha %+v", live)
	}
	if got := readModelViewerProtocolBytes(t, protocol, live[0].Texture.URL); !bytes.Equal(got, erased) {
		t.Fatalf("live pixels after painting alpha %v, want %v", got, erased)
	}

	writeBlenderTestPNG(t, normalPath, 8, color.NRGBA{R: 100, G: 150, B: 255, A: 255})
	updates = nil
	if result := sendScene(2, textures("diffuse", "2")); result.Status != blenderlink.AckApplied {
		t.Fatalf("scene with a rewritten file answered %+v", result)
	}
	live = lastBlenderLiveTextures(t, updates, opened.MemorySessionID)
	if len(live) != 1 || live[0].Key != normalKey || live[0].Texture.Encoding != "image" {
		t.Fatalf("live textures after the file changed %+v", live)
	}
	decoded, _, err := image.Decode(bytes.NewReader(readModelViewerProtocolBytes(t, protocol, live[0].Texture.URL)))
	if err != nil || decoded.Bounds().Dx() != 8 {
		t.Fatalf("live normal map decodes as %v, %v; want the rewritten 8x8 file", decoded, err)
	}

	updates = nil
	sendScene(3, textures("light_map", "2"))
	if len(updates) != 1 || !updates[0].Reload || updates[0].MemorySessionID != opened.MemorySessionID {
		t.Fatalf("a role change sent %+v, want one reload", updates)
	}
}

func withBlenderTextures(t *testing.T, scene blenderlink.Scene, textures []blenderTexture) blenderlink.Scene {
	t.Helper()
	var settings map[string]any
	if err := json.Unmarshal(scene.Settings, &settings); err != nil {
		t.Fatal(err)
	}
	for _, object := range settings["objects"].(map[string]any) {
		object.(map[string]any)["textures"] = textures
	}
	var err error
	if scene.Settings, err = json.Marshal(settings); err != nil {
		t.Fatal(err)
	}
	return scene
}

func lastBlenderLiveTextures(t *testing.T, updates []ModelViewerLiveUpdate, session string) []ModelViewerLiveTexture {
	t.Helper()
	var live []ModelViewerLiveTexture
	for _, update := range updates {
		if update.Reload || update.MemorySessionID != session {
			t.Fatalf("unexpected update %+v", update)
		}
		live = append(live, update.Textures...)
	}
	return live
}

func writeBlenderTestPNG(t *testing.T, path string, size int, fill color.NRGBA) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for offset := 0; offset < len(img.Pix); offset += 4 {
		img.Pix[offset], img.Pix[offset+1], img.Pix[offset+2], img.Pix[offset+3] = fill.R, fill.G, fill.B, fill.A
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}
