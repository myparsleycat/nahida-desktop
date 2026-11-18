package modelviewer

import (
	"context"
	"fmt"
	"image"
	"maps"
	"strconv"

	"nahida.live/desktop/internal/blenderlink"
	"nahida.live/desktop/internal/infra"
)

// Sources of a Blender texture: a file the desktop reads, or pixels the add-on sends in texture transfers.
const (
	blenderTextureFile   = "file"
	blenderTexturePixels = "pixels"
)

// ModelViewerLiveTexture is a session texture replaced from a Blender live link. Key is the transport texture key
// the session's meshes refer to.
type ModelViewerLiveTexture struct {
	Key     string                      `json:"key"`
	Texture ModelViewerTextureTransport `json:"texture"`
}

// blenderTextureChoice is the image an object shows in one role, under the texture key its meshes refer to.
type blenderTextureChoice struct {
	key     string
	role    string
	texture blenderTexture
}

// blenderTextureSlot is what a texture key of a Blender scene shows. pixels is the version of the image's pixels
// for the "pixels" source, 0 while none have arrived.
type blenderTextureSlot struct {
	name, role, source, path, version string
	pixels                            uint32
}

// blenderAppliedTextures is what the textures of one viewer session show, by texture key.
type blenderAppliedTextures struct {
	link  uint64
	slots map[string]blenderTextureSlot
}

// blenderTextureKey depends only on the image and its role, so a session keeps the key while the image's file,
// version or pixels change.
func blenderTextureKey(role, name string) string {
	return modelViewerTextureVariantKey("blender-image/"+name, role, name)
}

// blenderTextureChoices picks the first image of each role among an object's textures. An image without a role
// takes the one its name suggests.
func blenderTextureChoices(textures []blenderTexture) []blenderTextureChoice {
	var choices []blenderTextureChoice
	taken := map[string]bool{}
	for _, texture := range textures {
		if texture.Name == "" {
			continue
		}
		role := texture.Role
		if role == "" {
			role = classifyModelViewerTextureRole(texture.Name)
		}
		if taken[role] {
			continue
		}
		taken[role] = true
		choices = append(choices, blenderTextureChoice{
			key: blenderTextureKey(role, texture.Name), role: role, texture: texture,
		})
	}
	return choices
}

// textureSlots returns the slot of every texture key the scene's objects use.
func (s *blenderLinkScene) textureSlots() map[string]blenderTextureSlot {
	slots := map[string]blenderTextureSlot{}
	for _, object := range s.settings.Objects {
		for _, choice := range blenderTextureChoices(object.Textures) {
			slot := blenderTextureSlot{
				name: choice.texture.Name, role: choice.role, source: choice.texture.Source,
				path: choice.texture.Path, version: choice.texture.Version,
			}
			if slot.source == blenderTexturePixels {
				slot.pixels = s.pixels[slot.name].Version
			}
			slots[choice.key] = slot
		}
	}
	return slots
}

// showsPixels reports whether a texture key of the scene shows the pixels of the named image.
func (s *blenderLinkScene) showsPixels(name string) bool {
	for _, object := range s.settings.Objects {
		for _, choice := range blenderTextureChoices(object.Textures) {
			if choice.texture.Name == name && choice.texture.Source == blenderTexturePixels {
				return true
			}
		}
	}
	return false
}

// dropUnusedPixels forgets the pixels of images the scene no longer describes as "pixels".
func (s *blenderLinkScene) dropUnusedPixels() {
	used := map[string]bool{}
	for _, object := range s.settings.Objects {
		for _, texture := range object.Textures {
			if texture.Source == blenderTexturePixels {
				used[texture.Name] = true
			}
		}
	}
	maps.DeleteFunc(s.pixels, func(name string, _ blenderPixels) bool { return !used[name] })
}

// syncTextures replaces, in each open session of a connection, the textures whose image content changed since
// the session showed it. Sessions OpenScene has not recorded yet are skipped.
func (b *BlenderLink) syncTextures(ctx context.Context, link uint64, sessions []blenderSceneSession) {
	b.apply.Lock()
	defer b.apply.Unlock()
	b.mu.Lock()
	state := b.scenes[link]
	if state == nil {
		b.mu.Unlock()
		return
	}
	want := state.textureSlots()
	pixels := maps.Clone(state.pixels)
	b.mu.Unlock()

	open := map[string]bool{}
	for _, session := range sessions {
		open[session.id] = true
	}
	maps.DeleteFunc(b.applied, func(id string, applied *blenderAppliedTextures) bool {
		return applied.link == link && !open[id]
	})

	for _, session := range sessions {
		applied := b.applied[session.id]
		if applied == nil {
			continue
		}
		update := ModelViewerLiveUpdate{MemorySessionID: session.id, Meshes: []ModelViewerLiveMesh{}}
		for key, slot := range want {
			shown, ok := applied.slots[key]
			if !ok || shown == slot || slot.source == blenderTexturePixels && slot.pixels == 0 {
				continue
			}
			b.serial++
			id := "live/" + strconv.FormatUint(b.serial, 10) + "/" + key
			entry, ids, stored, err := b.service.storeBlenderTexture(
				ctx, session.id, id, session.game.materialProfile(), key, slot, pixels[slot.name],
			)
			// A texture that fails is tried again only once its image changes again.
			applied.slots[key] = slot
			if err != nil {
				if !infra.IsCancellationError(err) {
					_ = infra.ReportError(b.service.log, err, "BlenderLink.Texture", infra.Diagnostic{
						Operation: "replace-blender-texture", Fields: map[string]any{
							"session": session.id, "key": key, "image": slot.name, "source": slot.source,
							"path": slot.path,
						},
					})
				}
				continue
			}
			if !stored {
				continue
			}
			update.Textures = append(update.Textures, ModelViewerLiveTexture{Key: key, Texture: entry})
			b.keepBuffers(session.id, "texture:"+key, ids)
		}
		if b.emit != nil && len(update.Textures) > 0 {
			b.emit(EventLiveMeshes, update)
		}
	}
}

// storeBlenderTexture stores the content of a texture slot under the session's buffer ID id. stored is false for
// a file the texture loader could not read, which it reports itself.
func (t *Service) storeBlenderTexture(
	ctx context.Context,
	sessionID, id string,
	profile ModelViewerMaterialProfile,
	key string,
	slot blenderTextureSlot,
	pixels blenderPixels,
) (ModelViewerTextureTransport, []string, bool, error) {
	var payload modelViewerTexturePayload
	switch slot.source {
	case blenderTexturePixels:
		if pixels.Version != slot.pixels || len(pixels.Pixels) == 0 {
			return ModelViewerTextureTransport{}, nil, false, nil
		}
		payload = blenderPixelsPayload(key, slot, pixels)
	case blenderTextureFile:
		payloads, err := t.loadBlenderFileTextures(ctx, profile, map[string]blenderTextureSlot{key: slot})
		if err != nil {
			return ModelViewerTextureTransport{}, nil, false, err
		}
		var ok bool
		if payload, ok = payloads[key]; !ok {
			return ModelViewerTextureTransport{}, nil, false, nil
		}
	default:
		return ModelViewerTextureTransport{}, nil, false, fmt.Errorf("texture source %q is not supported", slot.source)
	}
	entry, ids, err := t.storeModelViewerTexture(sessionID, id, profile, payload, modelViewerPayloadOptions{}, false)
	return entry, ids, err == nil, err
}

// loadBlenderFileTextures reads the files of "file" slots as the mod loader reads texture files. A file it cannot
// read is reported and left out.
func (t *Service) loadBlenderFileTextures(
	ctx context.Context,
	profile ModelViewerMaterialProfile,
	slots map[string]blenderTextureSlot,
) (map[string]modelViewerTexturePayload, error) {
	var jobs []modelViewerTextureJob
	for key, slot := range slots {
		if slot.source == blenderTextureFile {
			jobs = append(jobs, modelViewerTextureJob{
				path: slot.path, resourceName: slot.name, keys: []string{key}, role: slot.role, canonicalKey: key,
			})
		}
	}
	diagnostics := &infra.DiagnosticBatch{}
	settings := modelViewerTextureSettings{
		TextureFormat: "jpeg-safe", JPEGQuality: 85, MaterialProfile: profile, diagnostics: diagnostics,
	}
	outputs, _, err := runModelViewerTextureJobs(ctx, settings, 1, jobs)
	if err != nil {
		return nil, err
	}
	diagnostics.Report(t.log, "BlenderLink.Texture", "load-blender-texture")
	return outputs[0], nil
}

// blenderPixels is the newest pixels of an image. invertAlpha is decided from the image's first pixels and kept
// while the image stays in the scene, so painting alpha does not flip it.
type blenderPixels struct {
	blenderlink.TextureUpdate
	invertAlpha bool
}

func newBlenderPixels(update blenderlink.TextureUpdate, previous blenderPixels, seen bool) blenderPixels {
	pixels := blenderPixels{TextureUpdate: update, invertAlpha: previous.invertAlpha}
	if !seen {
		analysis := analyzeModelViewerTexture(blenderPixelsImage(update))
		pixels.invertAlpha = modelViewerTextureShouldInvertAlpha(update.Image, analysis)
	}
	return pixels
}

// blenderPixelsImage scales an image's pixels to the texture loader's size limit. The result may share the
// update's pixels.
func blenderPixelsImage(update blenderlink.TextureUpdate) *image.NRGBA {
	rgba := &image.NRGBA{
		Pix: update.Pixels, Stride: update.Width * 4, Rect: image.Rect(0, 0, update.Width, update.Height),
	}
	return downscaleModelViewerTexture(rgba, maxModelViewerTextureOutputPixels)
}

// blenderPixelsPayload prepares an image's pixels as the texture loader prepares a decoded file: scaled to the
// loader's size limit, with the alpha inverted when the loader would invert it.
func blenderPixelsPayload(
	key string,
	slot blenderTextureSlot,
	pixels blenderPixels,
) modelViewerTexturePayload {
	rgba := blenderPixelsImage(pixels.TextureUpdate)
	if pixels.invertAlpha {
		// The pixels may still be the update's, which other sessions read.
		rgba = cloneModelViewerNRGBA(rgba)
		for offset := 3; offset < len(rgba.Pix); offset += 4 {
			rgba.Pix[offset] = 255 - rgba.Pix[offset]
		}
	}
	bounds := rgba.Bounds()
	return modelViewerTexturePayload{
		Key: key, Role: slot.role, Bytes: rgba.Pix, Width: uint32(bounds.Dx()), Height: uint32(bounds.Dy()),
	}
}
