package tools

import "strings"

func remapModelViewerTransportVariables(transport *ModelViewerTransport, names map[string]modelViewerVariableName) {
	if transport == nil || len(names) == 0 {
		return
	}
	publicName := func(value string) string {
		if name, ok := names[modelViewerNormalizeKey(value)]; ok {
			return name.PublicID
		}
		return value
	}
	remapDNF := func(dnf ModelViewerDNF) {
		for groupIndex := range dnf {
			for clauseIndex := range dnf[groupIndex] {
				dnf[groupIndex][clauseIndex].Var = publicName(dnf[groupIndex][clauseIndex].Var)
			}
		}
	}
	for meshIndex := range transport.Meshes {
		mesh := &transport.Meshes[meshIndex]
		remapDNF(mesh.Conditions)
		for _, variants := range [][]ModelViewerTextureVariant{
			mesh.TextureVariants,
			mesh.NormalMapVariants,
			mesh.LightMapVariants,
			mesh.MaterialMapVariants,
		} {
			for variantIndex := range variants {
				remapDNF(variants[variantIndex].Conditions)
			}
		}
		for targetIndex := range mesh.ShapeTargets {
			mesh.ShapeTargets[targetIndex].Var = publicName(mesh.ShapeTargets[targetIndex].Var)
		}
		for variantIndex := range mesh.PositionVariants {
			remapDNF(mesh.PositionVariants[variantIndex].Conditions)
		}
	}
	for variableIndex := range transport.Variables {
		variable := &transport.Variables[variableIndex]
		oldID := variable.ID
		if name, ok := names[modelViewerNormalizeKey(oldID)]; ok {
			variable.ID = name.PublicID
			if strings.EqualFold(variable.Label, humanizeModelViewerLabel(oldID)) || variable.Label == oldID {
				variable.Label = name.Label
			}
		}
		for effectIndex := range variable.Effects {
			effect := &variable.Effects[effectIndex]
			effect.Var = publicName(effect.Var)
			if effect.When != nil {
				effect.When.Var = publicName(effect.When.Var)
			}
		}
	}
	defaults := make(map[string]any, len(transport.DefaultState))
	for key, value := range transport.DefaultState {
		defaults[publicName(key)] = value
	}
	transport.DefaultState = defaults
	for ruleIndex := range transport.StateRules {
		rule := &transport.StateRules[ruleIndex]
		rule.Var = publicName(rule.Var)
		remapDNF(rule.Conditions)
	}
	for clipIndex := range transport.Animations {
		clip := &transport.Animations[clipIndex]
		oldID := clip.ID
		if name, ok := names[modelViewerNormalizeKey(oldID)]; ok {
			clip.ID = name.PublicID
			if strings.EqualFold(clip.Label, humanizeModelViewerLabel(oldID)) || clip.Label == oldID {
				clip.Label = humanizeModelViewerLabel(name.Label)
			}
		} else {
			clip.ID = publicName(clip.ID)
		}
		for variableIndex := range clip.VariableIDs {
			clip.VariableIDs[variableIndex] = publicName(clip.VariableIDs[variableIndex])
		}
		for frameIndex := range clip.Frames {
			values := make(map[string]any, len(clip.Frames[frameIndex].Values))
			for key, value := range clip.Frames[frameIndex].Values {
				values[publicName(key)] = value
			}
			clip.Frames[frameIndex].Values = values
		}
	}
}

func normalizeModelViewerTransportValueTypes(transport *ModelViewerTransport) {
	if transport == nil {
		return
	}
	for variableIndex := range transport.Variables {
		variable := &transport.Variables[variableIndex]
		variable.DefaultValue = modelViewerString(variable.DefaultValue)
		for valueIndex := range variable.Values {
			variable.Values[valueIndex].Value = modelViewerString(variable.Values[valueIndex].Value)
		}
	}
	animationVars := make(map[string]bool)
	for _, clip := range transport.Animations {
		for _, id := range clip.VariableIDs {
			animationVars[strings.ToLower(id)] = true
		}
	}
	for key, value := range transport.DefaultState {
		if value == nil {
			continue
		}
		// Electron defaults and rule values originate as INI strings. Animation
		// frame payloads remain numeric, but their initial state is also a string.
		if _, ok := value.(string); !ok || animationVars[strings.ToLower(key)] {
			transport.DefaultState[key] = modelViewerString(value)
		}
	}
}
