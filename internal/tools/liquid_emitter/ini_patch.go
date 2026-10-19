package liquidemitter

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
	modelviewer "nahida.live/desktop/internal/tools/model_viewer"
)

const captureCommand = `run = CommandList\LiquidGravityProbe\CaptureSurface`

var hookMarkerRE = regexp.MustCompile(`(?i)^\s*;\s*nahida-liquid\s+(\d+)\s+(begin|end)\s*$`)

// iniText keeps an INI as lines so hooks can be inserted and removed without
// touching any other byte of the file. Lines split on LF and keep their own
// CR, so a file mixing CRLF and LF lines round-trips unchanged.
type iniText struct {
	bom      []byte
	lines    []string
	terminal bool
}

func isUTF16(data []byte) bool {
	return bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff})
}

func parseINIText(data []byte) (*iniText, error) {
	if isUTF16(data) {
		return nil, infra.ContractError(ErrorUnsupportedINI + ":utf16")
	}
	text := &iniText{}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		text.bom = []byte{0xef, 0xbb, 0xbf}
		data = data[3:]
	}
	body := string(data)
	text.terminal = strings.HasSuffix(body, "\n")
	body = strings.TrimSuffix(body, "\n")
	if body != "" || text.terminal {
		text.lines = strings.Split(body, "\n")
	}
	return text, nil
}

func (t *iniText) bytes() []byte {
	body := strings.Join(t.lines, "\n")
	if t.terminal {
		body += "\n"
	}
	return append(append([]byte(nil), t.bom...), body...)
}

// damagedHook returns the 1-based line of the first hook block that lost a
// marker, or 0. Removing a block without its end marker would drop every
// line after it.
func (t *iniText) damagedHook() int {
	open, current := 0, 0
	for index, line := range t.lines {
		pid, begin, marker := hookMarker(line)
		switch {
		case !marker:
		case begin && open == 0:
			open, current = index+1, pid
		case begin || open == 0 || pid != current:
			return index + 1
		default:
			open = 0
		}
	}
	return open
}

func hookMarker(line string) (pid int, begin, ok bool) {
	match := hookMarkerRE.FindStringSubmatch(line)
	if match == nil {
		return 0, false, false
	}
	pid, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false, false
	}
	return pid, strings.EqualFold(match[2], "begin"), true
}

func (t *iniText) hookPIDs() map[int]bool {
	pids := make(map[int]bool)
	for _, line := range t.lines {
		if pid, begin, ok := hookMarker(line); ok && begin {
			pids[pid] = true
		}
	}
	return pids
}

// hookSections maps each hook's pid to the section that holds its first block.
func (t *iniText) hookSections() map[int]string {
	sections := make(map[int]string)
	current := ""
	for _, raw := range t.lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if pid, begin, ok := hookMarker(raw); ok && begin {
			if _, seen := sections[pid]; !seen {
				sections[pid] = current
			}
		}
	}
	return sections
}

// removeHooks deletes every hook block whose pid remove accepts and reports
// how many blocks it removed.
func (t *iniText) removeHooks(remove func(pid int) bool) int {
	output := make([]string, 0, len(t.lines))
	removed, dropping, current := 0, false, 0
	for _, line := range t.lines {
		pid, begin, marker := hookMarker(line)
		switch {
		case marker && begin && !dropping && remove(pid):
			dropping, current = true, pid
			removed++
			continue
		case dropping && marker && !begin && pid == current:
			dropping = false
			continue
		case dropping:
			continue
		}
		output = append(output, line)
	}
	t.lines = output
	return removed
}

// renumberHooks moves every hook block whose pid is a key of pids to the
// mapped pid and reports how many blocks it changed.
func (t *iniText) renumberHooks(pids map[int]int) int {
	changed, current, inside := 0, 0, false
	for index, line := range t.lines {
		indent, eol := lineIndent(line), lineEnding(line)
		pid, begin, marker := hookMarker(line)
		switch {
		case marker && begin && !inside:
			if to, ok := pids[pid]; ok {
				current, inside = pid, true
				changed++
				t.lines[index] = fmt.Sprintf("%s; nahida-liquid %d begin%s", indent, to, eol)
			}
		case inside && marker && !begin && pid == current:
			inside = false
			t.lines[index] = fmt.Sprintf("%s; nahida-liquid %d end%s", indent, pids[current], eol)
		case inside && hookPIDLineRE.MatchString(line):
			t.lines[index] = fmt.Sprintf("%sx243 = %d%s", indent, pids[current], eol)
		}
	}
	return changed
}

func lineIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func lineEnding(line string) string {
	if strings.HasSuffix(line, "\r") {
		return "\r"
	}
	return ""
}

var hookPIDLineRE = regexp.MustCompile(`(?i)^\s*x243\s*=\s*\d+\s*$`)

type drawSite struct {
	line   int
	indent string
	// pid is the hook directly above the draw, or -1.
	pid int
}

// drawSites finds the drawindexed lines with the draw's literal arguments
// that run under [TextureOverride<section>] with the draw's index buffer
// bound, following run = and CommandList assignments within the same file.
func (t *iniText) drawSites(source modelviewer.DrawSource) []drawSite {
	bodies := make(map[string][][2]int)
	current := ""
	for index, raw := range t.lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.ToLower(strings.TrimSpace(line[1 : len(line)-1]))
			bodies[current] = append(bodies[current], [2]int{index + 1, index + 1})
			continue
		}
		if current != "" {
			ranges := bodies[current]
			ranges[len(ranges)-1][1] = index + 1
		}
	}

	walk := &drawWalk{text: t, bodies: bodies, source: source, found: map[int]bool{}}
	// An empty name stands for the buffer bound before the override runs.
	walk.visit(strings.ToLower("TextureOverride"+source.Section), []string{""}, map[string]bool{})
	return walk.sites
}

type drawWalk struct {
	text       *iniText
	bodies     map[string][][2]int
	source     modelviewer.DrawSource
	found      map[int]bool
	sites      []drawSite
	expansions int
}

// ifFrame is an open if block: the buffers bound when it was entered and
// those bound at the end of each branch already walked.
type ifFrame struct {
	entry, exits []string
	hasElse      bool
}

// visit walks a section in execution order with the index buffers that may
// be bound on entry and returns those that may be bound on exit. Each branch
// of an if block starts from the buffers bound at the if, and the block ends
// with those any of its branches, or skipping it without an else, can leave.
func (w *drawWalk) visit(name string, bound []string, active map[string]bool) []string {
	if active[name] || w.expansions >= 256 {
		return bound
	}
	w.expansions++
	active[name] = true
	defer delete(active, name)

	var frames []ifFrame
	endIf := func() {
		frame := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		bound = unionBuffers(frame.exits, bound)
		if !frame.hasElse {
			bound = unionBuffers(bound, frame.entry)
		}
	}
	for _, span := range w.bodies[name] {
		for index := span[0]; index < span[1]; index++ {
			line := strings.TrimSpace(strings.SplitN(w.text.lines[index], ";", 2)[0])
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				frames = append(frames, ifFrame{entry: bound})
				continue
			case strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if ") || lower == "else":
				if len(frames) > 0 {
					frame := &frames[len(frames)-1]
					frame.exits = unionBuffers(frame.exits, bound)
					frame.hasElse = frame.hasElse || lower == "else"
					bound = frame.entry
				}
				continue
			case lower == "endif":
				if len(frames) > 0 {
					endIf()
				}
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
			switch nested := nestedSection(key, value); {
			case nested != "":
				bound = w.visit(nested, bound, active)
			case key == "ib":
				bound = []string{value}
			case drawMatches(key, value, w.source) && !w.found[index] &&
				slices.ContainsFunc(bound, func(ib string) bool { return ib == "" || w.source.MatchesIB(ib) }):
				w.found[index] = true
				w.sites = append(w.sites, drawSite{
					line:   index,
					indent: lineIndent(w.text.lines[index]),
					pid:    w.text.hookAbove(index),
				})
			}
		}
	}
	for len(frames) > 0 {
		endIf()
	}
	return bound
}

func unionBuffers(a, b []string) []string {
	return slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(a), b...))))
}

func nestedSection(key, value string) string {
	target := value
	if strings.HasPrefix(strings.ToLower(target), "ref ") {
		target = strings.TrimSpace(target[4:])
	}
	if key == "run" || strings.HasPrefix(strings.ToLower(target), "commandlist") {
		return strings.ToLower(target)
	}
	return ""
}

// drawMatches compares only the index range. EFMI writes the instance
// arguments of drawindexedinstanced as INSTANCE_COUNT and FIRST_INSTANCE.
func drawMatches(key, value string, source modelviewer.DrawSource) bool {
	fields := strings.Split(value, ",")
	var want map[int]int
	switch {
	case key == "drawindexed" && len(fields) == 3:
		want = map[int]int{0: source.IndexCount, 1: source.StartIndex, 2: source.BaseVertex}
	case key == "drawindexedinstanced" && len(fields) == 5:
		want = map[int]int{0: source.IndexCount, 2: source.StartIndex, 3: source.BaseVertex}
	default:
		return false
	}
	for field, expected := range want {
		number, err := strconv.Atoi(strings.TrimSpace(fields[field]))
		if err != nil || number != expected {
			return false
		}
	}
	return true
}

func (t *iniText) hookAbove(line int) int {
	if line == 0 {
		return -1
	}
	if pid, begin, ok := hookMarker(t.lines[line-1]); ok && !begin {
		return pid
	}
	return -1
}

// insertHooks puts one hook block directly above each site. Sites must come
// from the current lines.
func (t *iniText) insertHooks(sites []drawSite, pid, firstIndex, baseVertex int) {
	// drawSites lists sites in traversal order, which need not follow the file;
	// inserting bottom-up keeps every earlier line number valid.
	sites = slices.Clone(sites)
	slices.SortFunc(sites, func(a, b drawSite) int { return b.line - a.line })
	for _, site := range sites {
		eol := lineEnding(t.lines[site.line])
		block := []string{
			fmt.Sprintf("%s; nahida-liquid %d begin%s", site.indent, pid, eol),
			fmt.Sprintf("%sx243 = %d%s", site.indent, pid, eol),
			fmt.Sprintf("%sy243 = %d%s", site.indent, firstIndex, eol),
			fmt.Sprintf("%sz243 = %d%s", site.indent, baseVertex, eol),
			site.indent + captureCommand + eol,
			fmt.Sprintf("%s; nahida-liquid %d end%s", site.indent, pid, eol),
		}
		t.lines = append(t.lines[:site.line], append(block, t.lines[site.line:]...)...)
	}
}
