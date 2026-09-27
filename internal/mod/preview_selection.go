package mod

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/mod/metadata"
)

func applyDefaultPreview(info *ModInfo, modPath string, reports ...func(error)) {
	if len(info.PreviewImages) == 0 {
		return
	}
	raw, err := metadata.Read(modPath)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		reportScanFailure(fmt.Errorf("read preview selection for %s: %w", modPath, err), reports)
		return
	}
	var document struct {
		Preview string `json:"preview"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		reportScanFailure(fmt.Errorf("decode preview selection for %s: %w", modPath, err), reports)
		return
	}
	for _, image := range info.PreviewImages {
		relative, err := filepath.Rel(modPath, image)
		if err == nil && strings.EqualFold(filepath.ToSlash(relative), document.Preview) {
			info.Preview = &image
			return
		}
	}
}

// SetDefaultPreview persists a candidate image as the mod's preferred preview.
func (m *Mod) SetDefaultPreview(ctx context.Context, modPath, imagePath string) (err error) {
	stage := "validate"
	requestedModPath := modPath
	resolvedImagePath := ""
	defer func() {
		if err != nil {
			err = infra.ReportError(m.log, err, "Mod", infra.Diagnostic{
				Operation: "set-default-preview", Stage: stage,
				Fields: map[string]any{
					"modPath": requestedModPath, "resolvedModPath": modPath,
					"imagePath": imagePath, "resolvedImagePath": resolvedImagePath,
				},
			})
		}
	}()
	if _, err = m.ownedPath(ctx, modPath); err != nil {
		return err
	}
	if modPath, err = validDirectory(modPath); err != nil {
		return err
	}
	var info os.FileInfo
	if info, err = os.Lstat(imagePath); err != nil {
		return err
	}
	if !info.Mode().IsRegular() || !previewImagePath(imagePath) {
		return errors.New("INVALID_PREVIEW_IMAGE")
	}
	var absolute string
	if absolute, err = filepath.Abs(imagePath); err != nil {
		return err
	}
	resolvedImagePath = absolute
	if !pathWithin(modPath, absolute) {
		return errors.New("INVALID_PREVIEW_IMAGE")
	}

	stage = "scan"
	_, candidates := scanPreviewWalk(modPath, -1, mediaExtensions, true)
	valid := false
	for _, candidate := range candidates {
		if samePath(candidate.path, absolute) {
			valid = true
			break
		}
	}
	if !valid {
		return errors.New("INVALID_PREVIEW_IMAGE")
	}
	stage = "metadata"
	return updateSelectedPreview(modPath, absolute)
}

func refreshSelectedPreviewAfterPaste(modPath, imagePath string) error {
	raw, err := metadata.Read(modPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var document struct {
		Preview string `json:"preview"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("decode mod metadata: %w", err)
	}
	if document.Preview == "" || !previewImagePath(imagePath) {
		return nil
	}
	return updateSelectedPreview(modPath, imagePath)
}

func updateSelectedPreview(modPath, imagePath string) error {
	relative, err := filepath.Rel(modPath, imagePath)
	if err != nil || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return errors.New("INVALID_PREVIEW_IMAGE")
	}
	return metadata.Upsert(modPath, func(raw []byte) ([]byte, error) {
		document := make(map[string]json.RawMessage)
		if raw != nil {
			if err := json.Unmarshal(raw, &document); err != nil {
				return nil, fmt.Errorf("decode mod metadata: %w", err)
			}
			if document == nil {
				return nil, errors.New("invalid mod metadata object")
			}
		}
		encoded, err := json.Marshal(filepath.ToSlash(relative))
		if err != nil {
			return nil, err
		}
		document["preview"] = encoded
		return json.Marshal(document)
	})
}

func (m *Mod) previewBoundary(ctx context.Context, path string) string {
	if m == nil {
		return ""
	}
	game, err := m.ownedPath(ctx, path)
	if err != nil || game == nil {
		return ""
	}
	cleaned := filepath.Clean(path)
	best := ""
	roots := []string{game.ModFolderPath}
	if game.LinkedModFolderPath != nil {
		roots = append(roots, *game.LinkedModFolderPath)
	}
	for _, root := range roots {
		root = filepath.Clean(root)
		if pathWithin(root, cleaned) && len(root) > len(best) {
			best = root
		}
	}
	return best
}

// relocateSelectedPreviews rewrites ancestor preview paths after renameUnique.
// The saved path must use the returned folder name, including a collision suffix.
// Segment matching does not strip disabled prefixes, so an enabled duplicate that
// would normalize to the same path is left unchanged.
func relocateSelectedPreviews(oldPath, newPath, boundary string) error {
	oldPath = filepath.Clean(oldPath)
	newPath = filepath.Clean(newPath)
	dir := filepath.Dir(oldPath)
	bounded := boundary != "" && pathWithin(boundary, dir)
	entries := make([]metadata.WriteEntry, 0)
	for steps := 0; ; steps++ {
		if bounded {
			if !pathWithin(boundary, dir) {
				break
			}
		} else if steps >= previewSearchDepth {
			break
		}
		updated, ok, err := rewrittenPreviewFile(dir, oldPath, newPath)
		if err != nil {
			return err
		}
		if ok {
			entries = append(entries, metadata.WriteEntry{Dir: dir, Data: updated})
		}
		parent := filepath.Dir(dir)
		if samePath(parent, dir) {
			break
		}
		dir = parent
	}
	if len(entries) == 0 {
		return nil
	}
	if err := metadata.WriteBatch(entries); err != nil {
		return fmt.Errorf("update preview after rename: %w", err)
	}
	return nil
}

func rewrittenPreviewFile(dir, oldPath, newPath string) ([]byte, bool, error) {
	raw, err := metadata.Read(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read preview metadata: %w", err)
	}
	updated, ok, err := rewrittenPreview(raw, dir, oldPath, newPath)
	if err != nil {
		return nil, false, fmt.Errorf("rewrite preview metadata: %w", err)
	}
	return updated, ok, nil
}

func rewrittenPreview(raw []byte, ancestor, oldPath, newPath string) ([]byte, bool, error) {
	document, stored, ok := storedPreview(raw)
	if !ok {
		return nil, false, nil
	}
	next, ok := relocatedPreviewPath(stored, ancestor, oldPath, newPath)
	if !ok {
		return nil, false, nil
	}
	replacement, err := json.Marshal(next)
	if err != nil {
		return nil, false, err
	}
	document["preview"] = replacement
	updated, err := json.Marshal(document)
	if err != nil {
		return nil, false, err
	}
	return updated, true, nil
}

func storedPreview(raw []byte) (map[string]json.RawMessage, string, bool) {
	var document map[string]json.RawMessage
	if json.Unmarshal(raw, &document) != nil || document == nil {
		return nil, "", false
	}
	encoded, ok := document["preview"]
	if !ok {
		return nil, "", false
	}
	var stored string
	if json.Unmarshal(encoded, &stored) != nil || stored == "" {
		return nil, "", false
	}
	return document, stored, true
}

func relocatedPreviewPath(stored, ancestor, oldPath, newPath string) (string, bool) {
	oldPrefix, ok := localRelative(ancestor, oldPath)
	if !ok {
		return "", false
	}
	newPrefix, ok := localRelative(ancestor, newPath)
	if !ok {
		return "", false
	}
	storedParts := splitRelative(stored)
	oldParts := splitRelative(oldPrefix)
	if len(oldParts) == 0 || len(storedParts) < len(oldParts) {
		return "", false
	}
	for i, part := range oldParts {
		if !strings.EqualFold(storedParts[i], part) {
			return "", false
		}
	}
	next := append(splitRelative(newPrefix), storedParts[len(oldParts):]...)
	replaced := strings.Join(next, "/")
	if replaced == filepath.ToSlash(stored) {
		return "", false
	}
	return replaced, true
}

func localRelative(ancestor, target string) (string, bool) {
	relative, err := filepath.Rel(ancestor, target)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(relative), true
}

func splitRelative(path string) []string {
	return strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' })
}
