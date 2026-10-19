package liquidemitter

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
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

// drawSites finds the drawindexed lines with these literal arguments that run
// under [TextureOverride<section>], following run = and CommandList
// assignments within the same file.
func (t *iniText) drawSites(section string, startIndex, indexCount, baseVertex int) []drawSite {
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

	var sites []drawSite
	visited := map[string]bool{}
	queue := []string{strings.ToLower("TextureOverride" + section)}
	for len(queue) > 0 && len(visited) < 256 {
		name := queue[0]
		queue = queue[1:]
		if visited[name] {
			continue
		}
		visited[name] = true
		for _, span := range bodies[name] {
			for index := span[0]; index < span[1]; index++ {
				key, value, ok := strings.Cut(strings.TrimSpace(strings.SplitN(t.lines[index], ";", 2)[0]), "=")
				if !ok {
					continue
				}
				key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
				if nested := nestedSection(key, value); nested != "" {
					queue = append(queue, nested)
					continue
				}
				if drawMatches(key, value, startIndex, indexCount, baseVertex) {
					sites = append(sites, drawSite{
						line:   index,
						indent: lineIndent(t.lines[index]),
						pid:    t.hookAbove(index),
					})
				}
			}
		}
	}
	return sites
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
func drawMatches(key, value string, startIndex, indexCount, baseVertex int) bool {
	fields := strings.Split(value, ",")
	var want map[int]int
	switch {
	case key == "drawindexed" && len(fields) == 3:
		want = map[int]int{0: indexCount, 1: startIndex, 2: baseVertex}
	case key == "drawindexedinstanced" && len(fields) == 5:
		want = map[int]int{0: indexCount, 2: startIndex, 3: baseVertex}
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
