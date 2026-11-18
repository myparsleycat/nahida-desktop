package modelviewer

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

// characterMotionIndexJSON lists the hosted motion file of each character with its size and SHA-256.
//
//go:embed character_motion_index.json
var characterMotionIndexJSON []byte

const (
	characterMotionBaseURL = "https://nhd-public.nahida.live/desktop/character-motion/"
	// characterMotionMaxGLB bounds the decompressed size of a motion file.
	characterMotionMaxGLB = 256 << 20
	// characterMotionMaxFPS bounds the sample rate a motion file may declare.
	characterMotionMaxFPS = 240
)

type characterFileEntry struct {
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// valid reports whether the entry names a file below its cache folder with a size.
func (e characterFileEntry) valid() bool {
	clean := path.Clean(e.File)
	return clean == e.File && !path.IsAbs(clean) && !strings.HasPrefix(clean, "../") && clean != ".." && e.Size > 0
}

var characterMotionIndex = sync.OnceValues(func() (map[string]map[string]characterFileEntry, error) {
	var index struct {
		Format     int                                      `json:"format"`
		Characters map[string]map[string]characterFileEntry `json:"characters"`
	}
	if err := json.Unmarshal(characterMotionIndexJSON, &index); err != nil {
		return nil, fmt.Errorf("parse character motion index: %w", err)
	}
	if index.Format != 1 {
		return nil, fmt.Errorf("unsupported character motion index format %d", index.Format)
	}
	return index.Characters, nil
})

func characterMotionLookup(game, id string) (characterFileEntry, bool) {
	index, err := characterMotionIndex()
	if err != nil {
		return characterFileEntry{}, false
	}
	entry, ok := index[strings.ToLower(game)][id]
	return entry, ok
}

// HasCharacterMotion reports whether game motions exist for the character id of game (GIMI, SRMI, ZZMI, WWMI or
// EFMI).
func HasCharacterMotion(game, id string) bool {
	_, ok := characterMotionLookup(game, id)
	return ok
}

// LoadCharacterMotion returns the motions of the character id of game, downloading its file into the app data
// cache when the cache does not hold it yet.
func (t *Service) LoadCharacterMotion(ctx context.Context, game, id string) (*CharacterMotion, error) {
	entry, ok := characterMotionLookup(game, id)
	if !ok {
		return nil, fmt.Errorf("no character motion for %s/%s", game, id)
	}
	if !entry.valid() {
		return nil, fmt.Errorf("invalid character motion entry %q", entry.File)
	}
	if t.appData == nil {
		return nil, errors.New("character motion cache is not available")
	}
	t.motionPrune.Do(func() {
		t.motionFiles <- struct{}{}
		defer func() { <-t.motionFiles }()
		t.pruneCharacterMotionCache()
	})
	destination, err := t.appData.Resolve(filepath.Join(appdata.CharacterMotionDir, filepath.FromSlash(entry.File)))
	if err != nil {
		return nil, err
	}
	data, err := t.cachedCharacterFile(ctx, t.motionFiles, characterMotionBaseURL, entry, destination)
	if err != nil {
		return nil, infra.AnnotateError(err, infra.Diagnostic{
			Operation: "load-character-motion",
			Fields: map[string]any{
				"game": game, "character": id, "path": destination,
				"url": infra.SanitizeLogURL(characterMotionBaseURL + entry.File),
			},
		})
	}
	motion, err := parseCharacterMotion(data, game)
	if err != nil {
		return nil, fmt.Errorf("parse character motion %s: %w", destination, err)
	}
	return motion, nil
}

// cachedCharacterFile returns the verified file of entry below baseURL, downloading it to destination when the
// cached copy is missing or does not match the entry. gate admits one download at a time per cache. A download
// replaces the cached file atomically, so the cache is read without waiting for downloads.
func (t *Service) cachedCharacterFile(
	ctx context.Context,
	gate chan struct{},
	baseURL string,
	entry characterFileEntry,
	destination string,
) ([]byte, error) {
	if data, err := readCachedCharacterFile(ctx, destination, entry); err == nil {
		return data, nil
	}
	if t.download == nil {
		return nil, errors.New("character data download is not available")
	}

	select {
	case gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-gate }()
	// Another viewer may have downloaded the file while this one waited.
	if data, err := readCachedCharacterFile(ctx, destination, entry); err == nil {
		return data, nil
	}
	err := t.download.File(ctx, infra.DownloadRequest{
		URL: baseURL + entry.File, Destination: destination, Size: entry.Size, MaxSize: entry.Size,
	})
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", entry.File, err)
	}
	data, err := readCachedCharacterFile(ctx, destination, entry)
	if err != nil {
		if ctx.Err() == nil {
			_ = os.Remove(destination)
		}
		return nil, err
	}
	return data, nil
}

// readCachedCharacterFile reads destination under a disk slot and checks it against entry.
func readCachedCharacterFile(ctx context.Context, destination string, entry characterFileEntry) ([]byte, error) {
	release, err := diskio.Acquire(ctx, destination)
	if err != nil {
		return nil, err
	}
	defer release()
	data, err := os.ReadFile(destination)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", entry.File, err)
	}
	if err := verifyCharacterFile(data, entry); err != nil {
		return nil, err
	}
	return data, nil
}

func (t *Service) pruneCharacterMotionCache() {
	index, err := characterMotionIndex()
	if err != nil {
		return
	}
	dir, err := t.appData.Resolve(appdata.CharacterMotionDir)
	if err != nil {
		t.log.Warn(fmt.Sprintf("Character motion cache could not be resolved: %v", err), "ModelViewer.characterMotion")
		return
	}
	var keep []string
	for _, entries := range index {
		for _, entry := range entries {
			keep = append(keep, filepath.Join(dir, filepath.FromSlash(entry.File)))
		}
	}
	if err := pruneCharacterCache(dir, keep); err != nil {
		t.log.Warn(fmt.Sprintf("Character motion cache %s was not fully pruned: %v", dir, err),
			"ModelViewer.characterMotion")
	}
}

// pruneCharacterCache removes the files in the folders of dir that keep does not name: the files of an earlier index
// and unfinished downloads. It must not run while a file of the cache downloads.
func pruneCharacterCache(dir string, keep []string) error {
	games, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var errs []error
	for _, game := range games {
		if !game.IsDir() {
			continue
		}
		gameDir := filepath.Join(dir, game.Name())
		files, err := os.ReadDir(gameDir)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, entry := range files {
			file := filepath.Join(gameDir, entry.Name())
			if !entry.Type().IsRegular() ||
				slices.ContainsFunc(keep, func(kept string) bool { return platform.SamePathFold(kept, file) }) {
				continue
			}
			if err := os.Remove(file); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func verifyCharacterFile(data []byte, entry characterFileEntry) error {
	if int64(len(data)) != entry.Size {
		return fmt.Errorf("%s size is %d, expected %d", entry.File, len(data), entry.Size)
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), entry.SHA256) {
		return fmt.Errorf("%s SHA-256 does not match its index entry", entry.File)
	}
	return nil
}

// CharacterMotion is a character's skeleton, reference meshes and clips, in the space of the game's mods.
type CharacterMotion struct {
	Game      string
	Character string
	// FPS is the rate every clip is sampled at.
	FPS   float64
	Clips []CharacterMotionClip
	// Morphs lists the meshes whose shape keys some clip drives, with their keys in order.
	Morphs []CharacterMotionMorph

	nodes []motionNode
	// names holds each node's name, or "" for one without.
	names []string
	// toMod takes the skeleton's mesh space to mod space and fromMod back.
	toMod, fromMod motionAffine
	// restInv is each node's rest world matrix inverted and followed by the mesh-to-world bind.
	restInv []motionAffine
	// flipX mirrors mod space on X against the mesh space.
	flipX bool
	// unit is how many mod-space units make a metre; zero counts as one.
	unit float64
	refs []motionReference
	// skins lists the joint nodes of each skin in palette order.
	skins [][]int
}

// CharacterMotionClip is a clip sampled at its motion's FPS.
type CharacterMotionClip struct {
	Name  string
	Group string
	Loop  bool
	// LoopStart is the frame a looping clip continues from after its last frame.
	LoopStart int
	Frames    int

	channels []motionChannel
	morphs   []motionMorphChannel
}

// CharacterMotionMorph names a mesh whose shape keys a motion drives, and those keys in the order the clips give
// their weights.
type CharacterMotionMorph struct {
	Mesh    string
	Targets []string
}

// motionMorphChannel holds the weights of every key of Morphs[morph] per frame, or for a single frame, and how far
// each key moves over the frames.
type motionMorphChannel struct {
	morph  int
	values []float32
	ranges []float32
}

type motionNode struct {
	parent int
	rest   motionTRS
	world  motionAffine
}

type motionTRS struct {
	t [3]float64
	r [4]float64
	s [3]float64
}

const (
	motionTranslation = iota
	motionRotation
	motionScale
)

// motionChannel holds one record per frame, or a single record for a value the clip holds still.
type motionChannel struct {
	node   int
	path   int
	values []float32
}

// motionReference is a vertex of the source model in mod space with its skin and its four bone nodes and weights.
type motionReference struct {
	position [3]float32
	skin     int32
	nodes    [4]int32
	weights  [4]float32
}

// motionAffine is a 3x4 row-major affine matrix.
type motionAffine [12]float64

func motionIdentity() motionAffine {
	return motionAffine{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}
}

func (a motionAffine) mul(b motionAffine) motionAffine {
	var out motionAffine
	for row := range 3 {
		for col := range 4 {
			value := a[row*4]*b[col] + a[row*4+1]*b[4+col] + a[row*4+2]*b[8+col]
			if col == 3 {
				value += a[row*4+3]
			}
			out[row*4+col] = value
		}
	}
	return out
}

func (a motionAffine) apply(p [3]float64) [3]float64 {
	return [3]float64{
		a[0]*p[0] + a[1]*p[1] + a[2]*p[2] + a[3],
		a[4]*p[0] + a[5]*p[1] + a[6]*p[2] + a[7],
		a[8]*p[0] + a[9]*p[1] + a[10]*p[2] + a[11],
	}
}

func (a motionAffine) inverse() (motionAffine, bool) {
	c00 := a[5]*a[10] - a[6]*a[9]
	c01 := a[6]*a[8] - a[4]*a[10]
	c02 := a[4]*a[9] - a[5]*a[8]
	det := a[0]*c00 + a[1]*c01 + a[2]*c02
	if det == 0 || math.IsNaN(det) || math.IsInf(det, 0) {
		return motionAffine{}, false
	}
	inv := 1 / det
	var out motionAffine
	out[0] = c00 * inv
	out[1] = (a[2]*a[9] - a[1]*a[10]) * inv
	out[2] = (a[1]*a[6] - a[2]*a[5]) * inv
	out[4] = c01 * inv
	out[5] = (a[0]*a[10] - a[2]*a[8]) * inv
	out[6] = (a[2]*a[4] - a[0]*a[6]) * inv
	out[8] = c02 * inv
	out[9] = (a[1]*a[8] - a[0]*a[9]) * inv
	out[10] = (a[0]*a[5] - a[1]*a[4]) * inv
	for row := range 3 {
		out[row*4+3] = -(out[row*4]*a[3] + out[row*4+1]*a[7] + out[row*4+2]*a[11])
	}
	return out, true
}

func (trs motionTRS) affine() motionAffine {
	x, y, z, w := trs.r[0], trs.r[1], trs.r[2], trs.r[3]
	s := trs.s
	return motionAffine{
		(1 - 2*(y*y+z*z)) * s[0], 2 * (x*y - z*w) * s[1], 2 * (x*z + y*w) * s[2], trs.t[0],
		2 * (x*y + z*w) * s[0], (1 - 2*(x*x+z*z)) * s[1], 2 * (y*z - x*w) * s[2], trs.t[1],
		2 * (x*z - y*w) * s[0], 2 * (y*z + x*w) * s[1], (1 - 2*(x*x+y*y)) * s[2], trs.t[2],
	}
}

// motionTRSFromMatrix splits a column-major glTF matrix with positive scale into its parts.
func motionTRSFromMatrix(m [16]float64) motionTRS {
	trs := motionTRS{t: [3]float64{m[12], m[13], m[14]}}
	var columns [3][3]float64
	for col := range 3 {
		length := math.Sqrt(m[col*4]*m[col*4] + m[col*4+1]*m[col*4+1] + m[col*4+2]*m[col*4+2])
		trs.s[col] = length
		if length == 0 {
			length = 1
		}
		for row := range 3 {
			columns[col][row] = m[col*4+row] / length
		}
	}
	r00, r11, r22 := columns[0][0], columns[1][1], columns[2][2]
	r10, r01 := columns[0][1], columns[1][0]
	r20, r02 := columns[0][2], columns[2][0]
	r21, r12 := columns[1][2], columns[2][1]
	switch trace := r00 + r11 + r22; {
	case trace > 0:
		s := math.Sqrt(trace+1) * 2
		trs.r = [4]float64{(r21 - r12) / s, (r02 - r20) / s, (r10 - r01) / s, s / 4}
	case r00 > r11 && r00 > r22:
		s := math.Sqrt(1+r00-r11-r22) * 2
		trs.r = [4]float64{s / 4, (r01 + r10) / s, (r02 + r20) / s, (r21 - r12) / s}
	case r11 > r22:
		s := math.Sqrt(1+r11-r00-r22) * 2
		trs.r = [4]float64{(r01 + r10) / s, s / 4, (r12 + r21) / s, (r02 - r20) / s}
	default:
		s := math.Sqrt(1+r22-r00-r11) * 2
		trs.r = [4]float64{(r02 + r20) / s, (r12 + r21) / s, s / 4, (r10 - r01) / s}
	}
	return trs
}

// parseCharacterMotion reads a gzip-compressed motion GLB written by scripts/import-character-motion.mjs.
func parseCharacterMotion(compressed []byte, game string) (*CharacterMotion, error) {
	reader, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	glb, err := io.ReadAll(io.LimitReader(reader, characterMotionMaxGLB+1))
	if err != nil {
		return nil, err
	}
	if len(glb) > characterMotionMaxGLB {
		return nil, errors.New("character motion is too large")
	}
	doc, bin, err := splitCharacterGLB(glb)
	if err != nil {
		return nil, err
	}
	if doc.Extras.Format != 2 {
		return nil, fmt.Errorf("unsupported character motion format %d", doc.Extras.Format)
	}
	if doc.Extras.FPS <= 0 || doc.Extras.FPS > characterMotionMaxFPS {
		return nil, fmt.Errorf("invalid character motion frame rate %v", doc.Extras.FPS)
	}

	motion := &CharacterMotion{
		Game: strings.ToUpper(game), Character: doc.Extras.Character, FPS: doc.Extras.FPS,
		flipX: strings.EqualFold(game, "GIMI") || strings.EqualFold(game, "SRMI"),
		unit:  modUnitsPerMetre(game),
	}
	if err := motion.readNodes(doc); err != nil {
		return nil, err
	}
	accessor := func(index, components int) ([]float32, error) {
		return readCharacterAccessor(doc, bin, index, components)
	}
	if err := motion.readSkins(doc, accessor); err != nil {
		return nil, err
	}
	if err := motion.readClips(doc, accessor); err != nil {
		return nil, err
	}
	return motion, nil
}

// modUnitsPerMetre is how many mod-space units make a metre: WWMI mods keep Unreal's centimetres, the other games'
// mods use metres.
func modUnitsPerMetre(game string) float64 {
	if strings.EqualFold(game, "WWMI") {
		return 100
	}
	return 1
}

// fromMetres converts a length in metres to mod units.
func (m *CharacterMotion) fromMetres(length float64) float64 {
	if m.unit == 0 {
		return length
	}
	return length * m.unit
}

func (m *CharacterMotion) readNodes(doc *characterGLTF) error {
	m.nodes = make([]motionNode, len(doc.Nodes))
	m.names = make([]string, len(doc.Nodes))
	for i := range m.nodes {
		m.nodes[i].parent = -1
		m.names[i] = doc.Nodes[i].Name
	}
	for i, node := range doc.Nodes {
		rest := motionTRS{r: [4]float64{0, 0, 0, 1}, s: [3]float64{1, 1, 1}}
		if node.Matrix != nil {
			rest = motionTRSFromMatrix(*node.Matrix)
		}
		if node.Translation != nil {
			rest.t = *node.Translation
		}
		if node.Rotation != nil {
			rest.r = *node.Rotation
		}
		if node.Scale != nil {
			rest.s = *node.Scale
		}
		// The pose blends rotations as unit quaternions. affine() turns a zero one into no rotation, so it becomes
		// the identity.
		var ok bool
		if rest.r, ok = unitMotionQuaternion(rest.r); !ok {
			rest.r = [4]float64{0, 0, 0, 1}
		}
		m.nodes[i].rest = rest
		for _, child := range node.Children {
			if child < 0 || child >= len(m.nodes) || child == i || m.nodes[child].parent >= 0 {
				return fmt.Errorf("node %d has an invalid child %d", i, child)
			}
			m.nodes[child].parent = i
		}
	}
	rest := make([]motionTRS, len(m.nodes))
	for i := range m.nodes {
		rest[i] = m.nodes[i].rest
	}
	worlds, err := m.worlds(rest, nil)
	if err != nil {
		return err
	}
	for i := range m.nodes {
		m.nodes[i].world = worlds[i]
	}
	return nil
}

// worlds composes local transforms into world matrices, into out when it has room.
func (m *CharacterMotion) worlds(local []motionTRS, out []motionAffine) ([]motionAffine, error) {
	if len(out) != len(m.nodes) {
		out = make([]motionAffine, len(m.nodes))
	}
	state := make([]uint8, len(m.nodes))
	var visit func(int, int) error
	visit = func(i, depth int) error {
		if state[i] == 2 {
			return nil
		}
		if state[i] == 1 || depth > len(m.nodes) {
			return errors.New("node hierarchy has a cycle")
		}
		state[i] = 1
		matrix := local[i].affine()
		if parent := m.nodes[i].parent; parent >= 0 {
			if err := visit(parent, depth+1); err != nil {
				return err
			}
			matrix = out[parent].mul(matrix)
		}
		out[i], state[i] = matrix, 2
		return nil
	}
	for i := range m.nodes {
		if err := visit(i, 0); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (m *CharacterMotion) readSkins(doc *characterGLTF, accessor func(int, int) ([]float32, error)) error {
	binds, err := m.readSkinBinds(doc, accessor)
	if err != nil {
		return err
	}
	for _, node := range doc.Nodes {
		if node.Mesh == nil || node.Skin == nil || *node.Mesh < 0 || *node.Mesh >= len(doc.Meshes) ||
			*node.Skin < 0 || *node.Skin >= len(doc.Skins) {
			continue
		}
		skin := doc.Skins[*node.Skin]
		toMod := m.toMod.mul(binds[*node.Skin])
		for _, primitive := range doc.Meshes[*node.Mesh].Primitives {
			if err := m.readReference(primitive.Attributes, *node.Skin, skin.Joints, toMod, accessor); err != nil {
				return err
			}
		}
	}
	return nil
}

// readSkinBinds reads the skins' joints and binds the skeleton to mod space. It returns each skin's mesh-to-world
// bind.
func (m *CharacterMotion) readSkinBinds(
	doc *characterGLTF,
	accessor func(int, int) ([]float32, error),
) ([]motionAffine, error) {
	binds := make([]motionAffine, len(doc.Skins))
	jointIBMs := make([][]motionAffine, len(doc.Skins))
	for s, skin := range doc.Skins {
		if len(skin.Joints) == 0 || skin.InverseBindMatrices == nil {
			return nil, fmt.Errorf("skin %d has no joints", s)
		}
		values, err := accessor(*skin.InverseBindMatrices, 16)
		if err != nil {
			return nil, err
		}
		if len(values) != len(skin.Joints)*16 {
			return nil, fmt.Errorf("skin %d has %d bind matrices for %d joints", s, len(values)/16, len(skin.Joints))
		}
		jointIBMs[s] = make([]motionAffine, len(skin.Joints))
		for j, node := range skin.Joints {
			if node < 0 || node >= len(m.nodes) {
				return nil, fmt.Errorf("skin %d joint %d does not exist", s, node)
			}
			var ibm motionAffine
			for row := range 3 {
				for col := range 4 {
					ibm[row*4+col] = float64(values[j*16+col*4+row])
				}
			}
			jointIBMs[s][j] = ibm
		}
		// Every joint of a skin shares one mesh-to-world bind, its rest world times its inverse bind.
		binds[s] = m.nodes[skin.Joints[0]].world.mul(jointIBMs[s][0])
		m.skins = append(m.skins, skin.Joints)
	}
	if len(binds) == 0 {
		return nil, errors.New("character motion has no skin")
	}
	if err := m.bindSkeleton(binds[0]); err != nil {
		return nil, err
	}
	return binds, nil
}

// bindSkeleton sets the transforms between the skeleton's mesh space, where bind takes it to world space, and
// mod space.
func (m *CharacterMotion) bindSkeleton(bind motionAffine) error {
	bindInverse, ok := bind.inverse()
	if !ok {
		return errors.New("character motion bind is singular")
	}
	flip := motionIdentity()
	if m.flipX {
		flip[0] = -1
	}
	m.toMod = flip.mul(bindInverse)
	m.fromMod = bind.mul(flip)
	m.restInv = make([]motionAffine, len(m.nodes))
	for i, node := range m.nodes {
		inverse, ok := node.world.inverse()
		if !ok {
			inverse = motionIdentity()
		}
		m.restInv[i] = inverse.mul(m.fromMod)
	}
	return nil
}

func (m *CharacterMotion) readReference(
	attributes map[string]int,
	skin int,
	joints []int,
	toMod motionAffine,
	accessor func(int, int) ([]float32, error),
) error {
	positionIndex, okPosition := attributes["POSITION"]
	jointIndex, okJoints := attributes["JOINTS_0"]
	weightIndex, okWeights := attributes["WEIGHTS_0"]
	if !okPosition || !okJoints || !okWeights {
		return nil
	}
	positions, err := accessor(positionIndex, 3)
	if err != nil {
		return err
	}
	jointValues, err := accessor(jointIndex, 4)
	if err != nil {
		return err
	}
	weights, err := accessor(weightIndex, 4)
	if err != nil {
		return err
	}
	count := len(positions) / 3
	if len(jointValues) != count*4 || len(weights) != count*4 {
		return errors.New("reference mesh attributes differ in length")
	}
	for v := range count {
		p := toMod.apply([3]float64{float64(positions[v*3]), float64(positions[v*3+1]), float64(positions[v*3+2])})
		ref := motionReference{position: [3]float32{float32(p[0]), float32(p[1]), float32(p[2])}, skin: int32(skin)}
		for k := range 4 {
			ref.nodes[k] = -1
			joint := int(jointValues[v*4+k])
			if weights[v*4+k] > 0 && joint >= 0 && joint < len(joints) {
				ref.nodes[k], ref.weights[k] = int32(joints[joint]), weights[v*4+k]
			}
		}
		m.refs = append(m.refs, ref)
	}
	return nil
}

func (m *CharacterMotion) readClips(doc *characterGLTF, accessor func(int, int) ([]float32, error)) error {
	for a, animation := range doc.Animations {
		clip := CharacterMotionClip{Name: animation.Name, Frames: 1}
		var loopFrom *float64
		if a < len(doc.Extras.Clips) {
			info := doc.Extras.Clips[a]
			clip.Name, clip.Group, clip.Loop = info.Name, info.Group, info.Mode == "loop"
			loopFrom = info.LoopFrom
		}
		for _, channel := range animation.Channels {
			if channel.Sampler < 0 || channel.Sampler >= len(animation.Samplers) || channel.Target.Node < 0 ||
				channel.Target.Node >= len(m.nodes) {
				return fmt.Errorf("clip %s has an invalid channel", clip.Name)
			}
			path, components := motionTranslation, 3
			switch channel.Target.Path {
			case "rotation":
				path, components = motionRotation, 4
			case "scale":
				path = motionScale
			case "translation":
			case "weights":
				morph, err := m.readMorphChannel(doc, channel.Target.Node, animation.Samplers[channel.Sampler].Output,
					accessor)
				if err != nil {
					return fmt.Errorf("clip %s: %w", clip.Name, err)
				}
				if morph.values != nil {
					clip.Frames = max(clip.Frames, len(morph.values)/len(m.Morphs[morph.morph].Targets))
					clip.morphs = append(clip.morphs, morph)
				}
				continue
			default:
				continue
			}
			values, err := accessor(animation.Samplers[channel.Sampler].Output, components)
			if err != nil {
				return fmt.Errorf("clip %s: %w", clip.Name, err)
			}
			if len(values) == 0 {
				continue
			}
			clip.Frames = max(clip.Frames, len(values)/components)
			clip.channels = append(clip.channels, motionChannel{node: channel.Target.Node, path: path, values: values})
		}
		if loopFrom != nil && *loopFrom >= 0 {
			clip.Loop = true
			clip.LoopStart = min(int(math.Round(*loopFrom*m.FPS)), clip.Frames-1)
		}
		m.Clips = append(m.Clips, clip)
	}
	return nil
}

// readMorphChannel reads a channel of shape key weights, adding the mesh its node names to Morphs.
func (m *CharacterMotion) readMorphChannel(
	doc *characterGLTF,
	node, output int,
	accessor func(int, int) ([]float32, error),
) (motionMorphChannel, error) {
	morph := doc.Nodes[node].Extras.Morph
	if morph == nil || morph.Mesh == "" || len(morph.Targets) == 0 || len(morph.Targets) > characterOriginalMaxTargets {
		return motionMorphChannel{}, fmt.Errorf("node %d drives shape keys it does not name", node)
	}
	index := slices.IndexFunc(m.Morphs, func(known CharacterMotionMorph) bool { return known.Mesh == morph.Mesh })
	if index < 0 {
		m.Morphs = append(m.Morphs, CharacterMotionMorph{Mesh: morph.Mesh, Targets: morph.Targets})
		index = len(m.Morphs) - 1
	} else if !slices.Equal(m.Morphs[index].Targets, morph.Targets) {
		// Another node names the mesh with other keys; its weights cannot be told apart, so they are left out.
		return motionMorphChannel{}, nil
	}
	values, err := accessor(output, 1)
	if err != nil {
		return motionMorphChannel{}, err
	}
	count := len(morph.Targets)
	if len(values)%count != 0 {
		return motionMorphChannel{}, fmt.Errorf("mesh %s weights do not fill whole frames", morph.Mesh)
	}
	if len(values) == 0 {
		return motionMorphChannel{}, nil
	}
	low, high := slices.Clone(values[:count]), slices.Clone(values[:count])
	for frame := count; frame < len(values); frame += count {
		for k, w := range values[frame : frame+count] {
			low[k], high[k] = min(low[k], w), max(high[k], w)
		}
	}
	for k := range high {
		high[k] -= low[k]
	}
	return motionMorphChannel{morph: index, values: values, ranges: high}, nil
}

// MorphRange is how far the clip moves key of Morphs[morph] over its frames, or 0 when it does not drive it.
func (m *CharacterMotion) MorphRange(clip, morph, key int) float32 {
	if clip < 0 || clip >= len(m.Clips) {
		return 0
	}
	for _, channel := range m.Clips[clip].morphs {
		if channel.morph == morph && key >= 0 && key < len(channel.ranges) {
			return channel.ranges[key]
		}
	}
	return 0
}

// MorphWeights writes into weights[i], for each of Morphs the clip drives, the weights of its keys a fraction t of
// the way from frame to next, and reports in driven which morphs the clip drives. Both slices are indexed like
// Morphs, and weights[i] must hold one value per key.
func (m *CharacterMotion) MorphWeights(clip, frame, next int, t float64, weights [][]float32, driven []bool) error {
	if clip < 0 || clip >= len(m.Clips) {
		return fmt.Errorf("clip %d does not exist", clip)
	}
	if len(weights) != len(m.Morphs) || len(driven) != len(m.Morphs) {
		return errors.New("morph output does not match the motion")
	}
	clear(driven)
	t = min(max(t, 0), 1)
	if math.IsNaN(t) {
		t = 0
	}
	for _, channel := range m.Clips[clip].morphs {
		count := len(m.Morphs[channel.morph].Targets)
		out := weights[channel.morph]
		if len(out) != count {
			return errors.New("morph output does not match the motion")
		}
		last := len(channel.values)/count - 1
		from := channel.values[min(max(frame, 0), last)*count:][:count]
		to := channel.values[min(max(next, 0), last)*count:][:count]
		for k := range out {
			out[k] = from[k] + (to[k]-from[k])*float32(t)
		}
		driven[channel.morph] = true
	}
	return nil
}

// CharacterMotionPose evaluates frames of a motion. It reuses its buffers, so each goroutine needs its own.
type CharacterMotionPose struct {
	motion *CharacterMotion
	local  []motionTRS
	world  []motionAffine
	// held is the local pose Hold kept, and evaluated reports that local holds an evaluated pose.
	held      []motionTRS
	evaluated bool
}

func (m *CharacterMotion) NewPose() *CharacterMotionPose {
	return &CharacterMotionPose{
		motion: m, local: make([]motionTRS, len(m.nodes)), world: make([]motionAffine, len(m.nodes)),
		held: make([]motionTRS, len(m.nodes)),
	}
}

// Hold keeps the pose last evaluated, blend included, for EvaluateFrom to fade from. It reports false before the
// first evaluation.
func (p *CharacterMotionPose) Hold() bool {
	if !p.evaluated {
		return false
	}
	copy(p.held, p.local)
	return true
}

// Evaluate writes into out, for every node, the 3x4 row-major matrix that moves a mod vertex bound to that node
// from the rest pose to frame of clip.
func (p *CharacterMotionPose) Evaluate(clip, frame int, out [][12]float32) error {
	return p.EvaluateBetween(clip, frame, frame, 0, out)
}

// EvaluateBetween is Evaluate for the pose a fraction t of the way from frame to next: translations and scales
// are interpolated linearly and rotations along the shorter arc.
func (p *CharacterMotionPose) EvaluateBetween(clip, frame, next int, t float64, out [][12]float32) error {
	return p.EvaluateFrom(0, clip, frame, next, t, out)
}

// EvaluateFrom is EvaluateBetween with the pose Hold kept mixed in by the share hold, from 0 for none to 1 for the
// held pose alone, node by node in the same way frames are interpolated.
func (p *CharacterMotionPose) EvaluateFrom(hold float64, clip, frame, next int, t float64, out [][12]float32) error {
	m := p.motion
	if clip < 0 || clip >= len(m.Clips) {
		return fmt.Errorf("clip %d does not exist", clip)
	}
	if len(out) != len(m.nodes) {
		return errors.New("pose output does not match the skeleton")
	}
	t = min(max(t, 0), 1)
	if math.IsNaN(t) {
		t = 0
	}
	for i := range m.nodes {
		p.local[i] = m.nodes[i].rest
	}
	for _, channel := range m.Clips[clip].channels {
		components := 3
		if channel.path == motionRotation {
			components = 4
		}
		last := len(channel.values)/components - 1
		from := channel.values[min(max(frame, 0), last)*components:][:components]
		to := channel.values[min(max(next, 0), last)*components:][:components]
		local := &p.local[channel.node]
		switch channel.path {
		case motionTranslation:
			local.t = lerpMotionVec3(from, to, t)
		case motionScale:
			local.s = lerpMotionVec3(from, to, t)
		case motionRotation:
			if q, ok := slerpMotionRotation(from, to, t); ok {
				local.r = q
			}
		}
	}
	if hold = min(max(hold, 0), 1); hold > 0 {
		for i := range p.local {
			local, held := &p.local[i], &p.held[i]
			for k := range 3 {
				local.t[k] += (held.t[k] - local.t[k]) * hold
				local.s[k] += (held.s[k] - local.s[k]) * hold
			}
			local.r = slerpMotionQuaternion(local.r, held.r, hold)
		}
	}
	p.evaluated = true
	worlds, err := m.worlds(p.local, p.world)
	if err != nil {
		return err
	}
	p.world = worlds
	for i := range m.nodes {
		matrix := m.toMod.mul(worlds[i]).mul(m.restInv[i])
		for k, value := range matrix {
			out[i][k] = float32(value)
		}
	}
	return nil
}

func lerpMotionVec3(from, to []float32, t float64) [3]float64 {
	var out [3]float64
	for i := range out {
		out[i] = float64(from[i]) + (float64(to[i])-float64(from[i]))*t
	}
	return out
}

// slerpMotionRotation interpolates two quaternions along the shorter arc. A key with no length is left out, and
// false reports that no key gives the rotation.
func slerpMotionRotation(from, to []float32, t float64) ([4]float64, bool) {
	a, okA := normalizeMotionQuaternion(from)
	b, okB := normalizeMotionQuaternion(to)
	switch {
	case t == 0:
		return a, okA
	case t == 1:
		return b, okB
	case !okA:
		return b, okB
	case !okB:
		return a, true
	}
	return slerpMotionQuaternion(a, b, t), true
}

// slerpMotionQuaternion interpolates two unit quaternions along the shorter arc.
func slerpMotionQuaternion(a, b [4]float64, t float64) [4]float64 {
	cos := a[0]*b[0] + a[1]*b[1] + a[2]*b[2] + a[3]*b[3]
	if cos < 0 {
		cos = -cos
		b = [4]float64{-b[0], -b[1], -b[2], -b[3]}
	}
	wa, wb := 1-t, t
	// Nearly equal rotations divide by a vanishing sine, where the linear blend is as close.
	if cos < 0.9995 {
		angle := math.Acos(cos)
		sin := math.Sin(angle)
		wa, wb = math.Sin((1-t)*angle)/sin, math.Sin(t*angle)/sin
	}
	q := [4]float64{wa*a[0] + wb*b[0], wa*a[1] + wb*b[1], wa*a[2] + wb*b[2], wa*a[3] + wb*b[3]}
	length := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	return [4]float64{q[0] / length, q[1] / length, q[2] / length, q[3] / length}
}

func normalizeMotionQuaternion(value []float32) ([4]float64, bool) {
	return unitMotionQuaternion([4]float64{float64(value[0]), float64(value[1]), float64(value[2]), float64(value[3])})
}

func unitMotionQuaternion(q [4]float64) ([4]float64, bool) {
	length := math.Sqrt(q[0]*q[0] + q[1]*q[1] + q[2]*q[2] + q[3]*q[3])
	if length == 0 || math.IsNaN(length) || math.IsInf(length, 0) {
		return q, false
	}
	return [4]float64{q[0] / length, q[1] / length, q[2] / length, q[3] / length}, true
}

// NodeCount is the length of the output Evaluate writes.
func (m *CharacterMotion) NodeCount() int { return len(m.nodes) }

// NodeName is the name of node i in the skeleton, or "" for one without.
func (m *CharacterMotion) NodeName(i int) string {
	if i < 0 || i >= len(m.names) {
		return ""
	}
	return m.names[i]
}

// NodeParent is the parent of node i in the skeleton, or -1 for a root.
func (m *CharacterMotion) NodeParent(i int) int { return m.nodes[i].parent }
