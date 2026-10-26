package xxmi

import (
	"bytes"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

var iniOptionPattern = regexp.MustCompile(`^(\s*)([^\s=;#]+)(\s*=\s*)(.*)$`)

type iniDocument struct {
	bom      []byte
	lines    []string
	newline  string
	terminal bool
	changed  bool
}

func parseINI(data []byte) *iniDocument {
	doc := &iniDocument{newline: "\n"}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		doc.bom = []byte{0xef, 0xbb, 0xbf}
		data = data[3:]
	}
	if bytes.Contains(data, []byte("\r\n")) {
		doc.newline = "\r\n"
	}
	text := string(data)
	doc.terminal = strings.HasSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\n")
	text = strings.TrimSuffix(text, "\r")
	if text != "" {
		doc.lines = strings.Split(text, doc.newline)
	}
	return doc
}

func (d *iniDocument) Bytes() []byte {
	text := strings.Join(d.lines, d.newline)
	if d.terminal && len(d.lines) > 0 {
		text += d.newline
	}
	return append(append([]byte(nil), d.bom...), text...)
}

func (d *iniDocument) Changed() bool { return d.changed }

func (d *iniDocument) sectionBounds(section string) (int, int) {
	start := -1
	for index, line := range d.lines {
		name, ok := iniSectionName(line)
		if !ok {
			continue
		}
		if start >= 0 {
			return start, index
		}
		if strings.EqualFold(name, section) {
			start = index
		}
	}
	return start, len(d.lines)
}

func (d *iniDocument) SetOption(section, key, value string, spaced bool) {
	indexes := d.optionIndexes(section, key)
	if len(indexes) > 0 {
		first := indexes[0]
		match := iniOptionPattern.FindStringSubmatch(d.lines[first])
		replacement := match[1] + match[2] + match[3] + value + iniCommentSuffix(match[4])
		if d.lines[first] != replacement {
			d.lines[first] = replacement
			d.changed = true
		}
		for index := len(indexes) - 1; index > 0; index-- {
			duplicate := indexes[index]
			d.lines = append(d.lines[:duplicate], d.lines[duplicate+1:]...)
			d.changed = true
		}
		return
	}

	separator := "="
	if spaced {
		separator = " = "
	}
	insert := d.sectionEnd(section)
	d.lines = slices.Insert(d.lines, insert, key+separator+value)
	d.changed = true
}

// sectionEnd returns the line index that appends to the section, ahead of the blank lines that close it. A
// missing section is created at the end of the document.
func (d *iniDocument) sectionEnd(section string) int {
	start, end := d.sectionBounds(section)
	if start < 0 {
		if len(d.lines) > 0 && strings.TrimSpace(d.lines[len(d.lines)-1]) != "" {
			d.lines = append(d.lines, "")
		}
		d.lines = append(d.lines, "["+section+"]")
		start, end = len(d.lines)-1, len(d.lines)
		d.changed = true
	}
	for end > start+1 && strings.TrimSpace(d.lines[end-1]) == "" {
		end--
	}
	return end
}

type iniBlock struct {
	// name is "" for the lines above the first section.
	name   string
	header string
	body   []string
}

// blocks splits the document at its section headers.
func (d *iniDocument) blocks() []iniBlock {
	blocks := []iniBlock{{}}
	for _, line := range d.lines {
		if name, ok := iniSectionName(line); ok {
			blocks = append(blocks, iniBlock{name: name, header: line})
			continue
		}
		last := &blocks[len(blocks)-1]
		last.body = append(last.body, line)
	}
	return blocks
}

// iniTail returns where the run of blank and comment lines that ends body starts. In a d3dx.ini that run is
// the banner of the next section.
func iniTail(body []string) int {
	tail := len(body)
	for tail > 0 {
		line := strings.TrimSpace(body[tail-1])
		if line != "" && !strings.HasPrefix(line, ";") {
			break
		}
		tail--
	}
	return tail
}

// iniBanner returns where the comment block that ends body starts, along with the blank lines above it.
func iniBanner(body []string) int {
	banner := len(body)
	for banner > 0 && strings.HasPrefix(strings.TrimSpace(body[banner-1]), ";") {
		banner--
	}
	for banner > 0 && strings.TrimSpace(body[banner-1]) == "" {
		banner--
	}
	return banner
}

// iniVerbatimSection reports whether a rebuild keeps the previous body of the section instead of its values.
// [Loader] is listed with the command sections because a launch writes it, but its lines are plain options.
func iniVerbatimSection(name string) bool {
	return !iniSettingsSection(name) && !strings.EqualFold(name, "loader")
}

// mergeINI returns template holding what previous set. An option both set takes the previous value in the
// template's place, and one previous only has commented out keeps its line: it was turned off on purpose.
// Options the template does not set, and commented-out ones it does not mention, are kept ahead of the banner
// that ends their section. A section of commands keeps its previous body, where a line means something only in
// its place, and sections the template lacks follow at the end. Everything else of previous, its comments
// included, is dropped.
//
// base is the template previous was last merged from, or nil when that is not known. What previous still holds
// of it was never the user's, so the current template takes its place: an option line base has too, set or
// commented out alike, and a section of commands base has with the same commands.
//
// The result merges into itself, so a launch that repeats it leaves the file alone. lost reports whether a line
// of previous that base lacks is gone, which is when the caller backs the file up.
func mergeINI(template, base, previous []byte) (doc *iniDocument, lost bool) {
	template, _ = iniValidUTF8(template)
	tpl := parseINI(template)
	previous, lost = iniValidUTF8(previous)
	// A file with mixed line endings would otherwise parse into lines that span several options.
	prev := parseINI(bytes.ReplaceAll(previous, []byte("\r\n"), []byte("\n")))
	// The loss is judged on the whole file: the filter also drops comments and ignored duplicates of the user's. A
	// line base has word for word, such as a banner, is the template's.
	original, inherited, baseText := prev, make([]bool, len(prev.lines)), map[string]bool{}
	if len(base) > 0 {
		base, _ = iniValidUTF8(base)
		baseDoc := parseINI(bytes.ReplaceAll(base, []byte("\r\n"), []byte("\n")))
		prev, inherited = withoutTemplateLines(prev, baseDoc)
		for _, line := range baseDoc.lines {
			baseText[strings.TrimSpace(line)] = true
		}
	}
	out := &iniDocument{bom: tpl.bom, newline: tpl.newline, terminal: tpl.terminal}

	prevBody, prevHeader, prevOrder := map[string][]string{}, map[string]string{}, []string{}
	for _, block := range prev.blocks() {
		if block.name == "" {
			continue
		}
		key := strings.ToLower(block.name)
		if _, ok := prevHeader[key]; !ok {
			prevHeader[key] = block.header
			prevOrder = append(prevOrder, key)
		} else if iniVerbatimSection(block.name) {
			lost = true
			continue
		}
		prevBody[key] = append(prevBody[key], block.body...)
	}

	seen, carried := map[string]bool{}, map[string]bool{}
	for _, block := range tpl.blocks() {
		if block.name == "" {
			out.lines = append(out.lines, block.body...)
			continue
		}
		out.lines = append(out.lines, block.header)
		key := strings.ToLower(block.name)
		first := !seen[key]
		seen[key] = true
		old, had := prevBody[key]

		if iniVerbatimSection(block.name) {
			tail := iniTail(block.body)
			switch {
			case !first || !had:
				out.lines = append(out.lines, block.body...)
			case tail > 0:
				kept := old[:iniTail(old)]
				if strings.EqualFold(block.name, "include") {
					kept = iniRestoreIncludes(block.body[:tail], old)
				}
				out.lines = slices.Concat(out.lines, kept, block.body[tail:])
			default:
				// The template only documents the section, so the previous commands go below that text
				// instead of bringing a second copy of it along.
				commands := slices.DeleteFunc(slices.Clone(old), func(line string) bool {
					line = strings.TrimSpace(line)
					return line == "" || strings.HasPrefix(line, ";")
				})
				banner := iniBanner(block.body)
				out.lines = slices.Concat(out.lines, block.body[:banner], commands, block.body[banner:])
			}
			continue
		}

		body := make([]string, 0, len(block.body))
		for _, line := range block.body {
			match := iniOptionPattern.FindStringSubmatch(line)
			optionKey := iniMentionedKey(line)
			if len(match) == 0 && !iniSettingKeyPattern.MatchString(optionKey) {
				body = append(body, line)
				continue
			}
			id := key + "." + strings.ToLower(optionKey)
			if carried[id] {
				body = append(body, line)
				continue
			}
			if value, ok := prev.Option(block.name, optionKey); ok {
				if len(match) != 0 {
					carried[id] = true
					line = match[1] + match[2] + match[3] + value + iniCommentSuffix(match[4])
				}
			} else {
				for _, previousLine := range old {
					if strings.EqualFold(iniMentionedKey(previousLine), optionKey) {
						line = previousLine
						break
					}
				}
			}
			body = append(body, line)
		}
		if first && had {
			var extra []string
			for _, line := range old {
				if match := iniOptionPattern.FindStringSubmatch(line); len(match) != 0 {
					if len(tpl.optionIndexes(block.name, match[2])) == 0 {
						extra = append(extra, line)
					}
					continue
				}

				// A later template that sets the option would otherwise turn it back on. The key pattern
				// keeps prose such as "; 0 = off" from passing for one.
				key := iniMentionedKey(line)
				if iniSettingKeyPattern.MatchString(key) && !tpl.mentionsOption(block.name, key) {
					extra = append(extra, line)
				}
			}
			body = slices.Insert(body, iniTail(body), extra...)
		}
		out.lines = append(out.lines, body...)
	}

	for _, key := range prevOrder {
		if seen[key] {
			continue
		}
		if len(out.lines) > 0 && strings.TrimSpace(out.lines[len(out.lines)-1]) != "" {
			out.lines = append(out.lines, "")
		}
		body := prevBody[key]
		tail := iniTail(body)
		out.lines = append(append(out.lines, prevHeader[key]), body[:tail]...)
		if iniVerbatimSection(key) {
			continue
		}

		// The run that ends the section is dropped as the banner of the next one, but an option commented out
		// in it has to outlive that like one further up.
		for _, line := range body[tail:] {
			if iniSettingKeyPattern.MatchString(iniMentionedKey(line)) {
				out.lines = append(out.lines, line)
			}
		}
	}

	kept := make(map[string]bool, len(out.lines))
	for _, line := range out.lines {
		kept[strings.TrimSpace(line)] = true
	}
	for index, line := range original.lines {
		line = strings.TrimSpace(line)
		lost = lost || line != "" && !kept[line] && !inherited[index] && !baseText[line]
	}
	return out, lost
}

// withoutTemplateLines returns previous without what it still holds of base: the option lines base has too and,
// where the first section of commands of a name has the commands base has, that section along with the duplicates
// of it a merge ignores. A settings section left with nothing but its banner is dropped along with its header.
//
// inherited marks the lines of previous that are dropped as base's own, an option or command base has too, so
// losing them loses nothing of the user's. Comments, an option line whose trailing comment base lacks, the other
// lines of an option whose value is base's, and duplicate sections are dropped without that mark.
func withoutTemplateLines(previous, base *iniDocument) (filtered *iniDocument, inherited []bool) {
	// options holds, by section and state, the trailing comments base writes after the option.
	options, commands := map[string][]string{}, map[string]string{}
	for _, block := range base.blocks() {
		key := strings.ToLower(block.name)
		if block.name == "" {
			continue
		}
		if iniVerbatimSection(block.name) {
			if _, ok := commands[key]; !ok {
				commands[key] = iniCommands(block.body)
			}
			continue
		}
		for _, line := range block.body {
			if state, comment, ok := iniOptionState(line); ok {
				options[key+"."+state] = append(options[key+"."+state], comment)
			}
		}
	}

	// 3DMigoto reads the first value a section sets for a key. Where that value is base's, the other lines of the key
	// go with it: a later duplicate or a commented-out line left behind would take its place in the merge.
	resolved, owned, section := map[string]bool{}, map[string]bool{}, ""
	for _, line := range previous.lines {
		if name, ok := iniSectionName(line); ok {
			section = strings.ToLower(name)
			continue
		}
		state, _, ok := iniOptionState(line)
		if !ok || strings.HasPrefix(state, ";") {
			continue
		}
		option, _, _ := strings.Cut(state, "=")
		if id := section + "." + option; !resolved[id] {
			resolved[id] = true
			_, owned[id] = options[section+"."+state]
		}
	}

	dropped, inherited := make([]bool, len(previous.lines)), make([]bool, len(previous.lines))
	seen, matched := map[string]bool{}, map[string]bool{}
	for start := 0; start < len(previous.lines); {
		name, named := iniSectionName(previous.lines[start])
		end := start + 1
		for end < len(previous.lines) {
			if _, ok := iniSectionName(previous.lines[end]); ok {
				break
			}
			end++
		}
		if !named {
			start = end
			continue
		}
		key := strings.ToLower(name)

		if iniVerbatimSection(name) {
			first := !seen[key]
			seen[key] = true
			if first {
				baseCommands, ok := commands[key]
				matched[key] = ok && baseCommands == iniCommands(previous.lines[start+1:end])
			}
			for index := start; matched[key] && index < end; index++ {
				line := strings.TrimSpace(previous.lines[index])
				dropped[index] = true
				inherited[index] = first && !strings.HasPrefix(line, ";")
			}
			start = end
			continue
		}

		content := false
		for index := start + 1; index < end; index++ {
			state, comment, ok := iniOptionState(previous.lines[index])
			option, _, _ := strings.Cut(strings.TrimPrefix(state, ";"), "=")
			if comments, found := options[key+"."+state]; ok && found {
				dropped[index] = true
				inherited[index] = comment == "" || slices.Contains(comments, comment)
			} else if ok && owned[key+"."+option] {
				dropped[index] = true
			} else if iniContentLine(previous.lines[index]) {
				content = true
			}
		}
		if !content && slices.Contains(dropped[start+1:end], true) {
			for index := start; index < end; index++ {
				dropped[index] = true
			}
			inherited[start] = true
		}
		start = end
	}

	filtered = &iniDocument{bom: previous.bom, newline: previous.newline, terminal: previous.terminal}
	for index, line := range previous.lines {
		if !dropped[index] {
			filtered.lines = append(filtered.lines, line)
		}
	}
	return filtered, inherited
}

// iniOptionState names what an option line sets, as "key=value" with the key in lowercase, or as ";key=value"
// for a commented-out one, along with the comment that follows the value. Lines that differ only in spacing, the
// case of the key, or the trailing comment name the same state.
func iniOptionState(line string) (state, comment string, ok bool) {
	trimmed := strings.TrimSpace(line)
	match := iniOptionPattern.FindStringSubmatch(strings.TrimLeft(trimmed, " \t;#"))
	if len(match) == 0 || !iniSettingKeyPattern.MatchString(match[2]) {
		return "", "", false
	}
	comment = iniCommentSuffix(match[4])
	state = strings.ToLower(match[2]) + "=" + strings.TrimSpace(strings.TrimSuffix(match[4], comment))
	if trimmed[0] == ';' || trimmed[0] == '#' {
		state = ";" + state
	}
	return state, strings.TrimSpace(comment), true
}

// iniCommands returns the lines of a section body that are neither blank nor comments.
func iniCommands(body []string) string {
	var commands []string
	for _, line := range body {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, ";") {
			commands = append(commands, line)
		}
	}
	return strings.Join(commands, "\n")
}

// iniContentLine reports whether a line of a settings section is more than a blank line or prose: an option,
// set or commented out, or any other line that is not a comment.
func iniContentLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return false
	}
	return !strings.HasPrefix(trimmed, ";") || iniSettingKeyPattern.MatchString(iniMentionedKey(line))
}

// iniRestoreIncludes returns the previous [Include] body with each line the template sets and previous does not
// mention, set or commented out, put back ahead of its first option. A file that lost the package's own include,
// such as Core\EFMI\main.ini, loads no mod that depends on it, and keeping the section verbatim never repairs it.
func iniRestoreIncludes(template, previous []string) []string {
	mentioned := map[string]bool{}
	for _, line := range previous {
		if match := iniOptionPattern.FindStringSubmatch(strings.TrimLeft(line, " \t;#")); len(match) != 0 {
			mentioned[iniIncludeID(match)] = true
		}
	}
	set := map[string]bool{}
	var missing []string
	for _, line := range template {
		match := iniOptionPattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		set[iniIncludeID(match)] = true
		if !mentioned[iniIncludeID(match)] {
			missing = append(missing, line)
		}
	}

	// An include commented out in the run that ends the section would be dropped as the banner of the next one,
	// and the rebuild after that would turn it back on.
	tail := iniTail(previous)
	body := slices.Clone(previous[:tail])
	for _, line := range previous[tail:] {
		if match := iniOptionPattern.FindStringSubmatch(strings.TrimLeft(line, " \t;#")); len(match) != 0 &&
			set[iniIncludeID(match)] {
			body = append(body, line)
		}
	}

	at := slices.IndexFunc(body, iniOptionPattern.MatchString)
	if at < 0 {
		at = len(body)
	}
	return slices.Insert(body, at, missing...)
}

// iniIncludeID identifies a repeatable include by its key and path, and any other option, such as user_config, by
// its key alone: 3DMigoto reads the first value of a single-valued key, so a template default put ahead of the
// previous value would replace it.
func iniIncludeID(match []string) string {
	key := strings.ToLower(match[2])
	switch key {
	case "include", "include_recursive", "exclude_recursive":
		value := strings.TrimSpace(strings.TrimSuffix(match[4], iniCommentSuffix(match[4])))
		return key + "=" + strings.ToLower(strings.ReplaceAll(value, "/", `\`))
	}
	return key
}

// iniValidUTF8 returns data with every byte that is not part of a UTF-8 sequence read as Windows-1252. 3DMigoto
// reads a d3dx.ini as UTF-8 and stops at the first invalid byte, which silently drops the rest of the file.
// lossy reports a byte Windows-1252 does not define, which is replaced and cannot be told from the result.
func iniValidUTF8(data []byte) (valid []byte, lossy bool) {
	if utf8.Valid(data) {
		return data, false
	}
	valid = make([]byte, 0, len(data)+utf8.UTFMax)
	for len(data) > 0 {
		char, size := utf8.DecodeRune(data)
		if char == utf8.RuneError && size == 1 {
			char = charmap.Windows1252.DecodeByte(data[0])
			lossy = lossy || char == utf8.RuneError
		}
		valid = utf8.AppendRune(valid, char)
		data = data[size:]
	}
	return valid, lossy
}

var iniSettingKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// iniSettingsSection reports whether a d3dx.ini section holds independent options. The others are command
// lists, overrides, and sections the launch writes itself, where a line means something only in its place.
func iniSettingsSection(name string) bool {
	name = strings.ToLower(name)
	if slices.Contains([]string{
		"loader", "include", "constants", "present", "profile", "clearrendertargetview", "cleardepthstencilview",
		"clearunorderedaccessviewuint", "clearunorderedaccessviewfloat",
	}, name) {
		return false
	}
	return !slices.ContainsFunc([]string{
		"key", "commandlist", "builtincommandlist", "shaderoverride", "shaderregex", "textureoverride",
		"resource", "customshader", "builtincustomshader", "preset",
	}, func(prefix string) bool { return strings.HasPrefix(name, prefix) })
}

func iniSectionName(line string) (string, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || trimmed[0] != '[' || trimmed[len(trimmed)-1] != ']' {
		return "", false
	}
	return strings.TrimSpace(trimmed[1 : len(trimmed)-1]), true
}

// mentionsOption reports whether the section has the option, set or commented out.
func (d *iniDocument) mentionsOption(section, key string) bool {
	inSection := false
	for _, line := range d.lines {
		if name, ok := iniSectionName(line); ok {
			inSection = strings.EqualFold(name, section)
			continue
		}
		if !inSection {
			continue
		}
		if strings.EqualFold(iniMentionedKey(line), key) {
			return true
		}
	}
	return false
}

// iniMentionedKey returns the key of an option line, set or commented out, or "" for any other line.
func iniMentionedKey(line string) string {
	match := iniOptionPattern.FindStringSubmatch(strings.TrimLeft(line, " \t;#"))
	if len(match) == 0 {
		return ""
	}
	return match[2]
}

func iniCommentSuffix(value string) string {
	quote := byte(0)
	for index := range len(value) {
		char := value[index]
		if quote != 0 {
			if char == quote && (index == 0 || value[index-1] != '\\') {
				quote = 0
			}
			continue
		}
		if char == '\'' || char == '"' {
			quote = char
			continue
		}
		if (char != ';' && char != '#') || index == 0 || value[index-1] != ' ' && value[index-1] != '\t' {
			continue
		}
		start := index - 1
		for start > 0 && (value[start-1] == ' ' || value[start-1] == '\t') {
			start--
		}
		return value[start:]
	}
	return ""
}

func (d *iniDocument) RemoveOption(section, key string) {
	indexes := d.optionIndexes(section, key)
	for index := len(indexes) - 1; index >= 0; index-- {
		match := indexes[index]
		d.lines = append(d.lines[:match], d.lines[match+1:]...)
		d.changed = true
	}
}

// Option returns the value the section sets for key, without the comment that may follow it.
func (d *iniDocument) Option(section, key string) (string, bool) {
	indexes := d.optionIndexes(section, key)
	if len(indexes) == 0 {
		return "", false
	}
	value := iniOptionPattern.FindStringSubmatch(d.lines[indexes[0]])[4]
	return strings.TrimSpace(strings.TrimSuffix(value, iniCommentSuffix(value))), true
}

func (d *iniDocument) optionIndexes(section, key string) []int {
	indexes := []int{}
	inSection := false
	for index, line := range d.lines {
		if name, ok := iniSectionName(line); ok {
			inSection = strings.EqualFold(name, section)
			continue
		}
		if !inSection {
			continue
		}
		match := iniOptionPattern.FindStringSubmatch(line)
		if len(match) != 0 && strings.EqualFold(match[2], key) {
			indexes = append(indexes, index)
		}
	}
	return indexes
}
