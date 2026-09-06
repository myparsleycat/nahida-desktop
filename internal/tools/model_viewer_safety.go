package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const (
	maxModelViewerDraws            = 10_000
	maxModelViewerBufferFileBytes  = int64(512 * 1024 * 1024)
	maxModelViewerTotalBufferBytes = int64(2 * 1024 * 1024 * 1024)
)

type modelViewerLoadBudget struct {
	root       string
	seen       map[string]bool
	resolved   map[string]string
	totalBytes int64
}

func newModelViewerLoadBudget(root string) (*modelViewerLoadBudget, error) {
	resolved, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &modelViewerLoadBudget{root: filepath.Clean(resolved), seen: make(map[string]bool), resolved: make(map[string]string)}, nil
}

func (b *modelViewerLoadBudget) validateResources(baseDir string, resources []modelViewerResource) error {
	return b.validateReferencedResources(baseDir, resources, nil)
}

func (b *modelViewerLoadBudget) validateReferencedResources(baseDir string, resources []modelViewerResource, referenced map[string]bool) error {
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
			path, resolveErr = resolveModelViewerResourcePath(b.root, baseDir, resource.Filename)
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
			return contractError(fmt.Sprintf("Model Viewer resource is not a regular file: %s", resource.Filename))
		}
		if info.Size() > maxModelViewerBufferFileBytes {
			return contractError(fmt.Sprintf("Buffer file is too large (%.1f MiB).", float64(info.Size())/1048576))
		}
		if b.totalBytes+info.Size() > maxModelViewerTotalBufferBytes {
			return contractError("Mod buffer data exceeds the 2 GiB safety limit.")
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
func sanitizeModelViewerResourcePaths(sections []modINISection, root, baseDir string) []string {
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
		if _, err := resolveModelViewerResourcePath(root, baseDir, filename); err != nil {
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
	relative = filepath.FromSlash(strings.ReplaceAll(strings.TrimSpace(relative), `\`, string(filepath.Separator)))
	if relative == "" || filepath.IsAbs(relative) {
		return "", contractError(fmt.Sprintf("Model Viewer resource path must be relative: %s", relative))
	}
	target, err := filepath.Abs(filepath.Join(baseDir, relative))
	if err != nil {
		return "", err
	}
	ceiling := filepath.Dir(filepath.Clean(root))
	if !modelViewerPathWithin(ceiling, target) {
		return "", contractError(fmt.Sprintf("Model Viewer resource escapes the mod folder: %s", relative))
	}
	if resolved, evalErr := filepath.EvalSymlinks(target); evalErr == nil {
		resolvedCeiling := ceiling
		if realCeiling, ceilingErr := filepath.EvalSymlinks(ceiling); ceilingErr == nil {
			resolvedCeiling = realCeiling
		}
		if !modelViewerPathWithin(resolvedCeiling, resolved) {
			return "", contractError(fmt.Sprintf("Model Viewer resource symlink escapes the mod folder: %s", relative))
		}
	}
	return filepath.Clean(target), nil
}

func modelViewerPathWithin(root, target string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(target))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false
	}
	return true
}

func isModelViewerBufferResource(resource modelViewerResource) bool {
	extension := strings.ToLower(filepath.Ext(resource.Filename))
	return resource.Stride > 0 || extension == ".buf" || extension == ".vb" || extension == ".ib" || strings.Contains(strings.ToUpper(resource.Format), "UINT")
}
