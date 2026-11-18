package ui

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// motionGeometryKey names the geometry a character motion frame wrote, and motionClipPrefix starts the IDs of
// its clips in the animation bar.
const (
	motionGeometryKey = "motion"
	motionClipPrefix  = "motion:"
)

var errMotionNoBlend = errors.New("no mesh has bone weights")

// viewerMotion plays the game motions of the mod's character by skinning the meshes on the CPU. Like
// viewerCompute, one frame is skinned at a time on a goroutine, and the newest frame asked for meanwhile is the
// next one; everything else belongs to the UI thread.
type viewerMotion struct {
	scene  *viewerScene
	motion *modelviewer.CharacterMotion
	clips  []modelviewer.ModelViewerAnimationClip
	// clipIndexes maps the ID of each of clips to its index in motion.Clips.
	clipIndexes map[string]int
	// allClips lists the scene's own clips, then clips.
	allClips []modelviewer.ModelViewerAnimationClip
	meshes   []*viewerMotionMesh
	cancel   context.CancelFunc
	pose     *modelviewer.CharacterMotionPose
	busy     bool
	// latest is the frame to skin next.
	latest *viewerMotionRequest
	// shown marks meshes holding a skinned frame.
	shown bool
}

type viewerMotionRequest struct {
	clip, frame int
}

// viewerMotionMesh is a mesh the motion skins, with the base geometry it starts from.
type viewerMotionMesh struct {
	index     int
	positions []float32
	normals   []float32
	extra     []byte
	blend     []uint16
	palette   []int32
	// frame and skinned hold the last skinned frame's geometry and extra, for a mesh that loads after it.
	frame   []float32
	skinned []byte
	// spareFrame and spareSkinned are buffers no draw or upload reads any more, which a later frame is skinned
	// into.
	spareFrame   []float32
	spareSkinned []byte
}

// viewerMotionBase is the base geometry of a loaded mesh, which a motion takes instead of reading it again.
type viewerMotionBase struct {
	positions, normals []float32
	extra              []byte
}

func isMotionClip(clip *modelviewer.ModelViewerAnimationClip) bool {
	return clip != nil && strings.HasPrefix(clip.ID, motionClipPrefix)
}

// clipIndex returns the index in motion.Clips of clip, or -1 for a clip that is not one of the motion's.
func (m *viewerMotion) clipIndex(clip *modelviewer.ModelViewerAnimationClip) int {
	if clip == nil {
		return -1
	}
	if index, ok := m.clipIndexes[clip.ID]; ok {
		return index
	}
	return -1
}

// characterMotionAvailable reports whether game motions exist for the mod's character.
func (v *ModelViewer) characterMotionAvailable() bool {
	c := v.character
	return c != nil && v.tools != nil && modelviewer.HasCharacterMotion(c.game, c.ID)
}

// toggleCharacterMotion loads the character's motions into the animation bar, or removes them.
func (v *ModelViewer) toggleCharacterMotion() {
	if v.charMotion != nil || v.motionCancel != nil {
		v.dropMotion()
		return
	}
	scene, c := v.scene, v.character
	if scene == nil || c == nil || v.tools == nil {
		return
	}
	bases := map[int]viewerMotionBase{}
	for i, mesh := range scene.meshes {
		if mesh.info.BlendURL != "" && !mesh.unloaded && !mesh.cpuDropped && mesh.positions != nil {
			bases[i] = viewerMotionBase{positions: mesh.positions, normals: mesh.normals, extra: mesh.baseExtra}
		}
	}
	ctx, cancel := context.WithCancel(v.ctx)
	v.motionCancel = cancel
	v.revision++
	post := v.poster()
	tools, read := v.tools, v.read
	go func() {
		m, err := loadViewerMotion(ctx, tools, read, scene, bases, c.game, c.ID)
		post(func() {
			if v.motionCancel == nil || ctx.Err() != nil || v.scene != scene {
				cancel()
				return
			}
			v.motionCancel = nil
			v.revision++
			if err != nil {
				cancel()
				v.reportMotionError(err, c)
				return
			}
			m.cancel = cancel
			m.allClips = slices.Concat(scene.transport.Animations, m.clips)
			v.charMotion = m
			v.startAnimation(m.defaultClip())
		})
	}()
}

// defaultClip is the first idle clip. Star Rail motions have no groups, so their idle clip is found by name.
func (m *viewerMotion) defaultClip() string {
	clips := m.motion.Clips
	index := slices.IndexFunc(clips, func(clip modelviewer.CharacterMotionClip) bool { return clip.Group == "idle" })
	if index < 0 {
		index = max(slices.IndexFunc(clips, func(clip modelviewer.CharacterMotionClip) bool {
			return strings.EqualFold(clip.Name, "idle")
		}), 0)
	}
	return m.clips[index].ID
}

func (v *ModelViewer) reportMotionError(err error, c *viewerCharacter) {
	if infra.IsCancellationError(err) {
		return
	}
	if errors.Is(err, errMotionNoBlend) {
		v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.motion_no_blend"), "")
		return
	}
	err = infra.ReportError(v.log, err, "ModelViewer.characterMotion", infra.Diagnostic{
		Operation: "load-character-motion",
		Fields:    map[string]any{"modPath": v.path, "game": c.game, "character": c.ID},
	})
	v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.motion_error"), err.Error())
}

// loadViewerMotion reads the character's motion, the bone weights and base geometry of every mesh that has
// them, and maps each bone palette onto the skeleton. bases holds the base geometry of meshes already loaded.
func loadViewerMotion(
	ctx context.Context,
	tools interface {
		LoadCharacterMotion(context.Context, string, string) (*modelviewer.CharacterMotion, error)
	},
	read viewerReader,
	scene *viewerScene,
	bases map[int]viewerMotionBase,
	game, id string,
) (*viewerMotion, error) {
	motion, err := tools.LoadCharacterMotion(ctx, game, id)
	if err != nil {
		return nil, err
	}
	if len(motion.Clips) == 0 {
		return nil, fmt.Errorf("character motion %s/%s has no clips", game, id)
	}

	m := &viewerMotion{scene: scene, motion: motion, pose: motion.NewPose(), clipIndexes: map[string]int{}}
	palettes := map[string][]*viewerMotionMesh{}
	for index, mesh := range scene.meshes {
		info := mesh.info
		if info.BlendURL == "" || info.BlendPalette == "" {
			continue
		}
		data, err := read(ctx, info.BlendURL)
		if err != nil {
			return nil, fmt.Errorf("mesh %s bone weights: %w", info.ID, err)
		}
		base, ok := bases[index]
		if !ok {
			decoded, err := readViewerMesh(ctx, read, info, mesh.averagedOutline)
			if err != nil {
				return nil, fmt.Errorf("mesh %s: %w", info.ID, err)
			}
			base = viewerMotionBase{positions: decoded.positions, normals: decoded.normals, extra: decoded.extra}
		}
		vertices := len(base.positions) / 3
		if len(data) != vertices*16 {
			return nil, fmt.Errorf("mesh %s bone weights do not match its %d vertices", info.ID, vertices)
		}
		blend := make([]uint16, len(data)/2)
		for i := range blend {
			blend[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		item := &viewerMotionMesh{
			index: index, positions: base.positions, normals: base.normals, extra: base.extra, blend: blend,
		}
		m.meshes = append(m.meshes, item)
		palettes[info.BlendPalette] = append(palettes[info.BlendPalette], item)
	}
	if len(m.meshes) == 0 {
		return nil, errMotionNoBlend
	}
	for _, meshes := range palettes {
		var positions []float32
		var blend []uint16
		for _, mesh := range meshes {
			positions = append(positions, mesh.positions...)
			blend = append(blend, mesh.blend...)
		}
		palette := motion.MapPalette(positions, blend)
		for _, mesh := range meshes {
			mesh.palette = palette
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}

	for i, clip := range motion.Clips {
		frames := make([]modelviewer.ModelViewerAnimationFrame, clip.Frames)
		for f := range frames {
			frames[f] = modelviewer.ModelViewerAnimationFrame{
				Index: f,
				Time:  float64(f) / motion.FPS,
			}
		}
		id := motionClipPrefix + strconv.Itoa(i)
		m.clipIndexes[id] = i
		m.clips = append(m.clips, modelviewer.ModelViewerAnimationClip{
			ID: id, Label: clip.Name, FPS: motion.FPS,
			FrameStart: 0, FrameEnd: clip.Frames - 1, Loop: clip.Loop, LoopStart: clip.LoopStart, Frames: frames,
		})
	}
	return m, nil
}

// activeMotion is the loaded motion of the scene shown, or nil.
func (v *ModelViewer) activeMotion() *viewerMotion {
	if m := v.charMotion; m != nil && m.scene == v.scene {
		return m
	}
	return nil
}

// animationClips lists the mod's clips, then the character's motion clips.
func (v *ModelViewer) animationClips() []modelviewer.ModelViewerAnimationClip {
	if v.scene == nil {
		return nil
	}
	if m := v.activeMotion(); m != nil {
		return m.allClips
	}
	return v.scene.transport.Animations
}

// syncMotion gives the meshes back their own geometry when the clip shown is not a motion clip.
func (v *ModelViewer) syncMotion() {
	m := v.charMotion
	if m == nil {
		return
	}
	if m.scene != v.scene {
		v.stopMotion(false)
		return
	}
	if !isMotionClip(v.animationClip()) && m.shown {
		m.latest = nil
		v.restoreMotion(m)
		v.present()
	}
}

// requestMotion asks for frame index of clip, when it is a motion clip.
func (v *ModelViewer) requestMotion(clip *modelviewer.ModelViewerAnimationClip, index int) {
	m := v.activeMotion()
	if m == nil {
		return
	}
	number := m.clipIndex(clip)
	if number < 0 {
		return
	}
	m.latest = &viewerMotionRequest{clip: number, frame: index}
	v.dispatchMotion()
}

func (v *ModelViewer) dispatchMotion() {
	m := v.activeMotion()
	if m == nil || m.busy || m.latest == nil {
		return
	}
	request := *m.latest
	m.latest = nil
	m.busy = true
	spares := make([]viewerMotionResult, len(m.meshes))
	for i, mesh := range m.meshes {
		spares[i] = viewerMotionResult{current: mesh.spareFrame, extra: mesh.spareSkinned}
		mesh.spareFrame, mesh.spareSkinned = nil, nil
	}
	post := v.poster()
	go func() {
		results, err := skinViewerMotion(m, request, spares)
		post(func() {
			m.busy = false
			if v.charMotion != m {
				return
			}
			if err != nil {
				v.failMotion(err)
				return
			}
			// A clip changed while the frame was skinned leaves the meshes as they are, and a motion clip chosen
			// meanwhile has its frame waiting in latest.
			if selected := m.clipIndex(v.animationClip()); selected != request.clip {
				if selected >= 0 {
					for i, mesh := range m.meshes {
						mesh.spareFrame, mesh.spareSkinned = results[i].current, results[i].extra
					}
					v.dispatchMotion()
				}
				return
			}
			for i, mesh := range m.meshes {
				previous, previousSkinned := mesh.frame, mesh.skinned
				mesh.frame, mesh.skinned = results[i].current, results[i].extra
				v.showMotionMesh(m, mesh)
				// The draw copies a mesh's geometry as it presents, so only a GPU rebuild running on another
				// goroutine can still read the frame replaced.
				if !v.uploading {
					mesh.spareFrame, mesh.spareSkinned = previous, previousSkinned
				}
			}
			m.shown = true
			v.revision++
			v.dispatchMotion()
		})
	}()
}

// showMotionMesh puts the last skinned frame on a loaded mesh.
func (v *ModelViewer) showMotionMesh(m *viewerMotion, mesh *viewerMotionMesh) {
	target := m.scene.meshes[mesh.index]
	if target.unloaded || mesh.frame == nil || target.vertexCount*6 != len(mesh.frame) {
		return
	}
	target.current, target.geometryKey = mesh.frame, motionGeometryKey
	target.computed, target.dirty = true, true
	target.extra, target.extraDirty = mesh.skinned, true
}

// motionMeshLoaded shows the playing motion on mesh i, which has just loaded.
func (v *ModelViewer) motionMeshLoaded(i int) {
	m := v.activeMotion()
	if m == nil || !m.shown {
		return
	}
	for _, mesh := range m.meshes {
		if mesh.index == i {
			v.showMotionMesh(m, mesh)
		}
	}
}

type viewerMotionResult struct {
	current []float32
	extra   []byte
}

// skinViewerMotion skins every mesh of m at request with linear blend skinning, into the buffers of spares that
// fit.
func skinViewerMotion(
	m *viewerMotion,
	request viewerMotionRequest,
	spares []viewerMotionResult,
) ([]viewerMotionResult, error) {
	matrices := make([][12]float32, m.motion.NodeCount())
	if err := m.pose.Evaluate(request.clip, request.frame, matrices); err != nil {
		return nil, err
	}
	results := make([]viewerMotionResult, len(m.meshes))
	work := make(chan int)
	var group sync.WaitGroup
	for range min(len(m.meshes), runtime.GOMAXPROCS(0)) {
		group.Go(func() {
			for i := range work {
				results[i] = skinViewerMotionMesh(m.meshes[i], matrices, spares[i])
			}
		})
	}
	for i := range m.meshes {
		work <- i
	}
	close(work)
	group.Wait()
	return results, nil
}

func skinViewerMotionMesh(
	mesh *viewerMotionMesh,
	matrices [][12]float32,
	into viewerMotionResult,
) viewerMotionResult {
	vertices := len(mesh.positions) / 3
	current := into.current
	if len(current) != vertices*6 {
		current = make([]float32, vertices*6)
	}
	extra := into.extra
	if extra != nil && len(extra) == len(mesh.extra) {
		copy(extra, mesh.extra)
	} else {
		extra = slices.Clone(mesh.extra)
	}
	for vertex := range vertices {
		var blended [12]float32
		total := float32(0)
		for k := range 4 {
			weight := float32(mesh.blend[vertex*8+4+k]) / 65535
			if weight == 0 {
				continue
			}
			node := int32(-1)
			if group := int(mesh.blend[vertex*8+k]); group < len(mesh.palette) {
				node = mesh.palette[group]
			}
			matrix := &viewerMotionIdentity
			if node >= 0 && int(node) < len(matrices) {
				matrix = &matrices[node]
			}
			for i, value := range matrix {
				blended[i] += weight * value
			}
			total += weight
		}
		if total == 0 {
			blended = viewerMotionIdentity
		} else if total != 1 {
			for i := range blended {
				blended[i] /= total
			}
		}

		p := mesh.positions[vertex*3 : vertex*3+3]
		out := current[vertex*6 : vertex*6+6]
		for row := range 3 {
			out[row] = blended[row*4]*p[0] + blended[row*4+1]*p[1] + blended[row*4+2]*p[2] + blended[row*4+3]
		}
		if vertex*3+2 < len(mesh.normals) {
			normal := rotateMotion(&blended, mesh.normals[vertex*3:vertex*3+3])
			copy(out[3:6], normalizeMotion(normal))
		} else {
			clear(out[3:6])
		}
		if at := vertex * viewerVertexExtraStride; at+viewerVertexExtraStride <= len(extra) {
			for _, offset := range [2]int{0, 16} {
				var direction [3]float32
				for axis := range 3 {
					direction[axis] = math.Float32frombits(binary.LittleEndian.Uint32(extra[at+offset+axis*4:]))
				}
				rotated := rotateMotion(&blended, direction[:])
				for axis, value := range rotated {
					binary.LittleEndian.PutUint32(extra[at+offset+axis*4:], math.Float32bits(value))
				}
			}
		}
	}
	return viewerMotionResult{current: current, extra: extra}
}

var viewerMotionIdentity = [12]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}

func rotateMotion(m *[12]float32, v []float32) [3]float32 {
	return [3]float32{
		m[0]*v[0] + m[1]*v[1] + m[2]*v[2],
		m[4]*v[0] + m[5]*v[1] + m[6]*v[2],
		m[8]*v[0] + m[9]*v[1] + m[10]*v[2],
	}
}

func normalizeMotion(v [3]float32) []float32 {
	length := float32(math.Sqrt(float64(v[0]*v[0] + v[1]*v[1] + v[2]*v[2])))
	if length == 0 {
		return v[:]
	}
	return []float32{v[0] / length, v[1] / length, v[2] / length}
}

// restoreMotion gives the motion's meshes back the geometry their toggle state selects, once the scene is
// presented again.
func (v *ModelViewer) restoreMotion(m *viewerMotion) {
	for _, mesh := range m.meshes {
		mesh.spareFrame, mesh.spareSkinned = nil, nil
	}
	if !m.shown {
		return
	}
	m.shown = false
	for _, mesh := range m.meshes {
		mesh.frame, mesh.skinned = nil, nil
		target := m.scene.meshes[mesh.index]
		if target.unloaded || target.geometryKey != motionGeometryKey {
			continue
		}
		target.computed = false
		if target.cpuDropped {
			// A mesh without its arrays cannot rebuild them, so it takes the motion's copy of its base.
			target.current, target.geometryKey = interleaveViewerGeometry(mesh.positions, mesh.normals), baseGeometryKey
			target.extra, target.dirty, target.extraDirty = mesh.extra, true, true
			continue
		}
		target.geometryKey = ""
		target.extra, target.extraDirty = target.baseExtra, true
	}
	v.revision++
}

// stopMotion ends the motion and its loading. restore shows the meshes as their toggle state selects.
func (v *ModelViewer) stopMotion(restore bool) {
	if v.motionCancel != nil {
		v.motionCancel()
		v.motionCancel = nil
		v.revision++
	}
	m := v.charMotion
	if m == nil {
		return
	}
	v.charMotion = nil
	m.cancel()
	if restore && m.scene == v.scene {
		v.restoreMotion(m)
		v.present()
	}
}

// dropMotion ends the motion and, when one of its clips was playing, plays the scene's own clips again.
func (v *ModelViewer) dropMotion() {
	playing := isMotionClip(v.animationClip())
	v.stopMotion(true)
	if playing {
		v.startAnimation("")
	}
}

// failMotion reports a frame that could not be skinned and ends the motion.
func (v *ModelViewer) failMotion(err error) {
	v.dropMotion()
	if c := v.character; c != nil {
		v.reportMotionError(err, c)
	}
}
