package agent

import (
	"errors"
	"fmt"
	"strings"
)

// The patch text envelope is the apply_patch format of openai/codex (Apache-2.0), which opencode
// and other agents also implement and which GPT models are trained to write:
//
//	*** Begin Patch
//	*** Update File: mod.ini
//	@@ [TextureOverrideBody]
//	 hash = 12345678
//	-run = CommandListOld
//	+run = CommandListNew
//	*** Add File: notes.txt
//	+first line
//	*** Delete File: stale.ini
//	*** End Patch
const (
	patchBeginMarker  = "*** Begin Patch"
	patchEndMarker    = "*** End Patch"
	patchAddHeader    = "*** Add File:"
	patchDeleteHeader = "*** Delete File:"
	patchUpdateHeader = "*** Update File:"
	patchMoveHeader   = "*** Move to:"
	patchEOFMarker    = "*** End of File"
)

// patchTextModel reports whether a model edits files through the patch text envelope instead of
// JSON operations. opencode selects its apply_patch tool with the same rule.
func patchTextModel(model string) bool {
	id := strings.ToLower(model)
	return strings.Contains(id, "gpt-") && !strings.Contains(id, "gpt-4") && !strings.Contains(id, "oss")
}

// parsePatchText turns a patch envelope into the operations the sandbox applies. Each file section
// becomes one operation, and each "@@" chunk of an update section becomes one hunk.
func parsePatchText(text string) ([]PatchOperation, error) {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	begin := -1
	for index, line := range lines {
		if strings.TrimSpace(line) == patchBeginMarker {
			begin = index
			break
		}
	}
	if begin < 0 {
		return nil, fmt.Errorf("patchText must start with %q", patchBeginMarker)
	}
	end := len(lines)
	for index := len(lines) - 1; index > begin; index-- {
		if strings.TrimSpace(lines[index]) == patchEndMarker {
			end = index
			break
		}
	}

	operations := make([]PatchOperation, 0, 1)
	for index := begin + 1; index < end; {
		line := lines[index]
		number := index + 1
		index++

		switch {
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, patchAddHeader):
			added := make([]string, 0)
			for ; index < end && !strings.HasPrefix(lines[index], "*** "); index++ {
				if !strings.HasPrefix(lines[index], "+") {
					return nil, fmt.Errorf("patch line %d: every line of an added file starts with +", index+1)
				}
				added = append(added, lines[index][1:])
			}
			operations = append(operations, PatchOperation{
				Type:    "create",
				Path:    strings.TrimSpace(strings.TrimPrefix(line, patchAddHeader)),
				Content: strings.Join(added, "\n") + "\n",
			})
		case strings.HasPrefix(line, patchDeleteHeader):
			operations = append(operations, PatchOperation{
				Type: "delete", Path: strings.TrimSpace(strings.TrimPrefix(line, patchDeleteHeader)),
			})
		case strings.HasPrefix(line, patchUpdateHeader):
			path := strings.TrimSpace(strings.TrimPrefix(line, patchUpdateHeader))
			if index < end && strings.HasPrefix(lines[index], patchMoveHeader) {
				return nil, fmt.Errorf(
					"patch line %d: %q is not supported; rename with move_path",
					index+1,
					patchMoveHeader,
				)
			}
			hunks, next, err := parsePatchHunks(lines, index, end)
			if err != nil {
				return nil, err
			}
			if len(hunks) == 0 {
				return nil, fmt.Errorf("patch line %d: update of %q has no changes", number, path)
			}
			operations = append(operations, PatchOperation{Type: "update", Path: path, Hunks: hunks})
			index = next
		default:
			return nil, fmt.Errorf(
				"patch line %d: expected %q, %q, or %q, got %s",
				number, patchUpdateHeader, patchAddHeader, patchDeleteHeader, hunkPreview([]string{line}),
			)
		}
	}
	if len(operations) == 0 {
		return nil, errors.New("patchText contains no file sections")
	}
	return operations, nil
}

// parsePatchHunks reads the chunks of one update section, starting at lines[start] and stopping at
// the next file header. A chunk without "@@" context or removed lines appends to the end of the
// file, as codex does.
func parsePatchHunks(lines []string, start, end int) ([]PatchHunk, int, error) {
	hunks := make([]PatchHunk, 0, 1)
	var current *PatchHunk
	finish := func() {
		if current == nil {
			return
		}
		if current.Context == "" && len(current.OldLines) == 0 && len(current.NewLines) > 0 {
			current.EOF = true
		}
		if current.Context != "" || current.EOF || len(current.OldLines) > 0 {
			hunks = append(hunks, *current)
		}
		current = nil
	}

	index := start
	for ; index < end; index++ {
		line := lines[index]
		if line == patchEOFMarker {
			if current == nil {
				return nil, 0, fmt.Errorf("patch line %d: %q must follow a chunk", index+1, patchEOFMarker)
			}
			current.EOF = true
			finish()
			continue
		}
		if strings.HasPrefix(line, "*** ") {
			break
		}
		if strings.HasPrefix(line, "@@") {
			finish()
			current = &PatchHunk{Context: strings.TrimSpace(line[2:])}
			continue
		}
		if current == nil {
			if line == "" {
				continue
			}
			current = &PatchHunk{}
		}

		switch {
		case line == "":
			current.OldLines = append(current.OldLines, "")
			current.NewLines = append(current.NewLines, "")
		case line[0] == ' ':
			current.OldLines = append(current.OldLines, line[1:])
			current.NewLines = append(current.NewLines, line[1:])
		case line[0] == '-':
			current.OldLines = append(current.OldLines, line[1:])
		case line[0] == '+':
			current.NewLines = append(current.NewLines, line[1:])
		default:
			return nil, 0, fmt.Errorf(
				"patch line %d: a chunk line starts with a space, -, or +, got %s",
				index+1, hunkPreview([]string{line}),
			)
		}
	}
	finish()
	return hunks, index, nil
}
