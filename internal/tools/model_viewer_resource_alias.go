package tools

import (
	"os"
	"regexp"
	"strings"
)

const maxModelViewerResourceAliasDepth = 32

var modelViewerUAVSlotRE = regexp.MustCompile(`(?i)^cs-u\d+$`)

type modelViewerResourceAliases struct {
	descriptors map[string][]string
	defaultUAV  map[string][]string
	outputs     map[string][]string
	resources   map[string]modelViewerResource
}

type modelViewerResourceResolution uint8

const (
	modelViewerResourceUnresolved modelViewerResourceResolution = iota
	modelViewerResourceResolved
	modelViewerResourceInvalid
)

func resolveModelViewerEffectiveResources(sections []modINISection, resources []modelViewerResource) []modelViewerResource {
	aliases := collectModelViewerResourceAliases(sections, resources)
	output := append([]modelViewerResource(nil), resources...)
	for index := range output {
		resource := output[index]
		if resource.Filename != "" || parseModelViewerMihoyoResourceName(resource.Name) == nil {
			continue
		}
		resolved, ok := aliases.resolve(resource.Name)
		if !ok || resolved.Filename == "" || resolved.Stride < 12 {
			continue
		}
		output[index].Filename = resolved.Filename
		output[index].Stride = resolved.Stride
		output[index].Format = resolved.Format
	}
	return output
}

func resolveModelViewerEffectiveResourcesAt(root, baseDir string, sections []modINISection, resources []modelViewerResource) []modelViewerResource {
	output := resolveModelViewerEffectiveResources(sections, resources)
	for index := range output {
		if resources[index].Filename != "" || output[index].Filename == "" {
			continue
		}
		path, err := resolveModelViewerResourcePath(root, baseDir, output[index].Filename)
		if err != nil || !modelViewerPathWithin(root, path) {
			output[index].Filename = ""
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			output[index].Filename = ""
		}
	}
	return output
}

func collectModelViewerResourceAliases(sections []modINISection, resources []modelViewerResource) modelViewerResourceAliases {
	aliases := modelViewerResourceAliases{
		descriptors: make(map[string][]string),
		defaultUAV:  make(map[string][]string),
		outputs:     make(map[string][]string),
		resources:   make(map[string]modelViewerResource, len(resources)),
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
	for _, section := range sections {
		uav := make(map[string]string)
		defaultSlots := make(map[string]string)
		var stack []modelViewerSymbolicBranchFrame
		for _, raw := range section.Lines {
			line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				branch := parseModelViewerConditionDNF(strings.TrimSpace(line[3:]), modelViewerAliases(variables), variables)
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
			plainLeft := strings.TrimSpace(strings.TrimPrefix(strings.ToLower(left), "post "))
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
			slot := modelViewerUAVReference(right)
			if target != "" && slot != "" {
				aliases.add(aliases.outputs, target, uav[slot])
				if defaultActive {
					aliases.add(aliases.defaultUAV, target, defaultSlots[slot])
				}
			}
		}
	}
	return aliases
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

func (a modelViewerResourceAliases) resolveAt(name string, visiting map[string]bool, depth int) (modelViewerResource, modelViewerResourceResolution) {
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
	if resolved, status := a.resolveUnique(a.descriptors[key], visiting, depth+1); status != modelViewerResourceUnresolved {
		return resolved, status
	}
	if resolved, status := a.resolveUnique(a.defaultUAV[key], visiting, depth+1); status != modelViewerResourceUnresolved {
		return resolved, status
	}
	return a.resolveUnique(a.outputs[key], visiting, depth+1)
}

func (a modelViewerResourceAliases) resolveUnique(sources []string, visiting map[string]bool, depth int) (modelViewerResource, modelViewerResourceResolution) {
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
