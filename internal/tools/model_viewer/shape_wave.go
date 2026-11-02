package modelviewer

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// modelViewerShapeWaveRE matches A*(sin(phase*S)+B) in a compacted shader, whatever the phase is named.
var modelViewerShapeWaveRE = regexp.MustCompile(
	`([+-]?\d+(?:\.\d+)?)\*\(sin\(\w+\*([+-]?\d+(?:\.\d+)?)\)\+([+-]?\d+(?:\.\d+)?)\)`,
)

const maxModelViewerRunGuardDepth = 8

type modelViewerShapeWaveKey struct {
	phase string
	// slot indexes the offset table, which WWMI's own setter reaches from a shape
	// key ID by skipping one slot per batch; a mod's shader may not.
	slot int
	wave ModelViewerShapeWave
}

// findModelViewerShapeWaves finds the WWMI shape keys a mod animates with its own
// compute shader: it writes A*(sin(z0*S)+B) for the shape key in y0 into the custom
// shape key values bound to cs-u5, and z0 is a Present accumulator.
func findModelViewerShapeWaves(sections []modINISection, modDir string) []modelViewerShapeWaveKey {
	var variables map[string]any
	var output []modelViewerShapeWaveKey
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "CustomShader") {
			continue
		}
		shaderPath, phase, shapeID, bound := "", "", -1, false
		for _, raw := range section.Lines {
			left, right, ok := strings.Cut(cleanModelViewerShapeLine(raw), "=")
			if !ok {
				continue
			}
			right = strings.TrimSpace(right)
			switch strings.ToLower(strings.TrimSpace(left)) {
			case "cs":
				shaderPath = stripModelViewerQuotes(right)
			case "cs-u5":
				bound = true
			case "y0":
				if id, err := strconv.Atoi(right); err == nil {
					shapeID = id
				}
			case "z0":
				if match := modelViewerShapeDirectValueRE.FindStringSubmatch(right); match != nil {
					phase = modelViewerNormalizeKey(match[1])
				}
			}
		}
		if !bound || shaderPath == "" || phase == "" || shapeID < 0 {
			continue
		}

		shader, ok := readModelViewerComputeShader(modDir, modDir, shaderPath)
		if !ok {
			continue
		}
		compact := compactModelViewerShader(shader)
		match := modelViewerShapeWaveRE.FindStringSubmatch(compact)
		if match == nil || !strings.Contains(compact, "register(u5)") {
			continue
		}
		amplitude, _ := strconv.ParseFloat(match[1], 64)
		angularScale, _ := strconv.ParseFloat(match[2], 64)
		offset, _ := strconv.ParseFloat(match[3], 64)
		wave := ModelViewerShapeWave{AngularScale: angularScale, Amplitude: amplitude, Bias: amplitude * offset}

		token := ""
		for _, candidate := range sections {
			if found, ok := findModelViewerAccumulatorRateToken(candidate.Lines, phase); ok {
				token = found
				break
			}
		}
		if strings.HasPrefix(token, "$") {
			wave.SpeedVar, wave.Rate = modelViewerNormalizeKey(token[1:]), 1
		} else if rate, err := strconv.ParseFloat(token, 64); err == nil && rate > 0 {
			wave.Rate = rate
		} else {
			continue
		}

		if variables == nil {
			variables = modelViewerDirectConditionVariables(sections, collectModelViewerDefaultVariables(sections))
		}
		guard, reached := modelViewerRunGuard(sections, section.Header+section.Name, variables, 0)
		if !reached || len(guard) == 0 {
			continue
		}
		wave.When = guard
		slot := shapeID
		if strings.Contains(compact, "/127") {
			slot += shapeID / 127
		}
		output = append(output, modelViewerShapeWaveKey{phase: phase, slot: slot, wave: wave})
	}
	return output
}

// modelViewerShapeWaveClip is the clock the renderer advances wave phases by. Its
// frames carry no values: the phases accumulate across loops and follow live speeds.
func modelViewerShapeWaveClip(meshes []ModelViewerMeshTransport) (modelViewerPreparedAnimationClip, bool) {
	var phases []string
	for _, mesh := range meshes {
		for _, target := range mesh.ShapeTargets {
			if target.Wave != nil && !slices.Contains(phases, target.Var) {
				phases = append(phases, target.Var)
			}
		}
	}
	if len(phases) == 0 {
		return modelViewerPreparedAnimationClip{}, false
	}
	clip := buildModelViewerComputeFallbackClip("shapewave", "Shape Animation", 300, 30)
	clip.DeformerID = ""
	clip.VariableIDs = phases
	return clip, true
}

// modelViewerRunGuard returns the conditions under which [Present] reaches a run of
// target, directly or through command lists, and whether it reaches it at all.
func modelViewerRunGuard(
	sections []modINISection,
	target string,
	variables map[string]any,
	depth int,
) (ModelViewerDNF, bool) {
	if depth > maxModelViewerRunGuardDepth {
		return nil, false
	}
	target = modelViewerNormalizeKey(target)
	guard, reached := modelViewerDNFFalse(), false
	for _, section := range sections {
		present := strings.EqualFold(section.Header, "Present")
		if !present && !strings.EqualFold(section.Header, "CommandList") {
			continue
		}
		var caller ModelViewerDNF
		callerResolved := present
		if present {
			caller = modelViewerDNFTrue()
		}
		walkModelViewerBranchLines(section.Lines, func(line string, clauses []modelViewerConditionClause) {
			left, right, ok := strings.Cut(line, "=")
			if !ok ||
				!strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(strings.ToLower(left), "post ")), "run") ||
				modelViewerNormalizeKey(strings.TrimSpace(right)) != target {
				return
			}
			if !callerResolved {
				callerResolved = true
				var callerReached bool
				caller, callerReached = modelViewerRunGuard(sections, section.Header+section.Name, variables, depth+1)
				if !callerReached {
					caller = nil
				}
			}
			if caller == nil {
				return
			}
			reached = true
			guard = modelViewerDNFOr(guard, modelViewerDNFAnd(caller, modelViewerConditionsToDNF(clauses, variables)))
		})
	}
	return guard, reached
}
