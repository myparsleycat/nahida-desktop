package modelviewer

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
)

// XXMI-Tools (GIMI, SRMI and ZZMI mods) export rules; PORTING.md maps them to the Python they come from.

type xxmiExportGame uint8

const (
	xxmiGenshin xxmiExportGame = iota + 1
	xxmiStarRail
	xxmiZenless
)

// parseXXMIExportGame takes an XXMI-Tools game or the importer key of its mods.
func parseXXMIExportGame(name string) (xxmiExportGame, error) {
	switch name {
	case "GenshinImpact", "GIMI":
		return xxmiGenshin, nil
	case "HonkaiStarRail", "SRMI":
		return xxmiStarRail, nil
	case "ZenlessZoneZero", "ZZMI":
		return xxmiZenless, nil
	}
	return 0, fmt.Errorf("unsupported XXMI-Tools game %q", name)
}

func (g xxmiExportGame) importer() string {
	switch g {
	case xxmiGenshin:
		return "GIMI"
	case xxmiStarRail:
		return "SRMI"
	case xxmiZenless:
		return "ZZMI"
	default:
		return ""
	}
}

func (g xxmiExportGame) materialProfile() ModelViewerMaterialProfile {
	switch g {
	case xxmiStarRail:
		return modelViewerSRMIProfile
	case xxmiZenless:
		return modelViewerZZMIProfile
	default:
		return modelViewerGIMIProfile
	}
}

// xxmiLayoutElement is one entry of the 3DMigoto:VBLayout object property.
type xxmiLayoutElement struct {
	SemanticName  string
	SemanticIndex int
	Format        string
}

type xxmiOutlineSettings struct {
	Enabled bool
	// Precision is the number of decimals positions are rounded to before loops are welded.
	Precision      int
	GateDivergence float64
	CustomNormals  bool
}

// xxmiExportComponent is one hash.json component with the settings XXMI-Tools reads from the main object of
// its first part and from the scene.
type xxmiExportComponent struct {
	Game     xxmiExportGame
	FullName string
	// HasBlend and HasTexcoord report whether hash.json names a blend_vb and a texcoord_vb.
	HasBlend    bool
	HasTexcoord bool
	Layout      []xxmiLayoutElement
	IBFormat    string
	FlipWinding bool
	FlipNormal  bool
	FlipTangent bool
	// FlipBitangentSign is the 3DMigoto:Tangent property, which XXMI-Tools reads in place of a bitangent flag.
	FlipBitangentSign bool
	Mirror            bool
	// FlipV maps UV layer names to their 3DMigoto:<name> flip_v property.
	FlipV            map[string]bool
	NormalizeWeights bool
	Outlines         xxmiOutlineSettings
	Parts            []xxmiExportPart
}

type xxmiExportPart struct {
	FullName string
	Objects  []blenderExportMesh
}

type xxmiObjectDraw struct {
	Name        string
	VertexCount int
	IndexCount  int
	IndexOffset int
}

type xxmiPartDraws struct {
	FullName string
	Draws    []xxmiObjectDraw
}

// xxmiExportResult holds the files XXMI-Tools writes for a component, by file name, and the counts its INI
// template renders.
type xxmiExportResult struct {
	Files       map[string][]byte
	Parts       []xxmiPartDraws
	VertexCount int
	Strides     map[string]int
	Warnings    []string
}

type xxmiSemantic struct {
	semantic string
	index    int
	format   exportFormat
}

func (s xxmiSemantic) name() string { return exportSemanticName(s.semantic, s.index) }

// same is AbstractSemantic equality, under which BLENDWEIGHT equals BLENDWEIGHTS.
func (s xxmiSemantic) same(other xxmiSemantic) bool {
	alias := func(name string) string {
		if name == "BLENDWEIGHT" {
			return "BLENDWEIGHTS"
		}
		return name
	}
	return alias(s.semantic) == alias(other.semantic) && s.index == other.index
}

func xxmiBlendSemantic(semantic string) bool {
	return semantic == "BLENDINDICES" || semantic == "BLENDWEIGHTS" || semantic == "BLENDWEIGHT"
}

func xxmiBlenderFormat(semantic string) exportFormat {
	switch semantic {
	case "NORMAL", "TANGENT":
		return exportFormat{kind: exportFloat16, values: 3}
	case "BITANGENTSIGN":
		return exportFormat{kind: exportFloat16, values: 1}
	case "COLOR":
		return exportFormat{kind: exportFloat32, values: 4}
	case "TEXCOORD":
		return exportFormat{kind: exportFloat32, values: 2}
	case "POSITION":
		return exportFormat{kind: exportFloat32, values: 3}
	case "BLENDWEIGHTS", "BLENDWEIGHT":
		return exportFormat{kind: exportFloat32, values: 1}
	default:
		return exportFormat{kind: exportUint32, values: 1}
	}
}

// proxy is make_proxy_layout: the format Blender data is fetched into before conversion.
func (s xxmiSemantic) proxy(converted bool) exportFormat {
	blender := xxmiBlenderFormat(s.semantic)
	blend := xxmiBlendSemantic(s.semantic)
	switch {
	case s.format.normalized() || converted:
		if blend {
			return blender.withValues(s.format.values)
		}
		return blender
	case blend:
		return s.format
	case s.format.values != blender.values,
		s.format.valueBytes() > blender.valueBytes(),
		s.format.byteWidth() != blender.valueBytes():
		return blender
	}
	return s.format
}

type xxmiConverter func(exportColumn) exportColumn

type xxmiLayout struct {
	position, blend, texcoord []xxmiSemantic
	semanticConverters        map[string][]xxmiConverter
	formatConverters          map[string][]xxmiConverter
}

func addXXMISemantic(semantics []xxmiSemantic, semantic xxmiSemantic) []xxmiSemantic {
	for _, existing := range semantics {
		if existing.same(semantic) {
			return semantics
		}
	}
	return append(semantics, semantic)
}

func xxmiConverterKey(semantic string, index int) string { return semantic + "#" + fmt.Sprint(index) }

// buildXXMILayout is DataModelXXMI.from_obj.
func buildXXMILayout(component *xxmiExportComponent) (xxmiLayout, error) {
	layout := xxmiLayout{
		semanticConverters: make(map[string][]xxmiConverter),
		formatConverters:   make(map[string][]xxmiConverter),
	}
	positionSemantics := []string{"POSITION", "NORMAL", "TANGENT"}
	blendSemantics := []string{"BLENDWEIGHTS", "BLENDWEIGHT", "BLENDINDICES"}
	texcoordSemantics := []string{"TEXCOORD", "COLOR"}
	if !component.HasBlend {
		blendSemantics = nil
		if !component.HasTexcoord {
			positionSemantics = []string{"POSITION", "NORMAL", "TANGENT", "TEXCOORD", "COLOR"}
			texcoordSemantics = nil
		}
	}
	contains := func(list []string, value string) bool {
		for _, item := range list {
			if item == value {
				return true
			}
		}
		return false
	}
	for _, element := range component.Layout {
		format, err := parseExportFormat(element.Format)
		if err != nil {
			return xxmiLayout{}, fmt.Errorf("VBLayout %s%d: %w", element.SemanticName, element.SemanticIndex, err)
		}
		semantic := xxmiSemantic{semantic: element.SemanticName, index: element.SemanticIndex, format: format}
		key := xxmiConverterKey(semantic.semantic, semantic.index)
		switch {
		case contains(positionSemantics, semantic.semantic):
			if (semantic.semantic == "NORMAL" || semantic.semantic == "POSITION") && format.values == 4 {
				layout.semanticConverters[key] = []xxmiConverter{
					func(column exportColumn) exportColumn { return exportResize(column, 4, 1) },
				}
			}
			if semantic.semantic == "TANGENT" && format.values == 4 {
				layout.position = addXXMISemantic(layout.position, xxmiSemantic{
					semantic: "TANGENT", index: semantic.index, format: format.withValues(3),
				})
				layout.position = addXXMISemantic(layout.position, xxmiSemantic{
					semantic: "BITANGENTSIGN", index: semantic.index, format: format.withValues(1),
				})
				continue
			}
			layout.position = addXXMISemantic(layout.position, semantic)
		case contains(blendSemantics, semantic.semantic):
			// from_obj tests the enum by identity, so the BLENDWEIGHT alias is never normalized.
			if semantic.semantic == "BLENDWEIGHTS" && component.NormalizeWeights {
				layout.formatConverters[key] = []xxmiConverter{xxmiNormalizeWeights}
			}
			layout.blend = addXXMISemantic(layout.blend, semantic)
		case contains(texcoordSemantics, semantic.semantic):
			layout.texcoord = addXXMISemantic(layout.texcoord, semantic)
		}
	}
	if component.Game == xxmiZenless {
		for _, semantic := range layout.position {
			if semantic.semantic == "BITANGENTSIGN" && semantic.index == 0 {
				layout.formatConverters[xxmiConverterKey("BITANGENTSIGN", 0)] = []xxmiConverter{
					func(column exportColumn) exportColumn { return exportNegate(column, false) },
				}
			}
		}
	}

	// get_mesh_data puts each converter in front of the ones already listed.
	insert := func(key string, converter xxmiConverter) {
		layout.semanticConverters[key] = append([]xxmiConverter{converter}, layout.semanticConverters[key]...)
	}
	flipBitangent := component.FlipBitangentSign != component.Mirror
	for _, semantic := range layout.all() {
		key := xxmiConverterKey(semantic.semantic, semantic.index)
		flip := func(column exportColumn) exportColumn { return exportNegate(column, false) }
		if component.FlipNormal && semantic.semantic == "NORMAL" {
			insert(key, flip)
		}
		if component.FlipTangent && semantic.semantic == "TANGENT" {
			insert(key, flip)
		}
		if flipBitangent && semantic.semantic == "BITANGENTSIGN" {
			insert(key, flip)
		}
		if component.Mirror &&
			(semantic.semantic == "POSITION" || semantic.semantic == "NORMAL" || semantic.semantic == "TANGENT") {
			insert(key, func(column exportColumn) exportColumn { return exportNegate(column, true) })
		}
		if semantic.semantic == "TEXCOORD" && component.FlipV[semantic.name()] {
			insert(key, exportFlipV)
		}
	}
	return layout, nil
}

func (l xxmiLayout) all() []xxmiSemantic {
	all := make([]xxmiSemantic, 0, len(l.position)+len(l.blend)+len(l.texcoord))
	for _, list := range [][]xxmiSemantic{l.position, l.blend, l.texcoord} {
		for _, semantic := range list {
			all = addXXMISemantic(all, semantic)
		}
	}
	return all
}

func xxmiStride(semantics []xxmiSemantic) int {
	stride := 0
	for _, semantic := range semantics {
		stride += semantic.format.byteWidth()
	}
	return stride
}

// xxmiNormalizeWeights is converter_normalize_weights: each row divided by its float32 sum, a zero sum by one.
func xxmiNormalizeWeights(column exportColumn) exportColumn {
	if column.width <= 1 {
		return column
	}
	for row := range column.rows() {
		values := column.values[row*column.width : (row+1)*column.width]
		var sum float32
		for _, value := range values {
			sum += float32(value)
		}
		if sum == 0 {
			sum = 1
		}
		for k, value := range values {
			values[k] = float64(float32(value) / sum)
		}
	}
	return column
}

type xxmiObjectBuffers struct {
	indices  []uint32
	columns  map[string]exportColumn
	vertices int
}

// exportXXMIObject is BlenderDataExtractor.get_data for one object: its index buffer from zero and its vertex
// columns in export formats.
func exportXXMIObject(
	component *xxmiExportComponent,
	layout xxmiLayout,
	mesh *blenderExportMesh,
) (xxmiObjectBuffers, error) {
	if err := mesh.validate(); err != nil {
		return xxmiObjectBuffers{}, err
	}
	semantics := layout.all()
	for _, semantic := range semantics {
		switch semantic.semantic {
		case "TEXCOORD":
			if _, ok := mesh.UVs[semantic.name()]; !ok {
				return xxmiObjectBuffers{}, fmt.Errorf("mesh(%s) is missing UV layer %s", mesh.Name, semantic.name())
			}
		case "COLOR":
			if _, ok := mesh.Colors[semantic.name()]; !ok {
				return xxmiObjectBuffers{}, fmt.Errorf(
					"mesh(%s) is missing vertex color %s", mesh.Name, semantic.name())
			}
		}
	}

	// Loop data in proxy formats, with the vertex of each loop last, as the extractor lays it out.
	type loopField struct {
		semantic xxmiSemantic
		column   exportColumn
	}
	loops := len(mesh.LoopVertices)
	var fields []loopField
	for _, semantic := range semantics {
		key := xxmiConverterKey(semantic.semantic, semantic.index)
		proxy := semantic.proxy(len(layout.semanticConverters[key]) > 0)
		var column exportColumn
		switch semantic.semantic {
		case "NORMAL":
			column = fetchExportColumn(proxy, xxmiLoopValues(mesh.LoopNormals, loops, 3), 3)
		case "TANGENT":
			column = fetchExportColumn(proxy, xxmiLoopValues(mesh.LoopTangents, loops, 3), 3)
		case "BITANGENTSIGN":
			column = fetchExportColumn(proxy, xxmiLoopValues(mesh.LoopBitangentSigns, loops, 1), 1)
		case "COLOR":
			column = fetchExportColumn(proxy, mesh.Colors[semantic.name()], 4)
		case "TEXCOORD":
			column = fetchExportColumn(proxy, mesh.UVs[semantic.name()], 2)
		default:
			continue
		}
		fields = append(fields, loopField{semantic: semantic, column: column})
	}
	vertexIDs := exportColumn{kind: exportUint32, width: 1, values: make([]float64, loops)}
	for loop, vertex := range mesh.LoopVertices {
		vertexIDs.values[loop] = float64(vertex)
	}
	fields = append(fields, loopField{semantic: xxmiSemantic{semantic: "VERTEXID"}, column: vertexIDs})

	order := make([]int, loops)
	for loop := range order {
		order[loop] = loop
	}
	if component.FlipWinding != component.Mirror {
		for triangle := 0; triangle+2 < loops; triangle += 3 {
			order[triangle], order[triangle+2] = order[triangle+2], order[triangle]
		}
	}

	// The index buffer numbers rows by their bytes in the order they first appear, while the vertex rows
	// come from numpy.unique, which compares by value. The two disagree on rows that differ only in the
	// sign of a zero; the extractor writes both as they are.
	indices := make([]uint32, loops)
	byBytes := make(map[string]uint32, loops)
	var key []byte
	for position, loop := range order {
		key = key[:0]
		for _, field := range fields {
			key = exportRowKey(key, field.column, loop)
		}
		index, seen := byBytes[string(key)]
		if !seen {
			index = uint32(len(byBytes))
			byBytes[string(key)] = index
		}
		indices[position] = index
	}
	loopColumns := make([]exportColumn, len(fields))
	for k, field := range fields {
		loopColumns[k] = field.column
	}
	kept := xxmiUniqueLoops(loopColumns, order)
	gather := func(column exportColumn) exportColumn {
		out := exportColumn{kind: column.kind, width: column.width, values: make([]float64, 0, len(kept)*column.width)}
		for _, loop := range kept {
			out.values = append(out.values, column.values[loop*column.width:(loop+1)*column.width]...)
		}
		return out
	}

	masked := make(map[uint16]bool)
	for index, name := range mesh.GroupNames {
		if strings.HasPrefix(name, "MASK") {
			masked[uint16(index)] = true
		}
	}
	groupStarts := make([]int, len(mesh.GroupCounts)+1)
	for vertex, count := range mesh.GroupCounts {
		groupStarts[vertex+1] = groupStarts[vertex] + int(count)
	}
	vertexGroups := func(vertex int) ([]uint16, []float32) {
		if vertex+1 >= len(groupStarts) {
			return nil, nil
		}
		start, end := groupStarts[vertex], groupStarts[vertex+1]
		weights := make([]float32, end-start)
		copy(weights, mesh.GroupWeights[start:end])
		for k, group := range mesh.GroupIndices[start:end] {
			if masked[group] {
				weights[k] = 0
			}
		}
		return exportBlendGroups(mesh.GroupIndices[start:end], weights)
	}

	columns := make(map[string]exportColumn, len(semantics))
	for _, field := range fields {
		if field.semantic.semantic != "VERTEXID" {
			columns[field.semantic.name()] = gather(field.column)
		}
	}
	for _, semantic := range semantics {
		key := xxmiConverterKey(semantic.semantic, semantic.index)
		proxy := semantic.proxy(len(layout.semanticConverters[key]) > 0)
		switch semantic.semantic {
		case "POSITION":
			source := make([]float32, 0, len(kept)*3)
			for _, loop := range kept {
				vertex := int(mesh.LoopVertices[loop])
				source = append(source, mesh.Positions[vertex*3:vertex*3+3]...)
			}
			columns[semantic.name()] = fetchExportColumn(proxy, source, 3)
		case "BLENDINDICES", "BLENDWEIGHTS", "BLENDWEIGHT":
			column := exportColumn{kind: proxy.storage(), width: proxy.values}
			column.values = make([]float64, len(kept)*proxy.values)
			for row, loop := range kept {
				groups, weights := vertexGroups(int(mesh.LoopVertices[loop]))
				for k := range min(len(groups), proxy.values) {
					value := float64(weights[k])
					if semantic.semantic == "BLENDINDICES" {
						value = float64(groups[k])
						if exportCast(column.kind, value) != value {
							return xxmiObjectBuffers{}, fmt.Errorf(
								"mesh(%s): vertex group %d does not fit %s", mesh.Name, groups[k], semantic.name())
						}
					}
					column.values[row*proxy.values+k] = exportCast(column.kind, value)
				}
			}
			columns[semantic.name()] = column
		}
	}

	for _, semantic := range semantics {
		column, ok := columns[semantic.name()]
		if !ok {
			continue
		}
		key := xxmiConverterKey(semantic.semantic, semantic.index)
		for _, converter := range layout.semanticConverters[key] {
			column = converter(column)
		}
		for _, converter := range layout.formatConverters[key] {
			column = converter(column)
		}
		columns[semantic.name()] = exportEncode(semantic.format, column)
	}
	return xxmiObjectBuffers{indices: indices, columns: columns, vertices: len(kept)}, nil
}

// xxmiUniqueLoops is remove_duplicates, data[numpy.sort(numpy.unique(data, return_index=True)[1])], on
// the structured loop array in the given row order. numpy sorts it stably, comparing single-value fields
// by value and multi-value fields by their little-endian bytes, then drops each row equal by value to the
// row before it. Value-equal rows that the byte order separates therefore both stay.
func xxmiUniqueLoops(fields []exportColumn, order []int) []int {
	rowBytes := make([][]string, len(fields))
	for k, column := range fields {
		if column.width == 1 {
			continue
		}
		rowBytes[k] = make([]string, len(order))
		var buf []byte
		for position, loop := range order {
			buf = exportRowKey(buf[:0], column, loop)
			rowBytes[k][position] = string(buf)
		}
	}
	compare := func(a, b int) int {
		for k, column := range fields {
			if column.width == 1 {
				if c := cmp.Compare(column.values[order[a]], column.values[order[b]]); c != 0 {
					return c
				}
				continue
			}
			if c := strings.Compare(rowBytes[k][a], rowBytes[k][b]); c != 0 {
				return c
			}
		}
		return 0
	}
	equal := func(a, b int) bool {
		for _, column := range fields {
			for v := range column.width {
				if column.values[order[a]*column.width+v] != column.values[order[b]*column.width+v] {
					return false
				}
			}
		}
		return true
	}

	sorted := make([]int, len(order))
	for position := range sorted {
		sorted[position] = position
	}
	slices.SortStableFunc(sorted, compare)
	first := make([]int, 0, len(sorted))
	for k, position := range sorted {
		if k == 0 || !equal(sorted[k-1], position) {
			first = append(first, position)
		}
	}
	slices.Sort(first)
	kept := make([]int, len(first))
	for k, position := range first {
		kept[k] = order[position]
	}
	return kept
}

func xxmiLoopValues(values []float32, loops, width int) []float32 {
	if len(values) == loops*width {
		return values
	}
	return make([]float32, loops*width)
}

// exportXXMIComponent is ModExporter.generate_buffers for one component, followed by optimize_outlines.
func exportXXMIComponent(component *xxmiExportComponent) (xxmiExportResult, error) {
	layout, err := buildXXMILayout(component)
	if err != nil {
		return xxmiExportResult{}, err
	}
	result := xxmiExportResult{
		Files: make(map[string][]byte),
		Strides: map[string]int{
			"position": xxmiStride(layout.position),
			"blend":    xxmiStride(layout.blend),
			"texcoord": xxmiStride(layout.texcoord),
		},
	}
	buffers := map[string][]xxmiSemantic{
		"Position": layout.position,
		"Blend":    layout.blend,
		"Texcoord": layout.texcoord,
	}
	columns := make(map[string]exportColumn)
	var allIndices []uint32
	vertexOffset := 0
	for _, part := range component.Parts {
		draws := xxmiPartDraws{FullName: part.FullName}
		var partIndices []uint32
		indexOffset := 0
		for objectIndex := range part.Objects {
			mesh := &part.Objects[objectIndex]
			draw := xxmiObjectDraw{Name: mesh.Name}
			if !mesh.hasTriangles() {
				draws.Draws = append(draws.Draws, draw)
				continue
			}
			object, err := exportXXMIObject(component, layout, mesh)
			if err != nil {
				return xxmiExportResult{}, err
			}
			if len(layout.blend) > 0 && len(mesh.GroupNames) == 0 && object.vertices > 0 {
				result.Warnings = append(result.Warnings, fmt.Sprintf("mesh(%s) has no vertex groups", mesh.Name))
			}
			for _, index := range object.indices {
				partIndices = append(partIndices, index+uint32(vertexOffset))
			}
			for name, column := range object.columns {
				existing, ok := columns[name]
				if !ok {
					existing = exportColumn{kind: column.kind, width: column.width}
				}
				existing.values = append(existing.values, column.values...)
				columns[name] = existing
			}
			vertexOffset += object.vertices
			draw.VertexCount = object.vertices
			draw.IndexCount = len(object.indices)
			draw.IndexOffset = indexOffset
			indexOffset += draw.IndexCount
			draws.Draws = append(draws.Draws, draw)
		}
		result.Parts = append(result.Parts, draws)
		allIndices = append(allIndices, partIndices...)
		if len(partIndices) == 0 {
			continue
		}
		ib := make([]byte, 0, len(partIndices)*4)
		for _, index := range partIndices {
			ib = appendExportValue(ib, exportUint32, float64(index))
		}
		result.Files[part.FullName+".ib"] = ib
	}
	result.VertexCount = vertexOffset

	if component.Outlines.Enabled && vertexOffset > 0 {
		warning, err := optimizeXXMIOutlines(component, layout, columns, allIndices, vertexOffset)
		if err != nil {
			return xxmiExportResult{}, err
		}
		if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
	}
	for _, name := range []string{"Position", "Blend", "Texcoord"} {
		semantics := buffers[name]
		if len(semantics) == 0 || vertexOffset == 0 {
			continue
		}
		out := make([]byte, 0, vertexOffset*xxmiStride(semantics))
		for vertex := range vertexOffset {
			for _, semantic := range semantics {
				column := columns[semantic.name()]
				for k := range semantic.format.values {
					value := 0.0
					if k < column.width && vertex < column.rows() {
						value = column.values[vertex*column.width+k]
					}
					out = appendExportValue(out, semantic.format.storage(), value)
				}
			}
		}
		result.Files[component.FullName+name+".buf"] = out
	}
	return result, nil
}

// optimizeXXMIOutlines is ModExporter.optimize_outlines: angle-weighted face normals welded by rounded
// position, written to TANGENT (Genshin, Star Rail) or, projected into tangent space, to TEXCOORD1 (Zenless).
func optimizeXXMIOutlines(
	component *xxmiExportComponent,
	layout xxmiLayout,
	columns map[string]exportColumn,
	indices []uint32,
	vertices int,
) (string, error) {
	positions, ok := columns["POSITION"]
	if !ok || positions.width < 3 || len(indices)%3 != 0 {
		return "", nil
	}
	vec := func(column exportColumn, row int) [3]float32 {
		base := row * column.width
		return [3]float32{
			float32(column.values[base]), float32(column.values[base+1]), float32(column.values[base+2]),
		}
	}
	sub := func(a, b [3]float32) [3]float32 { return [3]float32{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }
	cross := func(a, b [3]float32) [3]float32 {
		return [3]float32{
			float32(a[1]*b[2]) - float32(a[2]*b[1]),
			float32(a[2]*b[0]) - float32(a[0]*b[2]),
			float32(a[0]*b[1]) - float32(a[1]*b[0]),
		}
	}
	dot := func(a, b [3]float32) float32 {
		return float32(float32(a[0]*b[0])+float32(a[1]*b[1])) + float32(a[2]*b[2])
	}
	norm := func(a [3]float32) float32 { return float32(math.Sqrt(float64(dot(a, a)))) }
	unit := func(a [3]float32) [3]float32 {
		length := norm(a)
		if length == 0 {
			length = 1
		}
		return [3]float32{a[0] / length, a[1] / length, a[2] / length}
	}
	atan2 := func(y, x float32) float32 { return float32(math.Atan2(float64(y), float64(x))) }

	normals, hasNormals := columns["NORMAL"]
	useLoopNormals := component.Outlines.CustomNormals && hasNormals && normals.width >= 3
	scale := float32(math.Pow10(component.Outlines.Precision))
	// numpy.unique compares the rounded rows by value, so negative zero joins positive zero.
	round := func(value float32) float32 {
		rounded := float32(math.RoundToEven(float64(value*scale))) / scale
		if rounded == 0 {
			return 0
		}
		return rounded
	}
	type group [3]float32
	groups := make(map[group]int)
	loopGroups := make([]int, len(indices))
	var accumulated [][3]float32
	for triangle := 0; triangle < len(indices); triangle += 3 {
		corners := [3][3]float32{
			vec(positions, int(indices[triangle])),
			vec(positions, int(indices[triangle+1])),
			vec(positions, int(indices[triangle+2])),
		}
		edge0 := sub(corners[1], corners[2])
		edge1 := sub(corners[2], corners[0])
		edge2 := sub(corners[0], corners[1])
		cross01, cross12, cross20 := cross(edge0, edge1), cross(edge1, edge2), cross(edge2, edge0)
		angles := [3]float32{
			atan2(norm(cross12), -dot(edge1, edge2)),
			atan2(norm(cross20), -dot(edge2, edge0)),
			atan2(norm(cross01), -dot(edge0, edge1)),
		}
		face := unit(cross01)
		for corner := range 3 {
			loop := triangle + corner
			normal := face
			if useLoopNormals {
				normal = unit(vec(normals, int(indices[loop])))
			}
			weighted := [3]float32{normal[0] * angles[corner], normal[1] * angles[corner], normal[2] * angles[corner]}
			p := corners[corner]
			key := group{round(p[0]), round(p[1]), round(p[2])}
			index, seen := groups[key]
			if !seen {
				index = len(accumulated)
				groups[key] = index
				accumulated = append(accumulated, [3]float32{})
			}
			loopGroups[loop] = index
			sum := &accumulated[index]
			sum[0], sum[1], sum[2] = sum[0]+weighted[0], sum[1]+weighted[1], sum[2]+weighted[2]
		}
	}
	outline := make([][3]float32, vertices)
	for loop, index := range indices {
		outline[index] = unit(accumulated[loopGroups[loop]])
	}

	target := xxmiSemantic{semantic: "TANGENT"}
	result := exportColumn{kind: exportFloat32, width: 3, values: make([]float64, vertices*3)}
	if component.Game == xxmiZenless {
		target = xxmiSemantic{semantic: "TEXCOORD", index: 1}
		tangents, okTangent := columns["TANGENT"]
		signs, okSign := columns["BITANGENTSIGN"]
		if !okTangent || !okSign || !hasNormals {
			return "", fmt.Errorf("component %s: outline projection needs NORMAL, TANGENT and BITANGENTSIGN",
				component.FullName)
		}
		gate := float32(component.Outlines.GateDivergence)
		degrees := float32(180 / math.Pi)
		result = exportColumn{kind: exportFloat32, width: 2, values: make([]float64, vertices*2)}
		for vertex := range vertices {
			tangent := unit(vec(tangents, vertex))
			normal := vec(normals, vertex)
			sign := float32(signs.values[vertex*signs.width])
			bitangent := cross(normal, tangent)
			bitangent = [3]float32{bitangent[0] * sign, bitangent[1] * sign, bitangent[2] * sign}
			aligned := max(-1, min(1, dot(outline[vertex], normal)))
			divergence := float32(math.Acos(float64(aligned))) * degrees
			fade := float32(0)
			if divergence >= gate {
				fade = 1
			}
			result.values[vertex*2] = float64(dot(tangent, outline[vertex]) * fade)
			result.values[vertex*2+1] = float64(dot(bitangent, outline[vertex]) * fade)
		}
	} else {
		for vertex, vector := range outline {
			for k := range 3 {
				result.values[vertex*3+k] = float64(vector[k])
			}
		}
	}
	for _, semantic := range layout.all() {
		if semantic.same(target) {
			columns[semantic.name()] = exportEncode(semantic.format, result)
			return "", nil
		}
	}
	return "Semantic not found in the buffer layout. Skipping outline optimization.", nil
}
