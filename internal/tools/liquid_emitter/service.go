package liquidemitter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/platform"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const (
	stageResolve  = "resolve"
	stageRuntime  = "runtime"
	stageGeometry = "geometry"
	stageLoad     = "load"
	stagePatch    = "patch-ini"
	stageWrite    = "write"
	stageFork     = "fork-copy"
)

type ModToggler interface {
	Enable(context.Context, string) (string, error)
	Disable(context.Context, string) (string, error)
}

type DrawSources interface {
	DrawSource(sessionID, meshID string) (modelviewer.DrawSource, bool)
}

type Options struct {
	Log       *infra.Log
	GamePaths modruntime.GamePaths
	Mod       ModToggler
	Viewer    DrawSources
	FS        *platform.FS
	// Runtimes installs the emitter runtime; nil manages it on its own.
	Runtimes *modruntime.Manager
}

type Service struct {
	log      *infra.Log
	games    modruntime.GamePaths
	mod      ModToggler
	viewer   DrawSources
	fs       *platform.FS
	runtimes *modruntime.Manager
	mu       sync.Mutex
	// writeFile is replaced in tests to fail a write part way through a commit.
	writeFile func(path string, data []byte) error
}

func NewWithOptions(opts Options) *Service {
	service := &Service{
		log:       opts.Log,
		games:     opts.GamePaths,
		mod:       opts.Mod,
		viewer:    opts.Viewer,
		fs:        opts.FS,
		runtimes:  opts.Runtimes,
		writeFile: writeAtomic,
	}
	if service.fs == nil {
		service.fs = platform.NewFS()
	}
	if service.runtimes == nil {
		service.runtimes = modruntime.New(modruntime.Options{
			Log: opts.Log, Mod: opts.Mod, Specs: RuntimeSpecs(),
		})
	}
	return service
}

//wails:ignore
func (s *Service) UseClient(client *db.Client) {
	if client != nil {
		s.games = client.GamePaths
	}
}

type scope struct {
	root, modsRoot string
	// importer names the game whose Mods folder holds root.
	importer string
	game     game
	stage    string
	fields   map[string]any
	// origin is set while applying to a folder just copied from another mod.
	origin *modMarker
	// renumbered and applied are what Inspection.Renumbered and
	// Inspection.Applied report for this call.
	renumbered map[int]int
	applied    *int
}

func (s *Service) Inspect(ctx context.Context, modPath string) (Inspection, error) {
	return s.run(ctx, "inspect", modPath, func(sc *scope) (Inspection, error) {
		return s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
			return s.inspection(sc, rt)
		})
	})
}

func (s *Service) InstallRuntime(ctx context.Context, modPath string) (Inspection, error) {
	return s.run(ctx, "install-runtime", modPath, func(sc *scope) (Inspection, error) {
		return s.withRuntime(ctx, sc, true, func(rt runtimeState) (Inspection, error) {
			return s.inspection(sc, rt)
		})
	})
}

func (s *Service) Apply(ctx context.Context, input ApplyInput) (Inspection, error) {
	return s.run(ctx, "apply", input.ModPath, func(sc *scope) (Inspection, error) {
		if input.Look != nil {
			if err := checkLook(input.Look); err != nil {
				return Inspection{}, err
			}
			sc.fields["look"] = *input.Look
		}
		sc.stage = stageGeometry
		sc.fields["meshId"], sc.fields["faceIndex"] = input.MeshID, input.FaceIndex
		if input.PID != nil {
			sc.fields["targetPid"] = *input.PID
		}
		source, triangle, anchor, err := s.resolveAnchor(sc, input)
		if err != nil {
			return Inspection{}, err
		}
		sc.fields["ini"], sc.fields["section"] = source.INIPath, source.Section

		switch input.Mode {
		case "", ApplyInPlace:
		case ApplyNewMod:
			return s.applyAsNewMod(ctx, sc, source, triangle, anchor, input.PID, input.Look)
		default:
			return Inspection{}, infra.ContractError(ErrorInvalidMode + ":" + input.Mode)
		}
		return s.withRuntime(ctx, sc, true, func(rt runtimeState) (Inspection, error) {
			if err := s.apply(sc, rt.layout(), source, triangle, anchor, input.PID, input.Look); err != nil {
				return Inspection{}, err
			}
			return s.inspection(sc, rt)
		})
	})
}

func (s *Service) Remove(ctx context.Context, input RemoveInput) (Inspection, error) {
	return s.run(ctx, "remove", input.ModPath, func(sc *scope) (Inspection, error) {
		return s.withRuntime(ctx, sc, false, func(rt runtimeState) (Inspection, error) {
			if err := s.remove(sc, rt.layout(), input.PIDs); err != nil {
				return Inspection{}, err
			}
			return s.inspection(sc, rt)
		})
	})
}

// SetLook stores the liquid look of one of the mod's emitters.
func (s *Service) SetLook(ctx context.Context, input LookInput) (Inspection, error) {
	return s.run(ctx, "set-look", input.ModPath, func(sc *scope) (Inspection, error) {
		if err := checkLook(&input.Look); err != nil {
			return Inspection{}, err
		}
		return s.withRuntime(ctx, sc, true, func(rt runtimeState) (Inspection, error) {
			if err := s.setLook(sc, rt.layout(), input.PID, input.Look); err != nil {
				return Inspection{}, err
			}
			return s.inspection(sc, rt)
		})
	})
}

// checkLook fills in the nozzle a caller from before nozzles left empty.
func checkLook(look *Look) error {
	if look.Nozzle == "" {
		look.Nozzle = NozzleSpray
	}
	if !validLook(*look) {
		return infra.ContractError(ErrorInvalidLook)
	}
	return nil
}

func (s *Service) run(
	ctx context.Context,
	operation, modPath string,
	action func(*scope) (Inspection, error),
) (Inspection, error) {
	sc := &scope{stage: stageResolve, fields: map[string]any{"modPath": modPath}}
	report := func(err error) error {
		if err == nil || infra.IsCancellationError(err) {
			return err
		}
		var contract infra.ContractError
		if errors.As(err, &contract) {
			return err
		}
		return infra.ReportError(s.log, err, diagnosticWhere, infra.Diagnostic{
			Operation: operationPrefix + operation,
			Stage:     sc.stage,
			Fields:    sc.fields,
		})
	}

	if s.games == nil {
		return Inspection{}, report(errors.New("liquid emitter is not bound to a database"))
	}
	location, err := modruntime.ResolveMod(ctx, s.games, modPath)
	if err != nil {
		return Inspection{}, report(err)
	}
	g, ok := gameFor(location.Importer)
	if !ok {
		return Inspection{}, infra.ContractError(ErrorUnsupportedGame + ":" + location.Root)
	}
	sc.root, sc.modsRoot, sc.importer, sc.game = location.Root, location.ModsRoot, location.Importer, g
	sc.fields["modsRoot"], sc.fields["importer"] = location.ModsRoot, location.Importer

	s.mu.Lock()
	defer s.mu.Unlock()
	inspection, err := action(sc)
	return inspection, report(err)
}

func (s *Service) resolveAnchor(
	sc *scope,
	input ApplyInput,
) (modelviewer.DrawSource, anchorTriangle, EmitterAnchor, error) {
	var none modelviewer.DrawSource
	source, ok := s.viewer.DrawSource(input.ViewerSessionID, input.MeshID)
	if !ok || !platform.SameOrChildPath(sc.root, source.INIPath) {
		return none, anchorTriangle{}, EmitterAnchor{}, infra.ContractError(ErrorViewerSession + ":" + input.MeshID)
	}
	indexOrder, err := source.TriangleVertices(input.FaceIndex)
	if err != nil {
		return none, anchorTriangle{}, EmitterAnchor{}, err
	}
	triangle, err := orderTriangle(input, indexOrder)
	if err != nil {
		return none, anchorTriangle{}, EmitterAnchor{}, err
	}
	point, okPoint := toVec3(input.Point)
	offset, okOffset := toVec3(input.Offset)
	direction, okDirection := toVec3(input.Direction)
	if !okPoint || !okOffset || !okDirection {
		return none, anchorTriangle{}, EmitterAnchor{}, infra.ContractError(ErrorInvalidAnchor + ":vector")
	}
	anchor := EmitterAnchor{
		MeshID:            input.MeshID,
		FaceIndex:         input.FaceIndex,
		FaceVertices:      slices.Clone(input.FaceVertices),
		TrianglePositions: slices.Clone(input.TrianglePositions),
		Barycentric:       slices.Clone(input.Barycentric),
		Point:             point[:],
		Offset:            offset[:],
		Direction:         direction[:],
	}
	return source, triangle, anchor, nil
}

type runtimeState struct {
	// root holds the profiles: the runtime folder, or its disabled spelling.
	root   string
	status modruntime.Status
}

// layout addresses the profiles below rt.root. A runtime folder that is gone
// has none to keep, and readIndex rebuilds the slots from the hooks once the
// runtime is installed; one that another tool's copy of the runtime occupies
// keeps that copy's tables. Either is read but never written, so a removal
// changes only the mod. A foreign copy elsewhere in Mods leaves rt.root this
// app's own, which stays writable.
func (rt runtimeState) layout() runtimeLayout {
	_, err := os.Stat(rt.root)
	occupied := rt.status.State == modruntime.StateForeign && samePhysicalPath(rt.status.Path, rt.root)
	return runtimeLayout{root: rt.root, readOnly: occupied || errors.Is(err, fs.ErrNotExist)}
}

// withRuntime runs action inside the runtime manager, which holds the disk
// slot of the Mods folder until it returns. With install, the runtime is
// enabled and brought up to date first.
func (s *Service) withRuntime(
	ctx context.Context,
	sc *scope,
	install bool,
	action func(runtimeState) (Inspection, error),
) (Inspection, error) {
	sc.stage = stageRuntime
	var inspection Inspection
	err := s.runtimes.Use(
		ctx,
		sc.importer,
		sc.modsRoot,
		runtimeName,
		install,
		func(root string, status modruntime.Status) error {
			sc.fields["runtime"] = status.State
			var err error
			inspection, err = action(runtimeState{root: root, status: status})
			return err
		},
	)
	return inspection, runtimeError(err)
}

func (s *Service) inspection(sc *scope, rt runtimeState) (Inspection, error) {
	sc.stage = stageLoad
	inspection := Inspection{
		ModPath:     sc.root,
		Importer:    sc.importer,
		Runtime:     rt.status,
		Emitters:    []Emitter{},
		MeshUnit:    sc.game.meshUnit,
		MeshDown:    sc.game.meshDown,
		Renumbered:  []PIDChange{},
		Applied:     sc.applied,
		DefaultLook: defaultLook(),
		MaxEmitters: maxEmitters,
	}
	for _, from := range slices.Sorted(maps.Keys(sc.renumbered)) {
		inspection.Renumbered = append(inspection.Renumbered, PIDChange{From: from, To: sc.renumbered[from]})
	}
	marker, err := readMarker(sc.root)
	if err != nil || marker.ModKey == "" {
		return inspection, err
	}
	key := marker.ModKey
	if generated, err := generatedHere(sc, marker); err != nil {
		return inspection, err
	} else if generated {
		inspection.SourcePath = marker.Source
	}
	layout := rt.layout()
	doc, err := readProfile(layout, key)
	if err != nil {
		return inspection, err
	}
	index, err := readIndex(layout, sc.modsRoot)
	if err != nil {
		return inspection, err
	}
	hooks := modHooks(sc.root)
	listed := make(map[int]bool)
	for _, emitter := range doc.Emitters {
		_, hooked := hooks[emitter.INIPath][emitter.PID]
		listed[emitter.PID] = true
		look := defaultLook()
		if emitter.Look != nil {
			look = *emitter.Look
		}
		inspection.Emitters = append(inspection.Emitters, Emitter{
			PID:     emitter.PID,
			INIPath: emitter.INIPath,
			Section: emitter.Section,
			Hooked:  hooked,
			Anchor:  emitter.Anchor,
			Look:    look,
		})
	}

	// Hooks whose profile was lost, with the runtime folder for example, have
	// no anchor to edit but are listed so they can be removed.
	var orphans []Emitter
	for iniPath, sections := range hooks {
		for pid, section := range sections {
			owner := index.ownerOf(pid)
			if listed[pid] || owner != "" && owner != key {
				continue
			}
			listed[pid] = true
			orphans = append(orphans, Emitter{
				PID: pid, INIPath: iniPath, Section: section, Hooked: true, Look: defaultLook(),
			})
		}
	}
	slices.SortFunc(orphans, func(a, b Emitter) int { return a.PID - b.PID })
	inspection.Emitters = append(inspection.Emitters, orphans...)
	return inspection, nil
}

// apply hooks the picked face. With target, the mod's emitter of that slot
// moves there and keeps its look unless look is given, leaving whatever INI
// held it before; otherwise an emitter is added. The mod's other emitters
// keep their hooks, including those stacked on the same draw.
func (s *Service) apply(
	sc *scope,
	layout runtimeLayout,
	source modelviewer.DrawSource,
	triangle anchorTriangle,
	anchor EmitterAnchor,
	target *int,
	look *Look,
) error {
	sourceFace, err := source.SourceFaceIndex(anchor.FaceIndex)
	if err != nil {
		return err
	}
	sc.stage = stageLoad
	key, renumbered, err := s.ownedKey(sc, layout)
	if err != nil {
		return err
	}
	sc.renumbered = renumbered
	if target != nil {
		// The caller named the emitter by the slot the copy shared with the
		// original before it was split off.
		if to, ok := renumbered[*target]; ok {
			target = &to
		}
	}
	sc.stage = stageLoad
	index, err := readIndex(layout, sc.modsRoot)
	if err != nil {
		return err
	}
	newKey := key == ""
	for newKey && (key == "" || index.Owners[key] != nil) {
		key = newModKey()
	}
	doc, err := readProfile(layout, key)
	if err != nil {
		return err
	}
	if target != nil {
		at := slices.IndexFunc(doc.Emitters, func(emitter profileEmitter) bool { return emitter.PID == *target })
		if at < 0 {
			return infra.ContractError(fmt.Sprintf("%s:%d", ErrorEmitterNotFound, *target))
		}
		if look == nil {
			look = doc.Emitters[at].Look
		}
	} else if len(doc.Emitters) >= maxEmitters {
		return infra.ContractError(fmt.Sprintf("%s:%d", ErrorEmitterLimit, maxEmitters))
	}
	iniRel, err := filepath.Rel(sc.root, source.INIPath)
	if err != nil {
		return err
	}
	iniRel = filepath.ToSlash(iniRel)
	original, err := os.ReadFile(source.INIPath)
	if err != nil {
		return err
	}

	sc.stage = stagePatch
	text, err := parseHookedINI(sc.root, source.INIPath, original)
	if err != nil {
		return err
	}
	others := make(map[string]*iniText)
	err = eachINI(sc.root, func(name string, other *iniText) {
		if !platform.SamePathFold(name, source.INIPath) {
			others[name] = other
		}
	})
	if err != nil {
		return err
	}
	sites := text.drawSites(source)
	if len(sites) == 0 {
		return infra.ContractError(ErrorDrawNotFound + ":" + source.Section)
	}
	if text.sharedDraw(source, sites) {
		return infra.ContractError(ErrorSharedDraw + ":" + source.Section)
	}
	if text.bindsVertexBuffersLate(source) {
		return infra.ContractError(ErrorUnsupportedDraw + ":" + source.Section)
	}

	// A hook on the draw that no emitter of this mod stands for is left from
	// another mod or a lost profile, and would capture without a profile.
	listed := make(map[int]bool, len(doc.Emitters))
	for _, emitter := range doc.Emitters {
		listed[emitter.PID] = true
	}
	replaced := map[int]bool{}
	for _, site := range sites {
		for _, hooked := range site.pids {
			if index.ownerOf(hooked) != key || !listed[hooked] {
				replaced[hooked] = true
			}
		}
	}
	pid := 0
	if target != nil {
		pid = *target
	} else if pid, err = index.allocate(); err != nil {
		return err
	}
	replaced[pid] = true
	sc.fields["pid"], sc.applied = pid, &pid
	drop := func(candidate int) bool { return replaced[candidate] }
	text.removeHooks(drop)
	firstIndex := source.StartIndex + sourceFace*3
	text.insertHooks(
		text.drawSites(source),
		pid, firstIndex, source.BaseVertex,
	)
	changes := []fileChange{{path: source.INIPath, data: text.bytes()}}
	for _, name := range slices.Sorted(maps.Keys(others)) {
		other := others[name]
		if other.removeHooks(drop) == 0 {
			continue
		}
		changes = append(changes, fileChange{path: name, data: other.bytes()})
		if len(other.hookPIDs()) == 0 {
			changes = append(changes, fileChange{path: name + backupSuffix, remove: true})
		}
	}

	priority := 0.0
	emitters := make([]profileEmitter, 0, len(doc.Emitters)+1)
	for _, emitter := range doc.Emitters {
		if replaced[emitter.PID] {
			continue
		}
		priority = math.Min(priority, emitter.Row[0])
		emitters = append(emitters, emitter)
	}
	direction, _ := toVec3(anchor.Direction)
	offset, _ := toVec3(anchor.Offset)
	profile, err := buildSurfaceProfile(triangle, direction, offset.scale(sc.game.meshUnit), priority-1)
	if err != nil {
		return err
	}
	emitters = append(emitters, profileEmitter{
		PID:        pid,
		INIPath:    iniRel,
		Section:    source.Section,
		FirstIndex: firstIndex,
		BaseVertex: source.BaseVertex,
		Row:        profile.row,
		Offset:     profile.offset,
		Anchor:     anchor,
		Look:       look,
	})
	doc.Emitters, doc.Enabled, doc.ModPath = emitters, true, sc.root

	// Slots recovered from hooks without a profile stay reserved until removed.
	owned := slices.DeleteFunc(slices.Clone(index.Owners[key]), func(candidate int) bool {
		return replaced[candidate]
	})
	for _, emitter := range doc.Emitters {
		owned = append(owned, emitter.PID)
	}
	slices.Sort(owned)
	index.Owners[key] = slices.Compact(owned)

	profileChanges, err := s.profileChanges(layout, index, key, doc)
	if err != nil {
		return err
	}
	changes = append(changes, profileChanges...)
	backup := source.INIPath + backupSuffix
	if _, err := os.Stat(backup); errors.Is(err, fs.ErrNotExist) {
		changes = append([]fileChange{{path: backup, data: original}}, changes...)
	}
	marker, err := markerChange(sc.root, key, sc.origin)
	if err != nil {
		return err
	}
	return s.commit(sc, append(changes, marker...))
}

func (s *Service) setLook(sc *scope, layout runtimeLayout, pid int, look Look) error {
	sc.stage = stageLoad
	key, renumbered, err := s.ownedKey(sc, layout)
	if err != nil {
		return err
	}
	sc.renumbered = renumbered
	if to, ok := renumbered[pid]; ok {
		pid = to
	}
	if key == "" {
		return infra.ContractError(ErrorNoEmitters)
	}
	sc.stage = stageLoad
	doc, err := readProfile(layout, key)
	if err != nil {
		return err
	}
	if len(doc.Emitters) == 0 {
		return infra.ContractError(ErrorNoEmitters)
	}
	at := slices.IndexFunc(doc.Emitters, func(emitter profileEmitter) bool { return emitter.PID == pid })
	if at < 0 {
		return infra.ContractError(fmt.Sprintf("%s:%d", ErrorEmitterNotFound, pid))
	}
	index, err := readIndex(layout, sc.modsRoot)
	if err != nil {
		return err
	}
	doc.Emitters[at].Look, doc.ModPath = &look, sc.root
	sc.fields["pid"], sc.fields["look"], sc.applied = pid, look, &pid

	changes, err := s.profileChanges(layout, index, key, doc)
	if err != nil {
		return err
	}
	marker, err := markerChange(sc.root, key, sc.origin)
	if err != nil {
		return err
	}
	return s.commit(sc, append(changes, marker...))
}

func (s *Service) remove(sc *scope, layout runtimeLayout, pids []int) error {
	sc.stage = stageLoad
	key, renumbered, err := s.ownedKey(sc, layout)
	sc.renumbered = renumbered
	if err != nil || key == "" {
		return err
	}
	if renumbered != nil && len(pids) > 0 {
		// The caller listed the copy's emitters before it was split off, by
		// the slots it shared with the original.
		moved := make([]int, 0, len(pids))
		for _, pid := range pids {
			if to, ok := renumbered[pid]; ok {
				moved = append(moved, to)
			}
		}
		if len(moved) == 0 {
			return nil
		}
		pids = moved
	}
	sc.stage = stageLoad
	index, err := readIndex(layout, sc.modsRoot)
	if err != nil {
		return err
	}
	doc, err := readProfile(layout, key)
	if err != nil {
		return err
	}
	targets := make(map[int]bool)
	for _, emitter := range doc.Emitters {
		if len(pids) == 0 || slices.Contains(pids, emitter.PID) {
			targets[emitter.PID] = true
		}
	}
	for _, pid := range index.Owners[key] {
		if len(pids) == 0 || slices.Contains(pids, pid) {
			targets[pid] = true
		}
	}
	sc.fields["pids"] = pids

	// A hook nobody owns is left over from lost profiles; inspection lists it
	// under this mod, so it is removable here.
	remove := func(pid int) bool {
		return targets[pid] || (len(pids) == 0 || slices.Contains(pids, pid)) && index.ownerOf(pid) == ""
	}
	sc.stage = stagePatch
	var changes []fileChange
	err = eachINI(sc.root, func(name string, text *iniText) {
		if text.removeHooks(remove) == 0 {
			return
		}
		changes = append(changes, fileChange{path: name, data: text.bytes()})
		if len(text.hookPIDs()) == 0 {
			changes = append(changes, fileChange{path: name + backupSuffix, remove: true})
		}
	})
	if err != nil {
		return err
	}

	remaining := doc.Emitters[:0]
	for _, emitter := range doc.Emitters {
		if !targets[emitter.PID] {
			remaining = append(remaining, emitter)
		}
	}
	doc.Emitters, doc.ModPath = remaining, sc.root
	owned := slices.DeleteFunc(slices.Clone(index.Owners[key]), func(pid int) bool { return targets[pid] })
	index.Owners[key] = owned
	marker, err := readMarker(sc.root)
	if err != nil {
		return err
	}
	if len(doc.Emitters) == 0 && len(owned) == 0 {
		delete(index.Owners, key)
	}
	// A generated mod keeps its marker without emitters: it records the
	// original to restore.
	if len(doc.Emitters) == 0 && len(owned) == 0 && marker.Source == "" {
		changes = append(changes, fileChange{path: filepath.Join(sc.root, modMarkerFile), remove: true})
	} else {
		marker, err := markerChange(sc.root, key, sc.origin)
		if err != nil {
			return err
		}
		changes = append(changes, marker...)
	}
	profileChanges, err := s.profileChanges(layout, index, key, doc)
	if err != nil {
		return err
	}
	return s.commit(sc, append(changes, profileChanges...))
}

// ownedKey returns the profile key of the mod at sc.root. A folder copied
// together with its marker still points at the original's profile and slots,
// so the copy is given its own key, slots and hooks before either is changed;
// when sc.root is the copy, the returned map gives each shared slot's new
// number. A read-only layout could not store the copy's own profile, so the
// copies keep sharing the key: the slot table is not written either.
func (s *Service) ownedKey(sc *scope, layout runtimeLayout) (string, map[int]int, error) {
	marker, err := readMarker(sc.root)
	if err != nil || marker.ModKey == "" {
		return "", nil, err
	}
	key := marker.ModKey
	if layout.readOnly {
		return key, nil, nil
	}
	doc, err := readProfile(layout, key)
	if err != nil {
		return "", nil, err
	}
	owner := doc.ModPath
	if owner == "" {
		owner = marker.ModPath
	}
	copies, err := markersElsewhere(sc.modsRoot, sc.root, key)
	if err != nil {
		return "", nil, err
	}
	if owner != "" && samePhysicalPath(owner, sc.root) {
		// Releasing or reusing a slot here would otherwise reach the hooks
		// the copies still share.
		for _, copied := range copies {
			copyScope := &scope{
				root:     copied,
				modsRoot: sc.modsRoot,
				importer: sc.importer,
				game:     sc.game,
				stage:    sc.stage,
				fields:   sc.fields,
			}
			sc.fields["forkedCopy"] = copied
			_, _, err := s.forkCopy(copyScope, layout, key, doc)
			sc.stage = copyScope.stage
			if err != nil {
				return "", nil, err
			}
		}
		return key, nil, nil
	}
	if len(copies) == 0 {
		// Only this folder carries the key, so the mod was moved or renamed.
		return key, nil, nil
	}
	return s.forkCopy(sc, layout, key, doc)
}

// markerChange rewrites the marker when it does not yet name this folder
// under this key, or when origin records the mod this folder was copied from.
func markerChange(root, key string, origin *modMarker) ([]fileChange, error) {
	marker, err := readMarker(root)
	if err != nil {
		return nil, err
	}
	next := modMarker{Version: 1, ModKey: key, ModPath: root}
	if marker.ModKey == key {
		next.Source, next.ReenableSource = marker.Source, marker.ReenableSource
	}
	if origin != nil {
		next.Source, next.ReenableSource = origin.Source, origin.ReenableSource
	}
	if marker.ModKey == key && marker.ModPath != "" && samePhysicalPath(marker.ModPath, root) &&
		marker.Source == next.Source && marker.ReenableSource == next.ReenableSource {
		return nil, nil
	}
	encoded, err := encodeJSON(next)
	if err != nil {
		return nil, err
	}
	return []fileChange{{path: filepath.Join(root, modMarkerFile), data: encoded}}, nil
}

func (s *Service) forkCopy(
	sc *scope,
	layout runtimeLayout,
	key string,
	shared profileDocument,
) (string, map[int]int, error) {
	sc.stage = stageFork
	sc.fields["copiedFrom"] = shared.ModPath
	index, err := readIndex(layout, sc.modsRoot)
	if err != nil {
		return "", nil, err
	}
	texts := make(map[string]*iniText)
	hooked := make(map[int]bool)
	err = eachINI(sc.root, func(name string, text *iniText) {
		for pid := range text.hookPIDs() {
			if index.ownerOf(pid) == key {
				hooked[pid], texts[name] = true, text
			}
		}
	})
	if err != nil {
		return "", nil, err
	}

	forked := ""
	for forked == "" || index.Owners[forked] != nil {
		forked = newModKey()
	}
	pids := make(map[int]int)
	move := func(from int) error {
		to, err := index.allocate()
		if err != nil {
			return err
		}
		pids[from] = to
		index.Owners[forked] = append(index.Owners[forked], to)
		return nil
	}
	doc := profileDocument{SchemaVersion: 1, Enabled: true, ModPath: sc.root}
	for _, emitter := range shared.Emitters {
		if !hooked[emitter.PID] {
			continue
		}
		if err := move(emitter.PID); err != nil {
			return "", nil, err
		}
		emitter.PID = pids[emitter.PID]
		doc.Emitters = append(doc.Emitters, emitter)
	}
	// Hooks whose profile was lost move as well, so the original keeps the
	// reservation of every slot it still hooks.
	for _, pid := range slices.Sorted(maps.Keys(hooked)) {
		if _, moved := pids[pid]; moved {
			continue
		}
		if err := move(pid); err != nil {
			return "", nil, err
		}
	}
	slices.Sort(index.Owners[forked])
	sc.fields["forkedKey"], sc.fields["forkedPIDs"] = forked, pids

	marker := filepath.Join(sc.root, modMarkerFile)
	if len(pids) == 0 {
		return "", nil, s.commit(sc, []fileChange{{path: marker, remove: true}})
	}
	var changes []fileChange
	for name, text := range texts {
		if text.renumberHooks(pids) > 0 {
			changes = append(changes, fileChange{path: name, data: text.bytes()})
		}
	}
	profileChanges, err := s.profileChanges(layout, index, forked, doc)
	if err != nil {
		return "", nil, err
	}
	encoded, err := encodeJSON(modMarker{Version: 1, ModKey: forked, ModPath: sc.root})
	if err != nil {
		return "", nil, err
	}
	changes = append(append(changes, profileChanges...), fileChange{path: marker, data: encoded})
	return forked, pids, s.commit(sc, changes)
}

// markersElsewhere lists the mod folders other than self, enabled or not,
// that carry the profile key.
func markersElsewhere(modsRoot, self, key string) ([]string, error) {
	const maxDepth = 6
	modsRoot, self = physicalPath(modsRoot), physicalPath(self)
	var found []string
	err := filepath.WalkDir(modsRoot, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			if name == modsRoot {
				return err
			}
			return nil
		}
		if entry.IsDir() {
			depth := strings.Count(strings.TrimPrefix(name, modsRoot), string(filepath.Separator))
			if name != modsRoot && (strings.HasPrefix(entry.Name(), ".") || depth > maxDepth) {
				return filepath.SkipDir
			}
			return nil
		}
		dir := filepath.Dir(name)
		if entry.Name() != modMarkerFile || platform.SamePathFold(dir, self) {
			return nil
		}
		if other, err := readModMarker(dir); err == nil && other == key {
			found = append(found, dir)
		}
		return nil
	})
	return found, err
}

// eachINI parses every INI below root. The tool never writes hooks into
// UTF-16 files, so those are skipped.
func eachINI(root string, visit func(name string, text *iniText)) error {
	return filepath.WalkDir(root, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.EqualFold(filepath.Ext(name), ".ini") {
			return err
		}
		data, err := os.ReadFile(name)
		if err != nil || isUTF16(data) {
			return err
		}
		text, err := parseHookedINI(root, name, data)
		if err != nil {
			return err
		}
		visit(name, text)
		return nil
	})
}

// parseHookedINI refuses a file whose hook blocks lost a marker, so neither
// the INI nor its backup is touched until the user repairs it.
func parseHookedINI(root, name string, data []byte) (*iniText, error) {
	text, err := parseINIText(data)
	if err != nil {
		return nil, err
	}
	if line := text.damagedHook(); line > 0 {
		rel, relErr := filepath.Rel(root, name)
		if relErr != nil {
			rel = name
		}
		return nil, infra.ContractError(fmt.Sprintf("%s:%s:%d", ErrorDamagedHook, filepath.ToSlash(rel), line))
	}
	return text, nil
}

// profileChanges writes or drops the mod's profile file, then the index and
// the loader that binds every remaining profile file.
func (s *Service) profileChanges(
	layout runtimeLayout,
	index profileIndex,
	key string,
	doc profileDocument,
) ([]fileChange, error) {
	if layout.readOnly {
		return nil, nil
	}
	var changes []fileChange
	if len(doc.Emitters) == 0 {
		changes = append(changes, fileChange{path: layout.profilePath(key), remove: true})
	} else {
		encoded, err := encodeProfile(doc)
		if err != nil {
			return nil, err
		}
		changes = append(changes, fileChange{path: layout.profilePath(key), data: encoded})
	}
	encoded, err := encodeJSON(index)
	if err != nil {
		return nil, err
	}
	return append(changes,
		fileChange{path: layout.indexPath(), data: encoded},
		fileChange{path: layout.loaderPath(), data: loaderINI(loaderKeys(layout, index, key, len(doc.Emitters) > 0))},
	), nil
}

type fileChange struct {
	path   string
	data   []byte
	remove bool
}

type fileSnapshot struct {
	data    []byte
	existed bool
}

// commit applies changes in order. A failure puts back every file already
// changed so the INI hooks and the profile tables never disagree.
func (s *Service) commit(sc *scope, changes []fileChange) error {
	sc.stage = stageWrite
	snapshots := make([]fileSnapshot, len(changes))
	for index, change := range changes {
		data, err := os.ReadFile(change.path)
		switch {
		case err == nil:
			snapshots[index] = fileSnapshot{data: data, existed: true}
		case !errors.Is(err, fs.ErrNotExist):
			return err
		}
	}
	for index, change := range changes {
		err := s.applyChange(change)
		if err == nil {
			continue
		}
		rollback := "restored"
		var rollbackErrs []error
		for undo := index - 1; undo >= 0; undo-- {
			if undoErr := s.restore(changes[undo].path, snapshots[undo]); undoErr != nil {
				rollbackErrs = append(rollbackErrs, undoErr)
			}
		}
		if len(rollbackErrs) > 0 {
			rollback = "failed"
			err = errors.Join(append([]error{err}, rollbackErrs...)...)
		}
		return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{
			"file":     change.path,
			"rollback": rollback,
		}})
	}
	return nil
}

func (s *Service) applyChange(change fileChange) error {
	if change.remove {
		return removeIfExists(change.path)
	}
	return s.writeFile(change.path, change.data)
}

func (s *Service) restore(path string, snapshot fileSnapshot) error {
	if snapshot.existed {
		return s.writeFile(path, snapshot.data)
	}
	return removeIfExists(path)
}

func writeAtomic(path string, data []byte) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".nhd-liquid-*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, removeIfExists(temp.Name()))
		}
	}()
	if _, err = temp.Write(data); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err = temp.Sync(); err != nil {
		return errors.Join(err, temp.Close())
	}
	if err = temp.Close(); err != nil {
		return err
	}
	if err = platform.ReplaceAtomic(temp.Name(), path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
