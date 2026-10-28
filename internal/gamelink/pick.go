package gamelink

type PickTrigger uint8

const (
	TriggerDesktop PickTrigger = 1
	TriggerKey     PickTrigger = 2
)

const (
	PickHitsOverflow    uint32 = 1 << 0
	PickDeferredSkipped uint32 = 1 << 1
)

const (
	HitEqual     uint8 = 1 << 0
	HitFrontFace uint8 = 1 << 1
)

type RecordOrigin uint8

const (
	OriginGame        RecordOrigin = 1
	OriginCommandList RecordOrigin = 2
)

type DrawCall uint8

const (
	CallDraw                         DrawCall = 1
	CallDrawIndexed                  DrawCall = 2
	CallDrawInstanced                DrawCall = 3
	CallDrawIndexedInstanced         DrawCall = 4
	CallDrawAuto                     DrawCall = 5
	CallDrawInstancedIndirect        DrawCall = 6
	CallDrawIndexedInstancedIndirect DrawCall = 7
)

type ShaderStage uint8

const (
	StageVS ShaderStage = 0
	StageHS ShaderStage = 1
	StageDS ShaderStage = 2
	StageGS ShaderStage = 3
	StagePS ShaderStage = 4

	stageCount = 5
)

type OverrideKind uint8

const (
	OverrideTexture OverrideKind = 1
	OverrideShader  OverrideKind = 2
)

type PickResult struct {
	CaptureID    uint64
	Frame        uint64
	Trigger      PickTrigger
	WindowWidth  uint32
	WindowHeight uint32
	CursorX      float32
	CursorY      float32
	Flags        uint32
	HitTotal     uint32
	Hits         []Hit
	Records      []Record
}

type Hit struct {
	Record    uint32
	Primitive uint32
	Depth     float32
	Flags     uint8
}

type Record struct {
	ID            uint32
	Origin        RecordOrigin
	DrawNumber    uint32
	Call          DrawCall
	Count         uint32
	Instances     uint32
	Start         uint32
	BaseVertex    int32
	StartInstance uint32
	Topology      uint8
	DepthFunc     uint8
	IB            *Buffer
	VBs           []VertexBuffer
	// Shaders is indexed by ShaderStage.
	Shaders     [stageCount]Shader
	SRVs        []Texture
	RTs         []Target
	DS          *Surface
	Overrides   []Override
	CommandList *CommandOrigin
}

type Buffer struct {
	Hash      uint32
	OrigHash  uint32
	HasRegion bool
	// RegionHash is what [TextureOverride] sections match when 3DMigoto hashes
	// a region of the buffer instead of the whole resource.
	RegionHash     uint32
	Offset         uint32
	FormatOrStride uint32
}

type VertexBuffer struct {
	Slot uint8
	Buffer
}

type Shader struct {
	Hash     uint64
	Replaced bool
}

type Texture struct {
	Stage     ShaderStage
	Slot      uint8
	Hash      uint32
	OrigHash  uint32
	Dimension uint8
	Width     uint32
	Height    uint32
	Format    uint32
}

type Target struct {
	Slot uint8
	Surface
}

type Surface struct {
	Hash   uint32
	Width  uint32
	Height uint32
	Format uint32
}

type Override struct {
	Kind    OverrideKind
	Section string
	INIPath string
}

type CommandOrigin struct {
	Section string
	INIPath string
	Line    string
}

const (
	hitSize      = 13
	minRecordLen = 85
)

func (m PickResult) encode(e *encoder) {
	e.u64(m.CaptureID)
	e.u64(m.Frame)
	e.u8(uint8(m.Trigger))
	e.u32(m.WindowWidth)
	e.u32(m.WindowHeight)
	e.f32(m.CursorX)
	e.f32(m.CursorY)
	e.u32(m.Flags)
	e.u32(m.HitTotal)

	e.count16(len(m.Hits))
	for _, h := range m.Hits {
		e.u32(h.Record)
		e.u32(h.Primitive)
		e.f32(h.Depth)
		e.u8(h.Flags)
	}

	e.count16(len(m.Records))
	for i := range m.Records {
		encodeRecord(e, &m.Records[i])
	}
}

func encodeRecord(e *encoder, r *Record) {
	e.u32(r.ID)
	e.u8(uint8(r.Origin))
	e.u32(r.DrawNumber)
	e.u8(uint8(r.Call))
	e.u32(r.Count)
	e.u32(r.Instances)
	e.u32(r.Start)
	e.i32(r.BaseVertex)
	e.u32(r.StartInstance)
	e.u8(r.Topology)
	e.u8(r.DepthFunc)

	e.bool(r.IB != nil)
	if r.IB != nil {
		encodeBuffer(e, *r.IB)
	}
	e.count8(len(r.VBs))
	for _, vb := range r.VBs {
		e.u8(vb.Slot)
		encodeBuffer(e, vb.Buffer)
	}
	for _, s := range r.Shaders {
		e.u64(s.Hash)
		e.bool(s.Replaced)
	}
	e.count16(len(r.SRVs))
	for _, t := range r.SRVs {
		e.u8(uint8(t.Stage))
		e.u8(t.Slot)
		e.u32(t.Hash)
		e.u32(t.OrigHash)
		e.u8(t.Dimension)
		e.u32(t.Width)
		e.u32(t.Height)
		e.u32(t.Format)
	}
	e.count8(len(r.RTs))
	for _, t := range r.RTs {
		e.u8(t.Slot)
		encodeSurface(e, t.Surface)
	}
	e.bool(r.DS != nil)
	if r.DS != nil {
		encodeSurface(e, *r.DS)
	}

	e.count16(len(r.Overrides))
	for _, o := range r.Overrides {
		e.u8(uint8(o.Kind))
		e.str(o.Section)
		e.str(o.INIPath)
	}
	if r.Origin == OriginCommandList {
		origin := r.CommandList
		if origin == nil {
			e.fail("command list record %d without its origin", r.ID)
			return
		}
		e.str(origin.Section)
		e.str(origin.INIPath)
		e.str(origin.Line)
	}
}

func encodeBuffer(e *encoder, b Buffer) {
	e.u32(b.Hash)
	e.u32(b.OrigHash)
	e.bool(b.HasRegion)
	e.u32(b.RegionHash)
	e.u32(b.Offset)
	e.u32(b.FormatOrStride)
}

func encodeSurface(e *encoder, s Surface) {
	e.u32(s.Hash)
	e.u32(s.Width)
	e.u32(s.Height)
	e.u32(s.Format)
}

func decodePickResult(d *decoder) PickResult {
	m := PickResult{
		CaptureID: d.u64(), Frame: d.u64(), Trigger: PickTrigger(d.u8()),
		WindowWidth: d.u32(), WindowHeight: d.u32(), CursorX: d.f32(), CursorY: d.f32(),
		Flags: d.u32(), HitTotal: d.u32(),
	}

	m.Hits = make([]Hit, d.count16(hitSize))
	for i := range m.Hits {
		m.Hits[i] = Hit{Record: d.u32(), Primitive: d.u32(), Depth: d.f32(), Flags: d.u8()}
	}

	m.Records = make([]Record, d.count16(minRecordLen))
	for i := range m.Records {
		m.Records[i] = decodeRecord(d)
	}
	return m
}

func decodeRecord(d *decoder) Record {
	r := Record{
		ID: d.u32(), Origin: RecordOrigin(d.u8()), DrawNumber: d.u32(), Call: DrawCall(d.u8()),
		Count: d.u32(), Instances: d.u32(), Start: d.u32(), BaseVertex: d.i32(), StartInstance: d.u32(),
		Topology: d.u8(), DepthFunc: d.u8(),
	}

	if d.bool() {
		ib := decodeBuffer(d)
		r.IB = &ib
	}
	r.VBs = make([]VertexBuffer, d.count8(22))
	for i := range r.VBs {
		r.VBs[i] = VertexBuffer{Slot: d.u8(), Buffer: decodeBuffer(d)}
	}
	for i := range r.Shaders {
		r.Shaders[i] = Shader{Hash: d.u64(), Replaced: d.bool()}
	}
	r.SRVs = make([]Texture, d.count16(23))
	for i := range r.SRVs {
		r.SRVs[i] = Texture{
			Stage: ShaderStage(d.u8()), Slot: d.u8(), Hash: d.u32(), OrigHash: d.u32(), Dimension: d.u8(),
			Width: d.u32(), Height: d.u32(), Format: d.u32(),
		}
	}
	r.RTs = make([]Target, d.count8(17))
	for i := range r.RTs {
		r.RTs[i] = Target{Slot: d.u8(), Surface: decodeSurface(d)}
	}
	if d.bool() {
		ds := decodeSurface(d)
		r.DS = &ds
	}

	r.Overrides = make([]Override, d.count16(5))
	for i := range r.Overrides {
		r.Overrides[i] = Override{Kind: OverrideKind(d.u8()), Section: d.str(), INIPath: d.str()}
	}
	switch r.Origin {
	case OriginCommandList:
		r.CommandList = &CommandOrigin{Section: d.str(), INIPath: d.str(), Line: d.str()}
	case OriginGame:
	default:
		d.fail("record origin %d", r.Origin)
	}
	return r
}

func decodeBuffer(d *decoder) Buffer {
	return Buffer{
		Hash: d.u32(), OrigHash: d.u32(), HasRegion: d.bool(), RegionHash: d.u32(), Offset: d.u32(),
		FormatOrStride: d.u32(),
	}
}

func decodeSurface(d *decoder) Surface {
	return Surface{Hash: d.u32(), Width: d.u32(), Height: d.u32(), Format: d.u32()}
}
