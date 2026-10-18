package efmioutline

import (
	"errors"
	"fmt"
	"strings"
)

const exclusionSuffix = " && vs != 203"

type part struct {
	id         string
	section    string
	label      string
	sections   []string
	lines      []int
	candidates int
	applied    int
}

func (p *part) isApplied() bool { return p.candidates == 0 && p.applied > 0 }

func (doc *document) parts() ([]*part, map[string]*part) {
	var ordered []*part
	byID := map[string]*part{}
	for _, draw := range doc.draws {
		if draw.class != classCandidate && draw.class != classApplied {
			continue
		}
		p := byID[draw.key]
		if p == nil {
			p = &part{id: draw.key, section: sectionBase(draw.section), label: draw.label}
			byID[draw.key] = p
			ordered = append(ordered, p)
		}
		if !containsFold(p.sections, draw.section) {
			p.sections = append(p.sections, draw.section)
		}
		p.lines = append(p.lines, draw.line+1)
		if draw.class == classCandidate {
			p.candidates++
		} else {
			p.applied++
		}
	}
	return ordered, byID
}

type manualItem struct {
	section string
	line    int
	text    string
	reason  manualReason
}

// manualItems lists what the analyzer could not decide: draws under conditions
// it cannot evaluate, and calls from the outline-only path into sections that
// draw geometry themselves.
func (doc *document) manualItems() []manualItem {
	var items []manualItem
	for _, draw := range doc.draws {
		if draw.class == classManual {
			items = append(items, doc.manualItem(draw.section, draw.line, draw.reason))
		}
	}
	for _, call := range doc.calls {
		if !doc.geometry[strings.ToLower(call.target)] {
			continue
		}
		conditions := pathConditions(call.path)
		if !anyCondition(conditions, func(c *condition) bool { return c.vs }) {
			continue
		}
		switch {
		case call.structural:
			items = append(items, doc.manualItem(call.section, call.line, reasonStructure))
		case anyCondition(conditions, func(c *condition) bool { return c.vsUnknown }),
			classifyPath(call.path, false) != classIgnore:
			items = append(items, doc.manualItem(call.section, call.line, reasonCall))
		}
	}
	return items
}

func (doc *document) manualItem(section string, line int, reason manualReason) manualItem {
	return manualItem{
		section: section,
		line:    line + 1,
		text:    strings.TrimSpace(doc.lines[line].text),
		reason:  reason,
	}
}

var errSelfCheck = errors.New("excluded draws did not verify")

// applyExclusions removes the selected parts from the outline pass. A
// selected draw whose innermost if holds nothing but draws of the part gets
// "&& vs != 203" appended to that if; any other draw is wrapped in its own
// "if vs != 203" block, because appending to an if with elif or else
// branches would run those branches in the outline pass instead.
func applyExclusions(doc *document, selected map[string]bool) ([]byte, error) {
	lines := append([]sourceLine(nil), doc.lines...)
	before := map[int]sourceLine{}
	after := map[int]sourceLine{}
	appended := map[int]bool{}

	for _, draw := range doc.draws {
		if draw.class != classCandidate || !selected[draw.key] {
			continue
		}
		if target, ok := appendTarget(draw); ok {
			if !appended[target.line] {
				appended[target.line] = true
				lines[target.line].text = withExclusion(lines[target.line].text, target.cond)
			}
			continue
		}

		indent := leadingSpace(lines[draw.line].text)
		eol := lines[draw.line].eol
		if eol == "" {
			eol = previousEOL(lines, draw.line)
			lines[draw.line].eol = eol
			after[draw.line] = sourceLine{text: indent + "endif"}
		} else {
			after[draw.line] = sourceLine{text: indent + "endif", eol: eol}
		}
		before[draw.line] = sourceLine{text: indent + "if vs != 203", eol: eol}
	}

	out := make([]sourceLine, 0, len(lines)+2*len(before))
	for index, line := range lines {
		if inserted, ok := before[index]; ok {
			out = append(out, inserted)
		}
		out = append(out, line)
		if inserted, ok := after[index]; ok {
			out = append(out, inserted)
		}
	}
	data := joinLines(out)
	if err := verifyExclusions(doc, parseDocument(data), selected); err != nil {
		return nil, err
	}
	return data, nil
}

func appendTarget(draw *drawRecord) (*branch, bool) {
	if len(draw.path) == 0 {
		return nil, false
	}
	innermost := draw.path[len(draw.path)-1]
	target := innermost.current()
	if len(innermost.frame.branches) != 1 || target.cond == nil || target.cond.vs || target.cond.root == nil ||
		!target.onlyDraws {
		return nil, false
	}
	return target, true
}

func withExclusion(text string, cond *condition) string {
	body := strings.TrimRight(text, " \t")
	if !cond.isOr() {
		return body + exclusionSuffix
	}
	keyword := len(leadingSpace(body)) + len("if")
	rest := body[keyword:]
	gap := leadingSpace(rest)
	if gap == "" {
		gap = " "
	}
	return body[:keyword] + gap + "(" + strings.TrimSpace(rest) + ")" + exclusionSuffix
}

// verifyExclusions re-reads the edited file: every selected draw must now be
// excluded from the outline pass and every other draw must behave as before.
func verifyExclusions(original, edited *document, selected map[string]bool) error {
	if len(original.draws) != len(edited.draws) {
		return fmt.Errorf("%w: draw count changed from %d to %d", errSelfCheck, len(original.draws), len(edited.draws))
	}
	for index, before := range original.draws {
		after := edited.draws[index]
		if after.key != before.key {
			return fmt.Errorf("%w: line %d moved to part %q", errSelfCheck, before.line+1, after.key)
		}
		if before.class == classCandidate && selected[before.key] {
			if after.class != classApplied {
				return fmt.Errorf("%w: line %d still reaches the outline pass", errSelfCheck, before.line+1)
			}
			continue
		}
		if after.class != before.class || after.r203 != before.r203 || after.r201 != before.r201 {
			return fmt.Errorf("%w: unselected line %d changed", errSelfCheck, before.line+1)
		}
	}
	return nil
}

func leadingSpace(text string) string {
	return text[:len(text)-len(strings.TrimLeft(text, " \t"))]
}

func previousEOL(lines []sourceLine, index int) string {
	for i := index - 1; i >= 0; i-- {
		if lines[i].eol != "" {
			return lines[i].eol
		}
	}
	return "\r\n"
}

func containsFold(values []string, value string) bool {
	for _, existing := range values {
		if strings.EqualFold(existing, value) {
			return true
		}
	}
	return false
}
