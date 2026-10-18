package efmioutline

import (
	"bytes"
	"regexp"
	"strings"
)

const (
	// outlinePass is the filter_index the classifier assigns to the outline
	// (inverted hull) vertex shader on both backends.
	outlinePass = 203
	// dx11OutlinePass is what the community ShaderOverride hash list forces the
	// DX11 outline shader to; Vulkan ignores that list because its hashes differ.
	dx11OutlinePass = 201
)

var (
	ifRE          = regexp.MustCompile(`(?i)^if\b(.*)$`)
	elifRE        = regexp.MustCompile(`(?i)^(?:elif|else\s+if)\b(.*)$`)
	elseRE        = regexp.MustCompile(`(?i)^else$`)
	endifRE       = regexp.MustCompile(`(?i)^endif$`)
	drawRE        = regexp.MustCompile(`(?i)^(?:(?:pre|post)\s+)?draw\w*\s*=`)
	geometryRE    = regexp.MustCompile(`(?i)^(?:(?:pre|post)\s+)?draw(?:indexed\w*|instanced\w*|auto)\s*=`)
	runRE         = regexp.MustCompile(`(?i)^(?:(?:pre|post)\s+)?run\s*=\s*(.+?)\s*$`)
	lodSuffixRE   = regexp.MustCompile(`(?i)[_.]?lod\d*$`)
	spaceRunRE    = regexp.MustCompile(`\s+`)
	exclusionRE   = regexp.MustCompile(`(?i)\s*&&\s*vs\s*!=\s*203\s*$`)
	utf8BOM       = []byte{0xEF, 0xBB, 0xBF}
	utf8BOMString = string(utf8BOM)
)

type sourceLine struct {
	text string
	eol  string
}

// splitLines keeps each line's own terminator so that joinLines restores the
// exact bytes; mod files mix CRLF and LF within one file.
func splitLines(data []byte) []sourceLine {
	var lines []sourceLine
	for len(data) > 0 {
		index := bytes.IndexByte(data, '\n')
		if index < 0 {
			lines = append(lines, sourceLine{text: string(data)})
			break
		}
		text, eol := data[:index], "\n"
		if len(text) > 0 && text[len(text)-1] == '\r' {
			text, eol = text[:len(text)-1], "\r\n"
		}
		lines = append(lines, sourceLine{text: string(text), eol: eol})
		data = data[index+1:]
	}
	return lines
}

func joinLines(lines []sourceLine) []byte {
	var out bytes.Buffer
	for _, line := range lines {
		out.WriteString(line.text)
		out.WriteString(line.eol)
	}
	return out.Bytes()
}

type branch struct {
	cond *condition // nil for else
	line int
	// onlyDraws stays true while every direct statement of the branch is a draw.
	onlyDraws  bool
	statements int
}

type frame struct {
	branches []*branch
}

type step struct {
	frame  *frame
	branch int
}

func (s step) current() *branch { return s.frame.branches[s.branch] }

type drawClass uint8

const (
	classIgnore drawClass = iota
	classCandidate
	classApplied
	classManual
)

type manualReason string

const (
	reasonCondition manualReason = "condition"
	reasonStructure manualReason = "structure"
	reasonCall      manualReason = "call"
)

type drawRecord struct {
	line       int
	section    string
	path       []step
	structural bool
	class      drawClass
	reason     manualReason
	key        string
	label      string
	r203, r201 tri
}

type callRecord struct {
	line       int
	section    string
	target     string
	path       []step
	structural bool
}

type document struct {
	lines    []sourceLine
	draws    []*drawRecord
	calls    []*callRecord
	geometry map[string]bool
}

func parseDocument(data []byte) *document {
	doc := &document{lines: splitLines(data), geometry: map[string]bool{}}
	var (
		section     string
		path        []step
		broken      bool
		sectionFrom int
		callsFrom   int
	)
	finishSection := func() {
		if broken || len(path) > 0 {
			for _, draw := range doc.draws[sectionFrom:] {
				draw.structural = true
			}
			for _, call := range doc.calls[callsFrom:] {
				call.structural = true
			}
		}
		path, broken = nil, false
		sectionFrom, callsFrom = len(doc.draws), len(doc.calls)
	}
	addStatement := func(draw bool) {
		if len(path) == 0 {
			return
		}
		current := path[len(path)-1].current()
		current.statements++
		current.onlyDraws = current.onlyDraws && draw
	}
	// addBranch reports false for an elif or else without an open if, or after
	// an else; 3DMigoto rejects those lines, so the section is not trusted.
	addBranch := func(cond *condition, line int) bool {
		if len(path) == 0 {
			return false
		}
		top := path[len(path)-1].frame
		if top.branches[len(top.branches)-1].cond == nil {
			return false
		}
		top.branches = append(top.branches, &branch{cond: cond, line: line, onlyDraws: true})
		path[len(path)-1].branch = len(top.branches) - 1
		return true
	}

	for index, line := range doc.lines {
		text := line.text
		if index == 0 {
			text = strings.TrimPrefix(text, utf8BOMString)
		}
		trimmed := strings.TrimSpace(text)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			finishSection()
			section = strings.TrimSpace(trimmed[1 : len(trimmed)-1])
			continue
		}
		if section == "" {
			continue
		}

		switch {
		case ifRE.MatchString(trimmed):
			addStatement(false)
			opened := &frame{branches: []*branch{{
				cond: parseCondition(ifRE.FindStringSubmatch(trimmed)[1]), line: index, onlyDraws: true,
			}}}
			path = append(path, step{frame: opened})
		case elifRE.MatchString(trimmed):
			if !addBranch(parseCondition(elifRE.FindStringSubmatch(trimmed)[1]), index) {
				broken = true
			}
		case elseRE.MatchString(trimmed):
			if !addBranch(nil, index) {
				broken = true
			}
		case endifRE.MatchString(trimmed):
			if len(path) == 0 {
				broken = true
				continue
			}
			path = path[:len(path)-1]
		case drawRE.MatchString(trimmed):
			addStatement(true)
			if geometryRE.MatchString(trimmed) {
				doc.geometry[strings.ToLower(section)] = true
			}
			doc.draws = append(doc.draws, &drawRecord{
				line: index, section: section, path: append([]step(nil), path...),
			})
		default:
			addStatement(false)
			if match := runRE.FindStringSubmatch(trimmed); match != nil {
				doc.calls = append(doc.calls, &callRecord{
					line: index, section: section, target: match[1], path: append([]step(nil), path...),
				})
			}
		}
	}
	finishSection()

	for _, draw := range doc.draws {
		doc.classifyDraw(draw)
	}
	return doc
}

func (doc *document) classifyDraw(draw *drawRecord) {
	draw.r203 = reach(draw.path, outlinePass, false)
	draw.r201 = reach(draw.path, dx11OutlinePass, false)

	conditions := pathConditions(draw.path)
	if !anyCondition(conditions, func(c *condition) bool { return c.vs }) {
		return
	}
	draw.label, draw.key = doc.partKey(draw)
	switch {
	case draw.structural:
		draw.class, draw.reason = classManual, reasonStructure
	case anyCondition(conditions, func(c *condition) bool { return c.vsUnknown }):
		draw.class, draw.reason = classManual, reasonCondition
	default:
		draw.class = classifyPath(draw.path, false)
		if draw.class == classManual {
			draw.reason = reasonCondition
		}
		if draw.class == classIgnore && draw.r203 == triFalse && classifyPath(draw.path, true) == classCandidate {
			draw.class = classApplied
		}
	}
}

// classifyPath compares where the draw runs on Vulkan (outline pass is 203)
// with DX11 under the community override (outline pass is 201). Only a draw
// that may run in the first but never in the second differs between backends.
func classifyPath(path []step, ignoreExclusion bool) drawClass {
	r203 := reach(path, outlinePass, ignoreExclusion)
	r201 := reach(path, dx11OutlinePass, ignoreExclusion)
	switch {
	case r201 == triFalse && r203 != triFalse:
		return classCandidate
	case r201 == triUnknown && r203 != triFalse && vsSplits(path, ignoreExclusion):
		return classManual
	default:
		return classIgnore
	}
}

// reach evaluates whether control can arrive at the end of path for the given
// pass. An elif or else branch requires every earlier branch to be false.
func reach(path []step, pass int, ignoreExclusion bool) tri {
	result := triTrue
	for _, s := range path {
		for _, earlier := range s.frame.branches[:s.branch] {
			result = triAnd(result, earlier.cond.eval(pass, ignoreExclusion).not())
		}
		result = triAnd(result, s.current().cond.eval(pass, ignoreExclusion))
	}
	return result
}

func vsSplits(path []step, ignoreExclusion bool) bool {
	for _, c := range pathConditions(path) {
		for _, atom := range c.vsAtoms {
			if atom.eval(outlinePass, ignoreExclusion) != atom.eval(dx11OutlinePass, ignoreExclusion) {
				return true
			}
		}
	}
	return false
}

func pathConditions(path []step) []*condition {
	var conditions []*condition
	for _, s := range path {
		for _, b := range s.frame.branches[:s.branch+1] {
			if b.cond != nil {
				conditions = append(conditions, b.cond)
			}
		}
	}
	return conditions
}

func anyCondition(conditions []*condition, match func(*condition) bool) bool {
	for _, c := range conditions {
		if match(c) {
			return true
		}
	}
	return false
}

// partKey groups draws by the innermost condition that names a part, so the
// LOD copy of a command list folds into the same part while the same variable
// drawn from another component stays separate.
func (doc *document) partKey(draw *drawRecord) (label, key string) {
	for i := len(draw.path) - 1; i >= 0 && label == ""; i-- {
		cond := draw.path[i].current().cond
		if cond == nil {
			break
		}
		text := stripExclusion(cond.text)
		if text != cond.text {
			label = text
			break
		}
		if !cond.vs {
			label = text
		}
	}
	if label == "" {
		label = strings.TrimSpace(doc.lines[draw.line].text)
	}
	label = spaceRunRE.ReplaceAllString(label, " ")
	return label, strings.ToLower(sectionBase(draw.section)) + "|" + strings.ToLower(label)
}

// stripExclusion removes a trailing "&& vs != 203" and the parentheses this
// tool adds around a disjunction before appending it.
func stripExclusion(text string) string {
	stripped := exclusionRE.ReplaceAllString(text, "")
	if stripped == text {
		return text
	}
	stripped = strings.TrimSpace(stripped)
	if strings.HasPrefix(stripped, "(") && strings.HasSuffix(stripped, ")") {
		inner := stripped[1 : len(stripped)-1]
		if balancedParens(inner) && parseCondition(inner).isOr() {
			return strings.TrimSpace(inner)
		}
	}
	return stripped
}

func balancedParens(text string) bool {
	depth := 0
	for _, r := range text {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func sectionBase(section string) string {
	return lodSuffixRE.ReplaceAllString(section, "")
}
