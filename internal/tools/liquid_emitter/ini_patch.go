package liquidemitter

import (
	"bytes"
	"cmp"
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

// hookSectionRE matches the line of a hook block naming the override it
// captures for, since the block may sit in a command list of another INI.
// Blocks written before the line existed lack it.
var hookSectionRE = regexp.MustCompile(`(?i)^\s*;\s*nahida-liquid\s+section\s+(\S.*?)\s*$`)

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

// hookSections maps each hook's pid to the override its first block names,
// or for a block written without one, the section that holds it.
func (t *iniText) hookSections() map[int]string {
	sections := make(map[int]string)
	current, first := "", -1
	for _, raw := range t.lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		if pid, begin, ok := hookMarker(raw); ok {
			first = -1
			if _, seen := sections[pid]; begin && !seen {
				sections[pid], first = current, pid
			}
			continue
		}
		if match := hookSectionRE.FindStringSubmatch(raw); match != nil && first >= 0 {
			sections[first] = match[1]
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
	// file indexes the unit member holding the site.
	file int
	// line is where a new hook block goes: the draw line, or for an implicit
	// draw the line below the hooks stacked under its ib = line.
	line int
	// key is the line the walk matched: the draw line or the ib = line.
	key    int
	indent string
	// pids are the hooks stacked directly above line, nearest first.
	pids []int
}

// iniUnit is the INIs of one 3DMigoto namespace, which run each other's
// command lists by plain name. The first member declares the override. A
// member of another namespace holds sections the unit runs as
// CommandList\NS\Name, and runs its own namespace's sections by plain name.
type iniUnit struct {
	texts      []*iniText
	namespace  string
	namespaces []string
}

func newINIUnit(texts ...*iniText) iniUnit {
	namespaces := make([]string, len(texts))
	for index, text := range texts {
		namespaces[index] = strings.ToLower(text.namespace())
	}
	return iniUnit{texts: texts, namespace: namespaces[0], namespaces: namespaces}
}

func (t *iniText) namespace() string {
	for _, raw := range t.lines {
		line := strings.TrimSpace(strings.SplitN(raw, ";", 2)[0])
		if strings.HasPrefix(line, "[") {
			return ""
		}
		if key, value, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(key), "namespace") {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// linePos is a line of one unit member.
type linePos struct{ file, line int }

func comparePos(a, b linePos) int {
	return cmp.Or(cmp.Compare(a.file, b.file), cmp.Compare(a.line, b.line))
}

// sectionSpan is the line range of one section body in a unit member.
type sectionSpan struct{ file, start, end int }

// drawSites finds the drawindexed lines with the draw's literal arguments
// that run under [TextureOverride<section>] with the draw's index buffer
// bound, following run = and CommandList assignments across the unit.
// An implicit draw has no such line, so its sites are the ib = lines that
// bind its buffer: the capture there runs with the same buffers bound as the
// drawindexed = auto of the hash that follows.
func (u iniUnit) drawSites(source modelviewer.DrawSource) []drawSite {
	return u.ownWalk(source).sites
}

// bindsVertexBuffersLate reports whether the override of an implicit draw
// binds a vertex buffer on some path after one of its sites has captured, as
// when it binds the buffer again over other vertex buffers.
func (w *drawWalk) bindsVertexBuffersLate() bool {
	return w.source.Implicit && w.lateVertexBuffers
}

func (u iniUnit) ownWalk(source modelviewer.DrawSource) *drawWalk {
	walk := newDrawWalk(u, u.sectionBodies(), source)
	// An empty name stands for the buffer bound before the override runs.
	walk.visit(strings.ToLower("TextureOverride"+source.Section), walkState{bound: []string{""}}, map[string]bool{})
	return walk
}

// sharedDraw reports whether any of the walk's sites can also run with
// another index buffer bound, through a command list that another section
// runs or another branch of the same override. The hook above it would fire
// for that other part as well, and an emitter added there would take this
// one's slot.
func (w *drawWalk) sharedDraw() bool {
	source := w.source
	own := strings.ToLower("TextureOverride" + source.Section)
	for name := range w.bodies {
		walk := newDrawWalk(w.unit, w.bodies, source)
		walk.visit(name, walkState{bound: []string{""}}, map[string]bool{})
		// The index buffer aliases of a frame animation hold only under its own
		// override, or a section that is only ever run (a command list or custom
		// shader) walked on its own. Another override reaching the site under
		// any of them, or under the buffer it inherits, draws its own vertex
		// buffers.
		called := strings.HasPrefix(name, "commandlist") || strings.HasPrefix(name, "customshader") ||
			strings.HasPrefix(name, "builtin")
		foreign := len(source.IBAliases) > 1 && name != own && !called
		for _, site := range w.sites {
			reached := walk.reached[linePos{site.file, site.key}]
			if foreign && len(reached) > 0 {
				return true
			}
			if slices.ContainsFunc(reached, func(ib string) bool {
				return ib != "" && !source.MatchesIB(ib)
			}) {
				return true
			}
		}
	}
	return false
}

// sectionBodies maps each lowercase section name to the line ranges of its
// bodies. A name may repeat in one file; across the unit, 3DMigoto keeps the
// section of the first INI that declares it and ignores later duplicates.
func (u iniUnit) sectionBodies() map[string][]sectionSpan {
	bodies := make(map[string][]sectionSpan)
	for file, text := range u.texts {
		current := ""
		for index, raw := range text.lines {
			line := strings.TrimSpace(raw)
			if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
				current = u.qualifiedName(file, strings.ToLower(strings.TrimSpace(line[1:len(line)-1])))
				if spans := bodies[current]; len(spans) > 0 && spans[0].file != file {
					current = ""
					continue
				}
				bodies[current] = append(bodies[current], sectionSpan{file, index + 1, index + 1})
				continue
			}
			if current != "" {
				spans := bodies[current]
				spans[len(spans)-1].end = index + 1
			}
		}
	}
	return bodies
}

// sectionName resolves a reference run from a section of member file. The
// unit's own namespace drops from CommandList\NS\Name, which names the same
// section as CommandListName, and a plain name in a member of another
// namespace names that namespace's section.
func (u iniUnit) sectionName(file int, target string) string {
	prefix, rest, ok := strings.Cut(target, `\`)
	if !ok {
		return u.qualifiedName(file, target)
	}
	if u.namespace == "" || !strings.HasPrefix(rest, u.namespace+`\`) {
		return target
	}
	return prefix + rest[len(u.namespace)+1:]
}

// qualifiedName spells a plain lowercase section name of member file as the
// unit references it, which is qualified for a member of another namespace.
func (u iniUnit) qualifiedName(file int, name string) string {
	namespace := u.namespaces[file]
	if namespace == "" || namespace == u.namespace {
		return name
	}
	for _, prefix := range []string{"commandlist", "customshader", "textureoverride", "shaderoverride", "resource"} {
		if rest, ok := strings.CutPrefix(name, prefix); ok && rest != "" {
			return prefix + `\` + namespace + `\` + rest
		}
	}
	return name
}

type drawWalk struct {
	unit   iniUnit
	bodies map[string][]sectionSpan
	source modelviewer.DrawSource
	found  map[linePos]bool
	sites  []drawSite
	// reached holds the buffers that may be bound at each draw with the
	// source's arguments, whether or not they match.
	reached    map[linePos][]string
	expansions int
	// lateVertexBuffers is set when a path binds a vertex buffer after the
	// capture of an implicit draw's site has run on it. The runtime keeps the
	// first capture of a draw, which would then hold stale buffers.
	lateVertexBuffers bool
	// covered maps each vertex buffer line bound directly below an implicit
	// site's ib = line to that site; its hook goes below them.
	covered map[linePos]linePos
}

func newDrawWalk(unit iniUnit, bodies map[string][]sectionSpan, source modelviewer.DrawSource) *drawWalk {
	return &drawWalk{
		unit: unit, bodies: bodies, source: source,
		found: map[linePos]bool{}, reached: map[linePos][]string{}, covered: map[linePos]linePos{},
	}
}

// walkState is what one execution path carries: the index buffers that may
// be bound and the implicit sites whose capture may already have run.
type walkState struct {
	bound    []string
	captured []linePos
}

func unionStates(a, b walkState) walkState {
	return walkState{bound: unionBuffers(a.bound, b.bound), captured: unionLines(a.captured, b.captured)}
}

func unionLines(a, b []linePos) []linePos {
	merged := append(slices.Clone(a), b...)
	slices.SortFunc(merged, comparePos)
	return slices.Compact(merged)
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
		text := w.unit.texts[span.file]
		for index := span.start; index < span.end; index++ {
			at := linePos{span.file, index}
			line := strings.TrimSpace(strings.SplitN(text.lines[index], ";", 2)[0])
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
				state = w.visit(w.unit.sectionName(span.file, nested), state, active)
			case key == "ib":
				state.bound = []string{value}
				if !w.source.Implicit || strings.EqualFold(value, "null") || !w.source.MatchesIB(value) {
					continue
				}
				w.reached[at] = unionBuffers(w.reached[at], state.bound)
				state.captured = unionLines(state.captured, []linePos{at})
				if !w.found[at] {
					w.found[at] = true
					// Vertex buffers bound right below the index buffer, as SRMI binds
					// vb1, come before the hook.
					last := index
					for last+1 < span.end && vertexBufferKeyRE.MatchString(lineKey(text.lines[last+1])) {
						last++
						w.covered[linePos{span.file, last}] = at
					}
					below := text.belowHooks(last + 1)
					w.sites = append(w.sites, drawSite{
						file:   span.file,
						line:   below,
						key:    index,
						indent: lineIndent(text.lines[index]),
						pids:   text.hooksAbove(below),
					})
				}
			case vertexBufferKeyRE.MatchString(key):
				owner, covered := w.covered[at]
				w.lateVertexBuffers = w.lateVertexBuffers ||
					slices.ContainsFunc(state.captured, func(site linePos) bool {
						return !covered || site != owner
					})
			case drawMatches(key, value, w.source):
				w.reached[at] = unionBuffers(w.reached[at], state.bound)
				if w.found[at] ||
					!slices.ContainsFunc(
						state.bound,
						func(ib string) bool { return ib == "" || w.source.MatchesIB(ib) },
					) {
					continue
				}
				w.found[at] = true
				w.sites = append(w.sites, drawSite{
					file:   span.file,
					line:   index,
					key:    index,
					indent: lineIndent(text.lines[index]),
					pids:   text.hooksAbove(index),
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
// already stacked there, naming section when it is set. Sites must come from
// the current lines of t.
func (t *iniText) insertHooks(sites []drawSite, pid, firstIndex, baseVertex int, section string) {
	// drawSites lists sites in traversal order, which need not follow the file;
	// inserting bottom-up keeps every earlier line number valid.
	sites = slices.Clone(sites)
	slices.SortFunc(sites, func(a, b drawSite) int { return b.line - a.line })
	for _, site := range sites {
		eol := lineEnding(t.lines[site.key])
		block := []string{fmt.Sprintf("%s; nahida-liquid %d begin%s", site.indent, pid, eol)}
		if section != "" {
			block = append(block, fmt.Sprintf("%s; nahida-liquid section %s%s", site.indent, section, eol))
		}
		block = append(block,
			fmt.Sprintf("%sx243 = %d%s", site.indent, pid, eol),
			fmt.Sprintf("%sy243 = %d%s", site.indent, firstIndex, eol),
			fmt.Sprintf("%sz243 = %d%s", site.indent, baseVertex, eol),
			site.indent+captureCommand+eol,
			fmt.Sprintf("%s; nahida-liquid %d end%s", site.indent, pid, eol),
		)
		t.lines = append(t.lines[:site.line], append(block, t.lines[site.line:]...)...)
	}
}
