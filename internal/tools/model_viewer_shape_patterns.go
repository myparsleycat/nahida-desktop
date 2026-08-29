package tools

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	modelViewerSimpleShapeValueRE  = regexp.MustCompile(`(?i)^x\d+\s*=\s*\$(\w+)\s*$`)
	modelViewerSimpleShapeBufferRE = regexp.MustCompile(`(?i)^cs-t\d+\s*=\s*copy\s+(\S+)\s*$`)
	modelViewerShapeX88RE          = regexp.MustCompile(`(?i)^x88\s*=\s*(.+?)\s*$`)
	modelViewerShapeDirectValueRE  = regexp.MustCompile(`(?i)^\$(\w+)$`)
	modelViewerShapeNegativeRE     = regexp.MustCompile(`(?i)^\$(\w+)\s*\*\s*-1$`)
	modelViewerShapeBuffer50RE     = regexp.MustCompile(`(?i)^cs-t(50|51)\s*=\s*copy\s+(\S+)\s*$`)
	modelViewerShapeRemapRE        = regexp.MustCompile(`(?i)^\$(\w+)\s*=\s*\(?\s*\$(\w+)\s*\*\s*2\s*-\s*1\s*\)?\s*$`)
	modelViewerShapeSliderRE       = regexp.MustCompile(`(?i)^x87\s*=\s*\$(\w+)\s*\*\s*x87\s*$`)
	modelViewerShapeSliderAnyRE    = regexp.MustCompile(`(?i)^x87\s*=.*\$(\w+)\s*$`)
	modelViewerSparseIDRE          = regexp.MustCompile(`(?i)^\$\\WWMIv1\\shapekey_id\s*=\s*(\d+)\s*$`)
	modelViewerSparseValueRE       = regexp.MustCompile(`(?i)^\$\\WWMIv1\\shapekey_value\s*=\s*\$(\w+)\s*$`)
	modelViewerSparseBindRE        = regexp.MustCompile(`(?i)^(cs-t(?:0|1|6|33))\s*=\s*(?:copy\s+|ref\s+)?(\S+)\s*$`)
	modelViewerSparseBatchRE       = regexp.MustCompile(`(?i)^global\s+\$shapekey_vertex_offset_batch(\d+)\s*=\s*(\d+)\s*$`)
	modelViewerMultiBufferRE       = regexp.MustCompile(`(?i)^cs-t(5[0-4])\s*=\s*copy\s+(\S+)\s*$`)
	modelViewerMultiScalarRE       = regexp.MustCompile(`(?i)^x(88|89)\s*=\s*\$(\w+)\s*$`)
)

func collectAdditionalModelViewerShapeKeys(sections []modINISection, resources map[string]modelViewerResource, modDir string) []modelViewerShapeKey {
	var output []modelViewerShapeKey
	output = append(output, collectSimpleModelViewerShapeKeys(sections, resources, modDir)...)
	output = append(output, collectX88ModelViewerShapeKeys(sections, resources, modDir)...)
	output = append(output, collectSparseModelViewerShapeKeys(sections, resources, modDir)...)
	output = append(output, collectMultiModelViewerShapeKeys(sections, resources, modDir)...)
	seen := make(map[string]bool)
	kept := output[:0]
	for _, shape := range output {
		if len(shape.Dimensions) == 0 {
			continue
		}
		dimension := shape.Dimensions[0]
		key := modelViewerNormalizeKey(dimension.VariableID) + "|" + strings.ToLower(shape.BasePath) + "|" + strings.ToLower(dimension.SmallerPath) + "|" + strings.ToLower(dimension.BiggerPath) + "|" + strconv.FormatBool(dimension.Sparse)
		if seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, shape)
	}
	return kept
}

func collectSimpleModelViewerShapeKeys(sections []modINISection, resources map[string]modelViewerResource, modDir string) []modelViewerShapeKey {
	var output []modelViewerShapeKey
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") {
			continue
		}
		variable := ""
		var buffers []string
		for _, raw := range section.Lines {
			line := cleanModelViewerShapeLine(raw)
			if variable == "" {
				if match := modelViewerSimpleShapeValueRE.FindStringSubmatch(line); match != nil {
					variable = modelViewerNormalizeKey(match[1])
				}
			}
			if match := modelViewerSimpleShapeBufferRE.FindStringSubmatch(line); match != nil {
				buffers = append(buffers, match[1])
			}
		}
		if variable == "" || len(buffers) < 2 || !strings.HasSuffix(strings.ToLower(buffers[0]), ".base") {
			continue
		}
		if shape, ok := directModelViewerShape(variable, buffers[0], "", buffers[1], "", resources, modDir); ok {
			output = append(output, shape)
		}
	}
	return output
}

func collectX88ModelViewerShapeKeys(sections []modINISection, resources map[string]modelViewerResource, modDir string) []modelViewerShapeKey {
	remaps := make(map[string]string)
	authoredSliders := make(map[string]bool)
	for _, section := range sections {
		for _, raw := range section.Lines {
			line := cleanModelViewerShapeLine(raw)
			if strings.EqualFold(section.Header, "CommandList") && strings.HasPrefix(strings.ToLower(section.Name), "drawslider") {
				if match := modelViewerShapeSliderRE.FindStringSubmatch(line); match != nil {
					authoredSliders[modelViewerNormalizeKey(match[1])] = true
				}
			}
			if match := modelViewerShapeRemapRE.FindStringSubmatch(line); match != nil {
				remaps[modelViewerNormalizeKey(match[1])] = modelViewerNormalizeKey(match[2])
			}
		}
	}
	var output []modelViewerShapeKey
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") {
			continue
		}
		baseName, variable, side := "", "", ""
		type pair struct{ variable, base, low, high string }
		pairs := make(map[string]pair)
		var candidates []pair
		for _, raw := range section.Lines {
			line := cleanModelViewerShapeLine(raw)
			if match := modelViewerShapeX88RE.FindStringSubmatch(line); match != nil {
				value := strings.TrimSpace(match[1])
				direct := modelViewerShapeDirectValueRE.FindStringSubmatch(value)
				negative := modelViewerShapeNegativeRE.FindStringSubmatch(value)
				switch {
				case direct != nil:
					variable, side = modelViewerNormalizeKey(direct[1]), "high"
				case negative != nil:
					variable, side = modelViewerNormalizeKey(negative[1]), "low"
				default:
					variable, side = "", ""
				}
				continue
			}
			match := modelViewerShapeBuffer50RE.FindStringSubmatch(line)
			if match == nil {
				continue
			}
			if match[1] == "50" {
				baseName = match[2]
				continue
			}
			if baseName == "" || variable == "" {
				continue
			}
			if original := remaps[variable]; original != "" {
				item := pairs[variable]
				item.variable, item.base = original, baseName
				if side == "low" {
					item.low = match[2]
				} else {
					item.high = match[2]
				}
				pairs[variable] = item
			} else {
				candidates = append(candidates, pair{variable: variable, base: baseName, high: match[2]})
			}
		}
		complete := make([]pair, 0, len(pairs))
		for _, item := range pairs {
			if item.low != "" && item.high != "" {
				complete = append(complete, item)
			}
		}
		if len(candidates)+len(complete) < 2 {
			continue
		}
		baseKeys := make(map[string]bool)
		for _, item := range candidates {
			baseKeys[strings.ToLower(item.base)] = true
		}
		for _, item := range complete {
			baseKeys[strings.ToLower(item.base)] = true
		}
		if len(baseKeys) != 1 {
			continue
		}
		for _, item := range candidates {
			if len(authoredSliders) > 0 && !authoredSliders[item.variable] {
				continue
			}
			base := runtimeModelViewerShapeBaseName(item.base, resources)
			if shape, ok := directModelViewerShape(item.variable, base, "", item.high, "", resources, modDir); ok {
				output = append(output, shape)
			}
		}
		for _, item := range complete {
			if len(authoredSliders) > 0 && !authoredSliders[item.variable] {
				continue
			}
			base := runtimeModelViewerShapeBaseName(item.base, resources)
			if shape, ok := directModelViewerShape(item.variable, base, item.low, item.high, "midpoint_pair", resources, modDir); ok {
				output = append(output, shape)
			}
		}
	}
	return output
}

func collectSparseModelViewerShapeKeys(sections []modINISection, resources map[string]modelViewerResource, modDir string) []modelViewerShapeKey {
	bindings := make(map[string]string)
	shapeIDs := make(map[string]int)
	batchOffsets := make(map[int]int)
	var sliders []string
	for _, section := range sections {
		pendingID := -1
		for _, raw := range section.Lines {
			line := cleanModelViewerShapeLine(raw)
			if strings.EqualFold(section.Header, "CommandList") && strings.HasPrefix(strings.ToLower(section.Name), "drawslider") {
				if match := modelViewerShapeSliderRE.FindStringSubmatch(line); match != nil {
					sliders = append(sliders, modelViewerNormalizeKey(match[1]))
				}
			}
			if match := modelViewerSparseBatchRE.FindStringSubmatch(line); match != nil {
				batch, _ := strconv.Atoi(match[1])
				offset, _ := strconv.Atoi(match[2])
				batchOffsets[batch] = offset
			}
			if match := modelViewerSparseIDRE.FindStringSubmatch(line); match != nil {
				pendingID, _ = strconv.Atoi(match[1])
				continue
			}
			if match := modelViewerSparseValueRE.FindStringSubmatch(line); match != nil && pendingID >= 0 {
				shapeIDs[modelViewerNormalizeKey(match[1])] = pendingID
				pendingID = -1
			}
			if match := modelViewerSparseBindRE.FindStringSubmatch(line); match != nil && bindings[strings.ToLower(match[1])] == "" {
				bindings[strings.ToLower(match[1])] = match[2]
			}
		}
	}
	base, baseOK := lookupModelViewerShapeResource(resources, bindings["cs-t6"])
	offsets, offsetsOK := lookupModelViewerShapeResource(resources, bindings["cs-t33"])
	vertexIDs, vertexIDsOK := lookupModelViewerShapeResource(resources, bindings["cs-t0"])
	deltas, deltasOK := lookupModelViewerShapeResource(resources, bindings["cs-t1"])
	if !baseOK || !offsetsOK || !vertexIDsOK || !deltasOK || base.Filename == "" || offsets.Filename == "" || vertexIDs.Filename == "" || deltas.Filename == "" {
		return nil
	}
	var output []modelViewerShapeKey
	seen := make(map[string]bool)
	for _, variable := range sliders {
		shapeID, ok := shapeIDs[variable]
		if !ok || seen[variable] {
			continue
		}
		seen[variable] = true
		batch := shapeID / 127
		dimension := modelViewerShapeKeyDimension{
			VariableID: variable, Sparse: true, BufferShapeID: shapeID + batch, SparseOffset: batchOffsets[batch],
			OffsetPath: shapeResourcePath(modDir, offsets), VertexIDPath: shapeResourcePath(modDir, vertexIDs), VertexDeltaPath: shapeResourcePath(modDir, deltas),
		}
		stride := base.Stride
		if stride == 0 {
			stride = 12
		}
		output = append(output, modelViewerShapeKey{BasePath: shapeResourcePath(modDir, base), VertexStride: stride, Dimensions: []modelViewerShapeKeyDimension{dimension}})
	}
	return output
}

func collectMultiModelViewerShapeKeys(sections []modINISection, resources map[string]modelViewerResource, modDir string) []modelViewerShapeKey {
	menuVars := make(map[string]bool)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CommandList") {
			continue
		}
		for _, raw := range section.Lines {
			if match := modelViewerShapeSliderAnyRE.FindStringSubmatch(cleanModelViewerShapeLine(raw)); match != nil {
				menuVars[modelViewerNormalizeKey(match[1])] = true
			}
		}
	}
	var output []modelViewerShapeKey
	for _, section := range sections {
		current := make(map[int]string)
		var sets []map[int]string
		scalars := make(map[int]string)
		flush := func() {
			if len(current) == 5 {
				sets = append(sets, current)
			}
			current = make(map[int]string)
		}
		for _, raw := range section.Lines {
			line := cleanModelViewerShapeLine(raw)
			if match := modelViewerMultiBufferRE.FindStringSubmatch(line); match != nil {
				slot, _ := strconv.Atoi(match[1])
				slot -= 50
				if slot == 0 {
					flush()
				}
				current[slot] = match[2]
				continue
			}
			if match := modelViewerMultiScalarRE.FindStringSubmatch(line); match != nil {
				register, _ := strconv.Atoi(match[1])
				scalars[register] = modelViewerNormalizeKey(match[2])
			}
		}
		flush()
		for _, set := range sets {
			for register, slots := range map[int][2]int{88: {1, 2}, 89: {3, 4}} {
				variable := scalars[register]
				if variable == "" || !menuVars[variable] {
					continue
				}
				if shape, ok := directModelViewerShape(variable, set[0], set[slots[1]], set[slots[0]], "midpoint_pair", resources, modDir); ok {
					output = append(output, shape)
				}
			}
		}
	}
	return output
}

func directModelViewerShape(variable, baseName, lowName, highName, mode string, resources map[string]modelViewerResource, modDir string) (modelViewerShapeKey, bool) {
	base, baseOK := lookupModelViewerShapeResource(resources, baseName)
	high, highOK := lookupModelViewerShapeResource(resources, highName)
	if !baseOK || !highOK || base.Filename == "" || high.Filename == "" {
		return modelViewerShapeKey{}, false
	}
	stride := base.Stride
	if stride == 0 {
		stride = 40
	}
	if high.Stride != 0 && high.Stride != stride || stride < 12 {
		return modelViewerShapeKey{}, false
	}
	if strings.EqualFold(base.Filename, high.Filename) {
		return modelViewerShapeKey{}, false
	}
	dimension := modelViewerShapeKeyDimension{VariableID: modelViewerNormalizeKey(variable), Mode: mode, BiggerPath: shapeResourcePath(modDir, high)}
	if lowName != "" {
		low, lowOK := lookupModelViewerShapeResource(resources, lowName)
		if !lowOK || low.Filename == "" || low.Stride != 0 && low.Stride != stride {
			return modelViewerShapeKey{}, false
		}
		dimension.SmallerPath = shapeResourcePath(modDir, low)
	}
	return modelViewerShapeKey{BasePath: shapeResourcePath(modDir, base), VertexStride: stride, PositionOffset: 0, NormalOffset: 12, TangentOffset: 24, Dimensions: []modelViewerShapeKeyDimension{dimension}}, true
}

func runtimeModelViewerShapeBaseName(name string, resources map[string]modelViewerResource) string {
	if !strings.HasSuffix(strings.ToLower(name), ".b") {
		return name
	}
	runtimeName := name[:len(name)-2]
	if resource, ok := lookupModelViewerShapeResource(resources, runtimeName); ok && resource.Filename != "" {
		return runtimeName
	}
	return name
}

func lookupModelViewerShapeResource(resources map[string]modelViewerResource, name string) (modelViewerResource, bool) {
	resource, ok := resources[modelViewerNormalizeKey(modelViewerTrimResourcePrefix(name))]
	return resource, ok
}

func shapeResourcePath(modDir string, resource modelViewerResource) string {
	return filepath.Join(modDir, filepath.FromSlash(resource.Filename))
}

func cleanModelViewerShapeLine(raw string) string {
	if comment := strings.Index(raw, ";"); comment >= 0 {
		raw = raw[:comment]
	}
	return strings.TrimSpace(raw)
}
