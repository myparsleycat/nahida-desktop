package modelviewer

import (
	"cmp"
	"os"
	"regexp"
	"strings"

	"nahida.live/desktop/internal/platform"
)

const maxModelViewerResourceAliasDepth = 32

var (
	modelViewerUAVSlotRE    = regexp.MustCompile(`(?i)^cs-u\d+$`)
	modelViewerVB0CaptureRE = regexp.MustCompile(`(?i)^(?:(?:copy|ref)\s+)?vb0(?:\s+unless_null)?$`)
)

type modelViewerResourceAliases struct {
	descriptors map[string][]string
	defaultUAV  map[string][]string
	outputs     map[string][]string
	// Texture copies can supply the default image for a runtime blend. Vertex
	// captures have one bind-pose source across the preview's toggle states.
	defaultCopies map[string][]string
	copies        map[string][]string
	// Bind-pose inputs of compute outputs a draw binds directly.
	computeInputs map[string][]string
	resources     map[string]modelViewerResource
}

type modelViewerResourceResolution uint8

const (
	modelViewerResourceUnresolved modelViewerResourceResolution = iota
	modelViewerResourceResolved
	modelViewerResourceInvalid
)

func resolveModelViewerEffectiveResources(
	sections []modINISection,
	resources []modelViewerResource,
) []modelViewerResource {
	aliases := collectModelViewerResourceAliases(sections, resources)
	output := append([]modelViewerResource(nil), resources...)
	for index := range output {
		resource := output[index]
		if resource.Filename != "" {
			continue
		}
		resolved, ok := aliases.resolve(resource.Name)
		if !ok || resolved.Filename == "" || resolved.Stride < 12 && !modelViewerResourceIsTexture(resource) {
			continue
		}
		output[index].Filename = resolved.Filename
		output[index].Stride = resolved.Stride
		output[index].Format = resolved.Format
		output[index].sourceName = cmp.Or(resolved.sourceName, resolved.Name)
	}
	return output
}

func resolveModelViewerEffectiveResourcesAt(
	root, baseDir string,
	sections []modINISection,
	resources []modelViewerResource,
	cache *modelViewerBufferCache,
) []modelViewerResource {
	candidates := append([]modelViewerResource(nil), resources...)
	for index, resource := range candidates {
		if resource.Filename == "" {
			continue
		}
		path, err := cache.resourcePath(root, baseDir, resource.Filename)
		if err != nil || !platform.SameOrChildPath(root, path) {
			continue
		}

		// Compute outputs can declare a file the mod omits and replace it at
		// runtime. Follow their resource aliases only when that file is missing.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			candidates[index].Filename = ""
		}
	}

	output := resolveModelViewerEffectiveResources(sections, candidates)
	for index := range output {
		if candidates[index].Filename != "" {
			continue
		}
		if output[index].Filename == "" {
			output[index] = resources[index]
			continue
		}
		path, err := cache.resourcePath(root, baseDir, output[index].Filename)
		if err != nil || !platform.SameOrChildPath(root, path) {
			output[index] = resources[index]
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			output[index] = resources[index]
		}
	}
	return output
}

func collectModelViewerResourceAliases(
	sections []modINISection,
	resources []modelViewerResource,
) modelViewerResourceAliases {
	aliases := modelViewerResourceAliases{
		descriptors:   make(map[string][]string),
		defaultUAV:    make(map[string][]string),
		outputs:       make(map[string][]string),
		defaultCopies: make(map[string][]string),
		copies:        make(map[string][]string),
		computeInputs: make(map[string][]string),
		resources:     make(map[string]modelViewerResource, len(resources)),
	}
	for _, resource := range resources {
		aliases.resources[modelViewerNormalizeKey(resource.Name)] = resource
	}
	variables := modelViewerDirectConditionVariables(sections, collectModelViewerDefaultVariables(sections))
	defaultState := make(map[string]string)
	for key, value := range variables {
		if !strings.HasPrefix(key, "__") {
			defaultState[modelViewerNormalizeKey(key)] = modelViewerString(value)
		}
	}
	vertexPositions := make(map[string][]string)
	vertexCaptures := make(map[string][]string)
	positionRuns := make(map[string][]string)
	for _, section := range sections {
		uav := make(map[string]string)
		defaultSlots := make(map[string]string)
		family := ""
		if strings.EqualFold(section.Header, "TextureOverride") && !isModelViewerPreviewSkippedOverride(section.Name) {
			family = extractSectionHash(section.Name)
		}
		_, positionOverride := trimModelViewerOverrideSuffix(section.Name, "Position")
		inheritsVB0 := family != ""
		var stack []modelViewerSymbolicBranchFrame
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				branch := parseModelViewerConditionDNF(
					strings.TrimSpace(line[3:]),
					modelViewerAliases(variables),
					variables,
				)
				stack = append(stack, modelViewerSymbolicBranchFrame{current: branch, seen: branch})
				continue
			case strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if "):
				if len(stack) == 0 {
					continue
				}
				expression := strings.TrimSpace(line[5:])
				if strings.HasPrefix(lower, "else if ") {
					expression = strings.TrimSpace(line[8:])
				}
				branch := parseModelViewerConditionDNF(expression, modelViewerAliases(variables), variables)
				frame := &stack[len(stack)-1]
				frame.current = modelViewerDNFAnd(modelViewerDNFNot(frame.seen), branch)
				frame.seen = modelViewerDNFOr(frame.seen, branch)
				continue
			case lower == "else":
				if len(stack) > 0 {
					frame := &stack[len(stack)-1]
					frame.current = modelViewerDNFNot(frame.seen)
				}
				continue
			case lower == "endif":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				continue
			}
			defaultActive := modelViewerDNFSatisfied(modelViewerSymbolicStackConditions(stack), defaultState)
			left, right, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			left = strings.TrimSpace(left)
			right = strings.TrimSpace(right)
			plainLeft := strings.ToLower(modelViewerAssignmentTarget(left))
			if family != "" && positionOverride {
				switch plainLeft {
				case "vb0":
					aliases.add(vertexPositions, family, modelViewerResourceToken(right))
				case "run":
					aliases.add(positionRuns, family, right)
				}
			}
			if plainLeft == "vb0" || plainLeft == "run" {
				inheritsVB0 = false
			}
			if strings.HasPrefix(strings.ToLower(right), "copy_desc ") {
				target := modelViewerResourceToken(plainLeft)
				source := modelViewerResourceToken(strings.TrimSpace(right[len("copy_desc "):]))
				aliases.add(aliases.descriptors, target, source)
				continue
			}
			if modelViewerUAVSlotRE.MatchString(plainLeft) {
				key := strings.ToLower(plainLeft)
				if strings.EqualFold(right, "null") {
					delete(uav, key)
					if defaultActive {
						delete(defaultSlots, key)
					}
					continue
				}
				uav[key] = modelViewerResourceToken(right)
				if defaultActive {
					defaultSlots[key] = modelViewerResourceToken(right)
				}
				continue
			}
			target := modelViewerResourceToken(plainLeft)
			if strings.HasPrefix(strings.ToLower(right), "copy ") &&
				modelViewerResourceIsTexture(aliases.resources[modelViewerNormalizeKey(target)]) {
				source := modelViewerResourceToken(right)
				aliases.add(aliases.copies, target, source)
				if defaultActive {
					aliases.add(aliases.defaultCopies, target, source)
				}
				continue
			}
			if inheritsVB0 && target != "" && !strings.HasPrefix(strings.ToLower(left), "post") &&
				modelViewerVB0CaptureRE.MatchString(right) {
				aliases.add(vertexCaptures, target, family)
				continue
			}
			slot := modelViewerUAVReference(right)
			if target != "" && slot != "" {
				aliases.add(aliases.outputs, target, uav[slot])
				if defaultActive {
					aliases.add(aliases.defaultUAV, target, defaultSlots[slot])
				}
			}
		}
	}
	// Capture overrides inherit vb0 from the position override for the hash
	// in their section name. The capture and the draw may be in either INI order.
	// A command list the position override runs can bind another vb0, which
	// leaves the source ambiguous unless it binds the same resource.
	if len(positionRuns) > 0 {
		commandLists := make(map[string]modINISection)
		for _, section := range sections {
			if strings.EqualFold(section.Header, "CommandList") {
				commandLists[modelViewerNormalizeKey(section.Header+section.Name)] = section
			}
		}
		for family, runs := range positionRuns {
			visited := make(map[string]bool)
			for _, run := range runs {
				aliases.addRunVB0(vertexPositions, family, run, commandLists, visited)
			}
		}
	}
	for target, families := range vertexCaptures {
		if len(families) == 1 {
			if positions := vertexPositions[families[0]]; len(positions) == 1 {
				aliases.add(aliases.copies, target, positions[0])
			}
		}
	}
	aliases.addEFMIArmatureInputs(sections)
	aliases.addWWMIShapeKeyApplierInputs(sections)
	return aliases
}

// addRunVB0 adds to positions[family] the vb0 bindings of command list run and of the lists it runs.
func (a modelViewerResourceAliases) addRunVB0(
	positions map[string][]string,
	family, run string,
	commandLists map[string]modINISection,
	visited map[string]bool,
) {
	key := modelViewerNormalizeKey(run)
	section, ok := commandLists[key]
	if !ok || visited[key] {
		return
	}
	visited[key] = true
	for _, raw := range section.Lines {
		left, right, ok := strings.Cut(strings.TrimSpace(strings.SplitN(raw, ";", 2)[0]), "=")
		if !ok {
			continue
		}
		right = strings.TrimSpace(right)
		switch strings.ToLower(modelViewerAssignmentTarget(strings.TrimSpace(left))) {
		case "vb0":
			a.add(positions, family, modelViewerResourceToken(right))
		case "run":
			a.addRunVB0(positions, family, right, commandLists, visited)
		}
	}
}

// addWWMIShapeKeyApplierInputs follows the base positions through the applier,
// which writes cs-t6 plus shape key deltas into cs-u6. A draw can bind that output
// or a copy of it, and the preview applies shape keys over the bind pose itself.
func (a modelViewerResourceAliases) addWWMIShapeKeyApplierInputs(sections []modINISection) {
	for _, section := range sections {
		input, output := "", ""
		applied := false
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			left, right, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			left, right = strings.TrimSpace(left), strings.TrimSpace(right)
			switch {
			case strings.EqualFold(left, "cs-t6"):
				input = modelViewerResourceToken(right)
			case strings.EqualFold(left, "cs-u6"):
				output = modelViewerResourceToken(right)
			case strings.EqualFold(left, "run") && strings.EqualFold(right, `CustomShader\WWMIv1\ShapeKeyApplier`):
				a.add(a.computeInputs, output, input)
				applied = true
			case applied && output != "" && strings.EqualFold(modelViewerResourceToken(right), output):
				a.add(a.computeInputs, modelViewerResourceToken(left), input)
			}
		}
	}
}

func (a modelViewerResourceAliases) add(edges map[string][]string, target, source string) {
	if target == "" || source == "" {
		return
	}
	targetKey := modelViewerNormalizeKey(target)
	for _, existing := range edges[targetKey] {
		if modelViewerNormalizeKey(existing) == modelViewerNormalizeKey(source) {
			return
		}
	}
	edges[targetKey] = append(edges[targetKey], source)
}

func (a modelViewerResourceAliases) resolve(name string) (modelViewerResource, bool) {
	resource, status := a.resolveAt(name, make(map[string]bool), 0)
	return resource, status == modelViewerResourceResolved
}

func (a modelViewerResourceAliases) resolveAt(
	name string,
	visiting map[string]bool,
	depth int,
) (modelViewerResource, modelViewerResourceResolution) {
	key := modelViewerNormalizeKey(name)
	resource, exists := a.resources[key]
	if !exists {
		return modelViewerResource{}, modelViewerResourceUnresolved
	}
	if visiting[key] || depth > maxModelViewerResourceAliasDepth {
		return modelViewerResource{}, modelViewerResourceInvalid
	}
	if resource.Filename != "" {
		return resource, modelViewerResourceResolved
	}
	visiting[key] = true
	defer delete(visiting, key)
	if resolved, status := a.resolveUnique(
		a.descriptors[key],
		visiting,
		depth+1,
	); status != modelViewerResourceUnresolved {
		return resolved, status
	}
	if resolved, status := a.resolveUnique(
		a.defaultUAV[key],
		visiting,
		depth+1,
	); status != modelViewerResourceUnresolved {
		return resolved, status
	}
	if resolved, status := a.resolveUnique(
		a.outputs[key],
		visiting,
		depth+1,
	); status != modelViewerResourceUnresolved {
		return resolved, status
	}
	if resolved, status := a.resolveUnique(
		a.defaultCopies[key],
		visiting,
		depth+1,
	); status != modelViewerResourceUnresolved {
		return resolved, status
	}
	if resolved, status := a.resolveUnique(a.copies[key], visiting, depth+1); status != modelViewerResourceUnresolved {
		return resolved, status
	}
	return a.resolveUnique(a.computeInputs[key], visiting, depth+1)
}

func modelViewerResourceIsTexture(resource modelViewerResource) bool {
	for key, value := range resource.Values {
		if strings.EqualFold(key, "type") {
			return strings.HasPrefix(strings.ToLower(strings.TrimSpace(value)), "texture")
		}
	}
	return false
}

func (a modelViewerResourceAliases) resolveUnique(
	sources []string,
	visiting map[string]bool,
	depth int,
) (modelViewerResource, modelViewerResourceResolution) {
	var selected modelViewerResource
	selectedKey := ""
	for _, source := range sources {
		resolved, status := a.resolveAt(source, visiting, depth)
		if status == modelViewerResourceInvalid {
			return modelViewerResource{}, modelViewerResourceInvalid
		}
		if status != modelViewerResourceResolved {
			continue
		}
		key := modelViewerNormalizeKey(resolved.Name + ":" + resolved.Filename)
		if selectedKey != "" && selectedKey != key {
			return modelViewerResource{}, modelViewerResourceInvalid
		}
		selected, selectedKey = resolved, key
	}
	if selectedKey == "" {
		return modelViewerResource{}, modelViewerResourceUnresolved
	}
	return selected, modelViewerResourceResolved
}

func modelViewerResourceToken(value string) string {
	value = strings.TrimSpace(value)
	for {
		lower := strings.ToLower(value)
		switch {
		case strings.HasPrefix(lower, "copy "):
			value = strings.TrimSpace(value[len("copy "):])
		case strings.HasPrefix(lower, "ref "):
			value = strings.TrimSpace(value[len("ref "):])
		default:
			if !strings.HasPrefix(strings.ToLower(value), "resource") {
				return ""
			}
			return modelViewerTrimResourcePrefix(value)
		}
	}
}

func modelViewerUAVReference(value string) string {
	value = strings.TrimSpace(value)
	for {
		lower := strings.ToLower(value)
		switch {
		case strings.HasPrefix(lower, "copy "):
			value = strings.TrimSpace(value[len("copy "):])
		case strings.HasPrefix(lower, "ref "):
			value = strings.TrimSpace(value[len("ref "):])
		default:
			if modelViewerUAVSlotRE.MatchString(value) {
				return strings.ToLower(value)
			}
			return ""
		}
	}
}
