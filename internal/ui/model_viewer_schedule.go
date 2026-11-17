package ui

import (
	"context"
	"maps"
	"math"
	"reflect"
	"slices"
	"sort"
	"sync/atomic"

	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

// viewerScheduleItem is a mesh, or with variant >= 0 one of its position variants, that a frame draws.
type viewerScheduleItem struct {
	mesh, variant int
}

// viewerUnused is the distance to an item no frame of the clip draws.
const viewerUnused = math.MaxInt

// viewerSchedule lists what each frame of an animation clip draws, so the scene can load the frames ahead in order
// and keep the meshes and variants it draws soonest. A clip that steps through more frames than the caches hold
// would otherwise evict, least recently drawn first, exactly the frame it plays next.
type viewerSchedule struct {
	frames [][]viewerScheduleItem
	// uses lists the frames that draw each item, ascending.
	uses map[viewerScheduleItem][]int
	loop bool
}

// buildViewerSchedule evaluates every frame of the clip over state, or returns nil once ctx is cancelled. It only
// reads the transport, so it runs off the UI thread.
func buildViewerSchedule(
	ctx context.Context,
	transport modelviewer.ModelViewerTransport,
	state map[string]any,
	form string,
	frames []modelviewer.ModelViewerAnimationFrame,
	loop bool,
) *viewerSchedule {
	schedule := &viewerSchedule{
		frames: make([][]viewerScheduleItem, len(frames)),
		uses:   map[viewerScheduleItem][]int{},
		loop:   loop,
	}
	for index, frame := range frames {
		if ctx.Err() != nil {
			return nil
		}
		frameState := maps.Clone(state)
		if frameState == nil {
			frameState = map[string]any{}
		}
		maps.Copy(frameState, frame.Values)
		_, states := modelviewer.EvaluateViewerState(transport, frameState, form)
		schedule.frames[index] = viewerStateItems(states)
		for _, item := range schedule.frames[index] {
			schedule.uses[item] = append(schedule.uses[item], index)
		}
	}
	return schedule
}

// viewerStateItems lists the meshes the states draw, each followed by its position variant.
func viewerStateItems(states []modelviewer.ViewerMeshState) []viewerScheduleItem {
	var items []viewerScheduleItem
	for i, state := range states {
		if !state.Visible {
			continue
		}
		items = append(items, viewerScheduleItem{mesh: i, variant: -1})
		if state.PositionVariant >= 0 {
			items = append(items, viewerScheduleItem{mesh: i, variant: state.PositionVariant})
		}
	}
	return items
}

// distance is how many frames after current the clip next draws item, or viewerUnused.
func (s *viewerSchedule) distance(item viewerScheduleItem, current int) int {
	uses := s.uses[item]
	if len(uses) == 0 {
		return viewerUnused
	}
	at, _ := slices.BinarySearch(uses, current)
	if at < len(uses) {
		return uses[at] - current
	}
	if s.loop {
		return uses[0] + len(s.frames) - current
	}
	return viewerUnused
}

// viewerScheduleKey is what a schedule was built for.
type viewerScheduleKey struct {
	clip  string
	form  string
	state map[string]any
}

func (k viewerScheduleKey) equal(other viewerScheduleKey) bool {
	return k.clip == other.clip && k.form == other.form && reflect.DeepEqual(k.state, other.state)
}

// syncSchedule builds the schedule of the clip being played under the shown toggle state, unless it is built
// already. A clip with shape waves has none: a wave's phase depends on the frames before it.
func (v *ModelViewer) syncSchedule() {
	scene := v.scene
	if scene == nil {
		return
	}
	clip := v.animationClip()
	if clip == nil || len(clip.Frames) < 2 || v.anim.waves != nil {
		v.cancelSchedule()
		v.anim.scheduleKey, scene.schedule = viewerScheduleKey{}, nil
		v.anim.scheduleRevision++
		return
	}
	key := viewerScheduleKey{clip: clip.ID, form: v.shownForm(), state: maps.Clone(v.resolved)}
	if key.equal(v.anim.scheduleKey) {
		return
	}
	v.cancelSchedule()
	v.anim.scheduleKey = key
	v.anim.scheduleRevision++
	revision := v.anim.scheduleRevision
	ctx, cancel := context.WithCancel(v.ctx)
	v.anim.scheduleCancel = cancel
	transport, frames, loop := scene.transport, clip.Frames, clip.Loop
	post := v.poster()
	go func() {
		defer cancel()
		schedule := buildViewerSchedule(ctx, transport, key.state, key.form, frames, loop)
		if schedule == nil {
			return
		}
		post(func() {
			if v.scene != scene || v.anim.scheduleRevision != revision {
				return
			}
			v.anim.scheduleCancel = nil
			scene.schedule = schedule
			v.refreshLoads()
		})
	}()
}

// cancelSchedule stops the schedule build in progress, if any.
func (v *ModelViewer) cancelSchedule() {
	if v.anim.scheduleCancel != nil {
		v.anim.scheduleCancel()
		v.anim.scheduleCancel = nil
	}
}

// viewerDemand marks the meshes and variants the viewer still wants loaded: the frame shown and the frames it loads
// ahead. A load waiting for a free reader checks its mark, so the frames playback skipped past are not read.
type viewerDemand struct {
	epoch    atomic.Uint64
	meshes   []atomic.Uint64
	variants [][]atomic.Uint64
}

func newViewerDemand(meshes []*viewerMesh) *viewerDemand {
	d := &viewerDemand{meshes: make([]atomic.Uint64, len(meshes)), variants: make([][]atomic.Uint64, len(meshes))}
	for i, mesh := range meshes {
		d.variants[i] = make([]atomic.Uint64, len(mesh.info.PositionVariants))
	}
	return d
}

func (d *viewerDemand) mark(item viewerScheduleItem) *atomic.Uint64 {
	if item.variant < 0 {
		return &d.meshes[item.mesh]
	}
	return &d.variants[item.mesh][item.variant]
}

// set makes items the wanted ones. The new marks are stored before the epoch moves on, so an item wanted both
// before and after never reads as unwanted.
func (d *viewerDemand) set(items []viewerScheduleItem) {
	next := d.epoch.Load() + 1
	for _, item := range items {
		d.mark(item).Store(next)
	}
	d.epoch.Store(next)
}

// wanted is safe to call from any goroutine.
func (d *viewerDemand) wanted(item viewerScheduleItem) bool {
	return d.mark(item).Load() >= d.epoch.Load()
}

// viewerPrefetchInFlight bounds the loads of frames ahead running or waiting at once, so the frame shown never
// queues behind a long list of later ones.
const viewerPrefetchInFlight = 2 * viewerLoadConcurrency

// refreshLoads asks for what the wanted states draw and, along the clip's schedule, for the frames ahead that fit
// the caches without dropping anything drawn sooner.
func (v *ModelViewer) refreshLoads() {
	scene := v.scene
	if scene == nil {
		return
	}
	if scene.demand == nil {
		scene.demand = newViewerDemand(scene.meshes)
	}
	items := viewerStateItems(scene.wanted)
	ahead := v.aheadItems()
	scene.demand.set(append(slices.Clone(items), ahead...))
	for _, item := range items {
		v.requestItem(item)
	}
	for _, item := range ahead {
		v.requestItem(item)
	}
}

// retryWanted asks again for an item whose load was skipped, if it is wanted now.
func (v *ModelViewer) retryWanted(item viewerScheduleItem) {
	if v.scene.demand != nil && v.scene.demand.wanted(item) {
		v.requestItem(item)
	}
}

func (v *ModelViewer) requestItem(item viewerScheduleItem) {
	mesh := v.scene.meshes[item.mesh]
	switch {
	case item.variant < 0:
		if mesh.unloaded {
			v.loadMesh(item.mesh)
		}
	case !mesh.unloaded && mesh.variants[item.variant] == nil &&
		mesh.geometryKey != variantGeometryKey(item.variant):
		v.loadVariant(item.mesh, item.variant)
	}
}

// aheadItems walks the frames after the one shown, nearest first, and lists the items to load ahead: while fewer
// than viewerPrefetchInFlight loads are running and each item fits its cache once the items drawn later than it
// are dropped.
func (v *ModelViewer) aheadItems() []viewerScheduleItem {
	scene := v.scene
	schedule := scene.schedule
	if schedule == nil {
		return v.upcomingItems()
	}
	if scene.resident == schedule && scene.residentAt == scene.evictions {
		return nil
	}
	inFlight := len(v.pendingMeshes) + len(v.pendingVariants)
	// A looping clip whose every frame is loaded, with nothing loading or waiting, needs no walk until something
	// is evicted.
	complete := schedule.loop && inFlight == 0 && !scene.waiting
	meshRoom := newViewerCacheRoom(scene, schedule, false, v)
	variantRoom := newViewerCacheRoom(scene, schedule, true, v)
	seen := map[viewerScheduleItem]bool{}
	var ahead []viewerScheduleItem
	count := len(schedule.frames)
	for step := 1; step < count && inFlight < viewerPrefetchInFlight; step++ {
		frame := scene.frame + step
		if frame >= count {
			if !schedule.loop {
				break
			}
			frame -= count
		}
		for _, item := range schedule.frames[frame] {
			if seen[item] {
				continue
			}
			seen[item] = true
			mesh := scene.meshes[item.mesh]
			if item.variant < 0 {
				if !mesh.unloaded || mesh.failed || v.pendingMeshes[item.mesh] {
					continue
				}
				if !meshRoom.take(mesh.gpuBytes(), step) {
					return ahead
				}
			} else {
				if mesh.variants[item.variant] != nil || mesh.unreadable[item.variant] ||
					v.pendingVariants[[2]int{item.mesh, item.variant}] {
					continue
				}
				// A variant of an unloaded mesh loads after its mesh, when the mesh's arrival refreshes the loads.
				if mesh.unloaded {
					complete = complete && mesh.failed
					continue
				}
				if !variantRoom.take(mesh.vertexCount*viewerVariantVertexBytes, step) {
					return ahead
				}
			}
			ahead = append(ahead, item)
			inFlight++
		}
	}
	if complete && len(ahead) == 0 {
		scene.resident, scene.residentAt = schedule, scene.evictions
	}
	return ahead
}

// upcomingItems are the items of the next two frames, for a clip whose schedule is not built yet.
func (v *ModelViewer) upcomingItems() []viewerScheduleItem {
	var items []viewerScheduleItem
	for _, states := range v.upcomingAnimationStates(v.shownForm()) {
		items = append(items, viewerStateItems(states)...)
	}
	return items
}

// viewerVariantVertexBytes is the size of a loaded position variant per vertex: its interleaved position and normal,
// and its extra data.
const viewerVariantVertexBytes = 6*4 + viewerVertexExtraStride

// viewerCacheRoom tracks how much a cache can still take while loading ahead: its free size, and the items it
// may drop, by how soon they are drawn.
type viewerCacheRoom struct {
	free int
	// dropped is how much of the droppable items the items taken so far need.
	dropped int
	// distances and sizes of the droppable items, nearest first; later holds the sizes from each index on.
	distances []int
	later     []int
}

func newViewerCacheRoom(scene *viewerScene, schedule *viewerSchedule, variants bool, v *ModelViewer) *viewerCacheRoom {
	type entry struct{ distance, size int }
	var entries []entry
	used, pending := 0, 0
	if variants {
		used = scene.variantBytes
		for ref := range v.pendingVariants {
			pending += scene.meshes[ref[0]].vertexCount * viewerVariantVertexBytes
		}
		for _, ref := range scene.variantUse {
			if scene.waitsFor(ref[0], ref[1]) || scene.meshes[ref[0]].geometryKey == variantGeometryKey(ref[1]) {
				continue
			}
			item := viewerScheduleItem{mesh: ref[0], variant: ref[1]}
			entries = append(
				entries,
				entry{schedule.distance(item, scene.frame), scene.meshes[ref[0]].variants[ref[1]].bytes()},
			)
		}
	} else {
		for i := range v.pendingMeshes {
			pending += scene.meshes[i].gpuBytes()
		}
		for _, ref := range scene.meshUse {
			mesh := scene.meshes[ref]
			size := mesh.gpuBytes()
			if scene.keepsMesh(ref) {
				continue
			}
			used += size
			entries = append(
				entries,
				entry{schedule.distance(viewerScheduleItem{mesh: ref, variant: -1}, scene.frame), size},
			)
		}
	}
	limit := scene.meshCacheBytes()
	if variants {
		limit = scene.variantCacheBytes()
	}
	sort.SliceStable(entries, func(a, b int) bool { return entries[a].distance < entries[b].distance })
	room := &viewerCacheRoom{
		free:      limit - used - pending,
		distances: make([]int, len(entries)),
		later:     make([]int, len(entries)+1),
	}
	for i := len(entries) - 1; i >= 0; i-- {
		room.distances[i] = entries[i].distance
		room.later[i] = room.later[i+1] + entries[i].size
	}
	return room
}

// take reserves size for an item drawn distance frames ahead, and reports whether it fits. Items are taken nearest
// first, so the items dropped for earlier ones are among those drawn later than this one.
func (r *viewerCacheRoom) take(size, distance int) bool {
	at := sort.SearchInts(r.distances, distance+1)
	if r.free+r.later[at]-r.dropped < size {
		return false
	}
	if size <= r.free {
		r.free -= size
	} else {
		r.dropped += size - max(r.free, 0)
		r.free = 0
	}
	return true
}
