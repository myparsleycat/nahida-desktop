package modelviewer

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/platform"
)

const (
	maxWwmiRecoveryEntries     = 4096
	maxWwmiRecoveryDirectories = 16
	maxWwmiRecoveryFiles       = 64
	maxWwmiRecoveryBytes       = 64 * 1024 * 1024
)

var wwmiRecoveryFileRE = regexp.MustCompile(`(?i)^Components-(\d+(?:-\d+)*)\s+t=[0-9a-f]{8}\.dds$`)

func recoverWwmiDumpTextures(
	ctx context.Context,
	modDir string,
	meshes []modelViewerDirectMesh,
	resources []modelViewerResource,
	declaredPaths []string,
) error {
	inspect := newWwmiRecoveryInspector(ctx, modDir)
	directories := make(map[string][]string)
	for meshIndex := range meshes {
		if err := ctx.Err(); err != nil {
			return err
		}
		mesh := &meshes[meshIndex]
		index := wwmiComponentIndex(mesh.component)
		if index == "" || mesh.textureAuthored || meshHasDiffuseTexture(mesh) && !mesh.textureDumpInferred ||
			hasBoundWwmiDiffuse(mesh) {
			continue
		}
		current := inspect(mesh.textureDefaultFile)

		// A dominant primary channel is only a recovery trigger. It never invalidates
		// an existing texture without a decoded, non-mask replacement in the same directory.
		if mesh.textureDefaultFile != "" && (current == nil || current.PrimaryColorFraction < 0.5) {
			continue
		}
		var candidates []wwmiDumpCandidate
		seenDirs := make(map[string]bool)
		for _, resource := range resources {
			match := wwmiDumpTexRE.FindStringSubmatch(slashPath(resource.Filename))
			if match == nil || !slices.Contains(strings.Split(match[1], "-"), index) {
				continue
			}
			dir := filepath.Dir(filepath.FromSlash(resource.Filename))
			if mesh.textureDefaultFile != "" && !platform.SamePathFold(
				filepath.Join(
					modDir,
					dir,
				),
				filepath.Dir(filepath.Join(modDir, filepath.FromSlash(mesh.textureDefaultFile))),
			) {
				continue
			}
			key := strings.ToLower(dir)
			if seenDirs[key] {
				continue
			}
			seenDirs[key] = true
			files, ok := directories[key]
			if !ok {
				if len(directories) >= maxWwmiRecoveryDirectories {
					continue
				}
				files = readWwmiRecoveryDirectory(ctx, modDir, dir)
				directories[key] = files
			}
			for _, file := range files {
				match := wwmiRecoveryFileRE.FindStringSubmatch(filepath.Base(file))
				if !slices.Contains(strings.Split(match[1], "-"), index) ||
					slices.ContainsFunc(declaredPaths, func(path string) bool {
						return platform.SamePathFold(path, filepath.Join(modDir, filepath.FromSlash(file)))
					}) {
					continue
				}
				hint := inspect(file)
				if hint == nil || !hint.Analyzed || !isLikelyWwmiDiffuse(*hint) || hint.PrimaryColorFraction >= 0.05 {
					continue
				}
				if current != nil &&
					(hint.Area < current.Area || wwmiDumpShareCount(file) > wwmiDumpShareCount(mesh.textureDefaultFile)) {
					continue
				}
				candidates = append(candidates, wwmiDumpCandidate{
					File: file, SRGB: hint.SRGB, Area: hint.Area, Bytes: hint.Bytes, Order: len(candidates),
					Analyzed: hint.Analyzed, ColorBlocks: hint.ColorBlockFraction,
				})
			}
		}
		if picked := pickWwmiDumpDiffuse(candidates); picked != "" {
			setInferredWwmiDiffuse(mesh, picked)
		}
	}
	return ctx.Err()
}

// attachSiblingWwmiDumpDiffuse binds the dedicated dump texture of a component
// that a texture-only INI beside the mod replaces by hash. 3DMigoto applies such
// a replacement to the mod's draws too, but the INI forms a unit of its own, so
// the dump binders never saw its files. Only a dedicated dump name ties a file
// to the component; a shared one is as likely a helper map of another part.
func attachSiblingWwmiDumpDiffuse(
	ctx context.Context,
	modDir string,
	meshes []modelViewerDirectMesh,
	siblings []modelViewerResource,
) error {
	if len(siblings) == 0 {
		return nil
	}
	inspect := newWwmiRecoveryInspector(ctx, modDir)
	for meshIndex := range meshes {
		if err := ctx.Err(); err != nil {
			return err
		}
		mesh := &meshes[meshIndex]
		index := wwmiComponentIndex(mesh.component)
		if index == "" || mesh.textureAuthored || hasBoundWwmiDiffuse(mesh) || meshHasDiffuseTexture(mesh) &&
			(!mesh.textureDumpInferred || wwmiDumpShareCount(mesh.textureDefaultFile) <= 1) {
			continue
		}

		var candidates []wwmiDumpCandidate
		for _, resource := range siblings {
			match := wwmiDumpTexRE.FindStringSubmatch(slashPath(resource.Filename))
			if match == nil || match[1] != index || classifyModelViewerTextureRole(resource.Name) != "diffuse" {
				continue
			}
			hint := inspect(resource.Filename)
			if hint == nil || !isLikelyWwmiDiffuse(*hint) {
				continue
			}
			candidates = append(candidates, wwmiDumpCandidate{
				File: resource.Filename, SRGB: hint.SRGB, Area: hint.Area, Bytes: hint.Bytes, Order: len(candidates),
				Analyzed: hint.Analyzed, ColorBlocks: hint.ColorBlockFraction,
			})
		}
		if picked := pickWwmiDumpDiffuse(candidates); picked != "" {
			setInferredWwmiDiffuse(mesh, picked)
		}
	}
	return ctx.Err()
}

func hasBoundWwmiDiffuse(mesh *modelViewerDirectMesh) bool {
	return slices.ContainsFunc(mesh.textureAssignments, func(assignment modelViewerDirectTextureAssignment) bool {
		return assignment.role == "diffuse" && (assignment.authored || !modelViewerDNFIsTrue(assignment.conditions))
	})
}

// setInferredWwmiDiffuse leaves the assignment's resource name empty: the file
// may come from another INI, whose resource names can collide with this one's.
func setInferredWwmiDiffuse(mesh *modelViewerDirectMesh, file string) {
	mesh.textureAssignments = slices.DeleteFunc(
		mesh.textureAssignments,
		func(assignment modelViewerDirectTextureAssignment) bool {
			return assignment.role == "diffuse"
		},
	)
	mesh.textureAssignments = append(mesh.textureAssignments, modelViewerDirectTextureAssignment{
		role: "diffuse", file: file, conditions: modelViewerDNFTrue(),
	})
	mesh.textureDefaultFile = file
	mesh.textureDumpInferred = true
}

func newWwmiRecoveryInspector(ctx context.Context, modDir string) func(string) *wwmiTextureHint {
	hints := make(map[string]*wwmiTextureHint)
	remainingBytes := int64(maxWwmiRecoveryBytes)
	return func(relative string) *wwmiTextureHint {
		key := strings.ToLower(slashPath(relative))
		if hint, ok := hints[key]; ok {
			return hint
		}
		if len(hints) >= maxWwmiRecoveryFiles || ctx.Err() != nil {
			return nil
		}
		hints[key] = nil
		path, err := resolveModelViewerResourcePath(modDir, modDir, relative)
		if err != nil || !platform.SameOrChildPath(modDir, path) {
			return nil
		}
		release, err := diskio.Acquire(ctx, path)
		if err != nil {
			return nil
		}
		defer release()
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Size() > remainingBytes {
			return nil
		}
		remainingBytes -= info.Size()
		hint := inspectWwmiTextureHint(path)
		hints[key] = hint
		return hint
	}
}

func readWwmiRecoveryDirectory(ctx context.Context, modDir, relative string) []string {
	path, err := resolveModelViewerResourcePath(modDir, modDir, relative)
	if err != nil || !platform.SameOrChildPath(modDir, path) {
		return nil
	}
	release, err := diskio.AcquireDir(ctx, path)
	if err != nil {
		return nil
	}
	defer release()
	dir, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = dir.Close() }()
	entries, err := dir.ReadDir(maxWwmiRecoveryEntries + 1)
	if err != nil && !errors.Is(err, io.EOF) || len(entries) > maxWwmiRecoveryEntries {
		return nil
	}
	var files []string
	for _, entry := range entries {
		if entry.Type().IsRegular() && wwmiRecoveryFileRE.MatchString(entry.Name()) {
			files = append(files, filepath.ToSlash(filepath.Join(relative, entry.Name())))
		}
	}
	sort.Strings(files)
	return files
}
