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
	// line is where a new hook block goes: the draw line, or for an implicit
	// draw the line below the hooks stacked under its ib = line.
	line int
	// key is the line the walk matched: the draw line or the ib = line.
	key    int
	indent string
	// pids are the hooks stacked directly above line, nearest first.
	pids []int
}

// drawSites finds the drawindexed lines with the draw's literal arguments
// that run under [TextureOverride<section>] with the draw's index buffer
// bound, following run = and CommandList assignments within the same file.
// An implicit draw has no such line, so its sites are the ib = lines that
// bind its buffer: the capture there runs with the same buffers bound as the
// drawindexed = auto of the hash that follows.
func (t *iniText) drawSites(source modelviewer.DrawSource) []drawSite {
	return t.ownWalk(source).sites
}

// bindsVertexBuffersLate reports whether the override of an implicit draw
// binds a vertex buffer on some path after one of its sites has captured, as
// when it binds the buffer again over other vertex buffers.
func (t *iniText) bindsVertexBuffersLate(source modelviewer.DrawSource) bool {
	return source.Implicit && t.ownWalk(source).lateVertexBuffers
}

func (t *iniText) ownWalk(source modelviewer.DrawSource) *drawWalk {
	walk := newDrawWalk(t, t.sectionBodies(), source)
	// An empty name stands for the buffer bound before the override runs.
	walk.visit(strings.ToLower("TextureOverride"+source.Section), walkState{bound: []string{""}}, map[string]bool{})
	return walk
}

// sharedDraw reports whether any of sites can also run with another index
// buffer bound, through a command list that another section runs or another
// branch of the same override. The hook above it would fire for that other
// part as well, and an emitter added there would take this one's slot.
func (t *iniText) sharedDraw(source modelviewer.DrawSource, sites []drawSite) bool {
	bodies := t.sectionBodies()
	own := strings.ToLower("TextureOverride" + source.Section)
	for name := range bodies {
		walk := newDrawWalk(t, bodies, source)
		walk.visit(name, walkState{bound: []string{""}}, map[string]bool{})
		// The index buffer aliases of a frame animation hold only under its own
		// override, or a section that is only ever run (a command list or custom
		// shader) walked on its own. Another override reaching the site under
		// any of them, or under the buffer it inherits, draws its own vertex
		// buffers.
		called := strings.HasPrefix(name, "commandlist") || strings.HasPrefix(name, "customshader") ||
			strings.HasPrefix(name, "builtin")
		foreign := len(source.IBAliases) > 1 && name != own && !called
		for _, site := range sites {
			if foreign && len(walk.reached[site.key]) > 0 {
				return true
			}
			if slices.ContainsFunc(walk.reached[site.key], func(ib string) bool {
				return ib != "" && !source.MatchesIB(ib)
			}) {
				return true
			}
		}
	}
	return false
}

// sectionBodies maps each lowercase section name to the line ranges of its
// bodies; a name may repeat in one file.
func (t *iniText) sectionBodies() map[string][][2]int {
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
	return bodies
}

type drawWalk struct {
	text   *iniText
	bodies map[string][][2]int
	source modelviewer.DrawSource
	found  map[int]bool
	sites  []drawSite
	// reached holds the buffers that may be bound at each draw with the
	// source's arguments, whether or not they match.
	reached    map[int][]string
	expansions int
	// lateVertexBuffers is set when a path binds a vertex buffer after the
	// capture of an implicit draw's site has run on it. The runtime keeps the
	// first capture of a draw, which would then hold stale buffers.
	lateVertexBuffers bool
	// covered maps each vertex buffer line bound directly below an implicit
	// site's ib = line to that site; its hook goes below them.
	covered map[int]int
}

func newDrawWalk(text *iniText, bodies map[string][][2]int, source modelviewer.DrawSource) *drawWalk {
	return &drawWalk{
		text: text, bodies: bodies, source: source,
		found: map[int]bool{}, reached: map[int][]string{}, covered: map[int]int{},
	}
}

// walkState is what one execution path carries: the index buffers that may
// be bound and the implicit sites whose capture may already have run.
type walkState struct {
	bound    []string
	captured []int
}

func unionStates(a, b walkState) walkState {
	return walkState{bound: unionBuffers(a.bound, b.bound), captured: unionLines(a.captured, b.captured)}
}

func unionLines(a, b []int) []int {
	return slices.Compact(slices.Sorted(slices.Values(append(slices.Clone(a), b...))))
}

func lineKey(line string) string {
	key, _, _ := strings.Cut(strings.SplitN(line, ";", 2)[0], "=")
	return strings.ToLower(strings.TrimSpace(key))
}

// ifFrame is an open if block: the state when it was entered and the states
// at the end of each branch already walked.
type ifFrame struct {
	entry, exits walkState
	hasElse      bool
}

// visit walks a section in execution order with the state that may hold on
// entry and returns the state that may hold on exit. Each branch of an if
// block starts from the state at the if, and the block ends with what any of
// its branches, or skipping it without an else, can leave.
func (w *drawWalk) visit(name string, state walkState, active map[string]bool) walkState {
	if active[name] || w.expansions >= 256 {
		return state
	}
	w.expansions++
	active[name] = true
	defer delete(active, name)

	var frames []ifFrame
	endIf := func() {
		frame := frames[len(frames)-1]
		frames = frames[:len(frames)-1]
		state = unionStates(frame.exits, state)
		if !frame.hasElse {
			state = unionStates(state, frame.entry)
		}
	}
	for _, span := range w.bodies[name] {
		for index := span[0]; index < span[1]; index++ {
			line := strings.TrimSpace(strings.SplitN(w.text.lines[index], ";", 2)[0])
			lower := strings.ToLower(line)
			switch {
			case strings.HasPrefix(lower, "if "):
				frames = append(frames, ifFrame{entry: state})
				continue
			case strings.HasPrefix(lower, "elif ") || strings.HasPrefix(lower, "else if ") || lower == "else":
				if len(frames) > 0 {
					frame := &frames[len(frames)-1]
					frame.exits = unionStates(frame.exits, state)
					frame.hasElse = frame.hasElse || lower == "else"
					state = frame.entry
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
				state = w.visit(nested, state, active)
			case key == "ib":
				state.bound = []string{value}
				if !w.source.Implicit || strings.EqualFold(value, "null") || !w.source.MatchesIB(value) {
					continue
				}
				w.reached[index] = unionBuffers(w.reached[index], state.bound)
				state.captured = unionLines(state.captured, []int{index})
				if !w.found[index] {
					w.found[index] = true
					// Vertex buffers bound right below the index buffer, as SRMI binds
					// vb1, come before the hook.
					last := index
					for last+1 < span[1] && vertexBufferKeyRE.MatchString(lineKey(w.text.lines[last+1])) {
						last++
						w.covered[last] = index
					}
					at := w.text.belowHooks(last + 1)
					w.sites = append(w.sites, drawSite{
						line:   at,
						key:    index,
						indent: lineIndent(w.text.lines[index]),
						pids:   w.text.hooksAbove(at),
					})
				}
			case vertexBufferKeyRE.MatchString(key):
				owner, covered := w.covered[index]
				w.lateVertexBuffers = w.lateVertexBuffers || slices.ContainsFunc(state.captured, func(site int) bool {
					return !covered || site != owner
				})
			case drawMatches(key, value, w.source):
				w.reached[index] = unionBuffers(w.reached[index], state.bound)
				if w.found[index] ||
					!slices.ContainsFunc(
						state.bound,
						func(ib string) bool { return ib == "" || w.source.MatchesIB(ib) },
					) {
					continue
				}
				w.found[index] = true
				w.sites = append(w.sites, drawSite{
					line:   index,
					key:    index,
					indent: lineIndent(w.text.lines[index]),
					pids:   w.text.hooksAbove(index),
				})
			}
		}
	}
	for len(frames) > 0 {
		endIf()
	}
	return state
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

var vertexBufferKeyRE = regexp.MustCompile(`^vb\d+$`)

// drawMatches compares only the index range. EFMI writes the instance
// arguments of drawindexedinstanced as INSTANCE_COUNT and FIRST_INSTANCE.
// 3DMigoto runs an auto draw over the whole bound index buffer from index 0
// with base vertex 0, so it matches any auto line.
func drawMatches(key, value string, source modelviewer.DrawSource) bool {
	if source.Implicit || key != "drawindexed" && key != "drawindexedinstanced" {
		return false
	}
	if source.Auto {
		return strings.EqualFold(value, "auto")
	}
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

// belowHooks returns the first line at or after line that does not belong to
// a complete hook block stacked there.
func (t *iniText) belowHooks(line int) int {
	for line < len(t.lines) {
		pid, begin, ok := hookMarker(t.lines[line])
		if !ok || !begin {
			return line
		}
		end := line + 1
		for ; end < len(t.lines); end++ {
			if other, otherBegin, marker := hookMarker(t.lines[end]); marker && !otherBegin && other == pid {
				break
			}
		}
		if end == len(t.lines) {
			return line
		}
		line = end + 1
	}
	return line
}

// hooksAbove lists the pids of the hook blocks stacked directly above line,
// nearest first. A line between two blocks ends the stack.
func (t *iniText) hooksAbove(line int) []int {
	var pids []int
	for end := line - 1; end >= 0; {
		pid, begin, ok := hookMarker(t.lines[end])
		if !ok || begin {
			break
		}
		start := end - 1
		for ; start >= 0; start-- {
			other, otherBegin, marker := hookMarker(t.lines[start])
			if !marker {
				continue
			}
			if !otherBegin || other != pid {
				return pids
			}
			break
		}
		if start < 0 {
			break
		}
		pids = append(pids, pid)
		end = start - 1
	}
	return pids
}

// insertHooks puts one hook block directly above each site, below any blocks
// already stacked there. Sites must come from the current lines.
func (t *iniText) insertHooks(sites []drawSite, pid, firstIndex, baseVertex int) {
	// drawSites lists sites in traversal order, which need not follow the file;
	// inserting bottom-up keeps every earlier line number valid.
	sites = slices.Clone(sites)
	slices.SortFunc(sites, func(a, b drawSite) int { return b.line - a.line })
	for _, site := range sites {
		eol := lineEnding(t.lines[site.key])
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
