package tools

import (
	"encoding/binary"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type modelViewerPreparedAnimationFrame struct {
	Index  int
	Time   float64
	Values map[string]any
}

type modelViewerPreparedAnimationClip struct {
	ID          string
	Label       string
	VariableIDs []string
	FPS         float64
	FrameStart  int
	FrameEnd    int
	Loop        bool
	Frames      []modelViewerPreparedAnimationFrame
}

const maxModelViewerAnimationFrames = 4096

var modelViewerPresentAssignmentRE = regexp.MustCompile(`(?i)^(?:post\s+)?\$([\w.]+)\s*=\s*(.+)$`)

func detectModelViewerPresentAnimations(sections []modINISection, defaults map[string]any, slotBindings []modelViewerSlotBinding) []modelViewerPreparedAnimationClip {
	manual := make(map[string]bool)
	for _, binding := range slotBindings {
		manual[modelViewerNormalizeKey(binding.Variable)] = true
	}
	discovered := make(map[string]modelViewerPreparedAnimationClip)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Present") {
			continue
		}
		for _, raw := range section.Lines {
			match := modelViewerPresentAssignmentRE.FindStringSubmatch(strings.TrimSpace(raw))
			if match == nil || !strings.Contains(strings.ToLower(match[2]), "time") {
				continue
			}
			variable := modelViewerNormalizeKey(match[1])
			if manual[variable] {
				continue
			}
			values := collectModelViewerDiscreteBranchValues(sections, variable)
			if len(values) < 2 {
				continue
			}
			fps := resolveModelViewerAnimationFPS(defaults, match[2])
			if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) {
				continue
			}
			clip := modelViewerPreparedAnimationClip{ID: variable, Label: humanizeModelViewerLabel(variable), VariableIDs: []string{variable}, FPS: fps, FrameStart: values[0], FrameEnd: values[len(values)-1], Loop: true}
			if !validModelViewerAnimationRange(clip.FrameStart, clip.FrameEnd) {
				continue
			}
			for frame := clip.FrameStart; frame <= clip.FrameEnd; frame++ {
				clip.Frames = append(clip.Frames, modelViewerPreparedAnimationFrame{Index: frame, Time: float64(frame-clip.FrameStart) / fps, Values: map[string]any{variable: float64(frame)}})
			}
			discovered[variable] = clip
		}
	}
	for _, clip := range detectModelViewerIncrementalAnimations(sections, defaults, manual) {
		if _, exists := discovered[clip.ID]; !exists {
			discovered[clip.ID] = clip
		}
	}
	for _, clip := range detectModelViewerAccumulatorAnimations(sections, defaults, manual) {
		if _, exists := discovered[clip.ID]; !exists {
			discovered[clip.ID] = clip
		}
	}
	keys := make([]string, 0, len(discovered))
	for key := range discovered {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	output := make([]modelViewerPreparedAnimationClip, 0, len(keys))
	for _, key := range keys {
		output = append(output, discovered[key])
	}
	return output
}

func detectModelViewerAccumulatorAnimations(sections []modINISection, defaults map[string]any, manual map[string]bool) []modelViewerPreparedAnimationClip {
	accumulatorRE := regexp.MustCompile(`(?i)^if\s+\(\s*\$([\w.]+)\s*\+\s*\(?\s*1\s*/\s*(\$?[\w.-]+)\s*\)?\s*\)\s*<\s*(\$?[\w.-]+)\s*$`)
	var clips []modelViewerPreparedAnimationClip
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Present") {
			continue
		}
		for index, raw := range section.Lines {
			match := accumulatorRE.FindStringSubmatch(strings.TrimSpace(raw))
			if match == nil {
				continue
			}
			aux, speedToken, endToken := match[1], match[2], match[3]
			incrementRE := regexp.MustCompile(`(?i)^\$` + regexp.QuoteMeta(aux) + `\s*=\s*\$` + regexp.QuoteMeta(aux) + `\s*\+\s*\(?\s*1\s*/\s*` + regexp.QuoteMeta(speedToken) + `\s*\)?$`)
			assignmentRE := regexp.MustCompile(`(?i)^\$([\w.]+)\s*=\s*\$` + regexp.QuoteMeta(aux) + `\s*//\s*1$`)
			resetRE := regexp.MustCompile(`(?i)^\$` + regexp.QuoteMeta(aux) + `\s*=\s*(\$?[\w.-]+)$`)
			incremented, variable, startToken := false, "", ""
			for _, candidate := range section.Lines[index+1:] {
				line := strings.TrimSpace(candidate)
				if incrementRE.MatchString(line) {
					incremented = true
					continue
				}
				if assignment := assignmentRE.FindStringSubmatch(line); assignment != nil && variable == "" {
					variable = modelViewerNormalizeKey(assignment[1])
				}
				if reset := resetRE.FindStringSubmatch(line); reset != nil && startToken == "" {
					startToken = reset[1]
				}
			}
			if !incremented || variable == "" || startToken == "" || manual[variable] || len(collectModelViewerDiscreteBranchValues(sections, variable)) < 2 {
				continue
			}
			start, startOK := resolveModelViewerNumericToken(startToken, defaults)
			end, endOK := resolveModelViewerNumericToken(endToken, defaults)
			speed, speedOK := resolveModelViewerNumericToken(speedToken, defaults)
			if !startOK || !endOK || !speedOK || start != math.Trunc(start) || end != math.Trunc(end) || speed <= 0 {
				continue
			}
			clip := buildModelViewerPreparedAnimationClip(variable, 60/speed, int(start), int(end))
			if clip != nil {
				clips = append(clips, *clip)
			}
		}
	}
	return clips
}

func buildModelViewerPreparedAnimationClip(variable string, fps float64, start, end int) *modelViewerPreparedAnimationClip {
	if fps <= 0 || math.IsNaN(fps) || math.IsInf(fps, 0) || !validModelViewerAnimationRange(start, end) {
		return nil
	}
	clip := &modelViewerPreparedAnimationClip{ID: variable, Label: humanizeModelViewerLabel(variable), VariableIDs: []string{variable}, FPS: fps, FrameStart: start, FrameEnd: end, Loop: true}
	for frame := start; frame <= end; frame++ {
		clip.Frames = append(clip.Frames, modelViewerPreparedAnimationFrame{Index: frame, Time: float64(frame-start) / fps, Values: map[string]any{variable: float64(frame)}})
	}
	return clip
}

func detectModelViewerIncrementalAnimations(sections []modINISection, defaults map[string]any, manual map[string]bool) []modelViewerPreparedAnimationClip {
	var clips []modelViewerPreparedAnimationClip
	moduloRE := regexp.MustCompile(`(?i)^if\s+\$([\w.]+)\s*%\s*(\$?[\w.-]+)\s*==\s*0$`)
	compareRE := regexp.MustCompile(`(?i)^(?:if|elif|else if)\s+\$([\w.]+)\s*<\s*(\$?[\w.-]+)$`)
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "Present") {
			continue
		}
		lines := section.Lines
		for index, raw := range lines {
			modulo := moduloRE.FindStringSubmatch(strings.TrimSpace(raw))
			if modulo == nil {
				continue
			}
			aux, speedToken := modulo[1], modulo[2]
			incrementAux := false
			for _, line := range lines {
				normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(line)), " ", "")
				if normalized == "$"+strings.ToLower(aux)+"=$"+strings.ToLower(aux)+"+1" || normalized == "post$"+strings.ToLower(aux)+"=$"+strings.ToLower(aux)+"+1" {
					incrementAux = true
				}
			}
			if !incrementAux {
				continue
			}
			for probe := index + 1; probe < len(lines); probe++ {
				compare := compareRE.FindStringSubmatch(strings.TrimSpace(lines[probe]))
				if compare == nil {
					continue
				}
				variable := modelViewerNormalizeKey(compare[1])
				if manual[variable] {
					continue
				}
				increment, startToken := false, ""
				for _, candidate := range lines[probe+1:] {
					normalized := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(candidate)), " ", "")
					if normalized == "$"+strings.ToLower(compare[1])+"=$"+strings.ToLower(compare[1])+"+1" {
						increment = true
						continue
					}
					prefix := "$" + strings.ToLower(compare[1]) + "="
					if strings.HasPrefix(normalized, prefix) && !strings.Contains(normalized, "+1") && startToken == "" {
						startToken = normalized[len(prefix):]
					}
				}
				start, startOK := resolveModelViewerNumericToken(startToken, defaults)
				end, endOK := resolveModelViewerNumericToken(compare[2], defaults)
				speed, speedOK := resolveModelViewerNumericToken(speedToken, defaults)
				if !increment || !startOK || !endOK || !speedOK || end <= start || speed <= 0 {
					continue
				}
				fps := 60 / speed
				clip := modelViewerPreparedAnimationClip{ID: variable, Label: humanizeModelViewerLabel(variable), VariableIDs: []string{variable}, FPS: fps, FrameStart: int(start), FrameEnd: int(end), Loop: true}
				if start != math.Trunc(start) || end != math.Trunc(end) || !validModelViewerAnimationRange(clip.FrameStart, clip.FrameEnd) {
					continue
				}
				for frame := clip.FrameStart; frame <= clip.FrameEnd; frame++ {
					clip.Frames = append(clip.Frames, modelViewerPreparedAnimationFrame{Index: frame, Time: float64(frame-clip.FrameStart) / fps, Values: map[string]any{variable: float64(frame)}})
				}
				clips = append(clips, clip)
				break
			}
		}
	}
	return clips
}

func validModelViewerAnimationRange(start, end int) bool {
	return end > start && end-start+1 <= maxModelViewerAnimationFrames
}

func collectModelViewerDiscreteBranchValues(sections []modINISection, variable string) []int {
	pattern := regexp.MustCompile(`(?i)^(?:if|elif|else if)\s+\$` + regexp.QuoteMeta(variable) + `\s*==\s*(-?\d+(?:\.\d+)?)$`)
	seen := make(map[int]bool)
	var values []int
	for _, section := range sections {
		for _, raw := range section.Lines {
			if match := pattern.FindStringSubmatch(strings.TrimSpace(raw)); match != nil {
				value, err := strconv.ParseFloat(match[1], 64)
				if err == nil && value == math.Trunc(value) && !seen[int(value)] {
					seen[int(value)] = true
					values = append(values, int(value))
				}
			}
		}
	}
	sort.Ints(values)
	return values
}

func resolveModelViewerAnimationFPS(defaults map[string]any, expression string) float64 {
	if value := modelViewerAsFloat(defaults["fps"]); value > 0 {
		return value
	}
	pattern := regexp.MustCompile(`(?i)\$([\w.]*fps[\w.]*)`)
	if match := pattern.FindStringSubmatch(expression); match != nil {
		return modelViewerAsFloat(defaults[modelViewerNormalizeKey(match[1])])
	}
	return math.NaN()
}

func resolveModelViewerNumericToken(token string, defaults map[string]any) (float64, bool) {
	token = strings.TrimSpace(token)
	if value, err := strconv.ParseFloat(token, 64); err == nil {
		return value, true
	}
	value, ok := defaults[modelViewerNormalizeKey(strings.TrimPrefix(token, "$"))]
	if !ok {
		return 0, false
	}
	number, err := modelViewerNumber(value)
	return number, err == nil
}

func modelViewerUint32Bytes(values []uint32) []byte {
	out := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(out[i*4:], value)
	}
	return out
}

func modelViewerFloat32Bytes(values []float32) []byte {
	out := make([]byte, len(values)*4)
	for i, value := range values {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(value))
	}
	return out
}
