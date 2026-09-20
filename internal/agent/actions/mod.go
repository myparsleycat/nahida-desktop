package actions

import (
	"context"
	"encoding/json"

	modservice "nahida.live/desktop/internal/mod"
)

// registerModActions registers the local mod management actions.
func registerModActions(registry *Registry, deps Dependencies) {
	if deps.Mod == nil {
		return
	}
	registry.add(simpleAction("mod.get_games", "List configured games.", "mod", RiskRead, objectSchema(nil),
		func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.Mod.GetGames(ctx)
		}))
	registry.add(
		simpleAction("mod.get_characters", "List character groups for a configured game.", "mod", RiskRead,
			objectSchema(map[string]any{"game": stringSchema()}, "game"),
			func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Game string `json:"game"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.Mod.GetCharacters(ctx, input.Game, nil)
			}),
	)
	registry.add(pathAction("mod.get_groups", "List subgroups under a configured Mods folder.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.GetSubGroups(ctx, path, nil)
		}))
	registry.add(pathAction("mod.get_mods", "List mods in a group.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.GetMods(ctx, path)
		}))
	registry.add(pathAction("mod.toggle", "Toggle a mod between enabled and disabled.", RiskWrite,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.Toggle(ctx, path)
		}))
	registry.add(pathAction("mod.enable", "Enable a mod.", RiskWrite,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.Enable(ctx, path)
		}))
	registry.add(pathAction("mod.disable", "Disable a mod.", RiskWrite,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.Disable(ctx, path)
		}))
	registry.add(pathAction("mod.disable_unmanaged", "Disable an unmanaged mod folder.", RiskWrite,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.DisableUnmanaged(ctx, path)
		}))
	registry.add(pathAction("mod.exclusive_toggle", "Exclusively enable one mod in its group.", RiskWrite,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.ExclusiveToggle(ctx, path)
		}))
	registry.add(action{
		definition: Definition{ID: "mod.rename", Description: "Rename a mod folder.", Domain: "mod",
			Risk: RiskWrite, Scopes: []string{"global", "mod"}, InputSchema: objectSchema(map[string]any{
				"rootId": stringSchema(), "relativePath": stringSchema(), "newName": stringSchema(),
			}, "rootId", "relativePath", "newName")},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
				NewName      string `json:"newName"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			path, err := actionCtx.resolve(input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			return deps.Mod.Rename(ctx, path, input.NewName)
		},
		describe: pathDescription("Rename a mod folder."),
	})
	registry.add(pathAction("mod.enable_all", "Enable every mod in a group.", RiskConfirm,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return actionOK(deps.Mod.EnableAll(ctx, path))
		}))
	registry.add(pathAction("mod.disable_all", "Disable every mod in a group.", RiskConfirm,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return actionOK(deps.Mod.DisableAll(ctx, path))
		}))
	registry.add(simpleAction("mod.get_presets", "List local mod presets for a game.", "mod", RiskRead,
		objectSchema(map[string]any{"game": stringSchema()}, "game"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Game string `json:"game"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Mod.GetPresets(ctx, input.Game)
		}))
	registry.add(
		simpleAction(
			"mod.create_preset",
			"Create a local mod preset from current state.",
			"mod",
			RiskWrite,
			objectSchema(
				map[string]any{
					"game":             stringSchema(),
					"name":             stringSchema(),
					"description":      nullableStringSchema(),
					"resolveConflicts": booleanSchema(),
				},
				"game",
				"name",
				"resolveConflicts",
			),
			func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Game             string  `json:"game"`
					Name             string  `json:"name"`
					Description      *string `json:"description"`
					ResolveConflicts bool    `json:"resolveConflicts"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.Mod.CreatePreset(ctx, input.Game, input.Name, input.Description, input.ResolveConflicts)
			},
		),
	)
	registry.add(idAction("mod.apply_preset", "Apply a local mod preset.", "presetId", RiskConfirm,
		func(ctx context.Context, id string) (any, error) { return deps.Mod.ApplyPreset(ctx, id) }))
	registry.add(idAction("mod.delete_preset", "Delete a local mod preset.", "presetId", RiskConfirm,
		func(ctx context.Context, id string) (any, error) { return actionOK(deps.Mod.DeletePreset(ctx, id)) }))
	registry.add(simpleAction("mod.rename_preset", "Rename a local mod preset.", "mod", RiskWrite,
		objectSchema(map[string]any{"presetId": stringSchema(), "newName": stringSchema()}, "presetId", "newName"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				PresetID string `json:"presetId"`
				NewName  string `json:"newName"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return actionOK(deps.Mod.UpdatePresetName(ctx, input.PresetID, input.NewName))
		}))
	registry.add(simpleAction("mod.get_preset_conflicts", "Inspect conflicts before creating a preset.", "mod",
		RiskRead, objectSchema(map[string]any{"game": stringSchema()}, "game"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Game string `json:"game"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Mod.GetPresetCreateConflicts(ctx, input.Game)
		}))
	registry.add(pathAction("mod.get_manual_groups", "List manually configured subgroups.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.Mod.GetManualSubGroups(ctx, path, nil)
		}))
	registry.add(action{
		definition: Definition{
			ID:          "mod.set_manual_group",
			Description: "Change manual subgroup state.",
			Domain:      "mod",
			Risk:        RiskWrite,
			Scopes:      []string{"global", "mod"},
			InputSchema: objectSchema(map[string]any{
				"rootId": stringSchema(), "relativePath": stringSchema(), "enabled": booleanSchema(),
			}, "rootId", "relativePath", "enabled"),
		},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
				Enabled      bool   `json:"enabled"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			path, err := actionCtx.resolve(input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			return actionOK(deps.Mod.SetManualSubGroup(ctx, path, input.Enabled))
		},
		describe: pathDescription("Change manual subgroup state."),
	})
	registry.add(twoPathAction("mod.import_folder", "Import a local folder into a mod group.", RiskConfirm,
		func(ctx context.Context, source, target string, _ json.RawMessage) (any, error) {
			return deps.Mod.CopyFolderToGroup(ctx, source, target)
		}))
	archiveImport := twoPathAction("mod.import_archive", "Extract a local archive into a mod group.",
		RiskConfirm, func(ctx context.Context, source, target string, raw json.RawMessage) (any, error) {
			var input struct {
				SourceRootID       string `json:"sourceRootId"`
				SourceRelativePath string `json:"sourceRelativePath"`
				TargetRootID       string `json:"targetRootId"`
				TargetRelativePath string `json:"targetRelativePath"`
				Mode               string `json:"mode"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Mod.ExtractArchiveToGroup(ctx, source, target, input.Mode)
		})
	archiveImport.definition.InputSchema = objectSchema(map[string]any{
		"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
		"targetRootId": stringSchema(), "targetRelativePath": stringSchema(),
		"mode": enumSchema("flatten_single_root", "keep_archive_root"),
	}, "sourceRootId", "sourceRelativePath", "targetRootId", "targetRelativePath", "mode")
	registry.add(archiveImport)
	registry.add(action{
		definition: Definition{
			ID:          "mod.update_toggle_key",
			Description: "Update a mod toggle key.",
			Domain:      "mod",
			Risk:        RiskConfirm,
			Scopes:      []string{"global", "mod"},
			InputSchema: objectSchema(map[string]any{
				"rootId":       stringSchema(),
				"relativePath": stringSchema(),
				"iniFileName":  stringSchema(),
				"sectionName":  stringSchema(),
				"variable":     stringSchema(),
				"value":        map[string]any{"type": "string"},
			}, "rootId", "relativePath", "iniFileName", "sectionName", "variable", "value"),
		},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
				INIFileName  string `json:"iniFileName"`
				SectionName  string `json:"sectionName"`
				Variable     string `json:"variable"`
				Value        string `json:"value"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			path, err := actionCtx.resolve(input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			return actionOK(deps.Mod.UpdateToggleKey(
				ctx, path, input.INIFileName, input.SectionName, input.Variable, input.Value,
			))
		},
		describe: pathDescription("Update a mod toggle key."),
	})
	registry.add(action{
		definition: Definition{
			ID:          "mod.classify_merge",
			Description: "Classify mod packs for merging.",
			Domain:      "mod",
			Risk:        RiskRead,
			Scopes:      []string{"global", "mod"},
			InputSchema: objectSchema(map[string]any{
				"paths": pathReferenceArraySchema(),
			}, "paths"),
		},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Paths []pathReference `json:"paths"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			paths, err := resolveActionPaths(actionCtx, input.Paths)
			if err != nil {
				return nil, err
			}
			return deps.Mod.ClassifyMergePacks(ctx, paths)
		},
	})
	registry.add(action{
		definition: Definition{ID: "mod.merge", Description: "Merge local mod packs.", Domain: "mod",
			Risk: RiskConfirm, Scopes: []string{"global", "mod"}, InputSchema: objectSchema(map[string]any{
				"group": pathReferenceSchema(), "placement": stringSchema(), "packName": stringSchema(),
				"root": map[string]any{"type": "object"},
			}, "group", "placement", "packName", "root")},
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			request, err := decodeAgentMergeRequest(actionCtx, raw)
			if err != nil {
				return nil, err
			}
			return deps.Mod.MergeMods(ctx, request)
		},
		describe: func(actionCtx actionContext, raw json.RawMessage) (string, string, error) {
			request, err := decodeAgentMergeRequest(actionCtx, raw)
			return "Merge local mod packs.", request.GroupPath, err
		},
	})
	registry.add(simpleAction("mod.get_compression", "Get mod compression state.", "mod", RiskRead, objectSchema(nil),
		func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.Mod.GetCompressionState(ctx)
		}))
	registry.add(simpleAction("mod.set_compression", "Set mod compression configuration.", "mod", RiskConfirm,
		objectSchema(map[string]any{"enabled": booleanSchema(), "method": enumSchema("zstd", "xpress4k"),
			"thresholdMiB": integerSchema(1, 64)}, "enabled", "method", "thresholdMiB"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Enabled      bool   `json:"enabled"`
				Method       string `json:"method"`
				ThresholdMiB int    `json:"thresholdMiB"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			if _, err := deps.Mod.SetCompressionConfig(
				ctx,
				modservice.CompressionConfig{Method: input.Method, ThresholdMiB: input.ThresholdMiB},
			); err != nil {
				return nil, err
			}
			return deps.Mod.SetCompressionEnabled(ctx, input.Enabled)
		}))
}
