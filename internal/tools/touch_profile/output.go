package touchprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
)

func (t *Service) generateTouchOutput(
	ctx context.Context,
	session *touchSession,
	sourceRoot, targetRoot, operation string,
) (TouchValidationResult, error) {
	draft := session.Draft
	if draft == nil {
		return TouchValidationResult{}, infra.ContractError("Touch profile has no draft for output generation")
	}
	namespace := sanitizeTouchNamespace(sourceRoot)
	varPrefix := "nhd_touch_" + strings.ToLower(namespace)
	analysis, err := rebaseTouchAnalysis(draft.Analysis, sourceRoot, targetRoot)
	if err != nil {
		return TouchValidationResult{}, err
	}
	interactive := []TouchComponentAnalysis{}
	for _, component := range analysis.Components {
		componentDraft := findTouchDraft(draft.Components, component.ID)
		if componentDraft != nil && componentDraft.Interactive && len(componentDraft.Zones) > 0 {
			interactive = append(interactive, component)
		}
	}
	if len(interactive) == 0 {
		return TouchValidationResult{}, infra.ContractError("No interactive components selected for touch conversion")
	}
	message := "Copying mod to touch output folder"
	if operation == "regenerate" {
		message = "Preparing regenerated touch output"
	}
	t.emitTouchProgress(draft.SessionID, "assets", .2, message, "")
	if err = copyTouchTree(ctx, sourceRoot, targetRoot); err != nil {
		return TouchValidationResult{}, err
	}
	assets := []TouchGeneratedAssets{}
	for index, component := range interactive {
		componentDraft := findTouchDraft(draft.Components, component.ID)
		t.emitTouchProgress(
			draft.SessionID,
			"assets",
			.3+float64(index)/float64(len(interactive))*.3,
			"Generating touch assets for "+component.Name,
			component.ID,
		)
		mesh, ok := session.Mesh[component.ID]
		if !ok {
			if operation == "regenerate" {
				return TouchValidationResult{}, infra.ContractError(
					fmt.Sprintf(
						"Touch mesh cache is missing for %s; analyze the mod again before regenerating",
						component.ID,
					),
				)
			}
			mesh, err = loadTouchMeshBuffers(component)
			if err != nil {
				return TouchValidationResult{}, err
			}
		}
		asset, assetErr := writeTouchComponentAssets(
			targetRoot,
			component,
			*componentDraft,
			mesh.Positions,
			mesh.Indices,
			touchAssetPrefix(component, namespace),
		)
		if assetErr != nil {
			return TouchValidationResult{}, assetErr
		}
		assets = append(assets, asset)
	}
	if err = copyTouchRuntimeShaders(targetRoot); err != nil {
		return TouchValidationResult{}, err
	}
	t.emitTouchProgress(draft.SessionID, "ini", .75, "Patching touch INI", "")
	analysisRoot := filepath.Join(sourceRoot, draft.Analysis.ModRootRelativeToSource)
	sourceINI, err := resolveTouchRelative(analysisRoot, draft.Analysis.INIRelativePath)
	if err != nil {
		return TouchValidationResult{}, err
	}
	targetINI, err := remapTouchPath(sourceINI, sourceRoot, targetRoot)
	if err != nil {
		return TouchValidationResult{}, err
	}
	analysis.Components = interactive
	if _, _, err = compileTouchINI(
		sourceINI,
		targetINI,
		analysis,
		draft.Components,
		assets,
		namespace,
		varPrefix,
		t.touchUseFrameGuard(ctx),
	); err != nil {
		return TouchValidationResult{}, err
	}
	t.emitTouchProgress(draft.SessionID, "validate", .9, "Validating generated touch mod", "")
	validation, err := validateTouchOutput(targetRoot, targetINI, interactive, draft.Components, assets)
	if err != nil {
		return validation, err
	}
	if !validation.OK {
		messages := []string{}
		for _, issue := range validation.Issues {
			if issue.Level == "error" {
				messages = append(messages, issue.Message)
			}
		}
		return validation, infra.ContractError("Touch validation failed: " + strings.Join(messages, "; "))
	}
	if err = writeTouchManifest(targetRoot, draft.RuntimeVersion); err != nil {
		return validation, err
	}
	return validation, nil
}

var touchDisabledPrefixRE = regexp.MustCompile(`(?i)^(?:disabled[\s_]*)+[\s_]+`)

func touchFolderBaseName(name string) string {
	return touchDisabledPrefixRE.ReplaceAllString(strings.TrimSpace(name), "") + touchFolderSuffix
}

func replaceTouchOutput(staging, output string) (err error) {
	id, _ := newTouchID()
	if len(id) > 8 {
		id = id[:8]
	}
	backup := output + ".backup-" + id
	oldMoved, newMoved := false, false
	defer func() {
		err = infra.WithCause(err, infra.AnnotateError(os.RemoveAll(staging), infra.Diagnostic{Stage: "cleanup"}))
	}()
	if err = os.Rename(output, backup); err != nil {
		return err
	}
	oldMoved = true
	if err = os.Rename(staging, output); err != nil {
		if restoreErr := os.Rename(backup, output); restoreErr != nil {
			return errors.Join(err, restoreErr)
		}
		return err
	}
	newMoved = true
	if err = os.RemoveAll(backup); err != nil {
		if newMoved {
			err = infra.WithCause(
				err,
				infra.AnnotateError(os.RemoveAll(output), infra.Diagnostic{Stage: "rollback-remove"}),
			)
		}
		if oldMoved {
			if restoreErr := os.Rename(backup, output); restoreErr != nil {
				return errors.Join(err, restoreErr)
			}
		}
		return err
	}
	return nil
}

func sanitizeTouchNamespace(root string) string {
	base := filepath.Base(root)
	if regexp.MustCompile(`(?i)^(body|face|hair|leg|legs|outfit|parts?)$`).
		MatchString(touchDisabledPrefixRE.ReplaceAllString(base, "")) {
		base = filepath.Base(filepath.Dir(root))
	}
	base = regexp.MustCompile(`[^a-zA-Z0-9]+`).ReplaceAllString(touchDisabledPrefixRE.ReplaceAllString(base, ""), "")
	if len(base) > 24 {
		base = base[:24]
	}
	if base == "" {
		return "Mod"
	}
	return base
}

func copyTouchRuntimeShaders(outputRoot string) error {
	target := filepath.Join(outputRoot, "Resources", "IM")
	if err := os.MkdirAll(target, 0755); err != nil {
		return err
	}
	for _, name := range touchShaderFiles {
		raw, err := touchRuntimeShaders.ReadFile("touch_runtime/" + name)
		if err != nil {
			return infra.WithCause(
				infra.ContractError(fmt.Sprintf("Bundled touch runtime shader missing: %s", name)),
				err,
			)
		}
		if err = os.WriteFile(filepath.Join(target, name), raw, 0600); err != nil {
			return err
		}
	}
	return nil
}

func writeTouchManifest(root, version string) error {
	raw, err := json.MarshalIndent(
		map[string]string{
			"kind":           touchProfileManifestKind,
			"runtimeVersion": version,
			"createdAt":      time.Now().UTC().Format(time.RFC3339Nano),
		},
		"",
		"  ",
	)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, touchProfileManifestFile), raw, 0600)
}
