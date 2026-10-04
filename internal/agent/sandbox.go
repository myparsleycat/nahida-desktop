package agent

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/google/uuid"

	agentactions "nahida.live/desktop/internal/agent/actions"
	"nahida.live/desktop/internal/platform"
)

const (
	maxReadBytes     = 1 << 20
	maxSearchMatches = 200
)

var (
	errSandboxPath     = errors.New("path is outside the agent sandbox")
	errSandboxReadOnly = errors.New("path is inside a read-only sandbox root")
)

type SandboxRoot struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Importer string `json:"importer,omitempty"`
	// ReadOnly marks reference material the agent may list, read, and search but never change.
	ReadOnly bool `json:"readOnly,omitempty"`
}

type Sandbox struct {
	roots map[string]*openedRoot
}

type openedRoot struct {
	view SandboxRoot
	root *os.Root
	// physical is the root with every junction resolved, which the read-only check compares against.
	physical string
}

type FileEntry struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	IsDir bool   `json:"isDir"`
}

type ReadFileResult struct {
	Path       string `json:"path"`
	Text       string `json:"text,omitempty"`
	Size       int64  `json:"size"`
	Encoding   string `json:"encoding,omitempty"`
	Binary     bool   `json:"binary"`
	Truncated  bool   `json:"truncated"`
	LineOffset int    `json:"lineOffset,omitempty"`
}

type SearchMatch struct {
	Path    string `json:"path"`
	Line    int    `json:"line"`
	Preview string `json:"preview"`
}

type PatchOperation struct {
	Type            string      `json:"type"`
	Path            string      `json:"path"`
	Content         string      `json:"content,omitempty"`
	ExpectedContent *string     `json:"expectedContent,omitempty"`
	OldString       string      `json:"oldString,omitempty"`
	NewString       string      `json:"newString,omitempty"`
	ReplaceAll      bool        `json:"replaceAll,omitempty"`
	Hunks           []PatchHunk `json:"hunks,omitempty"`
}

// PatchResult reports what one apply_patch call changed, plus any hunk that only matched after the
// matcher relaxed its whitespace or punctuation comparison.
type PatchResult struct {
	ChangedFiles []string `json:"changedFiles"`
	Warnings     []string `json:"warnings,omitempty"`
}

// preparedPatch is the resolved change for one file. Several update operations may target the same
// path: op is the first one, followups are the rest in input order, and updated carries the decoded
// text each later update is applied to.
type preparedPatch struct {
	op        PatchOperation
	followups []PatchOperation
	path      string
	text      string
	updated   string
	data      []byte
	format    textFormat
	exists    bool
	temp      string
	backup    string
	warnings  []string
}

func NewSandbox(roots []SandboxRoot) (*Sandbox, error) {
	sandbox := &Sandbox{roots: make(map[string]*openedRoot, len(roots))}
	for _, view := range roots {
		if view.ID == "" || view.Path == "" {
			_ = sandbox.Close()
			return nil, errors.New("sandbox root requires id and path")
		}
		canonical, err := canonicalExistingDir(view.Path)
		if err != nil {
			_ = sandbox.Close()
			return nil, fmt.Errorf("open sandbox root %s: %w", view.Name, err)
		}
		view.Path = canonical
		root, err := os.OpenRoot(canonical)
		if err != nil {
			_ = sandbox.Close()
			return nil, fmt.Errorf("open sandbox root %s: %w", view.Name, err)
		}
		physical, err := platform.FinalPath(canonical)
		if err != nil {
			_ = root.Close()
			_ = sandbox.Close()
			return nil, fmt.Errorf("open sandbox root %s: %w", view.Name, err)
		}
		sandbox.roots[view.ID] = &openedRoot{view: view, root: root, physical: physical}
	}
	return sandbox, nil
}

func (s *Sandbox) Close() error {
	if s == nil {
		return nil
	}
	var result error
	for _, root := range s.roots {
		result = errors.Join(result, root.root.Close())
	}
	return result
}

func (s *Sandbox) Roots() []SandboxRoot {
	out := make([]SandboxRoot, 0, len(s.roots))
	for _, root := range s.roots {
		out = append(out, root.view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (s *Sandbox) ListFiles(
	ctx context.Context,
	rootID, relativePath string,
	recursive bool,
	limit int,
) ([]FileEntry, error) {
	root, clean, err := s.resolve(rootID, relativePath)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	out := make([]FileEntry, 0)
	err = fs.WalkDir(root.root.FS(), clean, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if path == clean {
			return nil
		}
		if !recursive && filepath.Dir(path) != clean {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		out = append(
			out,
			FileEntry{Path: filepath.ToSlash(path), Name: entry.Name(), Size: info.Size(), IsDir: entry.IsDir()},
		)
		if len(out) >= limit {
			return fs.SkipAll
		}
		return nil
	})
	return out, err
}

func (s *Sandbox) ReadFile(rootID, relativePath string, startLine, endLine int) (ReadFileResult, error) {
	root, clean, err := s.resolve(rootID, relativePath)
	if err != nil {
		return ReadFileResult{}, err
	}
	file, err := root.root.Open(clean)
	if err != nil {
		return ReadFileResult{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return ReadFileResult{}, err
	}
	data, err := io.ReadAll(io.LimitReader(file, maxReadBytes+1))
	if err != nil {
		return ReadFileResult{}, err
	}
	result := ReadFileResult{Path: filepath.ToSlash(clean), Size: info.Size(), Truncated: len(data) > maxReadBytes}
	if result.Truncated {
		data = data[:maxReadBytes]
	}
	text, format, binaryFile := decodeText(data)
	result.Binary = binaryFile
	result.Encoding = format.encoding
	if binaryFile {
		return result, nil
	}
	lines := strings.Split(text, "\n")
	if startLine > 0 {
		start := min(startLine-1, len(lines))
		end := len(lines)
		if endLine >= startLine {
			end = min(endLine, len(lines))
		}
		result.Text = strings.Join(lines[start:end], "\n")
		result.LineOffset = start
		return result, nil
	}
	result.Text = text
	return result, nil
}

func (s *Sandbox) SearchFiles(ctx context.Context, rootID, pattern string, limit int) ([]FileEntry, error) {
	if pattern == "" {
		pattern = "**"
	}
	entries, err := s.ListFiles(ctx, rootID, ".", true, limit)
	if err != nil {
		return nil, err
	}
	out := entries[:0]
	for _, entry := range entries {
		matched, matchErr := doublestar.Match(pattern, entry.Path)
		if matchErr != nil {
			return nil, fmt.Errorf("invalid glob: %w", matchErr)
		}
		if matched || strings.Contains(strings.ToLower(entry.Name), strings.ToLower(pattern)) {
			out = append(out, entry)
		}
	}
	return out, nil
}

func (s *Sandbox) SearchText(ctx context.Context, rootID, pattern string, regex bool) ([]SearchMatch, error) {
	var expression *regexp.Regexp
	var err error
	if regex {
		expression, err = regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regular expression: %w", err)
		}
	}
	files, err := s.ListFiles(ctx, rootID, ".", true, 2000)
	if err != nil {
		return nil, err
	}
	out := make([]SearchMatch, 0)
	for _, entry := range files {
		if entry.IsDir || entry.Size > maxReadBytes {
			continue
		}
		result, readErr := s.ReadFile(rootID, entry.Path, 0, 0)
		if readErr != nil || result.Binary {
			continue
		}
		for index, line := range strings.Split(result.Text, "\n") {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			matched := strings.Contains(line, pattern)
			if expression != nil {
				matched = expression.MatchString(line)
			}
			if matched {
				out = append(out, SearchMatch{Path: entry.Path, Line: index + 1, Preview: line})
				if len(out) == maxSearchMatches {
					return out, nil
				}
			}
		}
	}
	return out, nil
}

func (s *Sandbox) ApplyPatch(rootID string, operations []PatchOperation) (PatchResult, error) {
	root, items, err := s.preparePatch(rootID, operations, false)
	if err != nil {
		return PatchResult{}, err
	}

	transactionID := uuid.NewString()
	for index := range items {
		item := &items[index]
		if item.op.Type != "delete" {
			parent := filepath.Dir(item.path)
			if err := root.root.MkdirAll(parent, 0o755); err != nil {
				cleanupPatchTemps(root, items)
				return PatchResult{}, err
			}
			item.temp = filepath.Join(parent, ".nahida-agent-"+transactionID+"-"+fmt.Sprint(index)+".tmp")
			if err := root.root.WriteFile(item.temp, item.data, 0o644); err != nil {
				cleanupPatchTemps(root, items)
				return PatchResult{}, err
			}
		}
		if item.exists {
			item.backup = filepath.Join(
				filepath.Dir(item.path),
				".nahida-agent-"+transactionID+"-"+fmt.Sprint(index)+".bak",
			)
		}
	}

	committed := 0
	for index := range items {
		item := &items[index]
		if item.exists {
			if err := root.root.Rename(item.path, item.backup); err != nil {
				rollbackPatch(root, items, committed)
				cleanupPatchTemps(root, items)
				return PatchResult{}, err
			}
		}
		if item.op.Type != "delete" {
			if err := root.root.Rename(item.temp, item.path); err != nil {
				if item.exists {
					_ = root.root.Rename(item.backup, item.path)
				}
				rollbackPatch(root, items, committed)
				cleanupPatchTemps(root, items)
				return PatchResult{}, err
			}
		}
		committed++
	}

	result := PatchResult{ChangedFiles: make([]string, 0, len(items)), Warnings: make([]string, 0)}
	for _, item := range items {
		if item.backup != "" {
			_ = root.root.Remove(item.backup)
		}
		result.ChangedFiles = append(result.ChangedFiles, filepath.ToSlash(item.path))
		result.Warnings = append(result.Warnings, item.warnings...)
	}
	return result, nil
}

func (s *Sandbox) PreparePatch(rootID string, operations []PatchOperation) ([]PatchOperation, []string, error) {
	_, items, err := s.preparePatch(rootID, operations, true)
	if err != nil {
		return nil, nil, err
	}
	prepared := make([]PatchOperation, 0, len(operations))
	targets := make([]string, len(items))
	for index, item := range items {
		prepared = append(prepared, item.op)
		prepared = append(prepared, item.followups...)
		targets[index] = filepath.ToSlash(item.path)
	}
	return prepared, targets, nil
}

func (s *Sandbox) preparePatch(
	rootID string,
	operations []PatchOperation,
	sealPreconditions bool,
) (*openedRoot, []preparedPatch, error) {
	root, _, err := s.resolve(rootID, ".")
	if err != nil {
		return nil, nil, err
	}
	if len(operations) == 0 {
		return nil, nil, errors.New("patch requires at least one operation")
	}
	items := make([]preparedPatch, 0, len(operations))
	indexes := make(map[string]int, len(operations))
	// An update that does not match is collected instead of returned, so one rejection names every
	// operation the model has to correct.
	var failures []error
	for number, operation := range operations {
		clean, err := validateRelativePath(operation.Path)
		if err != nil {
			return nil, nil, err
		}
		if err := s.guardWrite(filepath.Join(root.view.Path, clean), false); err != nil {
			return nil, nil, err
		}

		// Later updates to a path already in the patch apply to the result of the earlier ones, so
		// one call can carry every edit to a file. The first operation alone seals the file content.
		pathKey := strings.ToLower(clean)
		if index, ok := indexes[pathKey]; ok {
			earlier := &items[index]
			if earlier.op.Type != "update" || operation.Type != "update" {
				return nil, nil, fmt.Errorf(
					"patch contains duplicate path %q: only update operations may repeat a path; merge the others into one operation",
					clean,
				)
			}
			if operation.ExpectedContent != nil && earlier.text != *operation.ExpectedContent {
				return nil, nil, fmt.Errorf("patch precondition failed for %q", clean)
			}
			updated, warnings, err := applyUpdate(
				earlier.updated, operation, earlier.format.newline, filepath.ToSlash(clean),
			)
			if err != nil {
				failures = append(failures, fmt.Errorf("operation %d: %w", number+1, err))
				continue
			}
			for _, warning := range warnings {
				earlier.warnings = append(earlier.warnings, filepath.ToSlash(clean)+": "+warning)
			}
			earlier.updated = updated
			earlier.data = encodeText(updated, earlier.format)
			earlier.followups = append(earlier.followups, operation)
			continue
		}
		indexes[pathKey] = len(items)

		item := preparedPatch{op: operation, path: clean, format: textFormat{encoding: "utf-8", newline: "\n"}}
		existing, readErr := root.root.ReadFile(clean)
		if readErr == nil {
			item.exists = true
			text, format, binaryFile := decodeText(existing)
			if binaryFile {
				return nil, nil, fmt.Errorf("patch binary file %q: unsupported", clean)
			}
			item.format = format
			item.text = text
			if operation.ExpectedContent != nil && text != *operation.ExpectedContent {
				return nil, nil, fmt.Errorf("patch precondition failed for %q", clean)
			}
			if sealPreconditions && patchReplacesExisting(operation.Type) {
				item.op.ExpectedContent = &text
			}
		} else if !errors.Is(readErr, fs.ErrNotExist) && operation.Type != "delete" {
			return nil, nil, readErr
		} else if operation.ExpectedContent != nil {
			return nil, nil, fmt.Errorf("patch precondition failed for missing file %q", clean)
		}
		switch operation.Type {
		case "create":
			if item.exists {
				return nil, nil, fmt.Errorf("create %q: file already exists", clean)
			}
			item.data = encodeText(normalizeNewlines(operation.Content, item.format.newline), item.format)
		case "write":
			item.data = encodeText(normalizeNewlines(operation.Content, item.format.newline), item.format)
		case "update":
			if !item.exists {
				return nil, nil, fmt.Errorf("update %q: file does not exist", filepath.ToSlash(clean))
			}
			updated, warnings, err := applyUpdate(item.text, operation, item.format.newline, filepath.ToSlash(clean))
			if err != nil {
				// The file stays registered so later updates to it are still checked.
				failures = append(failures, fmt.Errorf("operation %d: %w", number+1, err))
				updated = item.text
			}
			for _, warning := range warnings {
				item.warnings = append(item.warnings, filepath.ToSlash(clean)+": "+warning)
			}
			item.updated = updated
			item.data = encodeText(updated, item.format)
		case "delete":
			if readErr != nil {
				return nil, nil, fmt.Errorf("delete %q: %w", clean, readErr)
			}
		default:
			return nil, nil, fmt.Errorf("unsupported patch operation %q", operation.Type)
		}
		items = append(items, item)
	}
	if len(failures) > 0 {
		return nil, nil, errors.Join(failures...)
	}
	return root, items, nil
}

func applyUpdate(text string, operation PatchOperation, newline, path string) (string, []string, error) {
	hasHunks := len(operation.Hunks) > 0
	hasReplace := operation.OldString != "" || operation.NewString != ""
	if hasHunks && hasReplace {
		return "", nil, fmt.Errorf("update %q: use oldString/newString or hunks, not both", path)
	}
	if hasHunks {
		return applyPatchHunks(text, operation.Hunks, path, newline)
	}
	updated, warning, err := applySearchReplace(
		text, operation.OldString, operation.NewString, path, newline, operation.ReplaceAll,
	)
	if warning != "" {
		return updated, []string{warning}, err
	}
	return updated, nil, err
}

// patchReplacesExisting reports whether a patch operation mutates an existing file. Preparing such
// an operation seals the file content it was resolved against, so it cannot apply to a newer file.
func patchReplacesExisting(operationType string) bool {
	switch operationType {
	case "write", "update", "delete":
		return true
	default:
		return false
	}
}

// patchNeedsApproval reports whether a patch operation pauses for user approval. Creating and
// editing files inside a writable root is automatic; only a deletion asks first.
func patchNeedsApproval(operationType string) bool {
	return operationType == "delete"
}

func rollbackPatch(root *openedRoot, items []preparedPatch, committed int) {
	for index := committed - 1; index >= 0; index-- {
		item := items[index]
		if item.op.Type != "delete" {
			_ = root.root.Remove(item.path)
		}
		if item.backup != "" {
			_ = root.root.Rename(item.backup, item.path)
		}
	}
}

func cleanupPatchTemps(root *openedRoot, items []preparedPatch) {
	for _, item := range items {
		if item.temp != "" {
			_ = root.root.Remove(item.temp)
		}
	}
}

func (s *Sandbox) MovePath(rootID, from, to string) error {
	root, source, err := s.resolve(rootID, from)
	if err != nil {
		return err
	}
	_, target, err := s.resolve(rootID, to)
	if err != nil {
		return err
	}
	if err := s.checkMove(filepath.Join(root.view.Path, source), filepath.Join(root.view.Path, target)); err != nil {
		return err
	}
	if err := root.root.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return root.root.Rename(source, target)
}

func (s *Sandbox) resolve(rootID, relativePath string) (*openedRoot, string, error) {
	root := s.roots[rootID]
	if root == nil {
		return nil, "", fmt.Errorf("unknown sandbox root %q", rootID)
	}
	clean, err := validateRelativePath(relativePath)
	if err != nil {
		return nil, "", err
	}
	return root, clean, nil
}

// ResolveExisting resolves a sandbox path that must already exist. It is one half of the resolver the
// desktop action registry binds to, so a nil sandbox reports itself as unavailable instead of
// panicking.
func (s *Sandbox) ResolveExisting(rootID, relativePath string) (string, error) {
	if s == nil {
		return "", errors.New("agent sandbox is unavailable")
	}
	root, clean, err := s.resolve(rootID, relativePath)
	if err != nil {
		return "", err
	}
	if _, err := root.root.Stat(clean); err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(root.view.Path, filepath.FromSlash(clean)))
	if err != nil {
		return "", err
	}
	if !pathWithin(root.view.Path, resolved) {
		return "", errSandboxPath
	}
	return resolved, nil
}

// ResolveTarget resolves a sandbox path that may be missing, such as an action's output file. The
// existing parent chain decides containment, so a symlink pointing outside the root is rejected.
func (s *Sandbox) ResolveTarget(rootID, relativePath string) (string, error) {
	if s == nil {
		return "", errors.New("agent sandbox is unavailable")
	}
	root, clean, err := s.resolve(rootID, relativePath)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root.view.Path, filepath.FromSlash(clean))
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		if !pathWithin(root.view.Path, resolved) {
			return "", errSandboxPath
		}
		return resolved, nil
	} else if !errors.Is(resolveErr, os.ErrNotExist) {
		return "", resolveErr
	}
	parent := filepath.Dir(target)
	for {
		resolved, resolveErr := filepath.EvalSymlinks(parent)
		if resolveErr == nil {
			if !pathWithin(root.view.Path, resolved) {
				return "", errSandboxPath
			}
			break
		}
		if !errors.Is(resolveErr, os.ErrNotExist) {
			return "", resolveErr
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", resolveErr
		}
		parent = next
	}
	if !pathWithin(root.view.Path, target) {
		return "", errSandboxPath
	}
	return target, nil
}

// physicalPath resolves the junctions and links in the part of path that exists and rejoins the
// missing remainder, so a file that is yet to be created is judged by where it would really land.
func physicalPath(path string) (string, error) {
	missing := ""
	for current := path; ; {
		resolved, err := platform.FinalPath(current)
		if err == nil {
			return filepath.Join(resolved, missing), nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		// A link whose target is missing still redirects whatever is created through it.
		if _, statErr := os.Lstat(current); statErr == nil {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", err
		}
		missing = filepath.Join(filepath.Base(current), missing)
		current = parent
	}
}

// guardWrite refuses a path inside a read-only root and, with tree set, a folder that contains one,
// which is off limits to anything that moves or rewrites a whole tree. The check is by physical
// location, not by the root a call names or the spelling of the path, so neither a writable root
// that contains a read-only one nor a junction into it can be used to reach it.
func (s *Sandbox) guardWrite(path string, tree bool) error {
	physical, err := physicalPath(path)
	if err != nil {
		return err
	}
	for _, root := range s.roots {
		if !root.view.ReadOnly {
			continue
		}
		if pathWithin(root.physical, physical) || tree && pathWithin(physical, root.physical) {
			return fmt.Errorf("%w: %q", errSandboxReadOnly, path)
		}
	}
	return nil
}

func (s *Sandbox) checkMove(source, target string) error {
	if err := s.guardWrite(source, true); err != nil {
		return err
	}
	return s.guardWrite(target, false)
}

// Writable returns the resolver that mutating desktop actions bind to. It resolves like the sandbox
// itself and then refuses read-only locations.
func (s *Sandbox) Writable() agentactions.Resolver {
	return writableSandbox{sandbox: s}
}

type writableSandbox struct {
	sandbox *Sandbox
}

func (w writableSandbox) ResolveExisting(rootID, relativePath string) (string, error) {
	return w.writable(w.sandbox.ResolveExisting(rootID, relativePath))
}

func (w writableSandbox) ResolveTarget(rootID, relativePath string) (string, error) {
	return w.writable(w.sandbox.ResolveTarget(rootID, relativePath))
}

// CheckWritable vets a path an earlier action resolved and stored, such as the mod folder of a
// session a read action opened, before a later action changes it.
func (w writableSandbox) CheckWritable(path string) error {
	return w.sandbox.guardWrite(path, true)
}

func (w writableSandbox) writable(path string, err error) (string, error) {
	if err != nil {
		return "", err
	}
	if err := w.sandbox.guardWrite(path, true); err != nil {
		return "", err
	}
	return path, nil
}

func validateRelativePath(path string) (string, error) {
	if path == "" {
		path = "."
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" || strings.Contains(path, ":") ||
		strings.HasPrefix(path, `\\`) {
		return "", fmt.Errorf("%w: %q", errSandboxPath, path)
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", errSandboxPath, path)
	}
	return clean, nil
}

func canonicalExistingDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	canonical, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(canonical)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: %s", canonical)
	}
	return filepath.Clean(canonical), nil
}

func pathWithin(root, child string) bool {
	return platform.SameOrChildPath(root, child)
}

type textFormat struct {
	encoding string
	newline  string
	bom      bool
}

func decodeText(data []byte) (string, textFormat, bool) {
	format := textFormat{encoding: "utf-8", newline: "\n"}
	if bytes.Contains(data, []byte("\r\n")) {
		format.newline = "\r\n"
	} else if bytes.IndexByte(data, '\r') >= 0 {
		format.newline = "\r"
	}
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		format.bom = true
		return string(data[3:]), format, false
	}
	if bytes.HasPrefix(data, []byte{0xff, 0xfe}) {
		format.encoding = "utf-16le"
		format.bom = true
		units := make([]uint16, (len(data)-2)/2)
		for i := range units {
			units[i] = binary.LittleEndian.Uint16(data[2+i*2:])
		}
		text := string(utf16.Decode(units))
		if strings.Contains(text, "\r\n") {
			format.newline = "\r\n"
		} else if strings.Contains(text, "\r") {
			format.newline = "\r"
		}
		return text, format, false
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return "", format, true
	}
	return string(data), format, false
}

func encodeText(text string, format textFormat) []byte {
	if format.encoding == "utf-16le" {
		units := utf16.Encode([]rune(text))
		data := make([]byte, 2+len(units)*2)
		binary.LittleEndian.PutUint16(data, 0xfeff)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(data[2+i*2:], unit)
		}
		return data
	}
	data := []byte(text)
	if format.bom {
		data = append([]byte{0xef, 0xbb, 0xbf}, data...)
	}
	return data
}

func normalizeNewlines(text, newline string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return strings.ReplaceAll(normalized, "\n", newline)
}
