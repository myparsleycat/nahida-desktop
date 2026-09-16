package modelviewer

import (
	"sort"
	"strings"
)

func buildModelViewerDirectVariables(
	sections []modINISection,
	bindings []modelViewerSlotBinding,
	defaults map[string]any,
) []ModelViewerVariable {
	seen := make(map[string]bool)
	var output []ModelViewerVariable
	sort.SliceStable(bindings, func(i, j int) bool {
		return bindings[i].AlwaysVisible && !bindings[j].AlwaysVisible
	})
	for _, binding := range bindings {
		if seen[binding.Variable] {
			continue
		}
		seen[binding.Variable] = true
		variable := ModelViewerVariable{
			ID:            binding.Variable,
			Label:         humanizeModelViewerLabel(binding.Variable),
			DefaultValue:  defaults[binding.Variable],
			Order:         len(output),
			Slot:          binding.Slot,
			ControlType:   "buttons",
			alwaysVisible: binding.AlwaysVisible,
		}
		variable.Label, variable.Effects = modelViewerDirectVariableMetadata(sections, binding, variable.Label)
		if variable.DefaultValue == nil && len(binding.Values) > 0 {
			variable.DefaultValue = binding.Values[0]
		}
		for _, value := range binding.Values {
			variable.Values = append(
				variable.Values,
				ModelViewerVariableValue{Value: value, Label: modelViewerString(value)},
			)
		}
		if slider := inferModelViewerSlider(binding.Variable, binding.Values, false); slider != nil {
			variable.ControlType = "slider"
			variable.Slider = slider
		}
		output = append(output, variable)
	}
	return output
}

func prependModelViewerShapeVariables(
	variables []ModelViewerVariable,
	shapeKeys []modelViewerShapeKey,
	defaults map[string]any,
) []ModelViewerVariable {
	seen := make(map[string]bool)
	output := make([]ModelViewerVariable, 0, len(variables))
	for _, shapeKey := range shapeKeys {
		for _, dimension := range shapeKey.Dimensions {
			id := modelViewerNormalizeKey(dimension.VariableID)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			defaultValue, ok := defaults[id]
			if !ok {
				defaultValue = float64(0)
			}
			output = append(output, ModelViewerVariable{
				ID:            id,
				Label:         humanizeModelViewerLabel(id),
				DefaultValue:  defaultValue,
				Values:        []ModelViewerVariableValue{},
				ControlType:   "slider",
				Slider:        &ModelViewerSlider{Min: 0, Max: 1, Step: 0.01},
				alwaysVisible: true,
			})
		}
	}
	for _, variable := range variables {
		id := modelViewerNormalizeKey(variable.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		output = append(output, variable)
	}
	for index := range output {
		output[index].Order = index
	}
	return output
}

func modelViewerDirectVariableMetadata(
	sections []modINISection,
	binding modelViewerSlotBinding,
	fallbackLabel string,
) (string, []ModelViewerMenuEffect) {
	label := fallbackLabel
	var effects []ModelViewerMenuEffect
	for _, section := range sections {
		if !strings.HasPrefix(strings.ToLower(section.Header), "key") {
			continue
		}
		containsBinding := false
		for _, line := range section.Lines {
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(line))
			if assignment != nil && modelViewerNormalizeKey(assignment[1]) == binding.Variable &&
				strings.Contains(assignment[2], ",") {
				containsBinding = true
				break
			}
		}
		if !containsBinding {
			continue
		}
		fullName := section.Header
		if strings.EqualFold(section.Header, "Key") {
			fullName += section.Name
		}
		if strings.HasPrefix(strings.ToLower(fullName), "key") && len(fullName) > 3 {
			label = fullName[3:]
		}
		for _, line := range section.Lines {
			assignment := modelViewerMenuAssignRE.FindStringSubmatch(strings.TrimSpace(line))
			if assignment == nil {
				continue
			}
			key, raw := assignment[1], strings.TrimSpace(assignment[2])
			if strings.Contains(raw, ",") || modelViewerNormalizeKey(key) == binding.Variable {
				continue
			}
			value := strings.TrimSpace(raw)
			if value != "" {
				effects = append(effects, ModelViewerMenuEffect{Var: modelViewerNormalizeKey(key), Value: value})
			}
		}
	}
	effects = append(effects, binding.Effects...)
	return label, effects
}

func modelViewerDirectConditionVariables(sections []modINISection, defaults map[string]any) map[string]any {
	output := cloneModelViewerState(defaults)
	bindings := collectModelViewerSlotBindings(sections, defaults)
	for _, binding := range bindings {
		key := "__domain:" + modelViewerNormalizeKey(binding.Variable)
		current, _ := output[key].([]any)
		for _, value := range binding.Values {
			current = appendUniqueModelViewerValue(current, value)
		}
		output[key] = current
	}
	for _, clip := range detectModelViewerPresentAnimations(sections, defaults, bindings) {
		for _, variable := range clip.VariableIDs {
			key := "__domain:" + modelViewerNormalizeKey(variable)
			current, _ := output[key].([]any)
			for frame := clip.FrameStart; frame <= clip.FrameEnd; frame++ {
				current = appendUniqueModelViewerValue(current, float64(frame))
			}
			output[key] = current
		}
	}
	output["__aliases"] = buildModelViewerBoolAliases(sections, output)
	return output
}
