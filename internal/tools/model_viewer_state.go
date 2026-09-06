package tools

func configureModelViewerState(transport *ModelViewerTransport, allSections []modINISection, allShapeKeys []modelViewerShapeKey, variableNames map[string]modelViewerVariableName) {
	defaults := collectModelViewerDefaultVariables(allSections)
	bindings := collectModelViewerSlotBindings(allSections, defaults)
	animations := detectModelViewerPresentAnimations(allSections, defaults, bindings)
	stateRules := extractModelViewerDirectStateRules(allSections, defaults)
	variables := prependModelViewerShapeVariables(buildModelViewerDirectVariables(allSections, bindings, defaults), allShapeKeys, defaults)
	tracked := make(map[string]bool)
	for _, variable := range variables {
		tracked[modelViewerNormalizeKey(variable.ID)] = true
		for _, effect := range variable.Effects {
			tracked[modelViewerNormalizeKey(effect.Var)] = true
		}
	}
	for _, rule := range stateRules {
		tracked[modelViewerNormalizeKey(rule.Var)] = true
	}
	for _, clip := range animations {
		for _, id := range clip.VariableIDs {
			tracked[modelViewerNormalizeKey(id)] = true
		}
	}
	normalizeModelViewerTransportConditions(transport, tracked)
	gating := modelViewerDirectGatingVariables(transport.Meshes, stateRules)
	animationVars := make(map[string]bool)
	for _, clip := range animations {
		for _, id := range clip.VariableIDs {
			animationVars[id] = true
		}
	}
	for _, variable := range variables {
		if !animationVars[variable.ID] && (variable.alwaysVisible || modelViewerVariableIsGating(variable, gating)) {
			transport.Variables = append(transport.Variables, variable)
		}
		transport.DefaultState[variable.ID] = variable.DefaultValue
	}
	for key, value := range defaults {
		transport.DefaultState[key] = value
	}
	for _, prepared := range animations {
		clip := ModelViewerAnimationClip{ID: prepared.ID, Label: prepared.Label, VariableIDs: prepared.VariableIDs, FPS: normalizeModelViewerAnimationFPS(prepared.FPS), FrameStart: prepared.FrameStart, FrameEnd: prepared.FrameEnd, Loop: prepared.Loop}
		for _, frame := range prepared.Frames {
			clip.Frames = append(clip.Frames, ModelViewerAnimationFrame(frame))
		}
		transport.Animations = append(transport.Animations, clip)
		for _, id := range clip.VariableIDs {
			if _, ok := transport.DefaultState[id]; !ok {
				transport.DefaultState[id] = float64(clip.FrameStart)
			}
		}
	}
	for _, rule := range stateRules {
		if !animationVars[rule.Var] {
			transport.StateRules = append(transport.StateRules, rule)
			if _, ok := transport.DefaultState[rule.Var]; !ok {
				transport.DefaultState[rule.Var] = rule.Value
			}
		}
	}
	remapModelViewerTransportVariables(transport, variableNames)
	normalizeModelViewerTransportValueTypes(transport)
}
