package agent

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// PatchHunk replaces one contiguous block of lines inside an `update` operation. Context, when
// set, moves the search position to that line first, which disambiguates repeated blocks; an empty
// OldLines turns the hunk into an insertion after the context line or at the end of the file.
type PatchHunk struct {
	Context  string   `json:"context,omitempty"`
	OldLines []string `json:"oldLines,omitempty"`
	NewLines []string `json:"newLines,omitempty"`
	EOF      bool     `json:"eof,omitempty"`
}

// textLine keeps one decoded line together with its own terminator, so a hunk edit leaves every
// line it does not touch byte-identical, mixed line endings included.
type textLine struct {
	text       string
	terminator string
}

// Match passes run from the strictest comparison to the loosest. A hunk takes the first match after
// the previous hunk, as the codex apply_patch format does, so hunks listed in file order pick
// repeated blocks one after another. Every other lookup needs a pass with exactly one candidate.
const (
	linePassExact = iota + 1
	linePassTrimRight
	linePassTrim
	linePassPunctuation
)

const hunkPreviewLimit = 120

// punctuationNormalizer maps the typographic characters a model commonly substitutes for ASCII.
var punctuationNormalizer = strings.NewReplacer(
	"\u2010", "-", "\u2011", "-", "\u2012", "-", "\u2013", "-", "\u2014", "-", "\u2015", "-", "\u2212", "-",
	"\u2018", "'", "\u2019", "'", "\u201a", "'", "\u201b", "'",
	"\u201c", "\"", "\u201d", "\"", "\u201e", "\"", "\u201f", "\"",
	"\u00a0", " ", "\u2002", " ", "\u2003", " ", "\u2009", " ", "\u3000", " ",
)

type hunkResolution struct {
	number     int
	start, end int
	context    int
	lines      []string
	warning    string
}

type lineMatch struct {
	found      bool
	index      int
	pass       int
	candidates []int
}

// applyPatchHunks resolves every hunk against the decoded file text and returns the rewritten text.
// Hunks must not overlap. Every hunk that fails to resolve is reported, so one retry can fix them
// all. Lines outside the hunks keep their own terminators, and a file without a trailing newline
// stays that way.
func applyPatchHunks(text string, hunks []PatchHunk, path, fallback string) (string, []string, error) {
	if len(hunks) == 0 {
		return "", nil, errors.New("update requires at least one hunk")
	}
	if fallback == "" {
		fallback = "\n"
	}

	lines := splitTextLines(text)
	content := make([]string, len(lines))
	for index := range lines {
		content[index] = lines[index].text
	}

	resolutions := make([]hunkResolution, 0, len(hunks))
	warnings := make([]string, 0)
	var failures []error
	cursor := 0
	for index, hunk := range hunks {
		reusePreviousBoundary := false
		if len(resolutions) > 0 {
			reusePreviousBoundary = keepsBoundary(content, resolutions[len(resolutions)-1])
		}
		resolution, next, err := resolveHunk(content, hunk, cursor, reusePreviousBoundary, path, index+1)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		resolution.number = index + 1
		resolutions = append(resolutions, resolution)
		if resolution.warning != "" {
			warnings = append(warnings, resolution.warning)
		}
		cursor = next
	}
	if len(failures) > 0 {
		return "", nil, errors.Join(failures...)
	}

	// A context line must survive the patch: another hunk may only cover it as the final old line it
	// keeps unchanged.
	for _, resolution := range resolutions {
		for _, other := range resolutions {
			if resolution.context < other.start || resolution.context >= other.end {
				continue
			}
			if resolution.context != other.end-1 || !keepsBoundary(content, other) {
				return "", nil, fmt.Errorf(
					"hunk %d: context %q not found in %q: hunk %d replaces that line",
					resolution.number, hunks[resolution.number-1].Context, path, other.number,
				)
			}
		}
	}

	// An insertion sorts ahead of a replacement starting at the same line, so it lands before the
	// replaced block instead of counting as an overlap.
	slices.SortStableFunc(resolutions, func(a, b hunkResolution) int {
		return cmp.Or(cmp.Compare(a.start, b.start), cmp.Compare(a.end, b.end))
	})
	for index := 1; index < len(resolutions); index++ {
		previous, current := resolutions[index-1], resolutions[index]
		if current.start < previous.end {
			return "", nil, fmt.Errorf(
				"hunk %d overlaps hunk %d at line %d of %q; merge them into one hunk",
				current.number, previous.number, current.start+1, path,
			)
		}
	}

	edited := append([]textLine(nil), lines...)
	for index := len(resolutions) - 1; index >= 0; index-- {
		resolution := resolutions[index]
		terminator := emittedTerminator(edited, resolution, fallback)
		replacement := make([]textLine, 0, len(resolution.lines))
		for _, line := range resolution.lines {
			replacement = append(replacement, textLine{text: line, terminator: terminator})
		}
		tail := append([]textLine(nil), edited[resolution.end:]...)
		edited = append(edited[:resolution.start], append(replacement, tail...)...)
	}

	// Every line but the last needs a terminator; only a file that already lacked a final newline
	// keeps its last line unterminated.
	for index := range max(len(edited)-1, 0) {
		if edited[index].terminator == "" {
			edited[index].terminator = fallback
		}
	}
	if len(edited) > 0 && text != "" && lines[len(lines)-1].terminator == "" {
		edited[len(edited)-1].terminator = ""
	}
	return joinTextLines(edited), warnings, nil
}

// applySearchReplace substitutes oldString with newString in decoded file text. Newlines in the
// search and replacement are rewritten to the file's existing style first, so a model that emits
// LF still matches a CRLF INI. A non-replaceAll call requires exactly one match. When the exact
// text is absent, whole lines are compared with the relaxed hunk passes and a unique block is
// replaced, which is reported as a warning.
func applySearchReplace(
	text, oldString, newString, path, newline string,
	replaceAll bool,
) (string, string, error) {
	if oldString == "" {
		return "", "", fmt.Errorf("update %q: oldString must not be empty", path)
	}
	old, next := oldString, newString
	if newline != "" {
		old = normalizeNewlines(oldString, newline)
		next = normalizeNewlines(newString, newline)
	}
	if old == next {
		return "", "", fmt.Errorf("update %q: oldString and newString are identical", path)
	}
	count := strings.Count(text, old)
	if count > 1 && !replaceAll {
		return "", "", fmt.Errorf(
			"update %q: oldString matches %d times; include a unique nearby section header or more surrounding lines, or set replaceAll",
			path,
			count,
		)
	}
	if replaceAll && count > 0 {
		return strings.ReplaceAll(text, old, next), "", nil
	}
	if count == 1 {
		return strings.Replace(text, old, next, 1), "", nil
	}

	// The relaxed match covers whole lines. A trailing newline in oldString decides whether the
	// last matched line's terminator belongs to the replaced range.
	lines := splitTextLines(text)
	content := make([]string, len(lines))
	for index := range lines {
		content[index] = lines[index].text
	}
	pattern := strings.Split(strings.ReplaceAll(oldString, "\r\n", "\n"), "\n")
	coversTerminator := len(pattern) > 1 && pattern[len(pattern)-1] == ""
	if coversTerminator {
		pattern = pattern[:len(pattern)-1]
	}
	match := findLineBlock(content, pattern, 0)
	if len(match.candidates) > 1 {
		return "", "", fmt.Errorf(
			"update %q: oldString is not in the file verbatim and resembles lines %s; copy the exact text with a unique nearby section header",
			path,
			lineNumberList(match.candidates),
		)
	}
	if !match.found {
		return "", "", fmt.Errorf(
			"update %q: oldString not found (it starts with %s); copy the exact text from the file, then retry this update",
			path,
			hunkPreview(pattern[:1]),
		)
	}

	start := 0
	for _, line := range lines[:match.index] {
		start += len(line.text) + len(line.terminator)
	}
	end := start
	for offset, line := range lines[match.index : match.index+len(pattern)] {
		end += len(line.text)
		if offset < len(pattern)-1 || coversTerminator {
			end += len(line.terminator)
		}
	}
	label := passLabel(match.pass)
	if label == "" {
		label = "line endings ignored"
	}
	warning := fmt.Sprintf("oldString matched at line %d with %s", match.index+1, label)
	return text[:start] + next + text[end:], warning, nil
}

func resolveHunk(
	lines []string,
	hunk PatchHunk,
	start int,
	reusePreviousBoundary bool,
	path string,
	number int,
) (hunkResolution, int, error) {
	if len(hunk.OldLines) == 0 && !hunk.EOF && hunk.Context == "" {
		return hunkResolution{}, 0, fmt.Errorf("hunk %d: empty oldLines requires a context line or eof: true", number)
	}

	cursor, contextIndex := start, -1
	if hunk.Context != "" {
		match := seekLineBlock(lines, []string{hunk.Context}, cursor)
		if !match.found && reusePreviousBoundary && cursor > 0 {
			// Adjacent hunks commonly keep the next section header as the final old line, then reuse
			// that unchanged boundary as the following hunk's context. Include only that boundary in
			// the fallback so a context farther behind the cursor cannot make hunks overlap.
			boundary := findLineBlock(lines[cursor-1:cursor], []string{hunk.Context}, 0)
			if boundary.found {
				match = lineMatch{found: true, index: cursor - 1, pass: boundary.pass}
			}
		}
		if !match.found && cursor > 0 {
			match = findLineBlock(lines, []string{hunk.Context}, 0)
		}
		switch {
		case match.found:
			cursor, contextIndex = match.index+1, match.index
		case len(match.candidates) > 1:
			return hunkResolution{}, 0, fmt.Errorf("hunk %d: ambiguous context %q in %q at lines %s; add more oldLines",
				number, hunk.Context, path, lineNumberList(match.candidates))
		default:
			return hunkResolution{}, 0, fmt.Errorf("hunk %d: context %q not found in %q", number, hunk.Context, path)
		}
	}

	if len(hunk.OldLines) == 0 {
		index := cursor
		if hunk.EOF || index > len(lines) {
			index = len(lines)
		}
		return hunkResolution{start: index, end: index, context: contextIndex, lines: hunk.NewLines}, index, nil
	}

	// The first block after the previous hunk wins, with or without its trailing blank line: a blank
	// line that separates patch sections is read as trailing context.
	pattern, replacement := hunk.OldLines, hunk.NewLines
	shorter, shorterReplacement := pattern, replacement
	if last := len(pattern) - 1; last > 0 && pattern[last] == "" {
		shorter = pattern[:last]
		if len(replacement) > 0 && replacement[len(replacement)-1] == "" {
			shorterReplacement = replacement[:len(replacement)-1]
		}
	}
	match := seekHunk(lines, pattern, cursor, hunk.EOF)
	if !match.found && len(shorter) < len(pattern) {
		if match = seekHunk(lines, shorter, cursor, hunk.EOF); match.found {
			pattern, replacement = shorter, shorterReplacement
		}
	}

	// Hunks may also arrive out of file order, so a block that only exists before the cursor is
	// accepted when it is unique in the whole file; the caller rejects overlapping hunks afterwards.
	if !match.found && cursor > 0 {
		match = findLineBlock(lines, pattern, 0)
		if !match.found && len(match.candidates) == 0 && len(shorter) < len(pattern) {
			if match = findLineBlock(lines, shorter, 0); match.found {
				pattern, replacement = shorter, shorterReplacement
			}
		}
	}
	if !match.found {
		if len(match.candidates) > 1 {
			return hunkResolution{}, 0, fmt.Errorf(
				"hunk %d: ambiguous match in %q at lines %s; add a unique section header as context or more oldLines, then retry this update",
				number,
				path,
				lineNumberList(match.candidates),
			)
		}
		return hunkResolution{}, 0, fmt.Errorf(
			"hunk %d: expected lines not found in %q: %s; copy the exact text and retry this update",
			number, path, hunkPreview(hunk.OldLines),
		)
	}

	warning := ""
	if label := passLabel(match.pass); label != "" {
		warning = fmt.Sprintf("hunk %d: matched at line %d with %s", number, match.index+1, label)
	}
	end := match.index + len(pattern)
	resolution := hunkResolution{
		start: match.index, end: end, context: contextIndex, lines: replacement, warning: warning,
	}
	return resolution, end, nil
}

// seekHunk finds the old lines of one hunk at or after the cursor: anchored to the end of the file
// when the hunk asks for it, else the first matching block.
func seekHunk(lines, pattern []string, cursor int, eof bool) lineMatch {
	if tail := len(lines) - len(pattern); eof && tail >= cursor {
		if match := seekLineBlock(lines[tail:], pattern, 0); match.found {
			return lineMatch{found: true, index: tail, pass: match.pass}
		}
	}
	return seekLineBlock(lines, pattern, cursor)
}

func keepsBoundary(lines []string, resolution hunkResolution) bool {
	if resolution.end <= resolution.start || len(resolution.lines) == 0 {
		return false
	}
	return lines[resolution.end-1] == resolution.lines[len(resolution.lines)-1]
}

var lineComparisons = []func(a, b string) bool{
	func(a, b string) bool { return a == b },
	func(a, b string) bool { return strings.TrimRight(a, " \t") == strings.TrimRight(b, " \t") },
	func(a, b string) bool { return strings.TrimSpace(a) == strings.TrimSpace(b) },
	func(a, b string) bool {
		return punctuationNormalizer.Replace(strings.TrimSpace(a)) ==
			punctuationNormalizer.Replace(strings.TrimSpace(b))
	},
}

// seekLineBlock locates the first line range equal to pattern at or after start, in the strictest
// pass that matches at all.
func seekLineBlock(lines []string, pattern []string, start int) lineMatch {
	for index, equal := range lineComparisons {
		if candidates := candidateLines(lines, pattern, start, equal); len(candidates) > 0 {
			return lineMatch{found: true, index: candidates[0], pass: index + 1}
		}
	}
	return lineMatch{}
}

// findLineBlock locates the single line range equal to pattern at or after start.
func findLineBlock(lines []string, pattern []string, start int) lineMatch {
	var strictest []int
	for index, equal := range lineComparisons {
		candidates := candidateLines(lines, pattern, start, equal)
		if len(candidates) == 0 {
			continue
		}
		if strictest == nil {
			strictest = candidates
		}
		if len(candidates) == 1 {
			return lineMatch{found: true, index: candidates[0], pass: index + 1}
		}
	}
	return lineMatch{candidates: strictest}
}

func candidateLines(lines []string, pattern []string, start int, equal func(a, b string) bool) []int {
	if len(pattern) == 0 || len(lines) < len(pattern) {
		return nil
	}
	if start < 0 {
		start = 0
	}
	candidates := make([]int, 0, 1)
	for index := start; index+len(pattern) <= len(lines); index++ {
		matched := true
		for offset := range pattern {
			if !equal(lines[index+offset], pattern[offset]) {
				matched = false
				break
			}
		}
		if matched {
			candidates = append(candidates, index)
		}
	}
	return candidates
}

// emittedTerminator picks the line ending for replacement lines: the ending of the first replaced
// line, else of the line before the insertion, else of the line it precedes, else the file default.
func emittedTerminator(lines []textLine, resolution hunkResolution, fallback string) string {
	if resolution.end > resolution.start && lines[resolution.start].terminator != "" {
		return lines[resolution.start].terminator
	}
	if resolution.start > 0 && lines[resolution.start-1].terminator != "" {
		return lines[resolution.start-1].terminator
	}
	if resolution.start < len(lines) && lines[resolution.start].terminator != "" {
		return lines[resolution.start].terminator
	}
	return fallback
}

func passLabel(pass int) string {
	switch pass {
	case linePassTrimRight:
		return "trailing whitespace ignored"
	case linePassTrim:
		return "indentation ignored"
	case linePassPunctuation:
		return "typographic punctuation normalized"
	default:
		return ""
	}
}

func splitTextLines(text string) []textLine {
	if text == "" {
		return nil
	}
	lines := make([]textLine, 0, strings.Count(text, "\n")+1)
	for len(text) > 0 {
		index := strings.IndexByte(text, '\n')
		if index < 0 {
			lines = append(lines, textLine{text: text})
			break
		}
		line, terminator := text[:index], "\n"
		if strings.HasSuffix(line, "\r") {
			line, terminator = line[:len(line)-1], "\r\n"
		}
		lines = append(lines, textLine{text: line, terminator: terminator})
		text = text[index+1:]
	}
	return lines
}

func joinTextLines(lines []textLine) string {
	var builder strings.Builder
	for _, line := range lines {
		builder.WriteString(line.text)
		builder.WriteString(line.terminator)
	}
	return builder.String()
}

func lineNumberList(candidates []int) string {
	numbers := make([]string, 0, len(candidates))
	for _, index := range candidates {
		numbers = append(numbers, fmt.Sprint(index+1))
	}
	return strings.Join(numbers, ", ")
}

func hunkPreview(lines []string) string {
	first := truncateForMessage(strings.TrimSpace(lines[0]), hunkPreviewLimit)
	if len(lines) > 1 {
		return fmt.Sprintf("%q and %d more lines", first, len(lines)-1)
	}
	return fmt.Sprintf("%q", first)
}

func truncateForMessage(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit]) + "…"
}
