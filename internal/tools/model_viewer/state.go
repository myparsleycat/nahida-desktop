package modelviewer

import "slices"

// modelViewerDrawnVariables extends the variables gating what the viewer draws with
// those the state rules test to set them, transitively.
func modelViewerDrawnVariables(gating map[string]bool, rules []ModelViewerStateRule) map[string]bool {
	drawn := make(map[string]bool, len(gating))
	for name := range gating {
		drawn[name] = true
	}
	for changed := true; changed; {
		changed = false
		for _, rule := range rules {
			if !drawn[modelViewerNormalizeKey(rule.Var)] {
				continue
			}
			for _, group := range rule.Conditions {
				for _, clause := range group {
					if name := modelViewerNormalizeKey(clause.Var); !drawn[name] {
						drawn[name], changed = true, true
					}
				}
			}
		}
	}
	return drawn
}

func configureModelViewerState(
	transport *ModelViewerTransport,
	allSections []modINISection,
	allShapeKeys []modelViewerShapeKey,
	variableNames map[string]modelViewerVariableName,
	computeAnimations []modelViewerPreparedAnimationClip,
	bufferRules []ModelViewerStateRule,
	includeAllVariables bool,
) {
	defaults := collectModelViewerDefaultVariables(allSections)
	bindings := collectModelViewerSlotBindings(allSections, defaults)
	animations := append(detectModelViewerPresentAnimations(allSections, defaults, bindings), computeAnimations...)
	if clip, ok := modelViewerShapeWaveClip(transport.Meshes); ok {
		animations = append(animations, clip)
	}
	stateRules := extractModelViewerDirectStateRules(allSections, defaults)
	variables := prependModelViewerShapeVariables(
		buildModelViewerDirectVariables(allSections, bindings, defaults),
		allShapeKeys,
		defaults,
	)
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
	for _, rule := range bufferRules {
		tracked[modelViewerNormalizeKey(rule.Var)] = true
	}
	for _, clip := range animations {
		for _, id := range clip.VariableIDs {
			tracked[modelViewerNormalizeKey(id)] = true
		}
	}
	for _, rule := range bufferRules {
		rule.Conditions = normalizeModelViewerDNFWithTracked(rule.Conditions, tracked)
		stateRules = append(stateRules, rule)
	}
	normalizeModelViewerTransportConditions(transport, tracked)
	gating := modelViewerDirectGatingVariables(transport)
	animationVars := make(map[string]bool)
	for _, clip := range animations {
		for _, id := range clip.VariableIDs {
			animationVars[id] = true
		}
	}
	drawn := modelViewerDrawnVariables(gating, stateRules)
	for _, variable := range variables {
		// A variable that changes nothing the viewer draws is still listed: presets and
		// the game take its value. A step counter that draws nothing itself is not, since
		// the variables its steps select are listed instead.
		variable.NoPreview = !variable.alwaysVisible && !modelViewerVariableIsGating(variable, drawn) &&
			!slices.ContainsFunc(variable.Effects, func(effect ModelViewerMenuEffect) bool {
				return drawn[modelViewerNormalizeKey(effect.Var)]
			})
		// An effect or a [Present] rule may already hold the value a choice sets, so
		// such a variable changes the model only in some states.
		variable.indirect = !variable.NoPreview && !variable.alwaysVisible &&
			!modelViewerVariableIsGating(variable, gating)
		hiddenCounter := variable.NoPreview && variable.stepCounter
		if includeAllVariables || !animationVars[variable.ID] && !hiddenCounter {
			transport.Variables = append(transport.Variables, variable)
		}
		transport.DefaultState[variable.ID] = variable.DefaultValue
	}
	for key, value := range defaults {
		transport.DefaultState[key] = value
	}
	for _, prepared := range animations {
		clip := ModelViewerAnimationClip{
			ID:          prepared.ID,
			Label:       prepared.Label,
			DeformerID:  prepared.DeformerID,
			VariableIDs: prepared.VariableIDs,
			FPS:         normalizeModelViewerAnimationFPS(prepared.FPS),
			FrameStart:  prepared.FrameStart,
			FrameEnd:    prepared.FrameEnd,
			Loop:        prepared.Loop,
		}
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
