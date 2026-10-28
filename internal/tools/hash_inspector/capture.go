package hashinspector

import (
	"cmp"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/platform"
)

// maxPrimitives bounds the primitive IDs a layer lists; one draw can cover the
// cursor pixel with many overlapping triangles.
const maxPrimitives = 16

type Capture struct {
	ID              string  `json:"id"`
	Importer        string  `json:"importer"`
	CapturedAt      string  `json:"capturedAt"`
	Trigger         string  `json:"trigger"`
	FailureCode     string  `json:"failureCode"`
	FailureDetail   string  `json:"failureDetail"`
	WindowWidth     uint32  `json:"windowWidth"`
	WindowHeight    uint32  `json:"windowHeight"`
	CursorX         float32 `json:"cursorX"`
	CursorY         float32 `json:"cursorY"`
	HitsOverflow    bool    `json:"hitsOverflow"`
	DeferredSkipped bool    `json:"deferredSkipped"`
	// Layers are ordered nearest first.
	Layers []Layer `json:"layers"`
}

type CaptureSummary struct {
	ID          string `json:"id"`
	CapturedAt  string `json:"capturedAt"`
	Trigger     string `json:"trigger"`
	FailureCode string `json:"failureCode"`
	Visible     string `json:"visible"`
	Layers      int    `json:"layers"`
}

type Layer struct {
	RecordID   uint32   `json:"recordId"`
	Visible    bool     `json:"visible"`
	Depth      float32  `json:"depth"`
	FrontFace  bool     `json:"frontFace"`
	Origin     string   `json:"origin"`
	DrawNumber uint32   `json:"drawNumber"`
	Call       string   `json:"call"`
	Count      uint32   `json:"count"`
	Instances  uint32   `json:"instances"`
	Start      uint32   `json:"start"`
	BaseVertex int32    `json:"baseVertex"`
	Topology   uint8    `json:"topology"`
	Primitives []uint32 `json:"primitives"`
	// IndexOffset is the first index of the triangle under the cursor, for an indexed triangle list drawn once.
	IndexOffset   *uint32        `json:"indexOffset"`
	IB            *BufferInfo    `json:"ib"`
	VBs           []BufferInfo   `json:"vbs"`
	Shaders       []ShaderInfo   `json:"shaders"`
	Textures      []TextureInfo  `json:"textures"`
	RenderTargets []SurfaceInfo  `json:"renderTargets"`
	DepthTarget   *SurfaceInfo   `json:"depthTarget"`
	Overrides     []OverrideInfo `json:"overrides"`
	CommandList   *CommandInfo   `json:"commandList"`
}

type BufferInfo struct {
	Slot       uint8  `json:"slot"`
	Hash       string `json:"hash"`
	OrigHash   string `json:"origHash"`
	RegionHash string `json:"regionHash"`
	// MatchHash is the hash a [TextureOverride] section matches this buffer by.
	MatchHash      string `json:"matchHash"`
	Offset         uint32 `json:"offset"`
	FormatOrStride uint32 `json:"formatOrStride"`
}

type ShaderInfo struct {
	Stage    string `json:"stage"`
	Hash     string `json:"hash"`
	Replaced bool   `json:"replaced"`
}

type TextureInfo struct {
	Stage     string `json:"stage"`
	Slot      uint8  `json:"slot"`
	Hash      string `json:"hash"`
	OrigHash  string `json:"origHash"`
	Dimension uint8  `json:"dimension"`
	Width     uint32 `json:"width"`
	Height    uint32 `json:"height"`
	Format    uint32 `json:"format"`
}

type SurfaceInfo struct {
	Slot   uint8  `json:"slot"`
	Hash   string `json:"hash"`
	Width  uint32 `json:"width"`
	Height uint32 `json:"height"`
	Format uint32 `json:"format"`
}

type OverrideInfo struct {
	Kind    string `json:"kind"`
	Section string `json:"section"`
	INIPath string `json:"iniPath"`
	// Mod is the folder below Mods that holds the INI, or "" when the INI is elsewhere.
	Mod string `json:"mod"`
}

type CommandInfo struct {
	Section string `json:"section"`
	INIPath string `json:"iniPath"`
	Mod     string `json:"mod"`
	Line    string `json:"line"`
}

var (
	stageNames = [...]string{"vs", "hs", "ds", "gs", "ps"}
	callNames  = map[gamelink.DrawCall]string{
		gamelink.CallDraw: "Draw", gamelink.CallDrawIndexed: "DrawIndexed", gamelink.CallDrawInstanced: "DrawInstanced",
		gamelink.CallDrawIndexedInstanced: "DrawIndexedInstanced", gamelink.CallDrawAuto: "DrawAuto",
		gamelink.CallDrawInstancedIndirect:        "DrawInstancedIndirect",
		gamelink.CallDrawIndexedInstancedIndirect: "DrawIndexedInstancedIndirect",
	}
)

const (
	topologyTriangleList = 4
	depthGreater         = 5
	depthGreaterEqual    = 7
)

// assembleCapture turns a pick into layers. modsRoot is the importer's Mods
// folder that INI paths are attributed to mods by.
func assembleCapture(result gamelink.PickResult, modsRoot string) Capture {
	capture := Capture{
		Trigger: triggerName(result.Trigger), WindowWidth: result.WindowWidth, WindowHeight: result.WindowHeight,
		CursorX: result.CursorX, CursorY: result.CursorY,
		HitsOverflow:    result.Flags&gamelink.PickHitsOverflow != 0,
		DeferredSkipped: result.Flags&gamelink.PickDeferredSkipped != 0,
		Layers:          []Layer{},
	}

	records := make(map[uint32]*gamelink.Record, len(result.Records))
	for i := range result.Records {
		records[result.Records[i].ID] = &result.Records[i]
	}
	layers := map[uint32]*Layer{}
	visible, haveVisible := uint32(0), false
	for _, hit := range result.Hits {
		record := records[hit.Record]
		if record == nil {
			continue
		}
		layer := layers[hit.Record]
		if layer == nil {
			layer = newLayer(record, modsRoot)
			layer.Depth = hit.Depth
			layers[hit.Record] = layer
		}
		if hit.Flags&gamelink.HitEqual != 0 && (!haveVisible || hit.Record > visible) {
			visible, haveVisible = hit.Record, true
		}
		if nearer(hit.Depth, layer.Depth, reversedDepth(record)) {
			layer.Depth = hit.Depth
		}
		layer.FrontFace = layer.FrontFace || hit.Flags&gamelink.HitFrontFace != 0
		if !slices.Contains(layer.Primitives, hit.Primitive) && len(layer.Primitives) < maxPrimitives {
			layer.Primitives = append(layer.Primitives, hit.Primitive)
		}
	}

	// The depth direction comes from the visible draw, or else from any draw that tests depth.
	reversed := false
	for i := range result.Records {
		if result.Records[i].DepthFunc != 0 {
			reversed = reversedDepth(&result.Records[i])
			break
		}
	}
	if haveVisible {
		layers[visible].Visible = true
		if records[visible].DepthFunc != 0 {
			reversed = reversedDepth(records[visible])
		}
	}
	for _, layer := range layers {
		slices.Sort(layer.Primitives)
		layer.IndexOffset = indexOffset(records[layer.RecordID], layer.Primitives)
		capture.Layers = append(capture.Layers, *layer)
	}
	// Nearest first; of two at one depth the later draw is the one on top.
	slices.SortFunc(capture.Layers, func(a, b Layer) int {
		if a.Depth != b.Depth {
			if nearer(a.Depth, b.Depth, reversed) {
				return -1
			}
			return 1
		}
		return cmp.Compare(b.RecordID, a.RecordID)
	})
	return capture
}

func triggerName(trigger gamelink.PickTrigger) string {
	if trigger == gamelink.TriggerKey {
		return "key"
	}
	return "desktop"
}

// reversedDepth reports whether the draw keeps the nearest surface at the
// largest depth, as games with reversed Z do.
func reversedDepth(record *gamelink.Record) bool {
	return record.DepthFunc == depthGreater || record.DepthFunc == depthGreaterEqual
}

func nearer(a, b float32, reversed bool) bool {
	if reversed {
		return a > b
	}
	return a < b
}

func indexOffset(record *gamelink.Record, primitives []uint32) *uint32 {
	indexed := record.Call == gamelink.CallDrawIndexed ||
		(record.Call == gamelink.CallDrawIndexedInstanced && record.Instances == 1)
	if !indexed || record.Topology != topologyTriangleList || len(primitives) == 0 {
		return nil
	}
	offset := record.Start + primitives[0]*3
	return &offset
}

func newLayer(record *gamelink.Record, modsRoot string) *Layer {
	layer := &Layer{
		RecordID: record.ID, DrawNumber: record.DrawNumber, Call: callNames[record.Call], Count: record.Count,
		Instances: record.Instances, Start: record.Start, BaseVertex: record.BaseVertex, Topology: record.Topology,
		Origin: "game", Primitives: []uint32{},
		VBs: []BufferInfo{}, Shaders: []ShaderInfo{}, Textures: []TextureInfo{}, RenderTargets: []SurfaceInfo{},
		Overrides: []OverrideInfo{},
	}
	if record.IB != nil {
		ib := bufferInfo(0, *record.IB)
		layer.IB = &ib
	}
	for _, vb := range record.VBs {
		layer.VBs = append(layer.VBs, bufferInfo(vb.Slot, vb.Buffer))
	}
	for stage, shader := range record.Shaders {
		if shader.Hash != 0 {
			layer.Shaders = append(layer.Shaders, ShaderInfo{
				Stage: stageNames[stage], Hash: fmt.Sprintf("%016x", shader.Hash), Replaced: shader.Replaced,
			})
		}
	}
	for _, t := range record.SRVs {
		layer.Textures = append(layer.Textures, TextureInfo{
			Stage: stageName(t.Stage), Slot: t.Slot, Hash: hash32(t.Hash), OrigHash: hash32(t.OrigHash),
			Dimension: t.Dimension, Width: t.Width, Height: t.Height, Format: t.Format,
		})
	}
	for _, rt := range record.RTs {
		layer.RenderTargets = append(layer.RenderTargets, surfaceInfo(rt.Slot, rt.Surface))
	}
	if record.DS != nil {
		ds := surfaceInfo(0, *record.DS)
		layer.DepthTarget = &ds
	}
	for _, o := range record.Overrides {
		kind := "texture"
		if o.Kind == gamelink.OverrideShader {
			kind = "shader"
		}
		layer.Overrides = append(layer.Overrides, OverrideInfo{
			Kind: kind, Section: o.Section, INIPath: o.INIPath, Mod: modFolder(modsRoot, o.INIPath),
		})
	}
	if origin := record.CommandList; record.Origin == gamelink.OriginCommandList && origin != nil {
		layer.Origin = "command-list"
		layer.CommandList = &CommandInfo{
			Section: origin.Section,
			INIPath: origin.INIPath,
			Mod:     modFolder(modsRoot, origin.INIPath),
			Line:    origin.Line,
		}
	}
	return layer
}

func stageName(stage gamelink.ShaderStage) string {
	if int(stage) < len(stageNames) {
		return stageNames[stage]
	}
	return fmt.Sprintf("stage%d", stage)
}

func bufferInfo(slot uint8, b gamelink.Buffer) BufferInfo {
	info := BufferInfo{
		Slot: slot, Hash: hash32(b.Hash), OrigHash: hash32(b.OrigHash), MatchHash: hash32(b.Hash),
		Offset: b.Offset, FormatOrStride: b.FormatOrStride,
	}
	if b.HasRegion {
		info.RegionHash = hash32(b.RegionHash)
		info.MatchHash = info.RegionHash
	}
	return info
}

func surfaceInfo(slot uint8, s gamelink.Surface) SurfaceInfo {
	return SurfaceInfo{Slot: slot, Hash: hash32(s.Hash), Width: s.Width, Height: s.Height, Format: s.Format}
}

func hash32(hash uint32) string {
	if hash == 0 {
		return ""
	}
	return fmt.Sprintf("%08x", hash)
}

// modFolder names the folder directly below modsRoot that holds iniPath. The DLL
// reports paths below its importer folder, so a junctioned Mods folder still
// compares lexically.
func modFolder(modsRoot, iniPath string) string {
	if modsRoot == "" || iniPath == "" || !platform.SameOrChildPath(modsRoot, iniPath) {
		return ""
	}
	rel, err := filepath.Rel(modsRoot, iniPath)
	if err != nil || rel == "." {
		return ""
	}
	folder, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return folder
}

func (c Capture) summary() CaptureSummary {
	summary := CaptureSummary{
		ID: c.ID, CapturedAt: c.CapturedAt, Trigger: c.Trigger, FailureCode: c.FailureCode, Layers: len(c.Layers),
	}
	for _, layer := range c.Layers {
		if layer.Visible {
			summary.Visible = layer.label()
			break
		}
	}
	return summary
}

func (l Layer) label() string {
	switch {
	case l.CommandList != nil:
		return l.CommandList.Section
	case len(l.Overrides) > 0:
		return l.Overrides[0].Section
	case l.IB != nil:
		return l.IB.MatchHash
	}
	for _, s := range l.Shaders {
		if s.Stage == "vs" {
			return s.Hash
		}
	}
	return ""
}
