package tools

import (
	"fmt"
	"strings"
)

type modelViewerSymbolicAssignment struct {
	resource   string
	authored   bool
	conditions ModelViewerDNF
	sequence   int
}

type modelViewerSymbolicBranchFrame struct {
	current ModelViewerDNF
	seen    ModelViewerDNF
	seq     int
}

type modelViewerSymbolicSectionState struct {
	buffers         map[string][]modelViewerSymbolicAssignment
	resolvedBuffers map[string][]modelViewerSymbolicAssignment
	textures        map[string][]modelViewerSymbolicAssignment
	thisHistory     []modelViewerSymbolicAssignment
	nonDiffuse      []string
	draws           []modelViewerDirectDrawRecord
	explicitDraw    bool
}

type modelViewerSymbolicScanContext struct {
	lookup        map[string]modINISection
	variables     map[string]any
	expansions    int
	draws         int
	seq           int
	sectionName   string
	resourcesOnly bool
}

func scanModelViewerSymbolicRoot(
	sections []modINISection,
	section modINISection,
	defaults map[string]any,
) (*modelViewerSymbolicSectionState, map[string]any, error) {
	lookup := make(map[string]modINISection)
	for _, candidate := range sections {
		lookup[modelViewerNormalizeKey(candidate.Header+candidate.Name)] = candidate
	}
	variables := modelViewerDirectConditionVariables(sections, defaults)

	// These callers inspect resource histories, so draw snapshots would be discarded.
	ctx := &modelViewerSymbolicScanContext{
		lookup: lookup, variables: variables, sectionName: section.Name, resourcesOnly: true,
	}
	state := &modelViewerSymbolicSectionState{
		buffers:  make(map[string][]modelViewerSymbolicAssignment),
		textures: make(map[string][]modelViewerSymbolicAssignment),
	}
	visiting := map[string]bool{modelViewerNormalizeKey(section.Header + section.Name): true}
	if err := ctx.scan(section.Lines, state, nil, visiting); err != nil {
		return nil, nil, err
	}
	return state, variables, nil
}

func collectModelViewerSymbolicDrawRecords(
	sections []modINISection,
	defaults map[string]any,
) ([]modelViewerDirectDrawRecord, []modelViewerSymbolicAssignment, error) {
	lookup := make(map[string]modINISection)
	for _, section := range sections {
		lookup[modelViewerNormalizeKey(section.Header+section.Name)] = section
	}
	ctx := &modelViewerSymbolicScanContext{
		lookup:    lookup,
		variables: modelViewerDirectConditionVariables(sections, defaults),
	}
	var output []modelViewerDirectDrawRecord
	var fallback []modelViewerSymbolicAssignment
	seenFallback := map[string]bool{}
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		if isModelViewerPreviewSkippedOverride(section.Name) {
			continue
		}
		ctx.sectionName = section.Name
		state := &modelViewerSymbolicSectionState{
			buffers:  make(map[string][]modelViewerSymbolicAssignment),
			textures: make(map[string][]modelViewerSymbolicAssignment),
		}
		if err := ctx.scan(
			section.Lines,
			state,
			nil,
			map[string]bool{modelViewerNormalizeKey(section.Header + section.Name): true},
		); err != nil {
			return nil, nil, err
		}
		if !state.explicitDraw && !sectionHandlingSkip(section) {
			records := ctx.implicitRecords(state)
			if len(records) > 0 {
				ctx.draws += len(records)
				if ctx.draws > maxModelViewerDraws {
					return nil, nil, contractError(
						fmt.Sprintf("Mod has too many draws (%d; limit %d).", ctx.draws, maxModelViewerDraws),
					)
				}
				state.draws = append(state.draws, records...)
			}
		}
		if len(state.bufferAssignments("ib")) == 0 {
			fallback = appendModelViewerFallbackVertexBuffers(
				fallback,
				seenFallback,
				state.bufferAssignments("vb0"),
			)
		}
		output = append(output, state.draws...)
	}
	return dedupeModelViewerDirectDrawRecords(keepModelViewerPreviewLODRecords(output)), fallback, nil
}

// modelViewerNestedSectionName returns a section to scan as a nested draw
// source. `run` follows any named section. Other assignments are followed
// only when the value is a CommandList* (EFMI ALPHA-10 draw callbacks).
func modelViewerNestedSectionName(key, value string) string {
	target := strings.TrimSpace(value)
	if strings.HasPrefix(strings.ToLower(target), "ref ") {
		target = strings.TrimSpace(target[4:])
	}
	if strings.EqualFold(strings.TrimSpace(key), "run") {
		return target
	}
	if strings.HasPrefix(strings.ToLower(target), "commandlist") {
		return target
	}
	return ""
}

func (c *modelViewerSymbolicScanContext) scan(
	lines []string,
	state *modelViewerSymbolicSectionState,
	stack []modelViewerSymbolicBranchFrame,
	visiting map[string]bool,
) error {
	stack = append([]modelViewerSymbolicBranchFrame(nil), stack...)
	for _, raw := range lines {
		line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "if "):
			branch := parseModelViewerConditionDNF(
				strings.TrimSpace(line[3:]),
				modelViewerAliases(c.variables),
				c.variables,
			)
			c.seq++
			stack = append(stack, modelViewerSymbolicBranchFrame{current: branch, seen: branch, seq: c.seq})
			continue
		case strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if "):
			if len(stack) == 0 {
				continue
			}
			expression := strings.TrimSpace(line[5:])
			if strings.HasPrefix(lower, "else if ") {
				expression = strings.TrimSpace(line[8:])
			}
			branch := parseModelViewerConditionDNF(expression, modelViewerAliases(c.variables), c.variables)
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

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		conditions := modelViewerSymbolicStackConditions(stack)
		sequence := modelViewerSymbolicStackSequence(stack)
		if len(conditions) == 0 {
			continue
		}
		if nested := modelViewerNestedSectionName(key, value); nested != "" {
			name := modelViewerNormalizeKey(nested)
			section, exists := c.lookup[name]
			if !exists || visiting[name] || c.expansions >= maxModelViewerDirectRunExpansions {
				continue
			}
			c.expansions++
			next := cloneModelViewerVisited(visiting)
			next[name] = true
			if err := c.scan(section.Lines, state, stack, next); err != nil {
				return err
			}
			continue
		}
		switch strings.ToLower(key) {
		case "ib", "vb0", "vb1", "vb2":
			resource := modelViewerTrimResourcePrefix(value)
			if resource != "" && !strings.EqualFold(resource, "null") {
				state.buffers[strings.ToLower(key)] = append(
					state.buffers[strings.ToLower(key)],
					modelViewerSymbolicAssignment{
						resource:   resource,
						conditions: cloneModelViewerDNF(conditions),
						sequence:   sequence,
					},
				)
				delete(state.resolvedBuffers, strings.ToLower(key))
			}
		case "drawindexed", "drawindexedinstanced":
			draw, ok := parseModelViewerDrawIndexed(key, value, c.variables)
			if !ok {
				continue
			}
			state.explicitDraw = true
			c.draws++
			if c.draws > maxModelViewerDraws {
				return contractError(
					fmt.Sprintf("Mod has too many draws (%d; limit %d).", c.draws, maxModelViewerDraws),
				)
			}
			if !c.resourcesOnly {
				state.draws = append(state.draws, c.snapshotRecords(state, draw, draw.Auto, conditions)...)
			}
		default:
			resource := modelViewerTrimTextureValue(value)
			if resource == "" {
				continue
			}
			if modelViewerNormalizeKey(key) == "this" {
				state.thisHistory = append(
					state.thisHistory,
					modelViewerSymbolicAssignment{
						resource:   resource,
						conditions: cloneModelViewerDNF(conditions),
						sequence:   sequence,
					},
				)
			}
			if isNonDiffusePsSlot(key, resource) {
				state.nonDiffuse = appendUniqueModelViewer(state.nonDiffuse, resource)
			}
			if role, resource, authored, texture := modelViewerTextureAssignment(key, value, c.sectionName); texture {
				state.textures[role] = append(
					state.textures[role],
					modelViewerSymbolicAssignment{
						resource:   resource,
						authored:   authored,
						conditions: cloneModelViewerDNF(conditions),
						sequence:   sequence,
					},
				)
			}
		}
	}
	return nil
}

func (c *modelViewerSymbolicScanContext) snapshotRecords(
	state *modelViewerSymbolicSectionState,
	draw modelViewerDrawInstruction,
	auto bool,
	drawConditions ModelViewerDNF,
) []modelViewerDirectDrawRecord {
	states := []modelViewerSymbolicBufferVariant{{conditions: cloneModelViewerDNF(drawConditions)}}
	for _, slot := range []string{"ib", "vb0", "vb1", "vb2"} {
		assignments := state.bufferAssignments(slot)
		if len(assignments) == 0 {
			continue
		}
		var next []modelViewerSymbolicBufferVariant
		for _, current := range states {
			for _, assignment := range assignments {
				conditions := modelViewerDNFAnd(current.conditions, assignment.conditions)
				if len(conditions) == 0 {
					continue
				}
				variant := current
				variant.conditions = conditions
				switch slot {
				case "ib":
					variant.state.ib = assignment.resource
				case "vb0":
					variant.state.vb0 = assignment.resource
				case "vb1":
					variant.state.vb1 = assignment.resource
				case "vb2":
					variant.state.vb2 = assignment.resource
				}
				next = append(next, variant)
			}
		}
		states = next
	}
	textures := modelViewerSymbolicTextureHistory(state, drawConditions)
	authored := false
	for _, assignment := range textures {
		authored = authored || assignment.authored && assignment.role == "diffuse"
	}
	thisFiles := modelViewerSymbolicResourceFiles(state.thisHistory, drawConditions)
	records := make([]modelViewerDirectDrawRecord, 0, len(states))
	for _, variant := range states {
		recordDraw := draw
		recordDraw.IBResourceName = variant.state.ib
		records = append(records, modelViewerDirectDrawRecord{
			sectionName:     c.sectionName,
			state:           variant.state,
			textureHistory:  append([]modelViewerDirectTextureAssignment(nil), textures...),
			authoredDiffuse: authored,
			nonDiffuse:      append([]string(nil), state.nonDiffuse...),
			thisFiles:       thisFiles,
			conditions:      cloneModelViewerDNF(variant.conditions),
			draw:            recordDraw,
			auto:            auto,
		})
	}
	return records
}

type modelViewerSymbolicBufferVariant struct {
	state      modelViewerDirectBufferState
	conditions ModelViewerDNF
}

func (c *modelViewerSymbolicScanContext) implicitRecords(
	state *modelViewerSymbolicSectionState,
) []modelViewerDirectDrawRecord {
	if len(state.buffers["ib"]) == 0 {
		return nil
	}
	return c.snapshotRecords(state, modelViewerDrawInstruction{}, true, modelViewerDNFTrue())
}

func appendModelViewerFallbackVertexBuffers(
	fallback []modelViewerSymbolicAssignment,
	seen map[string]bool,
	assignments []modelViewerSymbolicAssignment,
) []modelViewerSymbolicAssignment {
	for _, assignment := range assignments {
		if assignment.resource == "" {
			continue
		}
		key := modelViewerNormalizeKey(assignment.resource) + "|" + modelViewerSymbolicDNFKey(assignment.conditions)
		if seen[key] {
			continue
		}
		seen[key] = true
		fallback = append(fallback, assignment)
	}
	return fallback
}

func applyModelViewerFallbackVertexBuffers(
	state modelViewerDirectBufferState,
	conditions ModelViewerDNF,
	fallback []modelViewerSymbolicAssignment,
) []modelViewerSymbolicBufferVariant {
	base := modelViewerSymbolicBufferVariant{state: state, conditions: conditions}
	if state.vb0 != "" || state.ib == "" || len(fallback) == 0 {
		return []modelViewerSymbolicBufferVariant{base}
	}
	var hits []modelViewerSymbolicAssignment
	for _, assignment := range fallback {
		if modelViewerDNFIsTrue(assignment.conditions) || modelViewerDNFIntersects(conditions, assignment.conditions) {
			hits = append(hits, assignment)
		}
	}
	hits = preferModelViewerFallbackVertexBuffers(hits, state.ib)
	if len(hits) == 0 {
		return []modelViewerSymbolicBufferVariant{base}
	}
	if resource := uniqueModelViewerFallbackResource(hits); resource != "" {
		base.state.vb0 = resource
		return []modelViewerSymbolicBufferVariant{base}
	}
	var expanded []modelViewerSymbolicBufferVariant
	for _, hit := range hits {
		nextConditions := modelViewerDNFAnd(conditions, hit.conditions)
		if len(nextConditions) == 0 {
			continue
		}
		next := state
		next.vb0 = hit.resource
		expanded = append(expanded, modelViewerSymbolicBufferVariant{state: next, conditions: nextConditions})
	}
	if len(expanded) == 0 {
		return []modelViewerSymbolicBufferVariant{base}
	}
	return expanded
}

func preferModelViewerFallbackVertexBuffers(
	hits []modelViewerSymbolicAssignment,
	ibName string,
) []modelViewerSymbolicAssignment {
	if len(hits) <= 1 {
		return hits
	}
	var matched []modelViewerSymbolicAssignment
	for _, hit := range hits {
		if modelViewerKeyMatches(hit.resource, ibName, false) {
			matched = append(matched, hit)
		}
	}
	if len(matched) > 0 {
		hits = matched
	}
	if len(hits) <= 1 {
		return hits
	}
	suffix, ok := modelViewerNumericSuffix(ibName)
	if !ok {
		return hits
	}
	var sameSuffix []modelViewerSymbolicAssignment
	for _, hit := range hits {
		if value, has := modelViewerNumericSuffix(hit.resource); has && value == suffix {
			sameSuffix = append(sameSuffix, hit)
		}
	}
	if len(sameSuffix) > 0 {
		return sameSuffix
	}
	return hits
}

func uniqueModelViewerFallbackResource(hits []modelViewerSymbolicAssignment) string {
	if len(hits) == 0 {
		return ""
	}
	resource := hits[0].resource
	for _, hit := range hits[1:] {
		if modelViewerNormalizeKey(hit.resource) != modelViewerNormalizeKey(resource) {
			return ""
		}
	}
	return resource
}

func (s *modelViewerSymbolicSectionState) bufferAssignments(slot string) []modelViewerSymbolicAssignment {
	if assignments, cached := s.resolvedBuffers[slot]; cached {
		return assignments
	}
	if len(s.buffers[slot]) == 0 {
		return nil
	}
	if s.resolvedBuffers == nil {
		s.resolvedBuffers = make(map[string][]modelViewerSymbolicAssignment)
	}
	assignments := effectiveModelViewerSymbolicAssignments(s.buffers[slot])
	s.resolvedBuffers[slot] = assignments
	return assignments
}

func effectiveModelViewerSymbolicAssignments(input []modelViewerSymbolicAssignment) []modelViewerSymbolicAssignment {
	if len(input) == 0 {
		return nil
	}
	covered := modelViewerDNFFalse()
	output := make([]modelViewerSymbolicAssignment, 0, len(input))
	seen := make(map[string]bool)
	for index := len(input) - 1; index >= 0; index-- {
		assignment := input[index]
		conditions := modelViewerDNFAnd(assignment.conditions, modelViewerDNFNot(covered))
		covered = modelViewerDNFOr(covered, assignment.conditions)
		if len(conditions) == 0 {
			continue
		}
		key := fmt.Sprintf("%d|%s|%s", assignment.sequence, assignment.resource, modelViewerSymbolicDNFKey(conditions))
		if seen[key] {
			continue
		}
		seen[key] = true
		assignment.conditions = conditions
		output = append(output, assignment)
	}
	for left, right := 0, len(output)-1; left < right; left, right = left+1, right-1 {
		output[left], output[right] = output[right], output[left]
	}
	return output
}

func modelViewerSymbolicTextureHistory(
	state *modelViewerSymbolicSectionState,
	drawConditions ModelViewerDNF,
) []modelViewerDirectTextureAssignment {
	var output []modelViewerDirectTextureAssignment
	for role, history := range state.textures {
		for _, assignment := range effectiveModelViewerSymbolicAssignments(history) {
			conditions := modelViewerDNFAnd(drawConditions, assignment.conditions)
			if len(conditions) == 0 {
				continue
			}
			output = append(
				output,
				modelViewerDirectTextureAssignment{
					role:       role,
					resource:   assignment.resource,
					authored:   assignment.authored,
					conditions: conditions,
				},
			)
		}
	}
	return output
}

func modelViewerSymbolicResourceFiles(history []modelViewerSymbolicAssignment, conditions ModelViewerDNF) []string {
	var output []string
	for _, assignment := range effectiveModelViewerSymbolicAssignments(history) {
		if len(modelViewerDNFAnd(conditions, assignment.conditions)) > 0 {
			output = appendUniqueModelViewer(output, assignment.resource)
		}
	}
	return output
}

func modelViewerSymbolicStackConditions(stack []modelViewerSymbolicBranchFrame) ModelViewerDNF {
	conditions := modelViewerDNFTrue()
	for _, frame := range stack {
		conditions = modelViewerDNFAnd(conditions, frame.current)
		if len(conditions) == 0 {
			break
		}
	}
	return conditions
}

func modelViewerSymbolicStackSequence(stack []modelViewerSymbolicBranchFrame) int {
	if len(stack) == 0 {
		return 0
	}
	return stack[len(stack)-1].seq
}

func modelViewerAliases(variables map[string]any) map[string]ModelViewerDNF {
	aliases, _ := variables["__aliases"].(map[string]ModelViewerDNF)
	return aliases
}

func modelViewerSymbolicDNFKey(dnf ModelViewerDNF) string {
	return fmt.Sprintf("%#v", dnf)
}
