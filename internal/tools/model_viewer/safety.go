package modelviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	maxModelViewerDraws            = 10_000
	maxModelViewerBufferFileBytes  = int64(512 * 1024 * 1024)
	maxModelViewerTotalBufferBytes = int64(2 * 1024 * 1024 * 1024)
)

type modelViewerLoadBudget struct {
	root        string
	logicalRoot string
	seen        map[string]bool
	resolved    map[string]string
	totalBytes  int64
}

func newModelViewerLoadBudget(root string) (*modelViewerLoadBudget, error) {
	resolved, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &modelViewerLoadBudget{
		root:     filepath.Clean(resolved),
		seen:     make(map[string]bool),
		resolved: make(map[string]string),
	}, nil
}

func (b *modelViewerLoadBudget) validateResources(baseDir string, resources []modelViewerResource) error {
	return b.validateReferencedResources(baseDir, resources, nil)
}

func (b *modelViewerLoadBudget) validateReferencedResources(
	baseDir string,
	resources []modelViewerResource,
	referenced map[string]bool,
) error {
	for _, resource := range resources {
		if referenced != nil && !referenced[modelViewerNormalizeKey(resource.Name)] {
			continue
		}
		if strings.TrimSpace(resource.Filename) == "" {
			continue
		}
		resolveKey := baseDir + "\x00" + resource.Filename
		path, ok := b.resolved[resolveKey]
		if !ok {
			var resolveErr error
			path, resolveErr = resolveModelViewerResourcePathWith(
				b.root, baseDir, resource.Filename, b.logicalRoot, platform.FinalPath,
			)
			if resolveErr != nil {
				return fmt.Errorf("resource %s: %w", resource.Name, resolveErr)
			}
			b.resolved[resolveKey] = path
		}
		if !isModelViewerBufferResource(resource) || b.seen[strings.ToLower(path)] {
			continue
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return statErr
		}
		if !info.Mode().IsRegular() {
			return infra.ContractError(
				fmt.Sprintf("Model Viewer resource is not a regular file: %s", resource.Filename),
			)
		}
		if info.Size() > maxModelViewerBufferFileBytes {
			return infra.ContractError(
				fmt.Sprintf("Buffer file is too large (%.1f MiB).", float64(info.Size())/1048576),
			)
		}
		if b.totalBytes+info.Size() > maxModelViewerTotalBufferBytes {
			return infra.ContractError("Mod buffer data exceeds the 2 GiB safety limit.")
		}
		b.seen[strings.ToLower(path)] = true
		b.totalBytes += info.Size()
	}
	return nil
}

func collectModelViewerReferencedResources(sections []modINISection) map[string]bool {
	resources := collectModelViewerResources(sections)
	referenced := make(map[string]bool)
	names := make([]string, len(resources))
	needles := make([]string, len(resources))
	for index, resource := range resources {
		names[index] = modelViewerNormalizeKey(resource.Name)
		needles[index] = "resource" + names[index]
	}
	for _, section := range sections {
		if strings.EqualFold(section.Header, "Resource") {
			continue
		}
		for _, line := range section.Lines {
			normalizedLine := modelViewerNormalizeKey(line)
			if !strings.Contains(normalizedLine, "resource") {
				continue
			}
			for index, needle := range needles {
				if names[index] != "" && strings.Contains(normalizedLine, needle) {
					referenced[names[index]] = true
				}
			}
		}
	}
	return referenced
}

// sanitizeModelViewerResourcePaths mirrors Electron's resource resolver: an
// unsafe filename removes that resource from consideration instead of making
// the entire model fail to load. Size/type budget failures remain fatal.
func sanitizeModelViewerResourcePaths(
	sections []modINISection,
	root, baseDir string,
	cache *modelViewerBufferCache,
) []string {
	var skipped []string
	for index := range sections {
		section := &sections[index]
		if !strings.EqualFold(section.Header, "Resource") {
			continue
		}
		filename := modelViewerSectionValue(*section, "filename")
		if strings.TrimSpace(filename) == "" {
			continue
		}
		if _, err := cache.resourcePath(root, baseDir, filename); err != nil {
			setModelViewerSectionValue(section, "filename", "")
			skipped = append(skipped, section.Name)
		}
	}
	return skipped
}

func sanitizeModelViewerLogValue(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

func resolveModelViewerResourcePath(root, baseDir, relative string) (string, error) {
	return resolveModelViewerResourcePathWith(root, baseDir, relative, "", platform.FinalPath)
}

// resolveModelViewerResourcePathWith allows a filename to reach the parent of the mod folder, where sibling folders
// hold shared textures. Links are followed with finalPath: a filename written inside root must stay inside root's
// final location, and one in a sibling folder inside that sibling's. A sibling may itself be a link, as mod folders
// linked into the mods folder are, but a link below it must not lead out of it. A target that cannot be opened keeps
// its lexical path and fails when it is read.
// With a logicalRoot, sibling paths are relative to the requested mod folder and paths inside it use root.
func resolveModelViewerResourcePathWith(
	root, baseDir, relative, logicalRoot string,
	finalPath func(string) (string, error),
) (string, error) {
	relative = filepath.FromSlash(strings.ReplaceAll(strings.TrimSpace(relative), `\`, string(filepath.Separator)))
	if relative == "" || filepath.IsAbs(relative) {
		return "", infra.ContractError(fmt.Sprintf("Model Viewer resource path must be relative: %s", relative))
	}
	physicalRoot := filepath.Clean(root)
	if logicalRoot != "" {
		subdir, err := filepath.Rel(physicalRoot, baseDir)
		if err != nil {
			return "", err
		}
		root, baseDir = logicalRoot, filepath.Join(logicalRoot, subdir)
	}
	target, err := filepath.Abs(filepath.Join(baseDir, relative))
	if err != nil {
		return "", err
	}
	root = filepath.Clean(root)
	ceiling := filepath.Dir(root)
	if !platform.SameOrChildPath(ceiling, target) {
		return "", infra.ContractError(fmt.Sprintf("Model Viewer resource escapes the mod folder: %s", relative))
	}

	bound := physicalRoot
	if platform.SameOrChildPath(root, target) {
		if logicalRoot != "" {
			inside, err := filepath.Rel(root, target)
			if err != nil {
				return "", err
			}
			target = filepath.Join(physicalRoot, inside)
		}
	} else {
		bound = ceiling
		if relative, err := filepath.Rel(ceiling, target); err == nil {
			if sibling, _, nested := strings.Cut(relative, string(filepath.Separator)); nested {
				bound = filepath.Join(ceiling, sibling)
			}
		}
	}

	if finalTarget, targetErr := finalPath(target); targetErr == nil {
		finalBound, err := finalPath(bound)
		if err != nil {
			return "", err
		}
		if !platform.SameOrChildPath(finalBound, finalTarget) {
			return "", infra.ContractError(
				fmt.Sprintf("Model Viewer resource link escapes the mod folder: %s", relative),
			)
		}
	}
	return target, nil
}

// resolveModelViewerModBufferPath resolves a mod INI buffer filename and keeps
// it inside modDir, so callers can read or key the file without re-checking.
func resolveModelViewerModBufferPath(modDir, filename string) (string, bool) {
	path, err := resolveModelViewerResourcePath(modDir, modDir, filename)
	if err != nil || !platform.SameOrChildPath(modDir, path) {
		return "", false
	}
	return path, true
}

func isModelViewerBufferResource(resource modelViewerResource) bool {
	extension := strings.ToLower(filepath.Ext(resource.Filename))
	return resource.Stride > 0 || extension == ".buf" || extension == ".vb" || extension == ".ib" ||
		strings.Contains(strings.ToUpper(resource.Format), "UINT")
}
