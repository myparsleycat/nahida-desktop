package tools

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxHintDecodeBytes = 32 * 1024 * 1024
	maxHintDecodeArea  = 4096 * 4096
)

var (
	wwmiDumpTexRE        = regexp.MustCompile(`(?i)(?:^|[/\\])Components-(\d+(?:-\d+)*)\s+t=`)
	wwmiComponentIndexRE = regexp.MustCompile(`(?i)^Component(\d+)$`)
	pngSignature         = []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}
)

var (
	ddsPackedFourCC = map[string]bool{"ATI1": true, "ATI2": true, "BC4U": true, "BC4S": true, "BC5U": true, "BC5S": true}
	ddsPackedDXGI   = map[uint32]bool{80: true, 81: true, 83: true, 84: true}
)

type wwmiTextureHint struct {
	SRGB           bool
	ColorSpace     string
	Area           int
	Bytes          int64
	IsLikelyFlat   bool
	IsLikelyNormal bool
	IsLikelyPacked bool
}

type wwmiDumpCandidate struct {
	File  string
	SRGB  bool
	Area  int
	Bytes int64
	Order int
}

type wwmiRGBAAnalysis struct {
	channelRangeMax uint32
	luminanceStdDev float64
	meanR           float64
	meanG           float64
	meanB           float64
	blueDominance   float64
}

func isLikelyWwmiDiffuse(hint wwmiTextureHint) bool {
	if hint.ColorSpace == "linear" {
		return false
	}
	return !hint.IsLikelyFlat && !hint.IsLikelyNormal && !hint.IsLikelyPacked
}

func pickWwmiDumpDiffuse(candidates []wwmiDumpCandidate) string {
	if len(candidates) == 0 {
		return ""
	}
	ranked := append([]wwmiDumpCandidate(nil), candidates...)
	sort.SliceStable(ranked, func(i, j int) bool {
		left, right := ranked[i], ranked[j]
		if leftShare, rightShare := wwmiDumpShareCount(left.File), wwmiDumpShareCount(right.File); leftShare != rightShare {
			return leftShare < rightShare
		}
		if left.SRGB != right.SRGB {
			return left.SRGB
		}
		if left.Area != right.Area {
			return left.Area > right.Area
		}
		if left.Bytes != right.Bytes {
			return left.Bytes > right.Bytes
		}
		return left.Order < right.Order
	})
	return ranked[0].File
}

func wwmiDumpShareCount(file string) int {
	file = strings.ReplaceAll(file, `\`, "/")
	match := wwmiDumpTexRE.FindStringSubmatch(file)
	if len(match) < 2 {
		return 1
	}
	return len(strings.Split(match[1], "-"))
}

func wwmiComponentIndex(name string) string {
	match := wwmiComponentIndexRE.FindStringSubmatch(name)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func attachWwmiDumpTextures(meshes []modelViewerDirectMesh, resources []modelViewerResource, modDir string) {
	targets := make([]*modelViewerDirectMesh, 0)
	for index := range meshes {
		mesh := &meshes[index]
		if wwmiComponentIndex(mesh.component) == "" {
			continue
		}
		if mesh.textureAuthored {
			continue
		}
		targets = append(targets, mesh)
	}
	if len(targets) == 0 {
		return
	}

	hintCache := make(map[string]*wwmiTextureHint)
	inspect := func(relative string) *wwmiTextureHint {
		resolved := viewerResourcePath(modDir, relative)
		if resolved == "" {
			return nil
		}
		if cached, ok := hintCache[resolved]; ok {
			return cached
		}
		hint := inspectWwmiTextureHint(resolved)
		hintCache[resolved] = hint
		return hint
	}

	needsDump := make(map[string]bool)
	for _, mesh := range targets {
		index := wwmiComponentIndex(mesh.component)
		if index == "" {
			continue
		}
		if mesh.textureAuthored {
			continue
		}
		hint := inspect(mesh.textureDefaultFile)
		if hint == nil || !isLikelyWwmiDiffuse(*hint) {
			needsDump[index] = true
		}
	}

	excludedByIndex := make(map[string]map[string]bool)
	for _, mesh := range targets {
		index := wwmiComponentIndex(mesh.component)
		if index == "" || !needsDump[index] {
			continue
		}
		if excludedByIndex[index] == nil {
			excludedByIndex[index] = make(map[string]bool)
		}
		for _, file := range mesh.nonDiffuseTextureFiles {
			excludedByIndex[index][slashPath(file)] = true
		}
	}

	filesByIndex := make(map[string][]string)
	for _, resource := range resources {
		file := resource.Filename
		match := wwmiDumpTexRE.FindStringSubmatch(slashPath(file))
		if file == "" || match == nil {
			continue
		}
		for _, index := range strings.Split(match[1], "-") {
			if !needsDump[index] || excludedByIndex[index][slashPath(file)] {
				continue
			}
			filesByIndex[index] = append(filesByIndex[index], file)
		}
	}

	pickedByIndex := make(map[string]string)
	for index, files := range filesByIndex {
		var scored []wwmiDumpCandidate
		for order, file := range files {
			hint := inspect(file)
			if hint == nil || !isLikelyWwmiDiffuse(*hint) {
				continue
			}
			scored = append(scored, wwmiDumpCandidate{File: file, SRGB: hint.SRGB, Area: hint.Area, Bytes: hint.Bytes, Order: order})
		}
		if picked := pickWwmiDumpDiffuse(scored); picked != "" {
			pickedByIndex[index] = picked
		}
	}

	resourceByFile := make(map[string]string)
	for _, resource := range resources {
		if resource.Filename != "" {
			resourceByFile[slashPath(resource.Filename)] = resource.Name
		}
	}

	for _, mesh := range targets {
		dumpPick := pickedByIndex[wwmiComponentIndex(mesh.component)]
		if mesh.textureAuthored {
			continue
		}
		currentHint := inspect(mesh.textureDefaultFile)
		if currentHint != nil && isLikelyWwmiDiffuse(*currentHint) {
			mesh.textureAssignments = keepLikelyDiffuseAssignments(mesh.textureAssignments, inspect)
			continue
		}
		if dumpPick == "" {
			continue
		}
		resourceName := resourceByFile[slashPath(dumpPick)]
		kept := mesh.textureAssignments[:0]
		for _, assignment := range mesh.textureAssignments {
			if assignment.role != "diffuse" {
				kept = append(kept, assignment)
			}
		}
		mesh.textureAssignments = append(kept, modelViewerDirectTextureAssignment{role: "diffuse", resource: resourceName, file: dumpPick, conditions: modelViewerDNFTrue()})
		mesh.textureDefaultFile = dumpPick
	}
}

func keepLikelyDiffuseAssignments(assignments []modelViewerDirectTextureAssignment, inspect func(string) *wwmiTextureHint) []modelViewerDirectTextureAssignment {
	var kept []modelViewerDirectTextureAssignment
	for _, assignment := range assignments {
		if assignment.role != "diffuse" {
			kept = append(kept, assignment)
			continue
		}
		hint := inspect(assignment.file)
		if hint != nil && isLikelyWwmiDiffuse(*hint) {
			kept = append(kept, assignment)
		}
	}
	return kept
}

func inspectWwmiTextureHint(filePath string) *wwmiTextureHint {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil
	}
	size := info.Size()
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".png" {
		area := pngIhdrArea(readFilePrefix(filePath, 24))
		if size > maxHintDecodeBytes || area > maxHintDecodeArea {
			return &wwmiTextureHint{SRGB: true, ColorSpace: "srgb", Area: area, Bytes: size}
		}
		raw, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return nil
		}
		decoded, decodeErr := png.Decode(bytes.NewReader(raw))
		if decodeErr != nil {
			return nil
		}
		analysis := analyzeRGBAImage(decoded)
		return hintFromAnalysis(analysis, "srgb", decoded.Bounds().Dx()*decoded.Bounds().Dy(), size)
	}
	if ext == ".jpg" || ext == ".jpeg" {
		return &wwmiTextureHint{SRGB: true, ColorSpace: "srgb", Area: 0, Bytes: size}
	}
	if ext != ".dds" || size < 128 {
		return &wwmiTextureHint{ColorSpace: "unknown", Bytes: size}
	}

	header := readFilePrefix(filePath, int(min(int64(148), size)))
	if len(header) < 128 {
		return &wwmiTextureHint{ColorSpace: "unknown", Bytes: size}
	}
	area := int(binary.LittleEndian.Uint32(header[16:20]) * binary.LittleEndian.Uint32(header[12:16]))
	fourcc := string(header[84:88])
	dxgi := uint32(0xFFFFFFFF)
	if fourcc == "DX10" && len(header) >= 132 {
		dxgi = binary.LittleEndian.Uint32(header[128:132])
	}
	srgbState := parseDdsSrgbState(header)
	colorSpace := "unknown"
	if srgbState != nil {
		if *srgbState {
			colorSpace = "srgb"
		} else {
			colorSpace = "linear"
		}
	}
	packedFormat := ddsPackedFourCC[fourcc] || ddsPackedDXGI[dxgi]
	if colorSpace == "linear" || packedFormat || area > maxHintDecodeArea || size > maxHintDecodeBytes {
		return &wwmiTextureHint{SRGB: colorSpace == "srgb", ColorSpace: colorSpace, Area: area, Bytes: size, IsLikelyNormal: packedFormat, IsLikelyPacked: packedFormat}
	}
	raw, readErr := os.ReadFile(filePath)
	if readErr != nil {
		return nil
	}
	decoded, decodeErr := decodeModelViewerDDS(raw)
	if decodeErr != nil {
		return nil
	}
	return hintFromAnalysis(analyzeRGBAImage(decoded), colorSpace, area, size)
}

func parseDdsSrgbState(header []byte) *bool {
	if len(header) < 148 || string(header[:4]) != "DDS " {
		return nil
	}
	if string(header[84:88]) != "DX10" {
		return nil
	}
	dxgi := binary.LittleEndian.Uint32(header[128:132])
	switch dxgi {
	case 29, 72, 75, 78, 91, 93, 99:
		value := true
		return &value
	case 28, 71, 74, 77, 80, 83, 87, 88, 95, 98:
		value := false
		return &value
	default:
		return nil
	}
}

func pngIhdrArea(header []byte) int {
	if len(header) < 24 || !bytes.Equal(header[:8], pngSignature) || string(header[12:16]) != "IHDR" {
		return 0
	}
	width := binary.BigEndian.Uint32(header[16:20])
	height := binary.BigEndian.Uint32(header[20:24])
	if width == 0 || height == 0 {
		return 0
	}
	return int(width * height)
}

func hintFromAnalysis(analysis wwmiRGBAAnalysis, colorSpace string, area int, size int64) *wwmiTextureHint {
	means := []float64{analysis.meanR, analysis.meanG, analysis.meanB}
	packed := false
	hasLow := false
	hasMid := false
	for _, value := range means {
		if value <= 0.04 {
			hasLow = true
		}
		if absFloat(value-0.5) <= 0.15 {
			hasMid = true
		}
	}
	packed = hasLow && hasMid
	return &wwmiTextureHint{
		SRGB:       colorSpace == "srgb",
		ColorSpace: colorSpace,
		Area:       area,
		Bytes:      size,
		IsLikelyFlat: analysis.channelRangeMax <= 12 ||
			(analysis.luminanceStdDev <= 0.035 && analysis.channelRangeMax <= 24) ||
			analysis.luminanceStdDev <= 0.012,
		IsLikelyNormal: analysis.meanB >= 0.7 &&
			absFloat(analysis.meanR-0.5) <= 0.18 &&
			absFloat(analysis.meanG-0.5) <= 0.18 &&
			analysis.blueDominance >= 0.12 &&
			analysis.channelRangeMax <= 72 &&
			analysis.luminanceStdDev <= 0.12,
		IsLikelyPacked: packed,
	}
}

func analyzeRGBAImage(img image.Image) wwmiRGBAAnalysis {
	bounds := img.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	rgba := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(rgba, rgba.Bounds(), img, bounds.Min, draw.Src)
	return analyzeRGBA(rgba.Pix, width, height)
}

func analyzeRGBA(data []byte, width, height int) wwmiRGBAAnalysis {
	pixelCount := width * height
	expectedLen := pixelCount * 4
	if len(data) < expectedLen {
		return wwmiRGBAAnalysis{}
	}
	var minR, minG, minB byte = 255, 255, 255
	var maxR, maxG, maxB byte
	sampleCount := 0
	sumR, sumG, sumB := 0.0, 0.0, 0.0
	luminanceSum, luminanceSquareSum := 0.0, 0.0
	stride := int(math.Floor(math.Sqrt(float64(width*height) / 4096.0)))
	if stride < 1 {
		stride = 1
	}
	for y := 0; y < height; y += stride {
		for x := 0; x < width; x += stride {
			offset := (y*width + x) * 4
			r, g, b := data[offset], data[offset+1], data[offset+2]
			if r < minR {
				minR = r
			}
			if g < minG {
				minG = g
			}
			if b < minB {
				minB = b
			}
			if r > maxR {
				maxR = r
			}
			if g > maxG {
				maxG = g
			}
			if b > maxB {
				maxB = b
			}
			sumR += float64(r)
			sumG += float64(g)
			sumB += float64(b)
			luminance := (0.2126*float64(r) + 0.7152*float64(g) + 0.0722*float64(b)) / 255.0
			luminanceSum += luminance
			luminanceSquareSum += luminance * luminance
			sampleCount++
		}
	}
	mean := 0.0
	variance := 0.0
	meanR, meanG, meanB := 0.0, 0.0, 0.0
	if sampleCount > 0 {
		mean = luminanceSum / float64(sampleCount)
		variance = luminanceSquareSum/float64(sampleCount) - mean*mean
		if variance < 0 {
			variance = 0
		}
		meanR = sumR / float64(sampleCount) / 255.0
		meanG = sumG / float64(sampleCount) / 255.0
		meanB = sumB / float64(sampleCount) / 255.0
	}
	rangeMax := uint32(maxR) - uint32(minR)
	if gRange := uint32(maxG) - uint32(minG); gRange > rangeMax {
		rangeMax = gRange
	}
	if bRange := uint32(maxB) - uint32(minB); bRange > rangeMax {
		rangeMax = bRange
	}
	return wwmiRGBAAnalysis{
		channelRangeMax: rangeMax,
		luminanceStdDev: math.Sqrt(variance),
		meanR:           meanR,
		meanG:           meanG,
		meanB:           meanB,
		blueDominance:   meanB - maxFloat(meanR, meanG),
	}
}

func readFilePrefix(path string, length int) []byte {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = file.Close() }()
	buf := make([]byte, length)
	n, _ := file.Read(buf)
	return buf[:n]
}

func viewerResourcePath(modDir, relative string) string {
	relative = strings.TrimSpace(relative)
	if relative == "" {
		return ""
	}
	cleaned := filepath.Clean(filepath.Join(modDir, filepath.FromSlash(relative)))
	rel, err := filepath.Rel(modDir, cleaned)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return ""
	}
	return cleaned
}

func slashPath(value string) string {
	return strings.ReplaceAll(value, `\`, "/")
}

func absFloat(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}
