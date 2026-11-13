package liquidemitter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const ErrorSurfaceUnavailable = "LIQUID_SURFACE_UNAVAILABLE"

type bodyGeometry interface {
	SurfaceGeometry(context.Context, string, string) (modelviewer.SurfaceGeometry, error)
}

type surfaceMesh struct {
	Key         string `json:"key"`
	Identity    string `json:"identity"`
	Fingerprint string `json:"fingerprint"`
	Faces       int    `json:"faces"`
	Generation  uint32 `json:"generation"`
}

type topologyFace struct {
	vertices [3]uint32
	points   [3]vec3
	adjacent [3]int
}

func buildBody(geometry modelviewer.SurfaceGeometry) (*surfaceMesh, []byte, []byte, error) {
	if len(geometry.Indices) == 0 || len(geometry.Indices)%3 != 0 ||
		len(geometry.Positions) != len(geometry.VertexIDs)*3 {
		return nil, nil, nil, infra.ContractError(ErrorSurfaceUnavailable + ":geometry")
	}
	if _, err := bodyDimensions(len(geometry.Indices) / 3); err != nil {
		return nil, nil, nil, err
	}

	points := make(map[uint32]vec3, len(geometry.VertexIDs))
	for i, id := range geometry.VertexIDs {
		p := vec3{
			float64(geometry.Positions[i*3]),
			float64(geometry.Positions[i*3+1]),
			float64(geometry.Positions[i*3+2]),
		}
		if !finite(p[:]...) {
			return nil, nil, nil, infra.ContractError(ErrorSurfaceUnavailable + ":non-finite")
		}
		points[id] = p
	}

	faces := make([]topologyFace, len(geometry.Indices)/3)
	type edge struct {
		face, opposite int
		a, b           vec3
	}
	edges := map[[2]vec3][]edge{}
	for i := range faces {
		f := &faces[i]
		f.adjacent = [3]int{-1, -1, -1}
		for k := range 3 {
			raw := geometry.Indices[i*3+k]
			id := int64(raw) + int64(geometry.Draw.SourceVertexOffset())
			if id < 0 || id > math.MaxUint32 {
				return nil, nil, nil, infra.ContractError(ErrorSurfaceUnavailable + ":vertex")
			}
			p, ok := points[uint32(id)]
			if !ok {
				return nil, nil, nil, infra.ContractError(ErrorSurfaceUnavailable + ":vertex")
			}
			f.vertices[k] = raw
			f.points[k] = p
		}
		for k := range 3 {
			a, b := f.points[(k+1)%3], f.points[(k+2)%3]
			key := [2]vec3{a, b}
			if slices.Compare(a[:], b[:]) > 0 {
				key = [2]vec3{b, a}
			}
			edges[key] = append(edges[key], edge{i, k, a, b})
		}
	}

	for _, uses := range edges {
		if len(uses) != 2 {
			continue
		}
		a, b := uses[0], uses[1]
		if a.face == b.face || a.a != b.b || a.b != b.a {
			continue
		}
		left, right := faces[a.face], faces[b.face]
		na, oka := left.points[1].sub(left.points[0]).cross(left.points[2].sub(left.points[0])).unit()
		nb, okb := right.points[1].sub(right.points[0]).cross(right.points[2].sub(right.points[0])).unit()
		shared := left.vertices[(a.opposite+1)%3] == right.vertices[(b.opposite+2)%3] &&
			left.vertices[(a.opposite+2)%3] == right.vertices[(b.opposite+1)%3]
		if shared || oka && okb && na.dot(nb) > .5 {
			faces[a.face].adjacent[a.opposite] = b.face
			faces[b.face].adjacent[b.opposite] = a.face
		}
	}

	order := make([]int, len(faces))
	for i := range order {
		order[i] = i
	}
	var partition func([]int)
	partition = func(ids []int) {
		if len(ids) <= 32 {
			return
		}
		lo, hi := vec3{math.Inf(1), math.Inf(1), math.Inf(1)}, vec3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
		center := func(i, axis int) float64 { p := faces[i].points; return (p[0][axis] + p[1][axis] + p[2][axis]) / 3 }
		for _, i := range ids {
			for k := range 3 {
				lo[k] = math.Min(lo[k], center(i, k))
				hi[k] = math.Max(hi[k], center(i, k))
			}
		}
		axis := 0
		for k := 1; k < 3; k++ {
			if hi[k]-lo[k] > hi[axis]-lo[axis] {
				axis = k
			}
		}
		slices.SortFunc(ids, func(a, b int) int {
			if x, y := center(a, axis), center(b, axis); x < y {
				return -1
			} else if x > y {
				return 1
			}
			return a - b
		})
		middle := len(ids) / 2
		partition(ids[:middle])
		partition(ids[middle:])
	}

	partition(order)
	remap := make([]int, len(order))
	for i, original := range order {
		remap[original] = i
	}

	index := make([]byte, len(order)*12)
	topology := make([]byte, (len(order)+1)*16)
	put := func(at int, v float32) { binary.LittleEndian.PutUint32(topology[at:], math.Float32bits(v)) }
	put(0, float32(len(order)))
	for i, original := range order {
		f := faces[original]
		for k := range 3 {
			binary.LittleEndian.PutUint32(index[(i*3+k)*4:], f.vertices[k])
			adjacent := f.adjacent[k]
			if adjacent >= 0 {
				adjacent = remap[adjacent]
			}
			put((i+1)*16+k*4, float32(adjacent))
		}
	}

	digest := sha256.New()
	_, _ = digest.Write(index)
	_, _ = digest.Write(topology)
	sum := digest.Sum(nil)
	generation := binary.LittleEndian.Uint32(sum) & 0xffffff
	if generation == 0 {
		generation = 1
	}
	put(4, float32(generation))
	key := fmt.Sprintf("%x", sum[:16])
	mesh := &surfaceMesh{
		Key: key,
		Identity: strings.ToLower(
			fmt.Sprintf(
				"%s:%s:%s:%d:%d:%d",
				physicalPath(geometry.Draw.INIPath),
				geometry.Draw.Section,
				geometry.Draw.IB,
				geometry.Draw.StartIndex,
				geometry.Draw.IndexCount,
				geometry.Draw.BaseVertex,
			),
		),
		Fingerprint: geometry.Fingerprint,
		Faces:       len(order),
		Generation:  generation,
	}
	if _, err := bodyDimensions(mesh.Faces); err != nil {
		return nil, nil, nil, err
	}
	return mesh, index, topology, nil
}

func bodyDimensions(maximum int) ([]byte, error) {
	leaves := 2
	for leaves < (maximum+31)/32 {
		leaves *= 2
	}
	stride := 2 + maximum*4 + leaves*4
	height := (maximum + 127) / 128
	// Bound the shared atlas to 128 MiB as well as the D3D11 resource dimensions.
	if maximum < 1 || height > 16384 || int64(stride)*8*16 > 128<<20 {
		return nil, infra.ContractError(ErrorSurfaceUnavailable + ":capacity")
	}
	return fmt.Appendf(
		nil,
		"#define BODY_FACES %d\n#define BODY_LEAVES %d\n#define BODY_STRIDE %d\n#define BODY_TEX_WIDTH 384\n#define BODY_TEX_HEIGHT %d\n",
		maximum,
		leaves,
		stride,
		height,
	), nil
}

func bodyPath(layout runtimeLayout, key, suffix string) string {
	return filepath.Join(layout.root, "bodymesh", "data", key+suffix)
}

func bodyChanges(layout runtimeLayout, index profileIndex, current string, doc profileDocument) ([]fileChange, error) {
	if layout.readOnly {
		return nil, nil
	}
	emitters := []profileEmitter{}
	retained := map[string]bool{}
	for _, key := range slices.Sorted(maps.Keys(index.Owners)) {
		other := doc
		if key != current {
			var err error
			other, err = readProfile(layout, key)
			if err != nil {
				return nil, err
			}
		}
		for _, emitter := range other.Emitters {
			if emitter.Surface != nil {
				retained[emitter.Surface.Key] = true
			}
		}
		if other.Enabled {
			for _, emitter := range other.Emitters {
				if emitter.Surface != nil {
					mesh := *emitter.Surface
					mesh.Identity = key + "\x00" + mesh.Identity
					emitter.Surface = &mesh
				}
				emitters = append(emitters, emitter)
			}
		}
	}

	identities := map[string]uint32{}
	for _, e := range emitters {
		if e.Surface != nil {
			identities[e.Surface.Identity] = 0
		}
	}
	for i, key := range slices.Sorted(maps.Keys(identities)) {
		identities[key] = uint32(i + 1)
	}

	var out bytes.Buffer
	configured, yogurt := 0, 0
	for _, e := range emitters {
		if e.Surface != nil {
			configured = 1
		}
		if e.Look != nil && e.Look.Nozzle == NozzleYogurt {
			yogurt = 1
		}
	}
	fmt.Fprintf(
		&out,
		"namespace = LiquidGravityProbe\\BodyMesh\n[Constants]\n$\\LiquidGravityProbe\\body_configured = %d\n$\\LiquidGravityProbe\\yogurt_configured = %d\n[CommandListSelect]\n$count = 0\nResourceTopology = null\n",
		configured,
		yogurt,
	)
	mapping := make([]byte, maxProfiles*4)
	resources := map[string]*surfaceMesh{}
	maximum := 1
	for _, emitter := range emitters {
		mesh := emitter.Surface
		if mesh == nil {
			continue
		}
		if len(mesh.Key) != 32 || strings.Trim(mesh.Key, "0123456789abcdef") != "" || emitter.PID < 0 ||
			emitter.PID >= maxProfiles {
			return nil, infra.ContractError(ErrorSurfaceUnavailable + ":metadata")
		}
		maximum = max(maximum, mesh.Faces)
		resources[mesh.Key] = mesh
		model := identities[mesh.Identity]
		binary.LittleEndian.PutUint32(mapping[emitter.PID*4:], model)
		fmt.Fprintf(
			&out,
			"if x243 == %d\nib = ResourceIB_%s\nResourceTopology = ref ResourceTopology_%s\n$count = %d\nendif\n",
			emitter.PID,
			mesh.Key,
			mesh.Key,
			mesh.Faces*3,
		)
	}
	dimensions, err := bodyDimensions(maximum)
	if err != nil {
		return nil, err
	}
	leaves := 2
	for leaves < (maximum+31)/32 {
		leaves *= 2
	}
	stride := 2 + maximum*4 + leaves*4

	for _, key := range slices.Sorted(maps.Keys(resources)) {
		fmt.Fprintf(
			&out,
			"[ResourceIB_%s]\ntype = Buffer\nformat = R32_UINT\nfilename = data/%s.ib\n[ResourceTopology_%s]\ntype = StructuredBuffer\nstride = 16\nfilename = data/%s.topology\n",
			key,
			key,
			key,
			key,
		)
	}
	fmt.Fprintf(
		&out,
		"[ResourceModelMap]\ntype = Buffer\nformat = R32_UINT\nfilename = model-map.buf\n[ResourceAtlas]\ntype = RWStructuredBuffer\nstride = 16\narray = %d\n[ResourceTemp]\ntype = Texture2D\nformat = R32G32B32A32_FLOAT\nwidth = 384\nheight = %d\narray = 1\nmips = 1\nmsaa = 1\nbind_flags = render_target shader_resource\n",
		8*stride,
		(maximum+127)/128,
	)

	changes := []fileChange{
		{path: filepath.Join(layout.root, "bodymesh", "Generated.ini"), data: out.Bytes()},
		{path: filepath.Join(layout.root, "bodymesh", "Dimensions.hlsl"), data: dimensions},
		{path: filepath.Join(layout.root, "bodymesh", "model-map.buf"), data: mapping},
	}
	for _, change := range changes {
		if err := validateBodyPath(layout.root, change.path); err != nil {
			return nil, err
		}
	}
	dataDir := filepath.Join(layout.root, "bodymesh", "data")
	if err := validateBodyPath(layout.root, dataDir); err != nil {
		return nil, err
	}

	old, err := os.ReadFile(filepath.Join(layout.root, "bodymesh", "Dimensions.hlsl"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if !bytes.Equal(old, dimensions) {
		bundled, err := runtimeFiles(games[0].dir)
		if err != nil {
			return nil, err
		}
		files := map[string][]byte{}
		for name, file := range bundled {
			files[name] = file.Data
		}
		for _, shader := range modruntime.AffectedShaders(files, map[string]bool{"bodymesh/dimensions.hlsl": true}) {
			// F10 also caches shaders by their entry-point timestamps, independently of the disk bytecode.
			if shader != "bodymesh/dimensions.hlsl" {
				name := filepath.Join(layout.root, filepath.FromSlash(shader))
				if err := validateBodyPath(layout.root, name); err != nil {
					return nil, err
				}
				data, err := os.ReadFile(name)
				if err == nil {
					changes = append(changes, fileChange{path: name, data: data})
				} else if !os.IsNotExist(err) {
					return nil, err
				}
			}
			caches, err := modruntime.ShaderCachePaths(layout.root, shader)
			if err != nil {
				return nil, err
			}
			for _, cache := range caches {
				changes = append(changes, fileChange{path: cache, remove: true})
			}
		}
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		key, suffix, ok := strings.Cut(entry.Name(), ".")
		if entry.Type().IsRegular() && ok && (suffix == "ib" || suffix == "topology") &&
			len(key) == 32 && strings.Trim(key, "0123456789abcdef") == "" && !retained[key] {
			changes = append(
				changes,
				fileChange{path: filepath.Join(dataDir, entry.Name()), remove: true},
			)
		}
	}
	for _, change := range changes {
		if err := validateBodyPath(layout.root, change.path); err != nil {
			return nil, err
		}
	}
	return changes, nil
}
