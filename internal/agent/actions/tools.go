package actions

import (
	"context"
	"encoding/json"

	"nahida.live/desktop/internal/tools"
)

// registerToolActions registers the local fix, texture, touch-profile, body-shape and model-viewer actions.
func registerToolActions(registry *Registry, deps Dependencies) {
	if deps.Tools == nil {
		return
	}
	registerTextureDiagnosticActions(registry)
	registry.add(
		simpleAction("tools.get_scripts", "List installed local fix scripts.", "tools", RiskRead, objectSchema(nil),
			func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
				return deps.Tools.GetScripts(ctx)
			}),
	)
	registry.add(
		simpleAction("tools.get_presets", "List installed local fix presets.", "tools", RiskRead, objectSchema(nil),
			func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
				return deps.Tools.GetPresets(ctx)
			}),
	)
	registry.add(pathAction("tools.inspect_fixes", "Inspect a mod for applicable local fixes.", RiskRead,
		func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
				Importer     string `json:"importer"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.DiagnoseModForFix(ctx, path, input.Importer)
		}))
	registry.add(
		pathIDAction(
			"tools.run_script",
			"Run an installed fix script against a mod.",
			"scriptId",
			RiskConfirm,
			func(ctx context.Context, path, id string) (any, error) {
				return actionOK(deps.Tools.RunScript(ctx, id, path))
			},
		),
	)
	registry.add(
		pathIDAction(
			"tools.run_preset",
			"Run an installed fix preset against a mod.",
			"presetId",
			RiskConfirm,
			func(ctx context.Context, path, id string) (any, error) {
				return actionOK(deps.Tools.RunPreset(ctx, id, path))
			},
		),
	)
	registry.add(simpleAction("tools.bisect_state", "Get the current mod bisect state.", "tools", RiskRead,
		objectSchema(nil), func(context.Context, actionContext, json.RawMessage) (any, error) {
			return deps.Tools.BisectGetState(), nil
		}))
	registry.add(simpleAction("tools.bisect_start", "Start local mod bisect.", "tools", RiskConfirm,
		objectSchema(map[string]any{"game": stringSchema(), "excludePaths": pathReferenceListSchema()},
			"game", "excludePaths"),
		func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Game         string          `json:"game"`
				ExcludePaths []pathReference `json:"excludePaths"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			paths, err := resolveActionPaths(actionCtx, input.ExcludePaths)
			if err != nil {
				return nil, err
			}
			return deps.Tools.BisectStart(ctx, input.Game, paths)
		}))
	registry.add(
		simpleAction("tools.bisect_respond", "Record whether the current bisect round fixed the problem.", "tools",
			RiskWrite, objectSchema(map[string]any{"fixed": booleanSchema()}, "fixed"),
			func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Fixed bool `json:"fixed"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.Tools.BisectRespond(ctx, input.Fixed)
			}),
	)
	registry.add(simpleAction("tools.bisect_undo", "Undo the last mod bisect round.", "tools", RiskWrite,
		objectSchema(nil), func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.Tools.BisectUndoLastRound(ctx)
		}))
	registry.add(
		simpleAction("tools.bisect_cancel", "Cancel and roll back local mod bisect.", "tools", RiskConfirm,
			objectSchema(nil), func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
				return deps.Tools.BisectCancel(ctx)
			}),
	)
	registry.add(pathAction("tools.list_textures", "List textures inside a mod.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.ListTextureMod(ctx, path, nil)
		}))
	registry.add(simpleAction("tools.get_texture_settings", "Get texture resize settings.", "tools", RiskRead,
		objectSchema(nil), func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.Tools.GetTextureResizeSettings(ctx)
		}))
	registry.add(simpleAction("tools.get_texture_resize_state", "Get the active texture resize state.", "tools",
		RiskRead, objectSchema(nil), func(context.Context, actionContext, json.RawMessage) (any, error) {
			return deps.Tools.GetTextureResizeState(), nil
		}))
	registry.add(simpleAction("tools.get_texture_runtime_status", "Get installed texture upscaler runtime status.",
		"tools", RiskRead, objectSchema(nil),
		func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.Tools.GetTextureUpscaleRuntimeStatus(ctx)
		}))
	registry.add(simpleAction("tools.set_texture_settings", "Update texture resize settings.", "tools", RiskWrite,
		objectSchema(map[string]any{"settings": map[string]any{"type": "object"}}, "settings"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Settings tools.TextureResizeSettingsPatch `json:"settings"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.SaveTextureResizeSettings(ctx, input.Settings)
		}))
	registry.add(action{
		definition: Definition{
			ID:          "tools.resize_textures",
			Description: "Resize textures inside a mod.",
			Domain:      "tools",
			Risk:        RiskConfirm,
			Scopes:      []string{"global", "mod"},
			InputSchema: objectSchema(map[string]any{
				"rootId":       stringSchema(),
				"relativePath": stringSchema(),
				"settings":     map[string]any{"type": "object"},
			}, "rootId", "relativePath", "settings"),
		},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string                      `json:"rootId"`
				RelativePath string                      `json:"relativePath"`
				Settings     tools.TextureResizeSettings `json:"settings"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			path, err := actionCtx.resolve(input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			return deps.Tools.ResizeTextureMod(ctx, path, tools.TextureResizeModInput{Settings: input.Settings})
		},
		describe: pathDescription("Resize textures inside a mod."),
	})
	registry.add(
		simpleAction(
			"tools.persist_logs",
			"Read persist-toggle watcher logs.",
			"tools",
			RiskRead,
			objectSchema(nil),
			func(context.Context, actionContext, json.RawMessage) (any, error) {
				return deps.Tools.GetPersistLogs(), nil
			},
		),
	)
	registry.add(
		simpleAction("tools.start_persist_watcher", "Start the persist-toggle watcher.", "tools", RiskWrite,
			objectSchema(nil), func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
				return actionOK(deps.Tools.StartPersistWatcher(ctx))
			}),
	)
	registry.add(
		simpleAction("tools.stop_persist_watcher", "Stop the persist-toggle watcher.", "tools", RiskWrite,
			objectSchema(nil), func(context.Context, actionContext, json.RawMessage) (any, error) {
				return map[string]any{"stopped": deps.Tools.StopPersistWatcher()}, nil
			}),
	)
	persistState := pathAction(
		"tools.persist_toggle_state",
		"Persist model-viewer toggle variables to a local INI file.",
		RiskConfirm,
		func(_ context.Context, path string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string         `json:"rootId"`
				RelativePath string         `json:"relativePath"`
				State        map[string]any `json:"state"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.PersistModelViewerToggleState(path, input.State)
		},
	)
	persistState.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), "state": map[string]any{"type": "object"},
	}, "rootId", "relativePath", "state")
	registry.add(persistState)
	registry.add(pathAction("tools.wuwa_status", "Get the prepared Wuwa fixer status.", RiskRead,
		func(ctx context.Context, _ string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string  `json:"rootId"`
				RelativePath string  `json:"relativePath"`
				Importer     *string `json:"importer,omitempty"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.WuwaFixerGetStatus(ctx, input.Importer)
		}))
	registry.add(pathAction("tools.wuwa_backups", "List Wuwa fixer backups for a mod.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.WuwaFixerScanBackups(ctx, path)
		}))
	registry.add(pathAction("tools.wuwa_backup_size", "Get Wuwa fixer backup count and size.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.WuwaFixerGetBackupSize(ctx, path)
		}))
	wuwaRun := pathAction("tools.wuwa_run", "Run the prepared Wuwa fixer against a mod.", RiskConfirm,
		func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string                 `json:"rootId"`
				RelativePath string                 `json:"relativePath"`
				Options      tools.WuwaFixerOptions `json:"options"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return actionOK(deps.Tools.WuwaFixerRun(ctx, path, input.Options))
		})
	wuwaRun.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), "options": map[string]any{"type": "object"},
	}, "rootId", "relativePath", "options")
	registry.add(wuwaRun)
	registry.add(pathIDAction("tools.wuwa_restore", "Restore a Wuwa fixer backup group.", "groupKey",
		RiskConfirm, func(ctx context.Context, path, groupKey string) (any, error) {
			return actionOK(deps.Tools.WuwaFixerRollbackToGroup(ctx, path, groupKey))
		}))
	registry.add(pathAction("tools.wuwa_delete_backups", "Delete all Wuwa fixer backups for a mod.",
		RiskConfirm, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return actionOK(deps.Tools.WuwaFixerCleanBackups(ctx, path))
		}))
	registry.add(pathAction("tools.zzmi_prepare", "Inspect a mod for prepared ZZMI fixer tools.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.ZZMIFixerPrepare(ctx, path, false)
		}))
	zzmiRun := pathIDAction("tools.zzmi_run", "Run a prepared ZZMI fixer tool.", "tool", RiskConfirm,
		func(ctx context.Context, path, tool string) (any, error) {
			return deps.Tools.ZZMIFixerRun(ctx, tools.ZZMIFixerRunInput{Path: path, Tool: tool})
		})
	zzmiRun.definition.InputSchema["properties"].(map[string]any)["tool"] = enumSchema("hash", "jane", "dialyn")
	registry.add(zzmiRun)
	registry.add(pathAction("tools.zzmi_backups", "List ZZMI fixer backup sessions.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.ZZMIFixerListBackups(ctx, path)
		}))
	registry.add(pathIDAction("tools.zzmi_backup", "Get one ZZMI fixer backup session.", "sessionId",
		RiskRead, func(ctx context.Context, path, sessionID string) (any, error) {
			return deps.Tools.ZZMIFixerGetBackup(ctx, path, sessionID)
		}))
	zzmiRestore := pathAction("tools.zzmi_restore", "Restore files from a ZZMI fixer backup.", RiskConfirm,
		func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string  `json:"rootId"`
				RelativePath string  `json:"relativePath"`
				SessionID    string  `json:"sessionId"`
				EntryID      *string `json:"entryId,omitempty"`
				Force        bool    `json:"force"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.ZZMIFixerRestore(ctx, tools.ZZMIFixerRestoreInput{
				Path: path, SessionID: input.SessionID, EntryID: input.EntryID, Force: input.Force,
			})
		})
	zzmiRestore.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), "sessionId": stringSchema(),
		"entryId": nullableStringSchema(), "force": booleanSchema(),
	}, "rootId", "relativePath", "sessionId", "force")
	registry.add(zzmiRestore)
	zzmiDelete := pathAction("tools.zzmi_delete_backup", "Delete a ZZMI fixer backup.", RiskConfirm,
		func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string  `json:"rootId"`
				RelativePath string  `json:"relativePath"`
				SessionID    string  `json:"sessionId"`
				EntryID      *string `json:"entryId,omitempty"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return actionOK(deps.Tools.ZZMIFixerDeleteBackup(ctx, tools.ZZMIFixerDeleteBackupInput{
				Path: path, SessionID: input.SessionID, EntryID: input.EntryID,
			}))
		})
	zzmiDelete.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), "sessionId": stringSchema(),
		"entryId": nullableStringSchema(),
	}, "rootId", "relativePath", "sessionId")
	registry.add(zzmiDelete)
	registry.add(pathAction("tools.zzmi_delete_all_backups", "Delete all ZZMI fixer backups for a mod.",
		RiskConfirm, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return actionOK(deps.Tools.ZZMIFixerDeleteAllBackups(ctx, path))
		}))
	registry.add(pathAction("tools.touch_prepare", "Start a touch-profile session for a mod.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.TouchProfilePrepare(ctx, tools.TouchProfileLoadInput{ModPath: path})
		}))
	registry.add(simpleAction("tools.touch_mesh", "Get a touch-profile mesh descriptor.", "tools", RiskRead,
		objectSchema(map[string]any{"sessionId": stringSchema(), "componentId": stringSchema()},
			"sessionId", "componentId"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.TouchProfilePreviewInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.TouchProfileGetMeshDescriptor(ctx, input)
		}))
	registry.add(simpleAction("tools.touch_preview", "Get a touch-profile preview descriptor.", "tools", RiskRead,
		objectSchema(map[string]any{"sessionId": stringSchema(), "componentId": stringSchema()},
			"sessionId", "componentId"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.TouchProfilePreviewInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.TouchProfileGetPreviewDescriptor(ctx, input)
		}))
	registry.add(simpleAction("tools.touch_analyze", "Analyze touch-profile components.", "tools", RiskWrite,
		objectSchema(map[string]any{
			"sessionId": stringSchema(), "componentIds": stringArraySchema(),
			"mode": nullableStringSchema(), "boneSelections": map[string]any{"type": "array"},
			"weightThreshold": map[string]any{"type": "array"},
		}, "sessionId", "componentIds"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.TouchProfileAnalyzeInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.TouchProfileAnalyzeComponents(ctx, input)
		}))
	registry.add(simpleAction("tools.touch_update", "Update touch-profile zone settings.", "tools", RiskWrite,
		objectSchema(map[string]any{"sessionId": stringSchema(), "changes": map[string]any{"type": "array"}},
			"sessionId", "changes"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.TouchProfileUpdateZoneSettingsBatchInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.TouchProfileUpdateZoneSettingsBatch(ctx, input)
		}))
	registry.add(sessionApplyAction("tools.touch_apply", "Apply a touch profile to local mod files.",
		deps.Tools.TouchProfileApply))
	registry.add(sessionApplyAction("tools.touch_regenerate", "Regenerate local mod files from a touch profile.",
		deps.Tools.TouchProfileRegenerate))
	registry.add(idAction("tools.touch_discard", "Discard a touch-profile draft.", "sessionId", RiskWrite,
		func(ctx context.Context, id string) (any, error) { return deps.Tools.TouchProfileDiscardDraft(ctx, id) }))
	registry.add(idAction("tools.touch_close", "Close a touch-profile session.", "sessionId", RiskWrite,
		func(ctx context.Context, id string) (any, error) { return deps.Tools.TouchProfileCloseSession(ctx, id) }))
	touchRollback := twoPathAction(
		"tools.touch_rollback",
		"Roll back touch-profile generated mod files.",
		RiskConfirm,
		func(ctx context.Context, outputRoot, sourceRoot string, raw json.RawMessage) (any, error) {
			var input struct {
				SourceRootID             string `json:"sourceRootId"`
				SourceRelativePath       string `json:"sourceRelativePath"`
				TargetRootID             string `json:"targetRootId"`
				TargetRelativePath       string `json:"targetRelativePath"`
				SessionID                string `json:"sessionId"`
				ReenableSourceOnRollback bool   `json:"reenableSourceOnRollback"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.TouchProfileRollback(ctx, tools.TouchProfileRollbackInput{
				SessionID: input.SessionID, OutputModRoot: outputRoot, SourceModRoot: sourceRoot,
				ReenableSourceOnRollback: input.ReenableSourceOnRollback,
			})
		},
	)
	touchRollback.definition.InputSchema = objectSchema(map[string]any{
		"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
		"targetRootId": stringSchema(), "targetRelativePath": stringSchema(), "sessionId": stringSchema(),
		"reenableSourceOnRollback": booleanSchema(),
	}, "sourceRootId", "sourceRelativePath", "targetRootId", "targetRelativePath", "sessionId",
		"reenableSourceOnRollback")
	registry.add(touchRollback)
	registry.add(pathAction("tools.body_shape_load", "Load a mod into a body-shape session.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.BodyShapeLoadMod(ctx, path)
		}))
	registry.add(pathAction("tools.model_viewer_load", "Load a mod model-viewer descriptor.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.LoadModViewer(ctx, path)
		}))
	registry.add(pathAction("tools.model_viewer_grid_preview", "Load a model-viewer grid preview descriptor.",
		RiskRead, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.LoadModGridPreview(ctx, path)
		}))
	registry.add(simpleAction("tools.body_shape_mesh", "Get a mesh descriptor from a body-shape session.", "tools",
		RiskRead, objectSchema(map[string]any{"sessionId": stringSchema(), "meshId": stringSchema()},
			"sessionId", "meshId"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.BodyShapeMeshInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.BodyShapeGetMesh(ctx, input)
		}))
	registry.add(idAction("tools.body_shape_close", "Close a body-shape session.", "sessionId", RiskWrite,
		func(ctx context.Context, id string) (any, error) { return deps.Tools.BodyShapeCloseSession(ctx, id) }))
	registry.add(simpleAction("tools.body_shape_begin_export", "Prepare a body-shape export upload.", "tools",
		RiskWrite, objectSchema(map[string]any{
			"sessionId": stringSchema(), "meshId": stringSchema(), "includeWeights": booleanSchema(),
		}, "sessionId", "meshId", "includeWeights"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.BodyShapeBeginExportInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.BodyShapeBeginExport(ctx, input)
		}))
	registry.add(simpleAction("tools.body_shape_commit_export", "Commit uploaded body-shape changes to mod files.",
		"tools", RiskConfirm, objectSchema(map[string]any{
			"sessionId": stringSchema(), "exportId": stringSchema(),
			"amount": map[string]any{}, "axisScale": map[string]any{"type": "array"},
			"writeChangeLog": map[string]any{}, "changeSummary": map[string]any{"type": "object"},
		}, "sessionId", "exportId"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input tools.BodyShapeCommitExportInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.BodyShapeCommitExport(ctx, input)
		}))
	registry.add(idAction("tools.model_viewer_cleanup", "Close a model-viewer session.", "sessionId", RiskWrite,
		func(ctx context.Context, id string) (any, error) { return deps.Tools.CleanupModelViewer(ctx, id) }))
	registry.add(simpleAction("tools.model_viewer_ineffective_values", "Inspect ineffective model-viewer values.",
		"tools", RiskRead, objectSchema(map[string]any{
			"sessionId": stringSchema(), "state": map[string]any{"type": "object"},
		}, "sessionId", "state"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				SessionID string         `json:"sessionId"`
				State     map[string]any `json:"state"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.GetModelViewerIneffectiveValues(ctx, input.SessionID, input.State)
		}))
	registry.add(simpleAction("tools.fixer_4001_state", "Get local 4001 fixer state.", "tools", RiskRead,
		objectSchema(nil), func(context.Context, actionContext, json.RawMessage) (any, error) {
			return deps.Tools.FourThousandOneFixerGetState(), nil
		}))
	registry.add(pathAction("tools.fixer_4001_write_access", "Check importer write access for the 4001 fixer.",
		RiskRead, func(_ context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.FourThousandOneFixerCheckImporterWriteAccess(
				tools.Fixer4001PathInput{ImporterPath: &path},
			), nil
		}))
	registry.add(pathAction("tools.fixer_4001_diversification_state", "Inspect 4001 fixer diversification state.",
		RiskRead, func(_ context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.FourThousandOneFixerGetDiversificationState(
				tools.Fixer4001PathInput{ImporterPath: &path},
			)
		}))
	fixerDiversify := pathIDAction("tools.fixer_4001_diversify", "Diversify the configured importer's DLL.",
		"importerKey", RiskConfirm, func(ctx context.Context, path, importerKey string) (any, error) {
			return deps.Tools.FourThousandOneFixerDiversifyDllPadding(ctx, tools.Fixer4001ImporterInput{
				ImporterKey: importerKey, ImporterPath: &path,
			}), nil
		})
	registry.add(fixerDiversify)
	registry.add(pathAction("tools.fixer_4001_restore", "Restore a diversified importer DLL backup.",
		RiskConfirm, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Tools.FourThousandOneFixerRestoreDiversifiedDll(
				ctx, tools.Fixer4001PathInput{ImporterPath: &path},
			), nil
		}))
	fixerBuild := pathAction("tools.fixer_4001_build", "Build and apply a local 4001 fixer DLL.",
		RiskConfirm, func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
				Provider     string `json:"provider"`
				Version      string `json:"version"`
				ImporterKey  string `json:"importerKey"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.FourThousandOneFixerBuildDll(ctx, tools.Fixer4001BuildInput{
				Provider:     input.Provider,
				Version:      input.Version,
				ImporterKey:  input.ImporterKey,
				ImporterPath: &path,
			}), nil
		})
	fixerBuild.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), "provider": stringSchema(),
		"version": stringSchema(), "importerKey": stringSchema(),
	}, "rootId", "relativePath", "provider", "version", "importerKey")
	registry.add(fixerBuild)
	registry.add(simpleAction("tools.bisect_finalize", "Finish mod bisect and keep selected paths disabled.", "tools",
		RiskConfirm, objectSchema(map[string]any{"keepDisabled": pathReferenceListSchema()}, "keepDisabled"),
		func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				KeepDisabled []pathReference `json:"keepDisabled"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			paths, err := resolveActionPaths(actionCtx, input.KeepDisabled)
			if err != nil {
				return nil, err
			}
			return deps.Tools.BisectFinalize(ctx, paths)
		}))
	registry.add(simpleAction("tools.bisect_recover", "Recover an interrupted mod bisect session.", "tools",
		RiskConfirm, objectSchema(map[string]any{"game": stringSchema()}, "game"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Game string `json:"game"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Tools.BisectRecover(ctx, input.Game)
		}))
}
