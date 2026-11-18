package ui

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/myparsleycat/fairy"
	"github.com/myparsleycat/fairy/d3d"
	"github.com/myparsleycat/fairy/kit"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

var errMotionNoBlend = errors.New("no mesh has bone weights")

// viewerMotionNoNode is the node index of a bone weight that moves with no node of the skeleton.
const viewerMotionNoNode = 0xffff

// viewerMotion plays the game motions of the mod's character. The vertex shaders skin whatever geometry the mod's
// own animation shows with the meshes' bone weights and the pose of the motion's frame, as the game skins the mod's
// buffers. Everything belongs to the UI thread.
type viewerMotion struct {
	scene  *viewerScene
	motion *modelviewer.CharacterMotion
	clips  []modelviewer.ModelViewerAnimationClip
	play   viewerPlayback
	meshes []viewerMotionMesh
	cancel context.CancelFunc
	pose   *modelviewer.CharacterMotionPose
	// bones holds the pose shown, and posed numbers it for the GPU copy and the shadow map. posedAt is where in
	// the clips bones was evaluated.
	bones   [][12]float32
	posed   uint64
	posedAt viewerMotionPoseAt
	// boxes bounds the rest positions each node moves, the last one those that move with no node. clipBoxes bounds
	// them over every pose of each clip and clipBox over the clip playing, so the shadow volume stays still while it
	// plays; restBox bounds the meshes the motion does not pose. All are in the mod's space.
	boxes            []viewerMotionBox
	clipBoxes        []viewerMotionBox
	clipBox, restBox viewerMotionBox
	// palettes maps each blend palette's groups to skeleton nodes, as packMotionBlend takes them.
	palettes map[string][]int32
	// anchorNodes holds, by scene mesh, the node that turns the mesh's face direction, or -1.
	anchorNodes []int32
	// gpu is the motion's buffers on the device the scene draws on, made by the first draw, and gpuErr why they
	// could not be made or written, at gpuStage.
	gpu      *viewerSkinGPU
	gpuErr   error
	gpuStage string
}

// viewerMotionMesh is a mesh the motion poses: its bone weights as the shaders read them, four uint32 per vertex
// (see skinBlend in model_viewer.hlsl).
type viewerMotionMesh struct {
	index    int
	vertices int
	blend    []uint32
}

// viewerMotionBox is an axis-aligned box; ok is false while it holds no point.
type viewerMotionBox struct {
	minimum, maximum vec3
	ok               bool
}

func (b *viewerMotionBox) add(p vec3) {
	if !b.ok {
		b.minimum, b.maximum, b.ok = p, p, true
		return
	}
	for axis := range 3 {
		b.minimum[axis] = min(b.minimum[axis], p[axis])
		b.maximum[axis] = max(b.maximum[axis], p[axis])
	}
}

var viewerMotionIdentity = [12]float32{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0}

// motionNode is the skeleton node palette maps a bone weight's group to, or viewerMotionNoNode.
func motionNode(group uint16, palette []int32) uint32 {
	if int(group) < len(palette) && palette[group] >= 0 && palette[group] < viewerMotionNoNode {
		return uint32(palette[group])
	}
	return viewerMotionNoNode
}

// packMotionBlend lays out a mesh's bone weights, eight uint16 per vertex (four palette groups, then their four
// weights), for the shaders: each group becomes the skeleton node palette maps it to, or viewerMotionNoNode.
func packMotionBlend(blend []uint16, palette []int32) []uint32 {
	vertices := len(blend) / 8
	packed := make([]uint32, vertices*4)
	for vertex := range vertices {
		var nodes [4]uint32
		for k := range 4 {
			nodes[k] = motionNode(blend[vertex*8+k], palette)
		}
		weights := blend[vertex*8+4 : vertex*8+8]
		packed[vertex*4] = nodes[0] | nodes[1]<<16
		packed[vertex*4+1] = nodes[2] | nodes[3]<<16
		packed[vertex*4+2] = uint32(weights[0]) | uint32(weights[1])<<16
		packed[vertex*4+3] = uint32(weights[2]) | uint32(weights[3])<<16
	}
	return packed
}

// characterMotionAvailable reports whether game motions exist for the mod's character. A tool edits and picks the
// geometry the mod shows, so a viewer with one plays none.
func (v *ModelViewer) characterMotionAvailable() bool {
	c := v.character
	return c != nil && v.tools != nil && v.tool == nil && modelviewer.HasCharacterMotion(c.game, c.ID)
}

// toggleCharacterMotion loads the character's motions into their own lane of the playback controller, or removes
// them.
func (v *ModelViewer) toggleCharacterMotion() {
	if v.charMotion != nil || v.motionCancel != nil {
		v.stopMotion()
		return
	}
	scene, c := v.scene, v.character
	if scene == nil || c == nil || v.tools == nil {
		return
	}
	bases := map[int][]float32{}
	for i, mesh := range scene.meshes {
		if mesh.info.BlendURL != "" && !mesh.unloaded && !mesh.cpuDropped && mesh.positions != nil {
			bases[i] = mesh.positions
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
			v.charMotion = m
			v.startMotion(m.defaultClip())
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

// loadViewerMotion reads the character's motion and the bone weights of every mesh that has them, and maps each
// bone palette onto the skeleton by the meshes' base positions. bases holds the base positions of meshes already
// loaded.
func loadViewerMotion(
	ctx context.Context,
	tools interface {
		LoadCharacterMotion(context.Context, string, string) (*modelviewer.CharacterMotion, error)
	},
	read viewerReader,
	scene *viewerScene,
	bases map[int][]float32,
	game, id string,
) (*viewerMotion, error) {
	motion, err := tools.LoadCharacterMotion(ctx, game, id)
	if err != nil {
		return nil, err
	}
	if len(motion.Clips) == 0 {
		return nil, fmt.Errorf("character motion %s/%s has no clips", game, id)
	}

	type weighted struct {
		index     int
		positions []float32
		blend     []uint16
	}
	palettes := map[string][]weighted{}
	var order []string
	for index, mesh := range scene.meshes {
		info := mesh.info
		if info.BlendURL == "" || info.BlendPalette == "" {
			continue
		}
		data, err := read(ctx, info.BlendURL)
		if err != nil {
			return nil, fmt.Errorf("mesh %s bone weights: %w", info.ID, err)
		}
		positions, ok := bases[index]
		if !ok {
			decoded, err := readViewerMesh(ctx, read, info, mesh.averagedOutline)
			if err != nil {
				return nil, fmt.Errorf("mesh %s: %w", info.ID, err)
			}
			positions = decoded.positions
		}
		vertices := len(positions) / 3
		if len(data) != vertices*16 {
			return nil, fmt.Errorf("mesh %s bone weights do not match its %d vertices", info.ID, vertices)
		}
		blend := make([]uint16, len(data)/2)
		for i := range blend {
			blend[i] = binary.LittleEndian.Uint16(data[i*2:])
		}
		if _, ok := palettes[info.BlendPalette]; !ok {
			order = append(order, info.BlendPalette)
		}
		palettes[info.BlendPalette] = append(palettes[info.BlendPalette],
			weighted{index: index, positions: positions, blend: blend})
	}
	if len(order) == 0 {
		return nil, errMotionNoBlend
	}

	nodes := motion.NodeCount()
	m := &viewerMotion{
		scene: scene, motion: motion, pose: motion.NewPose(), bones: make([][12]float32, nodes),
		boxes: make([]viewerMotionBox, nodes+1), anchorNodes: make([]int32, len(scene.meshes)),
	}
	for i := range m.bones {
		m.bones[i] = viewerMotionIdentity
	}
	for i := range m.anchorNodes {
		m.anchorNodes[i] = -1
	}
	parents := make([]int32, nodes)
	for i := range parents {
		parents[i] = int32(motion.NodeParent(i))
	}
	sources := make([]modelviewer.MotionPalette, len(order))
	for i, name := range order {
		for _, mesh := range palettes[name] {
			sources[i].Positions = append(sources[i].Positions, mesh.positions...)
			sources[i].Blend = append(sources[i].Blend, mesh.blend...)
		}
	}
	mapped, err := motion.MapPalettes(ctx, sources)
	if err != nil {
		return nil, err
	}

	totals := make([]float64, nodes)
	m.palettes = make(map[string][]int32, len(order))
	for i, name := range order {
		palette := mapped[i]
		m.palettes[name] = palette
		for _, mesh := range palettes[name] {
			m.meshes = append(m.meshes, viewerMotionMesh{
				index: mesh.index, vertices: len(mesh.blend) / 8, blend: packMotionBlend(mesh.blend, palette),
			})
			clear(totals)
			addMotionBoxes(m.boxes, totals, mesh.positions, mesh.blend, palette)
			m.anchorNodes[mesh.index] = anchorNode(totals, parents)
		}
	}

	for i, mesh := range scene.transport.Meshes {
		posed := slices.ContainsFunc(m.meshes, func(posed viewerMotionMesh) bool { return posed.index == i })
		if !posed && mesh.Bounds != nil {
			m.restBox.add(mesh.Bounds.Min)
			m.restBox.add(mesh.Bounds.Max)
		}
	}

	pose, bones := motion.NewPose(), make([][12]float32, nodes)
	for i, clip := range motion.Clips {
		box, err := clipBox(pose, m.boxes, bones, i, clip.Frames)
		if err != nil {
			return nil, fmt.Errorf("clip %q: %w", clip.Name, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		m.clipBoxes = append(m.clipBoxes, box)

		frames := make([]modelviewer.ModelViewerAnimationFrame, clip.Frames)
		for f := range frames {
			frames[f] = modelviewer.ModelViewerAnimationFrame{
				Index: f,
				Time:  float64(f) / motion.FPS,
			}
		}
		m.clips = append(m.clips, modelviewer.ModelViewerAnimationClip{
			ID: strconv.Itoa(i), Label: clip.Name, FPS: motion.FPS,
			FrameStart: 0, FrameEnd: clip.Frames - 1, Loop: clip.Loop, LoopStart: clip.LoopStart, Frames: frames,
		})
	}
	return m, nil
}

// addMotionBoxes adds each vertex of a mesh to the box of every node that moves it and sums the weights of each node
// into totals.
func addMotionBoxes(boxes []viewerMotionBox, totals []float64, positions []float32, blend []uint16, palette []int32) {
	for vertex := range min(len(positions)/3, len(blend)/8) {
		p := vec3{float64(positions[vertex*3]), float64(positions[vertex*3+1]), float64(positions[vertex*3+2])}
		for k := range 4 {
			weight := blend[vertex*8+4+k]
			if weight == 0 {
				continue
			}
			node := motionNode(blend[vertex*8+k], palette)
			if node == viewerMotionNoNode || int(node) >= len(totals) {
				boxes[len(boxes)-1].add(p)
				continue
			}
			boxes[node].add(p)
			totals[node] += float64(weight)
		}
	}
}

// anchorNode is the deepest node whose subtree carries at least half of a mesh's bone weight, by the weight totals
// of each node and its parent (-1 for a root), the larger subtree breaking a tie, or -1 for a mesh with no weight. A
// face weighted to the neck, head, jaw and eyes anchors on the head.
func anchorNode(totals []float64, parents []int32) int32 {
	depths := make([]int, len(totals))
	order := make([]int, len(totals))
	var all float64
	for i, total := range totals {
		all += total
		order[i] = i
		for p := parents[i]; p >= 0 && depths[i] <= len(totals); p = parents[p] {
			depths[i]++
		}
	}
	if all <= 0 {
		return -1
	}

	slices.SortFunc(order, func(a, b int) int { return depths[b] - depths[a] })
	subtree := slices.Clone(totals)
	for _, i := range order {
		if p := parents[i]; p >= 0 {
			subtree[p] += subtree[i]
		}
	}
	anchor := int32(-1)
	for _, i := range order {
		if subtree[i]*2 < all {
			continue
		}
		if anchor < 0 || depths[i] > depths[anchor] || depths[i] == depths[anchor] && subtree[i] > subtree[anchor] {
			anchor = int32(i)
		}
	}
	return anchor
}

// motionBounds bounds the boxes as bones moves them, boxes[i] by bones[i] and the last box as it is. A vertex skins
// to a weighted mean of its nodes' moves, so it lies within the box around all the moved boxes this returns.
func motionBounds(boxes []viewerMotionBox, bones [][12]float32) (minimum, maximum vec3, ok bool) {
	var all viewerMotionBox
	for i, box := range boxes {
		if !box.ok {
			continue
		}
		bone := &viewerMotionIdentity
		if i < len(bones) {
			bone = &bones[i]
		}
		for corner := range 8 {
			p := box.minimum
			for axis := range 3 {
				if corner&(1<<axis) != 0 {
					p[axis] = box.maximum[axis]
				}
			}
			var moved vec3
			for row := range 3 {
				moved[row] = float64(bone[row*4])*p[0] + float64(bone[row*4+1])*p[1] + float64(bone[row*4+2])*p[2] +
					float64(bone[row*4+3])
			}
			all.add(moved)
		}
	}
	return all.minimum, all.maximum, all.ok
}

// anchor is the node that turns the face direction of scene mesh i, or -1.
func (m *viewerMotion) anchor(i int) int32 {
	if m == nil || i < 0 || i >= len(m.anchorNodes) {
		return -1
	}
	return m.anchorNodes[i]
}

// turnBy turns forward, a direction in the mod's space, by node, or leaves it for -1.
func (m *viewerMotion) turnBy(node int32, forward vec3) vec3 {
	if node < 0 || int(node) >= len(m.bones) {
		return forward
	}
	bone := &m.bones[node]
	var turned vec3
	for row := range 3 {
		turned[row] = float64(bone[row*4])*forward[0] + float64(bone[row*4+1])*forward[1] +
			float64(bone[row*4+2])*forward[2]
	}
	if turned.dot(turned) == 0 {
		return forward
	}
	return turned.normalize()
}

// activeMotion is the loaded motion of the scene shown, or nil.
func (v *ModelViewer) activeMotion() *viewerMotion {
	if m := v.charMotion; m != nil && m.scene == v.scene {
		return m
	}
	return nil
}

// startMotion selects a motion clip and plays it from its first frame when it has more than one.
func (v *ModelViewer) startMotion(clipID string) {
	m := v.activeMotion()
	if m == nil {
		return
	}
	m.play.clipID = clipID
	m.play.frame, m.play.resume = 0, nil
	clip := v.trackClip(trackMotion)
	m.play.playing = clip != nil && len(clip.Frames) > 1
	m.clipBox = viewerMotionBox{}
	if clip != nil {
		m.clipBox = m.clipBoxes[m.clipNumber(clip.ID)]
	}
	v.runTrackClock(trackMotion)
	v.setMotionFrame(0)
}

// clipBox bounds boxes over every pose of the motion's clip with frames frames, evaluated with pose into bones, or
// over the rest pose for a clip with no frames.
func clipBox(
	pose *modelviewer.CharacterMotionPose,
	boxes []viewerMotionBox,
	bones [][12]float32,
	clip, frames int,
) (viewerMotionBox, error) {
	for i := range bones {
		bones[i] = viewerMotionIdentity
	}

	var box viewerMotionBox
	for frame := range max(frames, 1) {
		if frames > 0 {
			if err := pose.Evaluate(clip, frame, bones); err != nil {
				return box, fmt.Errorf("frame %d: %w", frame, err)
			}
		}
		if minimum, maximum, ok := motionBounds(boxes, bones); ok {
			box.add(minimum)
			box.add(maximum)
		}
	}
	return box, nil
}

// clipNumber is the index in the motion of the clip with id.
func (m *viewerMotion) clipNumber(id string) int {
	return max(slices.IndexFunc(m.clips, func(c modelviewer.ModelViewerAnimationClip) bool { return c.ID == id }), 0)
}

// viewerMotionPoseAt is a pose a share t of the way from frame to next of clip.
type viewerMotionPoseAt struct {
	clip, frame, next int
	t                 float64
}

// setMotionFrame poses the skeleton at frame index of the motion's clip, or past it by the share of the frame
// already played.
func (v *ModelViewer) setMotionFrame(index int) {
	m, clip := v.activeMotion(), v.trackClip(trackMotion)
	if m == nil || clip == nil || len(clip.Frames) == 0 {
		return
	}
	m.play.frame = min(max(index, 0), len(clip.Frames)-1)
	if err := v.poseMotion(v.interpolation(trackMotion, time.Now())); err != nil {
		v.failMotionPlay(err, "evaluate-pose")
	}
}

// poseMotion poses the skeleton a share t of the way from the frame shown to the next, unless it is posed there.
func (v *ModelViewer) poseMotion(t float64) error {
	m, clip := v.activeMotion(), v.trackClip(trackMotion)
	if m == nil || clip == nil || len(clip.Frames) == 0 {
		return nil
	}
	at := viewerMotionPoseAt{clip: m.clipNumber(clip.ID), frame: m.play.frame, next: m.play.frame}
	if next, ok := v.nextFrame(trackMotion); ok && t > 0 {
		at.next, at.t = next, t
	}
	if m.posed != 0 && at == m.posedAt {
		return nil
	}
	if err := m.pose.EvaluateBetween(at.clip, at.frame, at.next, at.t, m.bones); err != nil {
		return err
	}
	m.posedAt = at
	v.revision++
	m.posed = v.revision
	return nil
}

// stopMotion ends the motion and its loading, and frees the motion's buffers.
func (v *ModelViewer) stopMotion() {
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
	if m.gpu != nil {
		_ = fairy.UseDevice(func(fairy.GPU) error {
			m.gpu.release()
			return nil
		})
		m.gpu = nil
	}
	v.revision++
}

// failMotionPlay reports a motion that could not be posed or drawn at stage and ends it.
func (v *ModelViewer) failMotionPlay(err error, stage string) {
	fields := map[string]any{"modPath": v.path}
	if c := v.character; c != nil {
		fields["game"], fields["character"] = c.game, c.ID
	}
	if m := v.activeMotion(); m != nil {
		fields["frame"], fields["meshes"], fields["nodes"] = m.play.frame, len(m.meshes), len(m.bones)
		if clip := v.trackClip(trackMotion); clip != nil {
			fields["clip"] = clip.Label
		}
	}
	v.stopMotion()
	err = infra.ReportError(v.log, err, "ModelViewer.characterMotion", infra.Diagnostic{
		Operation: "play-character-motion", Stage: stage, Fields: fields,
	})
	v.toasts.Show(kit.ToastError, T("page.tools.model_viewer.toast.motion_play_error"), err.Error())
}

// viewerSkinGPU is a motion's buffers on one device generation: the bone weights of each mesh it poses, by scene
// mesh index, and the pose.
type viewerSkinGPU struct {
	generation uint64
	blends     []viewerSkinBlend
	bones      d3d.Buffer
	bonesView  d3d.ShaderResourceView
	// posed is the pose bones holds.
	posed uint64
}

type viewerSkinBlend struct {
	buffer   d3d.Buffer
	view     d3d.ShaderResourceView
	vertices int
}

func (g *viewerSkinGPU) release() {
	for i := range g.blends {
		g.blends[i].view.Release()
		g.blends[i].buffer.Release()
	}
	g.bonesView.Release()
	g.bones.Release()
	*g = viewerSkinGPU{}
}

// viewerSkinBoneStride is the size of a SkinBone in model_viewer.hlsl.
const viewerSkinBoneStride = 48

// structuredView makes a shader resource view over all count elements of a structured buffer.
func structuredView(dev d3d.Device, buffer d3d.Buffer, count int) (d3d.ShaderResourceView, error) {
	return dev.CreateShaderResourceView(buffer, &d3d.SRVDesc{
		Format: d3d.FORMAT_UNKNOWN, ViewDimension: d3d.SRV_DIMENSION_BUFFER, U: [4]uint32{0, uint32(count)},
	})
}

// skinGPU returns the motion's buffers on dev of generation, with the pose shown written, making them on the first
// draw of a generation. When they cannot be made or written it returns nil and records why in gpuErr and gpuStage.
func (m *viewerMotion) skinGPU(ctx d3d.Context, dev d3d.Device, generation uint64) *viewerSkinGPU {
	if m.gpu != nil && m.gpu.generation != generation {
		m.gpu.release()
		m.gpu = nil
	}
	made := m.gpu == nil
	if made {
		g := &viewerSkinGPU{generation: generation, blends: make([]viewerSkinBlend, len(m.scene.meshes))}
		fail := func(err error) *viewerSkinGPU {
			g.release()
			m.gpuErr, m.gpuStage = fmt.Errorf("create character motion buffers: %w", err), "create-buffers"
			return nil
		}
		var err error
		for _, mesh := range m.meshes {
			if mesh.vertices == 0 {
				continue
			}
			blend := &g.blends[mesh.index]
			if blend.buffer, err = dev.CreateBuffer(&d3d.BufferDesc{
				ByteWidth: uint32(len(mesh.blend) * 4), Usage: d3d.USAGE_IMMUTABLE, BindFlags: d3d.BIND_SHADER_RESOURCE,
				MiscFlags: d3d.RESOURCE_MISC_BUFFER_STRUCTURED, StructureByteStride: 16,
			}, &d3d.SubresourceData{SysMem: unsafe.Pointer(&mesh.blend[0])}); err != nil {
				return fail(err)
			}
			if blend.view, err = structuredView(dev, blend.buffer, mesh.vertices); err != nil {
				return fail(err)
			}
			blend.vertices = mesh.vertices
		}
		if g.bones, err = dev.CreateBuffer(&d3d.BufferDesc{
			ByteWidth: uint32(len(m.bones) * viewerSkinBoneStride), Usage: d3d.USAGE_DYNAMIC,
			BindFlags: d3d.BIND_SHADER_RESOURCE, CPUAccessFlags: d3d.CPU_ACCESS_WRITE,
			MiscFlags: d3d.RESOURCE_MISC_BUFFER_STRUCTURED, StructureByteStride: viewerSkinBoneStride,
		}, nil); err != nil {
			return fail(err)
		}
		if g.bonesView, err = structuredView(dev, g.bones, len(m.bones)); err != nil {
			return fail(err)
		}
		m.gpu = g
	}
	if made || m.gpu.posed != m.posed {
		if err := d3d.WriteData(ctx, m.gpu.bones, m.bones); err != nil {
			m.gpuErr, m.gpuStage = fmt.Errorf("write character motion pose: %w", err), "write-pose"
			return nil
		}
		m.gpu.posed = m.posed
	}
	return m.gpu
}

// blend returns the bone weights of scene mesh i when the motion poses it and they still fit its vertices.
func (g *viewerSkinGPU) blend(i int, vertices int) (d3d.ShaderResourceView, bool) {
	if g == nil || i >= len(g.blends) || g.blends[i].view.P == nil || g.blends[i].vertices != vertices {
		return d3d.ShaderResourceView{}, false
	}
	return g.blends[i].view, true
}
