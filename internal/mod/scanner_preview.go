package mod

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	previewRootDepth   = 1
	previewSearchDepth = 3
)

type previewCandidate struct {
	score    int
	path     string
	location int
}

type previewBuckets struct {
	root     *previewCandidate
	enabled  *previewCandidate
	disabled *previewCandidate
}

func (b *previewBuckets) consider(root, path, fileName string, extensions map[string]bool) {
	activeName := stripDisabledFileSuffix(fileName)
	ext := strings.ToLower(filepath.Ext(activeName))
	if !extensions[ext] {
		return
	}
	lower := strings.ToLower(activeName)
	for _, fragment := range excludedPreviewFragments {
		if strings.Contains(lower, fragment) {
			return
		}
	}
	rel, relErr := filepath.Rel(root, path)
	if relErr != nil {
		return
	}
	segments := strings.FieldsFunc(filepath.ToSlash(rel), func(r rune) bool { return r == '/' })
	if len(segments) == 0 {
		return
	}
	location := 1
	if len(segments) == 1 {
		location = 0
	} else {
		for _, segment := range segments[:len(segments)-1] {
			if isDisabledFolderName(segment) {
				location = 2
				break
			}
		}
	}
	score := 0
	if strings.HasPrefix(lower, "preview") {
		score += 1000
	} else if strings.Contains(lower, "preview") {
		score += 500
	}
	if len(segments) == 1 {
		score += 200
	}
	if ext == ".mp4" || ext == ".webm" {
		score += 10
	}
	candidate := previewCandidate{score: score, path: path, location: location}
	slot := &b.enabled
	switch location {
	case 0:
		slot = &b.root
	case 2:
		slot = &b.disabled
	}
	if betterPreviewCandidate(*slot, candidate) {
		chosen := candidate
		*slot = &chosen
	}
}

func (b *previewBuckets) best() *previewCandidate {
	switch {
	case b.root != nil:
		return b.root
	case b.enabled != nil:
		return b.enabled
	default:
		return b.disabled
	}
}

func (b *previewBuckets) bestPath() *string {
	if best := b.best(); best != nil {
		return stringPointer(best.path)
	}
	return nil
}

func betterPreviewCandidate(current *previewCandidate, next previewCandidate) bool {
	if current == nil {
		return true
	}
	if next.score != current.score {
		return next.score > current.score
	}
	return naturalLess(next.path, current.path)
}

func walkDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

func findPreviewWalk(root string, maxDepth int, reports ...func(error)) *previewCandidate {
	return findPreviewWalkWithExtensions(root, maxDepth, mediaExtensions, reports...)
}

func findScannerPreviewWalk(root string, maxDepth int, reports ...func(error)) *previewCandidate {
	return findPreviewWalkWithExtensions(root, maxDepth, scannerMediaExtensions, reports...)
}

func findPreviewWalkWithExtensions(root string, maxDepth int, extensions map[string]bool, reports ...func(error)) *previewCandidate {
	var buckets previewBuckets
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			reportScanFailure(err, reports)
			return fs.SkipDir
		}
		if entry.IsDir() {
			if maxDepth >= 0 && path != root && walkDepth(root, path) >= maxDepth {
				return fs.SkipDir
			}
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		buckets.consider(root, path, entry.Name(), extensions)
		return nil
	})
	return buckets.best()
}

func listChildFolders(root string, reports ...func(error)) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		reportScanFailure(err, reports)
		return nil
	}
	folders := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			folders = append(folders, filepath.Join(root, entry.Name()))
		}
	}
	sort.Slice(folders, func(i, j int) bool {
		return naturalLess(filepath.Base(folders[i]), filepath.Base(folders[j]))
	})
	return folders
}

func findChildFolderPreview(
	folders []string,
	searchDepth int,
	disabled bool,
	extensions map[string]bool,
	reports ...func(error),
) *previewCandidate {
	var best *previewCandidate
	for _, folder := range folders {
		if isDisabledFolderName(filepath.Base(folder)) != disabled {
			continue
		}
		candidate := findPreviewWalkWithExtensions(folder, searchDepth, extensions, reports...)
		if candidate == nil {
			continue
		}
		if betterPreviewCandidate(best, *candidate) {
			chosen := *candidate
			best = &chosen
		}
	}
	return best
}

// findGroupPreview matches Electron rust find_group_preview: root files at
// depth 1, then child folders at searchDepth (1 without fallback, 3 with it).
func findGroupPreview(root string, searchDepth int, reports ...func(error)) *string {
	return findGroupPreviewWithExtensions(root, searchDepth, mediaExtensions, reports...)
}

func findScannerGroupPreview(root string, searchDepth int, reports ...func(error)) *string {
	return findGroupPreviewWithExtensions(root, searchDepth, scannerMediaExtensions, reports...)
}

func findGroupPreviewWithExtensions(root string, searchDepth int, extensions map[string]bool, reports ...func(error)) *string {
	if preview := findPreviewWalkWithExtensions(root, previewRootDepth, extensions, reports...); preview != nil {
		return stringPointer(preview.path)
	}
	if searchDepth <= previewRootDepth {
		return nil
	}
	folders := listChildFolders(root, reports...)
	if candidate := findChildFolderPreview(folders, searchDepth, false, extensions, reports...); candidate != nil {
		return stringPointer(candidate.path)
	}
	if candidate := findChildFolderPreview(folders, searchDepth, true, extensions, reports...); candidate != nil {
		return stringPointer(candidate.path)
	}
	return nil
}

// findPreview matches Electron preview.ts Boolean searchSubfolders, with rust
// WalkDir caps: false stays in the root (depth 1); true uses depth 3 per child.
func findPreview(root string, searchSubfolders bool, reports ...func(error)) *string {
	depth := previewRootDepth
	if searchSubfolders {
		depth = previewSearchDepth
	}
	return findGroupPreview(root, depth, reports...)
}
