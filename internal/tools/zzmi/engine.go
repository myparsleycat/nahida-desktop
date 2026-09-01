package zzmi

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var (
	hashLinePattern   = regexp.MustCompile(`(?i)^\s*hash\s*=\s*([a-f0-9]{8})\s*$`)
	assignmentPattern = regexp.MustCompile(`(?i)^\s*([a-z0-9_]+)\s*=\s*(.*?)\s*$`)
)

type textEncoding struct {
	name string
	bom  bool
	crlf bool
}

type section struct {
	start int
	end   int
	title string
	body  string
}

type engine struct {
	ctx         context.Context
	root        string
	pack        *RulePack
	log         Logger
	buffers     map[string][]byte
	bufferPaths map[string]string
	bufferKind  map[string]string
	applied     map[string]map[string]bool
	warnings    []string
}

func Run(ctx context.Context, target, tool string, pack *RulePack, log Logger) (Result, error) {
	if err := pack.Validate(); err != nil {
		return Result{}, err
	}
	root, err := filepath.EvalSymlinks(target)
	if err != nil {
		return Result{}, fmt.Errorf("resolve ZZMI target: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Result{}, err
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return Result{}, errors.New("ZZMI target is not a directory")
	}
	if tool != ToolHash && tool != ToolJane && tool != ToolDialyn {
		return Result{}, fmt.Errorf("unsupported ZZMI fixer tool %q", tool)
	}
	eng := &engine{
		ctx: ctx, root: root, pack: pack, log: log,
		buffers: map[string][]byte{}, bufferPaths: map[string]string{}, bufferKind: map[string]string{}, applied: map[string]map[string]bool{},
	}
	inis, err := eng.scanINI()
	if err != nil {
		return Result{}, err
	}
	result := Result{ScannedINI: len(inis)}
	changes := map[string]Change{}
	for _, filename := range inis {
		if err := ctx.Err(); err != nil {
			return Result{}, err
		}
		if log != nil {
			log("Scanning " + relativeDisplay(root, filename))
		}
		buffersBefore := maps.Clone(eng.buffers)
		pathsBefore := maps.Clone(eng.bufferPaths)
		kindsBefore := maps.Clone(eng.bufferKind)
		appliedBefore := cloneApplied(eng.applied)
		var fileChanges []Change
		if tool == ToolHash {
			fileChanges, err = eng.fixHashFile(filename)
		} else {
			fileChanges, err = eng.fixRemapperFile(filename, tool)
		}
		if err != nil {
			eng.buffers = buffersBefore
			eng.bufferPaths = pathsBefore
			eng.bufferKind = kindsBefore
			eng.applied = appliedBefore
			result.SkippedFiles++
			eng.warn(fmt.Sprintf("Skipped %s: %v", relativeDisplay(root, filename), err))
			continue
		}
		for _, change := range fileChanges {
			changes[strings.ToLower(filepath.Clean(change.Path))] = change
			if change.Kind == "ini" {
				result.ChangedINI++
			}
		}
	}
	for key, data := range eng.buffers {
		filename := eng.bufferPaths[key]
		original, readErr := os.ReadFile(filename)
		if readErr != nil {
			return Result{}, fmt.Errorf("read staged buffer %s: %w", filename, readErr)
		}
		if bytes.Equal(original, data) {
			continue
		}
		changes[strings.ToLower(filepath.Clean(filename))] = Change{Path: filename, Kind: "buf", Data: data}
		result.ChangedBUF++
	}
	result.Changes = make([]Change, 0, len(changes))
	for _, change := range changes {
		result.Changes = append(result.Changes, change)
	}
	sort.Slice(result.Changes, func(i, j int) bool {
		return strings.ToLower(result.Changes[i].Path) < strings.ToLower(result.Changes[j].Path)
	})
	result.Warnings = append(result.Warnings, eng.warnings...)
	return result, nil
}

func (e *engine) scanINI() ([]string, error) {
	files := []string{}
	err := filepath.WalkDir(e.root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := e.ctx.Err(); err != nil {
			return err
		}
		if path != e.root && entry.Type()&os.ModeSymlink != 0 {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		upper := strings.ToUpper(entry.Name())
		if path != e.root && entry.IsDir() && (strings.HasPrefix(upper, "DISABLED") || strings.HasPrefix(upper, "DESKTOP")) {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".ini") || strings.HasPrefix(upper, "DISABLED") || strings.HasPrefix(upper, "DESKTOP") {
			return nil
		}
		resolved, err := e.securePath(path)
		if err != nil {
			e.warn(fmt.Sprintf("Skipped unsafe path %s: %v", relativeDisplay(e.root, path), err))
			return nil
		}
		files = append(files, resolved)
		return nil
	})
	sort.Slice(files, func(i, j int) bool { return strings.ToLower(files[i]) < strings.ToLower(files[j]) })
	return files, err
}

func (e *engine) fixHashFile(filename string) ([]Change, error) {
	original, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	content, encoding, err := decodeINI(original)
	if err != nil {
		return nil, err
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	hashes := collectHashes(content)
	known := make(map[string]bool, len(hashes))
	for _, hash := range hashes {
		known[hash] = true
	}
	done := map[string]bool{}
	for len(hashes) > 0 {
		if err := e.ctx.Err(); err != nil {
			return nil, err
		}
		hash := hashes[len(hashes)-1]
		hashes = hashes[:len(hashes)-1]
		if done[hash] {
			continue
		}
		commands := e.pack.HashCommands[hash]
		for _, command := range commands {
			var queued []string
			content, queued, err = e.executeCommand(filename, content, hash, known, command)
			if err != nil {
				return nil, fmt.Errorf("%s %s: %w", hash, command.Op, err)
			}
			for _, item := range queued {
				item = strings.ToLower(item)
				known[item] = true
				if !done[item] {
					hashes = append(hashes, item)
				}
			}
		}
		done[hash] = true
	}
	updated, err := encodeINI(content, encoding)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(original, updated) {
		return nil, nil
	}
	return []Change{{Path: filename, Kind: "ini", Data: updated}}, nil
}

func (e *engine) executeCommand(filename, content, activeHash string, known map[string]bool, command Command) (string, []string, error) {
	switch command.Op {
	case "log":
		if e.log != nil && len(command.Args) > 0 {
			e.log(fmt.Sprint(command.Args[0]))
		}
		return content, nil, nil
	case "update_hash":
		newHash, err := stringArg(command.Args, 0)
		if err != nil || !isHash(newHash) {
			return content, nil, errors.New("invalid update_hash arguments")
		}
		return replaceHashLines(content, activeHash, strings.ToLower(newHash)), []string{newHash}, nil
	case "add_section_if_missing", "multiply_section_if_missing":
		hashes, err := hashArgs(command.Args, 0)
		if err != nil || len(hashes) == 0 {
			return content, nil, errors.New("invalid equivalent hashes")
		}
		for _, hash := range hashes {
			if known[strings.ToLower(hash)] {
				return content, nil, nil
			}
		}
		active := firstHashSection(content, activeHash)
		if active == nil {
			return content, nil, errors.New("active hash section is missing")
		}
		title, err := stringArg(command.Args, 1)
		if err != nil {
			return content, nil, err
		}
		body := ""
		if command.Op == "multiply_section_if_missing" {
			body = criticalContent(active.body)
		} else if len(command.Args) > 2 {
			body, err = stringArg(command.Args, 2)
			if err != nil {
				return content, nil, err
			}
		}
		newSection := fmt.Sprintf("\n[TextureOverride%s]\nhash = %s\n%s\n", title, hashes[0], body)
		return content[:active.end] + newSection + content[active.end:], []string{hashes[0]}, nil
	case "add_ib_check_if_missing":
		return addIBChecks(content, activeHash), nil, nil
	case "transfer_indexed_sections":
		updated, err := transferIndexed(content, activeHash, command.Kwargs)
		return updated, nil, err
	case "update_buffer_blend_indices":
		return content, nil, e.updateBlendBuffers(filename, content, command.Args)
	case "zzz_12_shrink_texcoord_color":
		updated, err := e.shrinkTexcoord(filename, content, activeHash, command.Args)
		return updated, nil, err
	case "zzz_13_remap_texcoord":
		updated, err := e.remapTexcoord(filename, content, activeHash, command.Args)
		return updated, nil, err
	default:
		return content, nil, fmt.Errorf("unsupported operation %s", command.Op)
	}
}

func replaceHashLines(content, oldHash, newHash string) string {
	lines := strings.Split(content, "\n")
	for index, line := range lines {
		match := hashLinePattern.FindStringSubmatch(line)
		if len(match) == 2 && strings.EqualFold(match[1], oldHash) {
			indent := line[:len(line)-len(strings.TrimLeft(line, " \t"))]
			lines[index] = indent + "hash = " + newHash + "\n; " + line
		}
	}
	return strings.Join(lines, "\n")
}

func addIBChecks(content, hash string) string {
	sections := parseSections(content)
	for index := len(sections) - 1; index >= 0; index-- {
		item := sections[index]
		if !sectionHasHash(item.body, hash) || containsAssignment(item.body, "run", "CommandListSkinTexture") {
			continue
		}
		lines := strings.Split(item.body, "\n")
		insert := -1
		for lineIndex, line := range lines {
			match := assignmentPattern.FindStringSubmatch(line)
			if len(match) == 3 && (strings.EqualFold(match[1], "match_first_index") || insert < 0 && strings.EqualFold(match[1], "hash")) {
				insert = lineIndex + 1
				if strings.EqualFold(match[1], "match_first_index") {
					break
				}
			}
		}
		if insert < 0 {
			continue
		}
		lines = append(lines, "")
		copy(lines[insert+1:], lines[insert:])
		lines[insert] = "run = CommandListSkinTexture"
		body := strings.Join(lines, "\n")
		content = content[:item.start] + body + content[item.end:]
	}
	return content
}

func transferIndexed(content, hash string, kwargs map[string]any) (string, error) {
	source, err := stringSlice(kwargs["src_indices"])
	if err != nil {
		return content, err
	}
	target, err := stringSlice(kwargs["trg_indices"])
	if err != nil || len(source) != len(target) {
		return content, errors.New("invalid transfer indices")
	}
	sections := parseSections(content)
	matched := []section{}
	for _, item := range sections {
		if sectionHasHash(item.body, hash) {
			matched = append(matched, item)
		}
	}
	if len(matched) == 0 {
		return content, nil
	}
	indexedCount := 0
	for _, item := range matched {
		if assignmentValue(item.body, "match_first_index") != "" {
			indexedCount++
		}
	}
	if indexedCount == 0 {
		return content, nil
	}
	title, err := indexedSectionBaseTitle(matched[0])
	if err != nil {
		return content, err
	}
	critical := map[string]string{}
	var unindexed string
	for _, item := range matched {
		index := assignmentValue(item.body, "match_first_index")
		if index == "" {
			unindexed = criticalContent(item.body)
		} else {
			critical[index] = criticalContent(item.body)
		}
	}
	for _, index := range source {
		if index != "-1" {
			if _, ok := critical[index]; !ok {
				return content, fmt.Errorf("source index %s is missing", index)
			}
		}
	}
	var replacement strings.Builder
	if unindexed != "" {
		fmt.Fprintf(&replacement, "[TextureOverride%sIB]\nhash = %s\n%s\n\n", title, hash, unindexed)
	}
	for index := range source {
		letter := string(rune('A' + index))
		fmt.Fprintf(&replacement, "[TextureOverride%s%s]\nhash = %s\nmatch_first_index = %s\n", title, letter, hash, target[index])
		if source[index] == "-1" {
			replacement.WriteString("ib = null\n\n")
		} else {
			replacement.WriteString(critical[source[index]] + "\n\n")
		}
	}
	var stripped strings.Builder
	previousEnd := 0
	for _, item := range matched {
		stripped.WriteString(content[previousEnd:item.start])
		previousEnd = item.end
	}
	stripped.WriteString(content[previousEnd:])
	withoutMatched := stripped.String()
	position := matched[0].start
	return withoutMatched[:position] + replacement.String() + withoutMatched[position:], nil
}

func indexedSectionBaseTitle(item section) (string, error) {
	const prefix = "TextureOverride"
	if len(item.title) < len(prefix) || !strings.EqualFold(item.title[:len(prefix)], prefix) {
		return "", errors.New("indexed section is not a TextureOverride")
	}
	title := item.title[len(prefix):]
	if assignmentValue(item.body, "match_first_index") == "" {
		if len(title) < 2 || !strings.EqualFold(title[len(title)-2:], "IB") {
			return "", errors.New("unindexed section title is missing the IB suffix")
		}
		return title[:len(title)-2], nil
	}
	if title == "" {
		return "", errors.New("indexed section title is missing a suffix")
	}
	return title[:len(title)-1], nil
}

func (e *engine) updateBlendBuffers(filename, content string, args []any) error {
	hash, err := stringArg(args, 0)
	if err != nil {
		return err
	}
	oldValues, err := uintSlice(args[1])
	if err != nil {
		return err
	}
	newValues, err := uintSlice(args[2])
	if err != nil || len(oldValues) != len(newValues) {
		return errors.New("invalid blend index mapping")
	}
	mapping := make(map[uint32]uint32, len(oldValues))
	for i := range oldValues {
		mapping[oldValues[i]] = newValues[i]
	}
	for _, resource := range referencedResources(content, hash, "vb2") {
		path, stride, err := e.resourceFile(filename, content, resource)
		if err != nil {
			return err
		}
		if stride != DefaultBufferStride {
			return fmt.Errorf("blend resource stride is %d, expected %d", stride, DefaultBufferStride)
		}
		if err := e.remapBlend(path, mapping, "hash:"+hash); err != nil {
			return err
		}
	}
	return nil
}

func (e *engine) shrinkTexcoord(filename, content, hash string, args []any) (string, error) {
	id, err := stringArg(args, 0)
	if err != nil {
		return content, err
	}
	for _, resource := range referencedResources(content, hash, "vb1") {
		path, stride, err := e.resourceFile(filename, content, resource)
		if err != nil {
			return content, err
		}
		if stride < 16 {
			return content, errors.New("texcoord stride is smaller than color data")
		}
		kind := id + ":shrink"
		if e.alreadyApplied(path, kind) {
			content = updateResourceStride(content, resource, stride-12)
			continue
		}
		data, err := e.buffer(path)
		if err != nil {
			return content, err
		}
		if len(data)%stride != 0 {
			return content, fmt.Errorf("buffer length is not divisible by stride %d", stride)
		}
		out := make([]byte, 0, len(data)-(len(data)/stride)*12)
		for offset := 0; offset < len(data); offset += stride {
			for component := range 4 {
				value := math.Float32frombits(binary.LittleEndian.Uint32(data[offset+component*4:]))
				out = append(out, unormByte(value))
			}
			out = append(out, data[offset+16:offset+stride]...)
		}
		e.setBuffer(path, out, kind)
		e.markApplied(path, kind)
		content = updateResourceStride(content, resource, stride-12)
	}
	return content, nil
}

func (e *engine) remapTexcoord(filename, content, hash string, args []any) (string, error) {
	id, err := stringArg(args, 0)
	if err != nil {
		return content, err
	}
	oldFormat, err := stringSlice(args[1])
	if err != nil {
		return content, err
	}
	newFormat, err := stringSlice(args[2])
	if err != nil || len(oldFormat) != len(newFormat) {
		return content, errors.New("invalid texcoord formats")
	}
	oldStride, err := formatStride(oldFormat)
	if err != nil {
		return content, err
	}
	newStride, err := formatStride(newFormat)
	if err != nil {
		return content, err
	}
	for _, resource := range referencedResources(content, hash, "vb1") {
		path, stride, err := e.resourceFile(filename, content, resource)
		if err != nil {
			return content, err
		}
		if stride != oldStride {
			return content, fmt.Errorf("resource stride %d does not match expected %d", stride, oldStride)
		}
		kind := id + ":remap"
		if e.alreadyApplied(path, kind) {
			content = updateResourceStride(content, resource, newStride)
			continue
		}
		data, err := e.buffer(path)
		if err != nil {
			return content, err
		}
		if len(data)%stride != 0 {
			return content, errors.New("texcoord buffer has a partial vertex")
		}
		out := make([]byte, 0, len(data)/stride*newStride)
		for offset := 0; offset < len(data); offset += stride {
			cursor := 0
			for i := range oldFormat {
				size, _ := chunkSize(oldFormat[i])
				converted, convertErr := convertChunk(data[offset+cursor:offset+cursor+size], oldFormat[i], newFormat[i])
				if convertErr != nil {
					return content, convertErr
				}
				out = append(out, converted...)
				cursor += size
			}
		}
		e.setBuffer(path, out, kind)
		e.markApplied(path, kind)
		content = updateResourceStride(content, resource, newStride)
	}
	return content, nil
}

func (e *engine) fixRemapperFile(filename, tool string) ([]Change, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	content, _, err := decodeINI(raw)
	if err != nil {
		return nil, err
	}
	content = strings.ReplaceAll(content, "\r\n", "\n")
	rules := e.pack.Dialyn
	if tool == ToolJane {
		rules = e.pack.Jane
	}
	valid := map[string]bool{}
	for _, hash := range rules.ValidHashes {
		valid[strings.ToLower(hash)] = true
	}
	resourceTargets := map[string]string{}
	for _, item := range parseSections(content) {
		hash := strings.ToLower(assignmentValue(item.body, "hash"))
		if mapped := rules.PositionToBlend[hash]; mapped != "" {
			hash = mapped
		}
		if !valid[hash] {
			continue
		}
		resource := trimResourcePrefix(assignmentValue(item.body, "vb2"))
		if resource != "" {
			resourceTargets[strings.ToLower(resource)] = hash
		}
	}
	if len(resourceTargets) == 0 {
		for _, item := range parseSections(content) {
			resource := trimResourcePrefix(assignmentValue(item.body, "vb2"))
			lower := strings.ToLower(resource)
			if tool == ToolDialyn && lower != "" {
				resourceTargets[lower] = rules.ValidHashes[0]
				continue
			}
			switch {
			case strings.Contains(lower, "hair") || strings.Contains(lower, "head"):
				resourceTargets[lower] = rules.ValidHashes[0]
			case strings.Contains(lower, "hand") || strings.Contains(lower, "finger") || strings.Contains(lower, "accessor") || strings.Contains(lower, "knife"):
				resourceTargets[lower] = rules.ValidHashes[len(rules.ValidHashes)-1]
			}
		}
	}
	for _, resource := range slices.Sorted(maps.Keys(resourceTargets)) {
		hash := resourceTargets[resource]
		path, stride, pathErr := e.resourceFile(filename, content, resource)
		if pathErr != nil {
			e.warn(fmt.Sprintf("Skipped resource %s in %s: %v", resource, relativeDisplay(e.root, filename), pathErr))
			continue
		}
		if stride != rules.Stride {
			e.warn(fmt.Sprintf("Skipped %s: stride is %d", relativeDisplay(e.root, path), stride))
			continue
		}
		mapping := rules.Mapping
		if tool == ToolJane && hash == rules.ValidHashes[len(rules.ValidHashes)-1] {
			mapping = rules.Secondary
		}
		if err := e.remapBlend(path, mapping, tool+":"+hash); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (e *engine) remapBlend(path string, mapping map[uint32]uint32, kind string) error {
	key := canonicalBufferKey(path)
	if e.alreadyApplied(path, kind) {
		return nil
	}
	if previous := e.bufferKind[key]; previous != "" && previous != kind {
		e.warn(fmt.Sprintf("Skipped conflicting remap for %s (%s, %s)", relativeDisplay(e.root, path), previous, kind))
		return nil
	}
	data, err := e.buffer(path)
	if err != nil {
		return err
	}
	if len(data)%DefaultBufferStride != 0 {
		return errors.New("blend buffer length is not divisible by 32")
	}
	out := bytes.Clone(data)
	changed := false
	for offset := 0; offset < len(out); offset += DefaultBufferStride {
		for component := range 4 {
			indexOffset := offset + 16 + component*4
			value := binary.LittleEndian.Uint32(out[indexOffset:])
			if mapped, ok := mapping[value]; ok && mapped != value {
				binary.LittleEndian.PutUint32(out[indexOffset:], mapped)
				changed = true
			}
		}
	}
	if changed {
		e.setBuffer(path, out, kind)
	}
	e.markApplied(path, kind)
	return nil
}

func (e *engine) resourceFile(iniPath, content, name string) (string, int, error) {
	name = trimResourcePrefix(name)
	for _, item := range parseSections(content) {
		if !strings.EqualFold(item.title, "Resource"+name) {
			continue
		}
		if !strings.EqualFold(assignmentValue(item.body, "type"), "Buffer") {
			return "", 0, errors.New("resource is not a Buffer")
		}
		stride, err := strconv.Atoi(assignmentValue(item.body, "stride"))
		if err != nil || stride <= 0 {
			return "", 0, errors.New("resource has invalid stride")
		}
		filename := strings.TrimSpace(assignmentValue(item.body, "filename"))
		if filename == "" {
			return "", 0, errors.New("resource has no filename")
		}
		resolved, err := e.securePath(filepath.Join(filepath.Dir(iniPath), filepath.FromSlash(strings.ReplaceAll(filename, "\\", "/"))))
		return resolved, stride, err
	}
	return "", 0, fmt.Errorf("resource %s was not found", name)
}

func (e *engine) securePath(candidate string) (string, error) {
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	logicalRelative, err := filepath.Rel(e.root, abs)
	if err != nil || logicalRelative == ".." || strings.HasPrefix(logicalRelative, ".."+string(filepath.Separator)) || filepath.IsAbs(logicalRelative) {
		return "", errors.New("path escapes the selected ZZMI target")
	}
	current := e.root
	for _, part := range strings.Split(logicalRelative, string(filepath.Separator)) {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("symbolic links and reparse points are not allowed")
		}
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	relative, err := filepath.Rel(e.root, resolved)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return "", errors.New("path escapes the selected ZZMI target")
	}
	return resolved, nil
}

func (e *engine) buffer(path string) ([]byte, error) {
	key := canonicalBufferKey(path)
	if data, ok := e.buffers[key]; ok {
		return data, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (e *engine) setBuffer(path string, data []byte, kind string) {
	key := canonicalBufferKey(path)
	e.buffers[key] = data
	e.bufferPaths[key] = path
	e.bufferKind[key] = kind
}
func (e *engine) alreadyApplied(path, kind string) bool {
	return e.applied[canonicalBufferKey(path)][kind]
}
func (e *engine) markApplied(path, kind string) {
	key := canonicalBufferKey(path)
	if e.applied[key] == nil {
		e.applied[key] = map[string]bool{}
	}
	e.applied[key][kind] = true
}
func canonicalBufferKey(path string) string {
	return strings.ToLower(filepath.Clean(path))
}
func cloneApplied(source map[string]map[string]bool) map[string]map[string]bool {
	result := make(map[string]map[string]bool, len(source))
	for path, fixes := range source {
		result[path] = maps.Clone(fixes)
	}
	return result
}
func trimResourcePrefix(value string) string {
	value = strings.TrimSpace(value)
	const prefix = "Resource"
	if len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix) {
		return value[len(prefix):]
	}
	return value
}

func unormByte(value float32) byte {
	scaled := float64(value) * 255
	if math.IsNaN(scaled) || scaled <= 0 {
		return 0
	}
	if scaled >= 255 {
		return 255
	}
	return byte(scaled)
}
func (e *engine) warn(message string) {
	e.warnings = append(e.warnings, message)
	if e.log != nil {
		e.log("Warning: " + message)
	}
}

func decodeINI(data []byte) (string, textEncoding, error) {
	encoding := textEncoding{crlf: bytes.Contains(data, []byte("\r\n"))}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		encoding.name, encoding.bom, data = "utf8", true, data[3:]
		return string(data), encoding, nil
	}
	if utf8.Valid(data) {
		encoding.name = "utf8"
		return string(data), encoding, nil
	}
	decoded, err := ioReadAll(transform.NewReader(bytes.NewReader(data), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		return "", encoding, errors.New("INI is neither UTF-8 nor GBK")
	}
	encoding.name = "gbk"
	return string(decoded), encoding, nil
}

func encodeINI(content string, encoding textEncoding) ([]byte, error) {
	if encoding.crlf {
		content = strings.ReplaceAll(strings.ReplaceAll(content, "\r\n", "\n"), "\n", "\r\n")
	}
	if encoding.name == "gbk" {
		return ioReadAll(transform.NewReader(strings.NewReader(content), simplifiedchinese.GBK.NewEncoder()))
	}
	data := []byte(content)
	if encoding.bom {
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	}
	return data, nil
}

var ioReadAll = func(reader interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buffer bytes.Buffer
	_, err := buffer.ReadFrom(reader)
	return buffer.Bytes(), err
}

func parseSections(content string) []section {
	lines := strings.SplitAfter(content, "\n")
	sections := []section{}
	offset := 0
	current := -1
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			if current >= 0 {
				sections[current].end = offset
				sections[current].body = content[sections[current].start:offset]
			}
			sections = append(sections, section{start: offset, title: strings.TrimSpace(trimmed[1 : len(trimmed)-1])})
			current = len(sections) - 1
		}
		offset += len(line)
	}
	if current >= 0 {
		sections[current].end = len(content)
		sections[current].body = content[sections[current].start:]
	}
	return sections
}

func collectHashes(content string) []string {
	result := []string{}
	for _, line := range strings.Split(content, "\n") {
		if m := hashLinePattern.FindStringSubmatch(line); len(m) == 2 {
			result = append(result, strings.ToLower(m[1]))
		}
	}
	return result
}
func firstHashSection(content, hash string) *section {
	for _, item := range parseSections(content) {
		if sectionHasHash(item.body, hash) {
			copy := item
			return &copy
		}
	}
	return nil
}
func sectionHasHash(body, hash string) bool {
	return strings.EqualFold(assignmentValue(body, "hash"), hash)
}
func assignmentValue(body, key string) string {
	for _, line := range strings.Split(body, "\n") {
		if m := assignmentPattern.FindStringSubmatch(line); len(m) == 3 && strings.EqualFold(m[1], key) {
			return strings.TrimSpace(m[2])
		}
	}
	return ""
}
func containsAssignment(body, key, value string) bool {
	return strings.EqualFold(assignmentValue(body, key), value)
}
func criticalContent(body string) string {
	lines := strings.Split(body, "\n")
	out := []string{}
	for i, line := range lines {
		if i == 0 && strings.HasPrefix(strings.TrimSpace(line), "[") {
			continue
		}
		if m := assignmentPattern.FindStringSubmatch(line); len(m) == 3 && (strings.EqualFold(m[1], "hash") || strings.EqualFold(m[1], "match_first_index")) {
			continue
		}
		out = append(out, line)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

func referencedResources(content, hash, target string) []string {
	byTitle := map[string]section{}
	for _, item := range parseSections(content) {
		byTitle[strings.ToLower(item.title)] = item
	}
	seen := map[string]bool{}
	visitedCommandLists := map[string]bool{}
	resources := []string{}
	var visit func(string)
	visit = func(body string) {
		for _, line := range strings.Split(body, "\n") {
			m := assignmentPattern.FindStringSubmatch(line)
			if len(m) != 3 {
				continue
			}
			key, value := strings.ToLower(m[1]), strings.TrimSpace(m[2])
			if key == strings.ToLower(target) {
				if !seen[strings.ToLower(value)] {
					seen[strings.ToLower(value)] = true
					resources = append(resources, value)
				}
			} else if key == "run" {
				title := strings.ToLower(value)
				if visitedCommandLists[title] {
					continue
				}
				if item, ok := byTitle[title]; ok {
					visitedCommandLists[title] = true
					visit(item.body)
				}
			}
		}
	}
	for _, item := range parseSections(content) {
		if sectionHasHash(item.body, hash) {
			visit(item.body)
		}
	}
	return resources
}

func updateResourceStride(content, resource string, stride int) string {
	sections := parseSections(content)
	for i := len(sections) - 1; i >= 0; i-- {
		item := sections[i]
		if !strings.EqualFold(item.title, "Resource"+trimResourcePrefix(resource)) {
			continue
		}
		lines := strings.Split(item.body, "\n")
		for j, line := range lines {
			m := assignmentPattern.FindStringSubmatch(line)
			if len(m) == 3 && strings.EqualFold(m[1], "stride") {
				lines[j] = "stride = " + strconv.Itoa(stride) + "\n; " + line
				break
			}
		}
		content = content[:item.start] + strings.Join(lines, "\n") + content[item.end:]
	}
	return content
}

func stringArg(args []any, index int) (string, error) {
	if index >= len(args) {
		return "", errors.New("missing string argument")
	}
	value, ok := args[index].(string)
	if !ok {
		return "", errors.New("argument is not a string")
	}
	return value, nil
}
func stringSlice(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("argument is not a sequence")
	}
	result := make([]string, len(items))
	for i, item := range items {
		value, ok := item.(string)
		if !ok {
			return nil, errors.New("sequence contains a non-string")
		}
		result[i] = value
	}
	return result, nil
}
func uintSlice(value any) ([]uint32, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errors.New("argument is not a sequence")
	}
	result := make([]uint32, len(items))
	for i, item := range items {
		number, ok := integerValue(item)
		if !ok || number < 0 || number > math.MaxUint32 {
			return nil, errors.New("sequence contains an invalid integer")
		}
		result[i] = uint32(number)
	}
	return result, nil
}

func integerValue(value any) (int64, bool) {
	switch number := value.(type) {
	case int64:
		return number, true
	case json.Number:
		parsed, err := number.Int64()
		return parsed, err == nil
	default:
		return 0, false
	}
}
func hashArgs(args []any, index int) ([]string, error) {
	if index >= len(args) {
		return nil, errors.New("missing hash argument")
	}
	if value, ok := args[index].(string); ok {
		return []string{value}, nil
	}
	return stringSlice(args[index])
}
func relativeDisplay(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.Base(path)
	}
	return relative
}

func formatStride(chunks []string) (int, error) {
	total := 0
	for _, chunk := range chunks {
		size, err := chunkSize(chunk)
		if err != nil {
			return 0, err
		}
		total += size
	}
	return total, nil
}
func chunkSize(chunk string) (int, error) {
	if len(chunk) < 2 {
		return 0, errors.New("invalid struct format")
	}
	count, err := strconv.Atoi(chunk[:len(chunk)-1])
	if err != nil {
		return 0, err
	}
	size := map[byte]int{'B': 1, 'e': 2, 'f': 4}[chunk[len(chunk)-1]]
	if size == 0 {
		return 0, errors.New("unsupported struct format")
	}
	return count * size, nil
}
func convertChunk(data []byte, oldChunk, newChunk string) ([]byte, error) {
	if oldChunk == newChunk {
		return bytes.Clone(data), nil
	}
	oldCount, _ := strconv.Atoi(oldChunk[:len(oldChunk)-1])
	newCount, _ := strconv.Atoi(newChunk[:len(newChunk)-1])
	if oldCount != newCount {
		return nil, errors.New("texcoord element count changed")
	}
	values := make([]float32, oldCount)
	switch oldChunk[len(oldChunk)-1] {
	case 'B':
		for i := range values {
			values[i] = float32(data[i]) / 255
		}
	case 'e':
		for i := range values {
			values[i] = halfToFloat(binary.LittleEndian.Uint16(data[i*2:]))
		}
	case 'f':
		for i := range values {
			values[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*4:]))
		}
	default:
		return nil, errors.New("unsupported old format")
	}
	size, _ := chunkSize(newChunk)
	out := make([]byte, size)
	switch newChunk[len(newChunk)-1] {
	case 'B':
		for i, v := range values {
			out[i] = unormByte(v)
		}
	case 'e':
		for i, v := range values {
			binary.LittleEndian.PutUint16(out[i*2:], floatToHalf(v))
		}
	case 'f':
		for i, v := range values {
			binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(v))
		}
	}
	return out, nil
}
func halfToFloat(value uint16) float32 {
	sign := uint32(value&0x8000) << 16
	exp := (value >> 10) & 0x1f
	mant := uint32(value & 0x3ff)
	var bits uint32
	switch exp {
	case 0:
		if mant == 0 {
			bits = sign
		} else {
			shift := uint32(0)
			for mant&0x400 == 0 {
				mant <<= 1
				shift++
			}
			mant &= 0x3ff
			bits = sign | ((127 - 15 - shift) << 23) | (mant << 13)
		}
	case 31:
		bits = sign | 0x7f800000 | (mant << 13)
	default:
		bits = sign | (uint32(exp+112) << 23) | (mant << 13)
	}
	return math.Float32frombits(bits)
}
func floatToHalf(value float32) uint16 {
	bits := math.Float32bits(value)
	sign := uint16(bits>>16) & 0x8000
	exp := int((bits>>23)&0xff) - 127 + 15
	mant := bits & 0x7fffff
	if exp <= 0 {
		return sign
	}
	if exp >= 31 {
		return sign | 0x7c00
	}
	return sign | uint16(exp<<10) | uint16(mant>>13)
}
