package modelviewer

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/webp"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

// characterOriginalIndexJSON lists the hosted original model file of each character with its size and SHA-256.
//
//go:embed character_original_index.json
var characterOriginalIndexJSON []byte

const (
	characterOriginalBaseURL = "https://nhd-public.nahida.live/desktop/character-original/"
	// characterOriginalMaxGLB bounds the decompressed size of an original model file.
	characterOriginalMaxGLB = 256 << 20
	// characterOriginalMaxTexture bounds the size of one hosted texture file.
	characterOriginalMaxTexture = 64 << 20
	// characterOriginalMaxPixels bounds the decoded size of one texture.
	characterOriginalMaxPixels = 8192 * 8192
	// characterOriginalMaxTargets bounds the shape keys of one mesh.
	characterOriginalMaxTargets = 256
)

var characterOriginalIndex = sync.OnceValues(func() (map[string]map[string]characterFileEntry, error) {
	var index struct {
		Format     int                                      `json:"format"`
		Characters map[string]map[string]characterFileEntry `json:"characters"`
	}
	if err := json.Unmarshal(characterOriginalIndexJSON, &index); err != nil {
		return nil, fmt.Errorf("parse character original index: %w", err)
	}
	if index.Format != 1 {
		return nil, fmt.Errorf("unsupported character original index format %d", index.Format)
	}
	return index.Characters, nil
})

func characterOriginalLookup(game, id string) (characterFileEntry, bool) {
	index, err := characterOriginalIndex()
	if err != nil {
		return characterFileEntry{}, false
	}
	entry, ok := index[strings.ToLower(game)][id]
	return entry, ok
}

// HasCharacterOriginal reports whether the original model of the character id of game (GIMI, SRMI, ZZMI, WWMI or
// EFMI) is hosted.
func HasCharacterOriginal(game, id string) bool {
	_, ok := characterOriginalLookup(game, id)
	return ok
}

// Roles of an original part, from its mesh and material names.
const (
	OriginalRoleFace    = "face"
	OriginalRoleEye     = "eye"
	OriginalRoleBrow    = "brow"
	OriginalRoleMouth   = "mouth"
	OriginalRoleHair    = "hair"
	OriginalRoleBody    = "body"
	OriginalRoleOverlay = "overlay"
)

// IsHeadRole reports whether role belongs to the face group that games draw apart from the parts mods replace.
func IsHeadRole(role string) bool {
	switch role {
	case OriginalRoleFace, OriginalRoleEye, OriginalRoleBrow, OriginalRoleMouth:
		return true
	}
	return false
}

// CharacterOriginal is a character's original model in the space of the game's mods.
type CharacterOriginal struct {
	Game      string
	Character string
	Parts     []CharacterOriginalPart
	// NodeNames names the skeleton nodes that the parts' Nodes index.
	NodeNames []string
}

// CharacterOriginalPart is one material of a mesh of the original model. Its vertices are in mod space, its UVs
// match the viewer's mod UVs, and its triangles wind counter-clockwise around their normals.
type CharacterOriginalPart struct {
	Mesh     string
	Material string
	Role     string
	// Shader names the source material's shader where the source records it.
	Shader    string
	Positions []float32
	Normals   []float32
	// Tangents holds four values per vertex, or nothing when the source has no tangents.
	Tangents []float32
	UVs      []float32
	// UVs1 is the second UV set, or nothing.
	UVs1 []float32
	// Colors holds RGBA8 vertex colours, or nothing.
	Colors  []uint8
	Indices []uint32
	// Nodes holds four skeleton nodes per vertex, -1 where a slot has no weight.
	Nodes   []int32
	Weights []float32
	// Textures maps the source material's shader slots to their textures.
	Textures map[string]CharacterOriginalTexture
	// Targets are the mesh's shape keys, each moving the part's vertices by its own mod-space offsets.
	Targets []CharacterOriginalTarget
	// Blink names the shape keys that close the eyes.
	Blink []string
}

// CharacterOriginalTarget is a shape key: three mod-space offsets per vertex of its part, and the weight the model
// holds it at when nothing drives it.
type CharacterOriginalTarget struct {
	Name      string
	Default   float32
	Positions []float32
}

// CharacterOriginalTexture is a hosted texture: a colour image and, when the alpha is kept apart, the image whose
// AlphaChannel ("r" or "a") holds it.
type CharacterOriginalTexture struct {
	Name         string
	Color        characterFileEntry
	Alpha        *characterFileEntry
	AlphaChannel string
	// LUT marks a colour LUT that stands in for a missing colour map; its output for white is the colour.
	LUT bool
}

type originalTextureFile struct {
	characterFileEntry
	Channel string `json:"channel"`
}

type originalTextureReference struct {
	Name  string               `json:"name"`
	Color *originalTextureFile `json:"color"`
	Alpha *originalTextureFile `json:"alpha"`
}

// LoadCharacterOriginal returns the original model of the character id of game, downloading its file into the app
// data cache when the cache does not hold it yet.
func (t *Service) LoadCharacterOriginal(
	ctx context.Context,
	game, id string,
	progress CharacterFileProgress,
) (*CharacterOriginal, error) {
	entry, ok := characterOriginalLookup(game, id)
	if !ok {
		return nil, fmt.Errorf("no original model for %s/%s", game, id)
	}
	if !entry.valid() {
		return nil, fmt.Errorf("invalid character original entry %q", entry.File)
	}
	if t.appData == nil {
		return nil, errors.New("character original cache is not available")
	}
	t.originalPrune.Do(func() {
		t.originalFiles <- struct{}{}
		defer func() { <-t.originalFiles }()
		t.pruneCharacterOriginalCache(context.WithoutCancel(ctx))
	})
	destination, err := t.appData.Resolve(filepath.Join(appdata.CharacterOriginalDir, filepath.FromSlash(entry.File)))
	if err != nil {
		return nil, err
	}
	data, err := t.cachedCharacterFile(ctx, t.originalFiles, characterOriginalBaseURL, entry, destination, progress)
	if err != nil {
		return nil, infra.AnnotateError(err, infra.Diagnostic{
			Operation: "load-character-original",
			Fields: map[string]any{
				"game": game, "character": id, "stage": "download-model", "path": destination,
				"url": infra.SanitizeLogURL(characterOriginalBaseURL + entry.File),
			},
		})
	}
	original, err := parseCharacterOriginal(data, game)
	if err != nil {
		return nil, fmt.Errorf("parse character original %s: %w", destination, err)
	}
	return original, nil
}

// LoadCharacterOriginalTexture returns a hosted texture with its alpha merged in, downloading its files into the
// app data cache when the cache does not hold them yet. Its rows run bottom-up, as in the game's own textures, so
// the original UVs address it the way mod UVs address mod textures.
func (t *Service) LoadCharacterOriginalTexture(
	ctx context.Context,
	texture CharacterOriginalTexture,
	progress CharacterFileProgress,
) (*image.NRGBA, error) {
	colorData, err := t.characterOriginalTextureFile(ctx, texture.Color, progress)
	if err != nil {
		return nil, err
	}
	var alphaData []byte
	if texture.Alpha != nil {
		if alphaData, err = t.characterOriginalTextureFile(ctx, *texture.Alpha, progress); err != nil {
			return nil, err
		}
	}
	out, err := mergeOriginalTexture(colorData, alphaData, texture.AlphaChannel)
	if err != nil {
		return nil, fmt.Errorf("decode texture %s: %w", texture.Name, err)
	}
	flipRows(out)
	return out, nil
}

func flipRows(img *image.NRGBA) {
	width := img.Rect.Dx() * 4
	for top, bottom := 0, img.Rect.Dy()-1; top < bottom; top, bottom = top+1, bottom-1 {
		a := img.Pix[top*img.Stride : top*img.Stride+width]
		b := img.Pix[bottom*img.Stride : bottom*img.Stride+width]
		for i := range a {
			a[i], b[i] = b[i], a[i]
		}
	}
}

func (t *Service) characterOriginalTextureFile(
	ctx context.Context,
	entry characterFileEntry,
	progress CharacterFileProgress,
) ([]byte, error) {
	if !entry.valid() || entry.Size > characterOriginalMaxTexture || !strings.HasPrefix(entry.File, "textures/") {
		return nil, fmt.Errorf("invalid character original texture %q", entry.File)
	}
	if t.appData == nil {
		return nil, errors.New("character original cache is not available")
	}
	destination, err := t.appData.Resolve(filepath.Join(appdata.CharacterOriginalDir, filepath.FromSlash(entry.File)))
	if err != nil {
		return nil, err
	}
	data, err := t.cachedCharacterFile(ctx, t.originalFiles, characterOriginalBaseURL, entry, destination, progress)
	if err != nil {
		return nil, infra.AnnotateError(err, infra.Diagnostic{
			Operation: "load-character-original",
			Fields: map[string]any{
				"stage": "download-texture", "path": destination,
				"url": infra.SanitizeLogURL(characterOriginalBaseURL + entry.File),
			},
		})
	}
	return data, nil
}

// pruneCharacterOriginalCache removes the model files the index does not name and the textures that no cached
// model of the index uses.
func (t *Service) pruneCharacterOriginalCache(ctx context.Context) {
	index, err := characterOriginalIndex()
	if err != nil {
		return
	}
	dir, err := t.appData.Resolve(appdata.CharacterOriginalDir)
	if err != nil {
		t.log.Warn(fmt.Sprintf("Character original cache could not be resolved: %v", err),
			"ModelViewer.characterOriginal")
		return
	}
	var keep []string
	for _, entries := range index {
		for _, entry := range entries {
			if !entry.valid() {
				continue
			}
			file := filepath.Join(dir, filepath.FromSlash(entry.File))
			keep = append(keep, file)
			data, err := readCachedCharacterFile(ctx, file, entry)
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				continue
			}
			textures, err := characterOriginalTextureFiles(data)
			if err != nil {
				continue
			}
			for _, texture := range textures {
				keep = append(keep, filepath.Join(dir, filepath.FromSlash(texture)))
			}
		}
	}
	if err := pruneCharacterCache(dir, keep); err != nil {
		t.log.Warn(fmt.Sprintf("Character original cache %s was not fully pruned: %v", dir, err),
			"ModelViewer.characterOriginal")
	}
}

func readCharacterOriginalGLB(compressed []byte) (*characterGLTF, []byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, nil, err
	}
	glb, err := io.ReadAll(io.LimitReader(reader, characterOriginalMaxGLB+1))
	if err != nil {
		return nil, nil, err
	}
	if len(glb) > characterOriginalMaxGLB {
		return nil, nil, errors.New("character original is too large")
	}
	doc, bin, err := splitCharacterGLB(glb)
	if err != nil {
		return nil, nil, err
	}
	if doc.Extras.Format != 1 {
		return nil, nil, fmt.Errorf("unsupported character original format %d", doc.Extras.Format)
	}
	return doc, bin, nil
}

func characterOriginalTextureFiles(compressed []byte) ([]string, error) {
	doc, _, err := readCharacterOriginalGLB(compressed)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, material := range doc.Materials {
		for _, texture := range material.Extras.Textures {
			for _, file := range []*originalTextureFile{texture.Color, texture.Alpha} {
				if file != nil && file.valid() {
					files = append(files, file.File)
				}
			}
		}
	}
	return files, nil
}

// parseCharacterOriginal reads a gzip-compressed original model GLB written by
// scripts/import-character-original.mjs.
func parseCharacterOriginal(compressed []byte, game string) (*CharacterOriginal, error) {
	doc, bin, err := readCharacterOriginalGLB(compressed)
	if err != nil {
		return nil, err
	}
	skeleton := &CharacterMotion{
		Game:  strings.ToUpper(game),
		flipX: strings.EqualFold(game, "GIMI") || strings.EqualFold(game, "SRMI"),
	}
	if err := skeleton.readNodes(doc); err != nil {
		return nil, err
	}
	accessor := func(index, components int) ([]float32, error) {
		return readCharacterAccessor(doc, bin, index, components)
	}
	binds, err := skeleton.readSkinBinds(doc, accessor)
	if err != nil {
		return nil, err
	}

	original := &CharacterOriginal{Game: skeleton.Game, Character: doc.Extras.Character, NodeNames: skeleton.names}
	for _, node := range doc.Nodes {
		if node.Mesh == nil || node.Skin == nil || *node.Mesh < 0 || *node.Mesh >= len(doc.Meshes) ||
			*node.Skin < 0 || *node.Skin >= len(doc.Skins) {
			continue
		}
		mesh := doc.Meshes[*node.Mesh]
		toMod := skeleton.toMod.mul(binds[*node.Skin])
		for p := range mesh.Primitives {
			part, err := readOriginalPart(doc, *node.Mesh, p, doc.Skins[*node.Skin].Joints, toMod, accessor)
			if err != nil {
				return nil, fmt.Errorf("mesh %s: %w", mesh.Name, err)
			}
			original.Parts = append(original.Parts, part)
		}
	}
	if len(original.Parts) == 0 {
		return nil, errors.New("character original has no parts")
	}
	return original, nil
}

// readOriginalTargets reads a primitive's shape keys over its vertices buffer vertices, keeps the offsets of the
// used vertices and turns them into mod space with the part's linear transform.
func readOriginalTargets(
	part *CharacterOriginalPart,
	names []string,
	defaults []float32,
	targets []map[string]int,
	linear motionAffine,
	vertices int,
	used []int,
	accessor func(int, int) ([]float32, error),
) error {
	if len(targets) == 0 {
		return nil
	}
	if len(targets) != len(names) || len(targets) > characterOriginalMaxTargets ||
		(defaults != nil && len(defaults) != len(names)) {
		return fmt.Errorf("primitive has %d shape keys, %d names and %d default weights", len(targets), len(names),
			len(defaults))
	}
	part.Targets = make([]CharacterOriginalTarget, len(targets))
	for k, target := range targets {
		index, ok := target["POSITION"]
		if !ok {
			return fmt.Errorf("shape key %s has no positions", names[k])
		}
		offsets, err := accessor(index, 3)
		if err != nil {
			return fmt.Errorf("shape key %s: %w", names[k], err)
		}
		if len(offsets) != vertices*3 {
			return fmt.Errorf("shape key %s has %d values for %d vertices", names[k], len(offsets), vertices)
		}
		offsets = compactOriginalVertices(offsets, 3, used)
		for v := range used {
			d := linear.apply([3]float64{float64(offsets[v*3]), float64(offsets[v*3+1]), float64(offsets[v*3+2])})
			offsets[v*3], offsets[v*3+1], offsets[v*3+2] = float32(d[0]), float32(d[1]), float32(d[2])
		}
		part.Targets[k] = CharacterOriginalTarget{Name: names[k], Positions: offsets}
		if defaults != nil {
			part.Targets[k].Default = defaults[k]
		}
	}
	return nil
}

func readOriginalPart(
	doc *characterGLTF,
	meshIndex, primitiveIndex int,
	joints []int,
	toMod motionAffine,
	accessor func(int, int) ([]float32, error),
) (CharacterOriginalPart, error) {
	mesh := doc.Meshes[meshIndex]
	primitive := mesh.Primitives[primitiveIndex]
	part := CharacterOriginalPart{Mesh: mesh.Name, Role: primitive.Extras.Role}
	if primitive.Material != nil && *primitive.Material >= 0 && *primitive.Material < len(doc.Materials) {
		material := doc.Materials[*primitive.Material]
		part.Material, part.Shader = material.Name, material.Extras.Shader
		part.Textures = make(map[string]CharacterOriginalTexture, len(material.Extras.Textures))
		for slot, reference := range material.Extras.Textures {
			if texture, ok := reference.texture(); ok {
				part.Textures[slot] = texture
			}
		}
	}

	read := func(name string, components int, required bool) ([]float32, error) {
		index, ok := primitive.Attributes[name]
		if !ok {
			if required {
				return nil, fmt.Errorf("primitive has no %s", name)
			}
			return nil, nil
		}
		return accessor(index, components)
	}
	positions, err := read("POSITION", 3, true)
	if err != nil {
		return part, err
	}
	count := len(positions) / 3
	attributes := []struct {
		name       string
		components int
		required   bool
		out        *[]float32
	}{
		{"NORMAL", 3, false, &part.Normals},
		{"TANGENT", 4, false, &part.Tangents},
		{"TEXCOORD_0", 2, false, &part.UVs},
		{"TEXCOORD_1", 2, false, &part.UVs1},
		{"WEIGHTS_0", 4, true, &part.Weights},
	}
	for _, attribute := range attributes {
		values, err := read(attribute.name, attribute.components, attribute.required)
		if err != nil {
			return part, err
		}
		if values != nil && len(values) != count*attribute.components {
			return part, fmt.Errorf("%s has %d values for %d vertices", attribute.name, len(values), count)
		}
		*attribute.out = values
	}
	jointValues, err := read("JOINTS_0", 4, true)
	if err != nil {
		return part, err
	}
	colors, err := read("COLOR_0", 4, false)
	if err != nil {
		return part, err
	}
	if len(jointValues) != count*4 || (colors != nil && len(colors) != count*4) {
		return part, errors.New("vertex attributes differ in length")
	}
	if primitive.Indices == nil {
		return part, errors.New("primitive has no indices")
	}
	indices, err := accessor(*primitive.Indices, 1)
	if err != nil {
		return part, err
	}
	if len(indices)%3 != 0 {
		return part, fmt.Errorf("%d indices do not form triangles", len(indices))
	}

	// Primitives of one mesh may share a vertex buffer; each part keeps only the vertices its triangles use.
	vertices := count
	used, rebased, err := compactOriginalIndices(indices, vertices)
	if err != nil {
		return part, err
	}
	positions = compactOriginalVertices(positions, 3, used)
	jointValues = compactOriginalVertices(jointValues, 4, used)
	colors = compactOriginalVertices(colors, 4, used)
	for _, attribute := range attributes {
		*attribute.out = compactOriginalVertices(*attribute.out, attribute.components, used)
	}
	count = len(used)

	// Normals follow the inverse transpose; a mirroring transform reverses the winding and tangent handedness.
	linear := toMod
	linear[3], linear[7], linear[11] = 0, 0, 0
	normalMatrix, ok := linear.inverse()
	if !ok {
		return part, errors.New("part transform is singular")
	}
	mirrored := linear[0]*(linear[5]*linear[10]-linear[6]*linear[9])-
		linear[1]*(linear[4]*linear[10]-linear[6]*linear[8])+
		linear[2]*(linear[4]*linear[9]-linear[5]*linear[8]) < 0

	part.Positions = make([]float32, len(positions))
	for v := range count {
		p := toMod.apply([3]float64{float64(positions[v*3]), float64(positions[v*3+1]), float64(positions[v*3+2])})
		part.Positions[v*3], part.Positions[v*3+1], part.Positions[v*3+2] = float32(p[0]), float32(p[1]), float32(p[2])
		if part.Normals != nil {
			n := part.Normals[v*3:][:3]
			x := normalMatrix[0]*float64(n[0]) + normalMatrix[4]*float64(n[1]) + normalMatrix[8]*float64(n[2])
			y := normalMatrix[1]*float64(n[0]) + normalMatrix[5]*float64(n[1]) + normalMatrix[9]*float64(n[2])
			z := normalMatrix[2]*float64(n[0]) + normalMatrix[6]*float64(n[1]) + normalMatrix[10]*float64(n[2])
			length := math.Sqrt(x*x + y*y + z*z)
			if length > 0 {
				x, y, z = x/length, y/length, z/length
			}
			n[0], n[1], n[2] = float32(x), float32(y), float32(z)
		}
		if part.Tangents != nil {
			tangent := part.Tangents[v*4:][:4]
			d := linear.apply([3]float64{float64(tangent[0]), float64(tangent[1]), float64(tangent[2])})
			length := math.Sqrt(d[0]*d[0] + d[1]*d[1] + d[2]*d[2])
			if length > 0 {
				d = [3]float64{d[0] / length, d[1] / length, d[2] / length}
			}
			tangent[0], tangent[1], tangent[2] = float32(d[0]), float32(d[1]), float32(d[2])
			if mirrored {
				tangent[3] = -tangent[3]
			}
		}
	}
	if err := readOriginalTargets(&part, mesh.Extras.TargetNames, mesh.Weights, primitive.Targets, linear, vertices,
		used, accessor); err != nil {
		return part, err
	}
	part.Blink = mesh.Extras.Blink
	if colors != nil {
		part.Colors = make([]uint8, len(colors))
		for i, value := range colors {
			part.Colors[i] = uint8(math.Round(float64(min(max(value, 0), 1)) * 255))
		}
	}

	part.Nodes = make([]int32, count*4)
	for v := range count {
		for k := range 4 {
			part.Nodes[v*4+k] = -1
			joint := int(jointValues[v*4+k])
			if part.Weights[v*4+k] > 0 && joint >= 0 && joint < len(joints) {
				part.Nodes[v*4+k] = int32(joints[joint])
			} else {
				part.Weights[v*4+k] = 0
			}
		}
	}

	part.Indices = rebased
	if mirrored {
		for i := 0; i < len(part.Indices); i += 3 {
			part.Indices[i+1], part.Indices[i+2] = part.Indices[i+2], part.Indices[i+1]
		}
	}
	return part, nil
}

// compactOriginalIndices lists, in ascending order, the vertices of a vertices-long buffer that indices use, and
// returns indices renumbered to that list.
func compactOriginalIndices(indices []float32, vertices int) (used []int, rebased []uint32, err error) {
	at := make([]int, vertices)
	for _, value := range indices {
		if value < 0 || int(value) >= vertices {
			return nil, nil, fmt.Errorf("index %v is past %d vertices", value, vertices)
		}
		at[int(value)] = 1
	}
	for v, mark := range at {
		if mark != 0 {
			at[v] = len(used)
			used = append(used, v)
		}
	}
	rebased = make([]uint32, len(indices))
	for i, value := range indices {
		rebased[i] = uint32(at[int(value)])
	}
	return used, rebased, nil
}

// compactOriginalVertices keeps the width values of each used vertex, or returns nil for nil values.
func compactOriginalVertices(values []float32, width int, used []int) []float32 {
	if values == nil {
		return nil
	}
	out := make([]float32, len(used)*width)
	for i, v := range used {
		copy(out[i*width:(i+1)*width], values[v*width:(v+1)*width])
	}
	return out
}

func (r originalTextureReference) texture() (CharacterOriginalTexture, bool) {
	if r.Color == nil || !r.Color.valid() {
		return CharacterOriginalTexture{}, false
	}
	texture := CharacterOriginalTexture{Name: r.Name, Color: r.Color.characterFileEntry}
	if r.Alpha != nil && r.Alpha.valid() && (r.Alpha.Channel == "r" || r.Alpha.Channel == "a") {
		alpha := r.Alpha.characterFileEntry
		texture.Alpha, texture.AlphaChannel = &alpha, r.Alpha.Channel
	}
	return texture, true
}

// mergeOriginalTexture decodes a colour WebP and, when alphaData is set, replaces its alpha with channel of that
// image. Colours under zero alpha are kept, since game textures store data there.
func mergeOriginalTexture(colorData, alphaData []byte, channel string) (*image.NRGBA, error) {
	decoded, err := decodeOriginalWebP(colorData)
	if err != nil {
		return nil, err
	}
	out := nrgbaOf(decoded)
	if alphaData == nil {
		return out, nil
	}
	alphaImage, err := decodeOriginalWebP(alphaData)
	if err != nil {
		return nil, err
	}
	alpha := nrgbaOf(alphaImage)
	if alpha.Rect.Size() != out.Rect.Size() {
		return nil, fmt.Errorf("alpha image is %v, colour image %v", alpha.Rect.Size(), out.Rect.Size())
	}
	offset := 0
	if channel == "a" {
		offset = 3
	}
	for y := range out.Rect.Dy() {
		src := alpha.Pix[y*alpha.Stride:]
		dst := out.Pix[y*out.Stride:]
		for x := range out.Rect.Dx() {
			dst[x*4+3] = src[x*4+offset]
		}
	}
	return out, nil
}

func decodeOriginalWebP(data []byte) (image.Image, error) {
	config, err := webp.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width*config.Height > characterOriginalMaxPixels {
		return nil, fmt.Errorf("texture size %dx%d is unsupported", config.Width, config.Height)
	}
	return webp.Decode(bytes.NewReader(data))
}

// nrgbaOf converts img to non-premultiplied RGBA without passing through premultiplied colour, which would lose
// the colour of transparent texels.
func nrgbaOf(img image.Image) *image.NRGBA {
	bounds := img.Bounds()
	switch src := img.(type) {
	case *image.NRGBA:
		if src.Rect.Min == (image.Point{}) {
			return src
		}
	case *image.NYCbCrA:
		out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
		for y := range bounds.Dy() {
			for x := range bounds.Dx() {
				px, py := bounds.Min.X+x, bounds.Min.Y+y
				yi, ci := src.YOffset(px, py), src.COffset(px, py)
				r, g, b := color.YCbCrToRGB(src.Y[yi], src.Cb[ci], src.Cr[ci])
				i := y*out.Stride + x*4
				out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = r, g, b, src.A[src.AOffset(px, py)]
			}
		}
		return out
	}
	out := image.NewNRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	draw.Draw(out, out.Rect, img, bounds.Min, draw.Src)
	return out
}
