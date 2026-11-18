package modelviewer

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/x448/float16"
)

// The Blender exporters this file ports (PORTING.md) build their buffers with numpy. Columns keep every value
// as the exact value of its numpy dtype, so each step can round where numpy rounds.

// blenderExportMesh is one Blender object as the live link sends it: the evaluated, triangulated mesh in the
// exporter's object space, with corner attributes per loop and vertex groups in Blender's own order.
type blenderExportMesh struct {
	Name string
	// Polygons counts the faces of the object's own mesh data; the exporters skip objects without any.
	Polygons int
	// Positions holds vertex.undeformed_co, three values per vertex.
	Positions []float32
	// LoopVertices holds the vertex of each loop; three loops make a triangle.
	LoopVertices       []uint32
	LoopNormals        []float32
	LoopTangents       []float32
	LoopBitangentSigns []float32
	// UVs and Colors are keyed by layer name (TEXCOORD.xy, COLOR, ...): two and four values per loop.
	UVs    map[string][]float32
	Colors map[string][]float32
	// GroupCounts gives each vertex's number of vertex.groups entries, which follow in GroupIndices and
	// GroupWeights in Blender's order.
	GroupCounts  []uint8
	GroupIndices []uint16
	GroupWeights []float32
	// GroupNames are the object's vertex group names by index.
	GroupNames []string
}

func (m *blenderExportMesh) validate() error {
	vertices := len(m.Positions) / 3
	loops := len(m.LoopVertices)
	switch {
	case len(m.Positions)%3 != 0:
		return fmt.Errorf("object %q: position count %d is not a multiple of 3", m.Name, len(m.Positions))
	case loops%3 != 0:
		return fmt.Errorf("object %q: loop count %d is not a multiple of 3", m.Name, loops)
	case len(m.LoopNormals) != 0 && len(m.LoopNormals) != loops*3,
		len(m.LoopTangents) != 0 && len(m.LoopTangents) != loops*3,
		len(m.LoopBitangentSigns) != 0 && len(m.LoopBitangentSigns) != loops:
		return fmt.Errorf("object %q: corner attributes do not cover %d loops", m.Name, loops)
	case len(m.GroupCounts) != 0 && len(m.GroupCounts) != vertices:
		return fmt.Errorf("object %q: vertex group counts do not cover %d vertices", m.Name, vertices)
	case len(m.GroupIndices) != len(m.GroupWeights):
		return fmt.Errorf("object %q: vertex group indices and weights differ in length", m.Name)
	}
	for _, vertex := range m.LoopVertices {
		if int(vertex) >= vertices {
			return fmt.Errorf("object %q: loop vertex %d out of range", m.Name, vertex)
		}
	}
	for name, uvs := range m.UVs {
		if len(uvs) != loops*2 {
			return fmt.Errorf("object %q: UV layer %s does not cover %d loops", m.Name, name, loops)
		}
	}
	for name, colors := range m.Colors {
		if len(colors) != loops*4 {
			return fmt.Errorf("object %q: color attribute %s does not cover %d loops", m.Name, name, loops)
		}
	}
	total := 0
	for _, count := range m.GroupCounts {
		total += int(count)
	}
	if total != len(m.GroupIndices) {
		return fmt.Errorf("object %q: vertex group counts sum to %d, not %d", m.Name, total, len(m.GroupIndices))
	}
	return nil
}

type exportValueKind uint8

const (
	exportFloat32 exportValueKind = iota
	exportFloat16
	exportUint32
	exportUint16
	exportUint8
	exportSint32
	exportSint16
	exportSint8
	exportUnorm16
	exportUnorm8
	exportSnorm16
	exportSnorm8
)

// exportFormat is a DXGI format as the exporters model it: one value type repeated values times.
type exportFormat struct {
	kind   exportValueKind
	values int
}

func parseExportFormat(name string) (exportFormat, error) {
	name = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(name)), "DXGI_FORMAT_")
	channels, suffix, ok := strings.Cut(name, "_")
	if !ok || channels == "" {
		return exportFormat{}, fmt.Errorf("unsupported format %q", name)
	}
	bits := ""
	values := 0
	for _, channel := range []string{"R", "G", "B", "A"} {
		rest, found := strings.CutPrefix(channels, channel)
		if !found {
			break
		}
		end := 0
		for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
			end++
		}
		if end == 0 || (bits != "" && rest[:end] != bits) {
			return exportFormat{}, fmt.Errorf("unsupported format %q", name)
		}
		bits = rest[:end]
		channels = rest[end:]
		values++
	}
	if channels != "" || values == 0 {
		return exportFormat{}, fmt.Errorf("unsupported format %q", name)
	}
	kinds := map[string]exportValueKind{
		"32FLOAT": exportFloat32, "16FLOAT": exportFloat16,
		"32UINT": exportUint32, "16UINT": exportUint16, "8UINT": exportUint8,
		"32SINT": exportSint32, "16SINT": exportSint16, "8SINT": exportSint8,
		"16UNORM": exportUnorm16, "8UNORM": exportUnorm8,
		"16SNORM": exportSnorm16, "8SNORM": exportSnorm8,
	}
	kind, ok := kinds[bits+suffix]
	if !ok {
		return exportFormat{}, fmt.Errorf("unsupported format %q", name)
	}
	return exportFormat{kind: kind, values: values}, nil
}

func (f exportFormat) valueBytes() int {
	switch f.kind {
	case exportFloat32, exportUint32, exportSint32:
		return 4
	case exportFloat16, exportUint16, exportSint16, exportUnorm16, exportSnorm16:
		return 2
	default:
		return 1
	}
}

func (f exportFormat) byteWidth() int { return f.values * f.valueBytes() }

func (f exportFormat) normalized() bool {
	return f.kind == exportUnorm16 || f.kind == exportUnorm8 || f.kind == exportSnorm16 || f.kind == exportSnorm8
}

// storage is the numpy dtype that holds the format's values.
func (f exportFormat) storage() exportValueKind {
	switch f.kind {
	case exportUnorm16:
		return exportUint16
	case exportUnorm8:
		return exportUint8
	case exportSnorm16:
		return exportSint16
	case exportSnorm8:
		return exportSint8
	default:
		return f.kind
	}
}

func (f exportFormat) withValues(values int) exportFormat {
	return exportFormat{kind: f.kind, values: values}
}

// exportColumn is a numpy array of rows of width values, all of dtype kind (a storage kind).
type exportColumn struct {
	kind   exportValueKind
	width  int
	values []float64
}

func (c exportColumn) rows() int {
	if c.width == 0 {
		return 0
	}
	return len(c.values) / c.width
}

func exportFloat32Value(value float64) float64 { return float64(float32(value)) }

// exportFloat16Value rounds a float32 value to half precision, ties to even, as numpy's astype does.
func exportFloat16Value(value float64) float64 {
	return float64(float16.Fromfloat32(float32(value)).Float32())
}

// exportCast is numpy's astype from a float or integer value to kind. Out-of-range integers wrap, which is
// what the C conversions numpy uses produce on x86 for values inside the int32 range.
func exportCast(kind exportValueKind, value float64) float64 {
	if kind == exportFloat32 {
		return exportFloat32Value(value)
	}
	if kind == exportFloat16 {
		return exportFloat16Value(value)
	}
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	whole := int64(math.Trunc(value))
	switch kind {
	case exportUint32:
		return float64(uint32(whole))
	case exportUint16:
		return float64(uint16(whole))
	case exportUint8:
		return float64(uint8(whole))
	case exportSint32:
		return float64(int32(whole))
	case exportSint16:
		return float64(int16(whole))
	case exportSint8:
		return float64(int8(whole))
	default:
		return value
	}
}

// exportSanitize is numpy.nan_to_num on a float column.
func exportSanitize(kind exportValueKind, value float64) float64 {
	if math.IsNaN(value) {
		return 0
	}
	limit := math.MaxFloat32
	if kind == exportFloat16 {
		limit = 65504
	}
	if math.IsInf(value, 1) {
		return limit
	}
	if math.IsInf(value, -1) {
		return -limit
	}
	return value
}

// fetchExportColumn is foreach_get of float32 Blender data into an array of format, with values of width per
// row taken from source (stride values per row), then nan_to_num.
func fetchExportColumn(format exportFormat, source []float32, stride int) exportColumn {
	rows := 0
	if stride > 0 {
		rows = len(source) / stride
	}
	column := exportColumn{kind: format.storage(), width: format.values, values: make([]float64, rows*format.values)}
	float := column.kind == exportFloat32 || column.kind == exportFloat16
	for row := range rows {
		for k := range format.values {
			value := 0.0
			if k < stride {
				value = float64(source[row*stride+k])
			}
			value = exportCast(column.kind, value)
			if float {
				value = exportSanitize(column.kind, value)
			}
			column.values[row*format.values+k] = value
		}
	}
	return column
}

// exportEncode is the format's type encoder, applied when a column's dtype differs from the format's.
func exportEncode(format exportFormat, column exportColumn) exportColumn {
	storage := format.storage()
	if column.kind == storage {
		return column
	}
	out := exportColumn{kind: storage, width: column.width, values: make([]float64, len(column.values))}
	scale := 0.0
	switch format.kind {
	case exportUnorm16:
		scale = 65535
	case exportUnorm8:
		scale = 255
	case exportSnorm16:
		scale = 32767
	case exportSnorm8:
		scale = 127
	default:
	}
	for i, value := range column.values {
		if scale != 0 {
			// numpy.around(data * scale) in the column's float32 precision.
			value = math.RoundToEven(exportFloat32Value(value * scale))
		}
		out.values[i] = exportCast(storage, value)
	}
	return out
}

func exportResize(column exportColumn, width int, fill float64) exportColumn {
	if column.width == width {
		return column
	}
	rows := column.rows()
	out := exportColumn{kind: column.kind, width: width, values: make([]float64, rows*width)}
	for row := range rows {
		for k := range width {
			value := fill
			if k < column.width {
				value = column.values[row*column.width+k]
			}
			out.values[row*width+k] = value
		}
	}
	return out
}

func exportNegate(column exportColumn, firstOnly bool) exportColumn {
	for i := range column.values {
		if !firstOnly || i%column.width == 0 {
			column.values[i] = -column.values[i]
		}
	}
	return column
}

// exportFlipV is converter_flip_texcoord_v: float32, then v = 1 - v.
func exportFlipV(column exportColumn) exportColumn {
	if column.kind != exportFloat32 {
		column = exportColumn{kind: exportFloat32, width: column.width, values: column.values}
	}
	for row := range column.rows() {
		index := row*column.width + 1
		column.values[index] = exportFloat32Value(1 - column.values[index])
	}
	return column
}

func appendExportValue(out []byte, kind exportValueKind, value float64) []byte {
	switch kind {
	case exportFloat32:
		return binary.LittleEndian.AppendUint32(out, math.Float32bits(float32(value)))
	case exportFloat16:
		return binary.LittleEndian.AppendUint16(out, uint16(float16.Fromfloat32(float32(value))))
	case exportUint32, exportSint32:
		return binary.LittleEndian.AppendUint32(out, uint32(int64(value)))
	case exportUint16, exportSint16:
		return binary.LittleEndian.AppendUint16(out, uint16(int64(value)))
	default:
		return append(out, byte(int64(value)))
	}
}

// exportRowKey appends the bytes of a row.
func exportRowKey(out []byte, column exportColumn, row int) []byte {
	for k := range column.width {
		out = appendExportValue(out, column.kind, column.values[row*column.width+k])
	}
	return out
}

// exportSemanticName is AbstractSemantic.get_name: the semantic, its index when above zero, and .xy for
// texture coordinates.
func exportSemanticName(semantic string, index int) string {
	name := semantic
	if index > 0 {
		name += strconv.Itoa(index)
	}
	if semantic == "TEXCOORD" {
		name += ".xy"
	}
	return name
}

// exportBlendGroups returns a vertex's groups sorted by weight, heaviest first and otherwise in Blender's
// order, as sorted(vertex.groups, key=weight, reverse=True) does.
func exportBlendGroups(indices []uint16, weights []float32) ([]uint16, []float32) {
	order := make([]int, len(indices))
	for i := range order {
		order[i] = i
	}
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && weights[order[j]] > weights[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
	sortedIndices := make([]uint16, len(order))
	sortedWeights := make([]float32, len(order))
	for i, from := range order {
		sortedIndices[i], sortedWeights[i] = indices[from], weights[from]
	}
	return sortedIndices, sortedWeights
}
