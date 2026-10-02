// Package namespace analyzes and rewrites explicit 3DMigoto INI namespaces.
package namespace

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// Document describes one INI without resolving includes or implicit filesystem namespaces.
// Persistent contains bare variable names; References contains qualified namespaces.
// Both lists are case-insensitively unique, in source order, with original spelling.
// Includes and RecursiveIncludes record include keys in any section or preamble, including
// duplicates. Values are trimmed and outer quotes removed, without path resolution
// or expression evaluation. Malformed values are retained verbatim after trimming.
// Unsupported documents have no Fingerprint and must not be rewritten.
type Document struct {
	Namespace         string
	Persistent        []string
	Fingerprint       string
	References        []string
	Includes          []string
	RecursiveIncludes []string
	Unsupported       bool
}

type iniEncoding uint8

const (
	iniUTF8 iniEncoding = iota
	iniUTF8BOM
	iniUTF16LE
	iniUTF16BE
)

type iniToken struct {
	start    int
	end      int
	isQuoted bool
}

type iniLine struct {
	text           string
	ending         string
	tokens         []iniToken
	namespaceToken int
	persistEnd     int
	pathStart      int
}

type iniSource struct {
	encoding iniEncoding
	lines    []iniLine
	document Document
}

type iniReplacement struct {
	from string
	to   string
}

// Parse accepts strict UTF-8 (with optional BOM) or BOM-marked UTF-16 LE/BE.
// Invalid encodings return an error. Unsupported syntax is reported in the document
// with a nil error so callers can inspect it without treating it as safe to edit.
// Fingerprint is the hex SHA-256 of Normalize(data, nil).
func Parse(data []byte) (Document, error) {
	source, err := analyzeINI(data)
	if err != nil {
		return Document{Unsupported: true}, err
	}
	if !source.document.Unsupported {
		hash := sha256.Sum256([]byte(normalizeINI(source, nil)))
		source.document.Fingerprint = hex.EncodeToString(hash[:])
	}
	return source.document, nil
}

// Rewrite changes explicit namespace declarations and unquoted qualified references.
// Matches are case-insensitive against the entire namespace before the final
// symbol backslash. Child namespaces require their own explicit mapping.
// Replacements are simultaneous, not recursive. Encoding, BOM, line endings and
// every byte outside replaced spans are preserved. Unsafe syntax returns an error.
func Rewrite(data []byte, replacements map[string]string) ([]byte, error) {
	source, err := analyzeINI(data)
	if err != nil {
		return nil, err
	}
	if source.document.Unsupported {
		return nil, errors.New("namespace: unsupported ini syntax")
	}
	mapping, err := prepareINIReplacements(replacements)
	if err != nil {
		return nil, err
	}

	var output strings.Builder
	for _, line := range source.lines {
		var copied int
		for index, token := range line.tokens {
			if token.isQuoted || line.pathStart >= 0 && token.start >= line.pathStart {
				continue
			}
			original := line.text[token.start:token.end]
			replaced := replaceINIToken(original, index == line.namespaceToken, mapping)
			if original == replaced {
				continue
			}
			output.WriteString(line.text[copied:token.start])
			output.WriteString(replaced)
			copied = token.end
		}
		output.WriteString(line.text[copied:])
		output.WriteString(line.ending)
	}
	return encodeINI(output.String(), source.encoding), nil
}

// Normalize returns canonical, LF-separated INI code for comparison, not execution.
// It removes comments and formatting and masks Constants global persist initializers.
// Namespace mappings use the same rules as Rewrite. Token separators prevent code
// such as "a b" and "ab", or "= =" and "==", from colliding. Quoted strings and
// path values retain their case and internal whitespace; other code also retains case.
func Normalize(data []byte, replacements map[string]string) (string, error) {
	source, err := analyzeINI(data)
	if err != nil {
		return "", err
	}
	if source.document.Unsupported {
		return "", errors.New("namespace: unsupported ini syntax")
	}
	mapping, err := prepareINIReplacements(replacements)
	if err != nil {
		return "", err
	}
	return normalizeINI(source, mapping), nil
}

func analyzeINI(data []byte) (iniSource, error) {
	text, encoding, err := decodeINI(data)
	if err != nil {
		return iniSource{}, err
	}
	source := iniSource{
		encoding: encoding,
		lines:    []iniLine{},
		document: Document{
			Persistent: []string{}, References: []string{}, Includes: []string{}, RecursiveIncludes: []string{},
		},
	}
	section := ""
	hasNamespace := false
	persistent := map[string]bool{}
	references := map[string]bool{}
	for len(text) > 0 {
		end := strings.IndexAny(text, "\r\n")
		if end < 0 {
			end = len(text)
		}
		line := iniLine{text: text[:end], namespaceToken: -1, persistEnd: -1, pathStart: -1}
		text = text[end:]
		if strings.HasPrefix(text, "\r\n") {
			line.ending, text = "\r\n", text[2:]
		} else if len(text) > 0 {
			line.ending, text = text[:1], text[1:]
		}
		var safe bool
		line.tokens, safe = tokenizeINI(line.text)
		if len(line.tokens) >= 3 && line.text[line.tokens[0].start:line.tokens[0].end] == "[" &&
			line.text[line.tokens[len(line.tokens)-1].start:line.tokens[len(line.tokens)-1].end] == "]" {
			// Section names can contain punctuation such as WWMI's Resource...-0_t_...
			// suffixes; they are names rather than subtraction expressions.
			name := iniToken{start: line.tokens[1].start, end: line.tokens[len(line.tokens)-2].end}
			line.tokens = []iniToken{line.tokens[0], name, line.tokens[len(line.tokens)-1]}
		}
		if strings.HasPrefix(section, "key") && len(line.tokens) == 2 {
			key := line.text[line.tokens[0].start:line.tokens[0].end]
			assignment := line.text[line.tokens[1].start:line.tokens[1].end]
			binding := strings.TrimSpace(line.text[line.tokens[1].end:])
			if strings.EqualFold(key, "key") && assignment == "=" && (binding == ";" || binding == "#") {
				// Bare semicolon/hash keys are bindings, not comments.
				start := strings.IndexByte(line.text[line.tokens[1].end:], binding[0]) + line.tokens[1].end
				line.tokens = append(line.tokens, iniToken{start: start, end: start + 1})
			}
		}

		// A namespace declaration owns the entire unquoted value, including spaces.
		if len(line.tokens) >= 3 &&
			strings.EqualFold(line.text[line.tokens[0].start:line.tokens[0].end], "namespace") &&
			line.text[line.tokens[1].start:line.tokens[1].end] == "=" {
			value := iniToken{start: line.tokens[2].start, end: line.tokens[len(line.tokens)-1].end}
			line.tokens = append(line.tokens[:2], value)
		}
		if !safe {
			source.document.Unsupported = true
		}
		if len(line.tokens) == 0 {
			source.lines = append(source.lines, line)
			continue
		}
		words := make([]string, len(line.tokens))
		for i, token := range line.tokens {
			words[i] = line.text[token.start:token.end]
		}
		lower := strings.ToLower(words[0])
		switch {
		case lower == "[":
			if len(words) != 3 || words[2] != "]" || line.tokens[1].isQuoted || !validININame(words[1]) {
				source.document.Unsupported = true
				section = "?"
				break
			}
			section = strings.ToLower(words[1])
			if strings.HasPrefix(section, "include") || section == "namespace" {
				source.document.Unsupported = true
			}
		case lower == "namespace":
			if section != "" || hasNamespace || len(words) != 3 || words[1] != "=" || !validININame(words[2]) {
				source.document.Unsupported = true
				break
			}
			hasNamespace = true
			source.document.Namespace = words[2]
			line.namespaceToken = 2
		case strings.HasPrefix(lower, "include"):
			source.document.Unsupported = true
			if len(words) >= 2 && words[1] == "=" {
				line.pathStart = line.tokens[1].end
				value := includeINIValue(line.text[line.pathStart:])
				if value != "" {
					switch lower {
					case "include":
						source.document.Includes = append(source.document.Includes, value)
					case "include_recursive":
						source.document.RecursiveIncludes = append(source.document.RecursiveIncludes, value)
					}
				}
			}
		case lower == "global" || lower == "persist":
			isPersistent := len(words) > 1 && strings.EqualFold(words[1], "persist")
			nameIndex := 1
			if isPersistent {
				nameIndex = 2
			}
			if lower != "global" || section != "constants" || len(words) <= nameIndex ||
				!validINIVariable(words[nameIndex]) {
				source.document.Unsupported = true
				break
			}
			if len(words) > nameIndex+1 && (words[nameIndex+1] != "=" || len(words) == nameIndex+2) {
				source.document.Unsupported = true
				break
			}
			if isPersistent {
				name := words[nameIndex][1:]
				if persistent[strings.ToLower(name)] {
					source.document.Unsupported = true
				} else {
					persistent[strings.ToLower(name)] = true
					source.document.Persistent = append(source.document.Persistent, name)
				}
				line.persistEnd = nameIndex + 1
			}
		default:
			assignment := slices.Index(words, "=")
			if section == "" || assignment < 0 && !isINIStatement(words) {
				source.document.Unsupported = true
			}
			if assignment >= 0 {
				if assignment == 0 || assignment == len(words)-1 {
					source.document.Unsupported = true
					break
				}
				key := strings.ToLower(words[assignment-1])
				if isINIPathKey(key) && assignment+1 < len(words) {
					line.pathStart = line.tokens[assignment+1].start
				}
			}
		}

		for i, token := range line.tokens {
			if i == line.namespaceToken || token.isQuoted || line.pathStart >= 0 && token.start >= line.pathStart {
				continue
			}
			word := words[i]
			if !strings.Contains(word, `\`) {
				continue
			}
			_, namespace, _, ok := qualifiedINIToken(word)
			if !ok {
				source.document.Unsupported = true
				continue
			}
			if !references[strings.ToLower(namespace)] {
				references[strings.ToLower(namespace)] = true
				source.document.References = append(source.document.References, namespace)
			}
		}
		source.lines = append(source.lines, line)
	}
	return source, nil
}

func includeINIValue(value string) string {
	value = strings.TrimSpace(value)
	tokens, safe := tokenizeINI(value)
	if !safe {
		return value
	}
	if len(tokens) == 0 {
		return ""
	}
	value = strings.TrimSpace(value[:tokens[len(tokens)-1].end])
	if len(tokens) == 1 && tokens[0].isQuoted {
		return strings.TrimSpace(value[1 : len(value)-1])
	}
	return value
}

func tokenizeINI(text string) ([]iniToken, bool) {
	tokens := []iniToken{}
	for i := 0; i < len(text); {
		r, size := utf8.DecodeRuneInString(text[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		// WWMI uses #PoolName[index] for pool resource queries in expressions.
		isPoolQuery := r == '#' && len(tokens) > 0 && strings.HasPrefix(strings.ToLower(text[i:]), "#pool")
		if r == ';' || r == '#' && !isPoolQuery {
			break
		}
		token := iniToken{start: i}
		if r == '"' || r == '\'' {
			token.isQuoted = true
			quote := byte(r)
			i++
			closed := false
			for i < len(text) {
				if text[i] != quote {
					i++
					continue
				}
				i++
				if i < len(text) && text[i] == quote {
					i++
					continue
				}
				closed = true
				break
			}
			if !closed {
				return tokens, false
			}
		} else if isINIWordRune(r) || isPoolQuery {
			if end := qualifiedINIEnd(text, i); end > i {
				token.end = end
				tokens = append(tokens, token)
				i = end
				continue
			}
			i += size
			for i < len(text) {
				r, size = utf8.DecodeRuneInString(text[i:])
				if !isINIWordRune(r) {
					break
				}
				i += size
			}
		} else {
			i += size
			if i+1 < len(text) && (text[token.start:i+2] == "===" || text[token.start:i+2] == "!==") {
				i += 2
				token.end = i
				tokens = append(tokens, token)
				continue
			}
			if i < len(text) {
				switch text[token.start : i+1] {
				case "==", "!=", "<=", ">=", "&&", "||", "**", "<<", ">>":
					i++
				}
			}
		}
		token.end = i
		tokens = append(tokens, token)
	}
	return tokens, true
}

func isINIWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || strings.ContainsRune(`_$\.:`, r)
}

func validININame(name string) bool {
	if name == "" || !utf8.ValidString(name) {
		return false
	}
	for _, segment := range strings.Split(name, `\`) {
		if segment == "" || segment == "." || segment == ".." || strings.TrimSpace(segment) != segment {
			return false
		}
		for _, r := range segment {
			if !unicode.IsLetter(r) && !unicode.IsNumber(r) && !strings.ContainsRune("_.- ", r) {
				return false
			}
		}
	}
	return true
}

func validINIVariable(word string) bool {
	return strings.HasPrefix(word, "$") && !strings.ContainsAny(word, `\ -`) && validININame(word[1:])
}

func isINIStatement(words []string) bool {
	switch strings.ToLower(words[0]) {
	case "if", "elif", "while":
		return len(words) > 1
	case "else":
		return len(words) == 1 || len(words) > 2 && strings.EqualFold(words[1], "if")
	case "endif", "endwhile", "break", "continue", "return":
		return len(words) == 1
	case "vs", "ps", "gs", "hs", "ds", "cs":
		// Preserve standalone slot comparisons found in shipped mods. Whether the
		// engine executes them is outside namespace analysis; no namespace is hidden.
		if len(words) != 5 || words[1] != "-" || words[3] != "==" || !strings.EqualFold(words[4], "null") {
			return false
		}
		return strings.HasPrefix(words[2], "t") && len(words[2]) > 1 && strings.Trim(words[2][1:], "0123456789") == ""
	default:
		return false
	}
}

func isINIPathKey(key string) bool {
	switch key {
	case "filename", "include", "include_recursive", "exclude_recursive", "vs", "ps", "gs", "hs", "ds", "cs":
		return true
	default:
		return false
	}
}

func qualifiedINIToken(word string) (string, string, string, bool) {
	prefix, rest, found := strings.Cut(word, `\`)
	if !found {
		return "", "", "", false
	}
	if !isINIQualifiedPrefix(prefix) {
		return "", "", "", false
	}
	last := strings.LastIndex(rest, `\`)
	if last <= 0 || !validININame(rest) {
		return "", "", "", false
	}
	return prefix + `\`, rest[:last], rest[last:], true
}

func isINIQualifiedPrefix(prefix string) bool {
	switch strings.ToLower(prefix) {
	case "$",
		"pool",
		"$pool",
		"#pool",
		"resource",
		"commandlist",
		"customshader",
		"textureoverride",
		"shaderoverride",
		"shaderregex",
		"preset",
		"key",
		"constants",
		"present",
		"clear",
		"builtincommandlist",
		"builtincustomshader":
		return true
	default:
		return false
	}
}

// Qualified symbols may contain spaces in namespace segments. The last backslash
// separates the symbol name; expression delimiters and the next qualified symbol
// bound the scan, rather than whitespace within the namespace.
func qualifiedINIEnd(text string, start int) int {
	first := strings.IndexByte(text[start:], '\\')
	if first < 0 || !isINIQualifiedPrefix(text[start:start+first]) {
		return start
	}
	limit := start + first + 1
	for limit < len(text) {
		r, size := utf8.DecodeRuneInString(text[limit:])
		if strings.ContainsRune("=<>!&|+*/%,;#[]()\"'", r) || r == '$' {
			break
		}
		if unicode.IsSpace(r) {
			next := limit + size
			for next < len(text) && (text[next] == ' ' || text[next] == '\t') {
				next++
			}
			if slash := strings.IndexByte(
				text[next:],
				'\\',
			); slash >= 0 &&
				isINIQualifiedPrefix(text[next:next+slash]) {
				break
			}
		}
		limit += size
	}
	last := strings.LastIndexByte(text[start:limit], '\\')
	if last <= first {
		return start
	}
	end := start + last + 1
	for end < limit {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !isINIWordRune(r) || r == '\\' {
			break
		}
		end += size
	}
	return end
}

func prepareINIReplacements(replacements map[string]string) ([]iniReplacement, error) {
	mapping := []iniReplacement{}
	seen := map[string]string{}
	for from, to := range replacements {
		if !validININame(from) || !validININame(to) {
			return nil, fmt.Errorf("namespace: invalid replacement %q -> %q", from, to)
		}
		lower := strings.ToLower(from)
		if previous, exists := seen[lower]; exists {
			if previous != to {
				return nil, fmt.Errorf("namespace: conflicting replacements for %q", from)
			}
			continue
		}
		seen[lower] = to
		mapping = append(mapping, iniReplacement{from: lower, to: to})
	}
	slices.SortFunc(mapping, func(a, b iniReplacement) int {
		if len(a.from) != len(b.from) {
			return len(b.from) - len(a.from)
		}
		return strings.Compare(a.from, b.from)
	})
	return mapping, nil
}

func replaceINIToken(word string, isNamespace bool, mapping []iniReplacement) string {
	if isNamespace {
		for _, replacement := range mapping {
			if strings.EqualFold(word, replacement.from) {
				return replacement.to
			}
		}
		return word
	}
	prefix, namespace, suffix, ok := qualifiedINIToken(word)
	if !ok {
		return word
	}
	for _, replacement := range mapping {
		if !strings.EqualFold(namespace, replacement.from) {
			continue
		}
		return prefix + replacement.to + suffix
	}
	return word
}

func normalizeINI(source iniSource, mapping []iniReplacement) string {
	lines := []string{}
	for _, line := range source.lines {
		parts := []string{}
		for i, token := range line.tokens {
			if line.persistEnd >= 0 && i >= line.persistEnd {
				break
			}
			if line.pathStart >= 0 && token.start >= line.pathStart {
				last := line.tokens[len(line.tokens)-1].end
				parts = append(parts, line.text[line.pathStart:last])
				break
			}
			word := line.text[token.start:token.end]
			if !token.isQuoted {
				word = replaceINIToken(word, i == line.namespaceToken, mapping)
			}
			parts = append(parts, word)
		}
		if len(parts) > 0 {
			lines = append(lines, strings.Join(parts, " "))
		}
	}
	return strings.Join(lines, "\n")
}

func decodeINI(data []byte) (string, iniEncoding, error) {
	encoding := iniUTF8
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe, 0, 0}), bytes.HasPrefix(data, []byte{0, 0, 0xfe, 0xff}):
		return "", encoding, errors.New("namespace: utf-32 is unsupported")
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		encoding, data = iniUTF8BOM, data[3:]
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		encoding, data = iniUTF16LE, data[2:]
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		encoding, data = iniUTF16BE, data[2:]
	}
	text := ""
	if encoding == iniUTF16LE || encoding == iniUTF16BE {
		if len(data)%2 != 0 {
			return "", encoding, errors.New("namespace: truncated utf-16")
		}
		var order binary.ByteOrder = binary.LittleEndian
		if encoding == iniUTF16BE {
			order = binary.BigEndian
		}
		units := make([]uint16, len(data)/2)
		for i := range units {
			units[i] = order.Uint16(data[2*i:])
		}
		for i := 0; i < len(units); i++ {
			unit := units[i]
			if unit >= 0xd800 && unit <= 0xdbff {
				if i+1 == len(units) || units[i+1] < 0xdc00 || units[i+1] > 0xdfff {
					return "", encoding, errors.New("namespace: unpaired utf-16 surrogate")
				}
				i++
			} else if unit >= 0xdc00 && unit <= 0xdfff {
				return "", encoding, errors.New("namespace: unpaired utf-16 surrogate")
			}
		}
		text = string(utf16.Decode(units))
	} else {
		if !utf8.Valid(data) {
			return "", encoding, errors.New("namespace: invalid utf-8 or unknown encoding")
		}
		text = string(data)
	}
	for _, r := range text {
		if r == '\ufeff' || unicode.IsControl(r) && r != '\t' && r != '\r' && r != '\n' {
			return "", encoding, errors.New("namespace: unexpected bom or control character")
		}
	}
	return text, encoding, nil
}

func encodeINI(text string, encoding iniEncoding) []byte {
	switch encoding {
	case iniUTF8:
		return []byte(text)
	case iniUTF8BOM:
		return append([]byte{0xef, 0xbb, 0xbf}, []byte(text)...)
	default:
		units := utf16.Encode([]rune(text))
		data := make([]byte, 2+2*len(units))
		var order binary.ByteOrder = binary.LittleEndian
		data[0], data[1] = 0xff, 0xfe
		if encoding == iniUTF16BE {
			order = binary.BigEndian
			data[0], data[1] = 0xfe, 0xff
		}
		for i, unit := range units {
			order.PutUint16(data[2+2*i:], unit)
		}
		return data
	}
}
