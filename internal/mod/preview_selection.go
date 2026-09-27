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
