package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"nahida.live/desktop/internal/menumaker"
	modservice "nahida.live/desktop/internal/mod"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
	"nahida.live/desktop/internal/xxmi"
)

type ActionRisk string

const (
	ActionRiskRead    ActionRisk = "read"
	ActionRiskWrite   ActionRisk = "write"
	ActionRiskConfirm ActionRisk = "confirm"
)

type DesktopActionDefinition struct {
	ID          string         `json:"id"`
	Description string         `json:"description"`
	Domain      string         `json:"domain"`
	Risk        ActionRisk     `json:"risk"`
	Scopes      []string       `json:"scopes"`
	InputSchema map[string]any `json:"inputSchema"`
}

type desktopActionHint struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Risk        ActionRisk `json:"risk"`
}

type desktopActionDependencies struct {
	mod       *modservice.Mod
	tools     *tools.Tools
	settings  *setting.Setting
	transfer  *transfer.Transfer
	xxmi      *xxmi.XXMI
	menuMaker *menumaker.MenuMaker
}

type desktopActionContext struct {
	sandbox *Sandbox
	scope   AgentScope
}

type desktopAction struct {
	definition DesktopActionDefinition
	execute    func(context.Context, desktopActionContext, json.RawMessage) (any, error)
	validate   func(json.RawMessage) error
	risk       func(json.RawMessage) ActionRisk
	describe   func(desktopActionContext, json.RawMessage) (string, string, error)
}

type desktopActionRegistry struct {
	actions map[string]desktopAction
}

type desktopActionCall struct {
	ActionID  string          `json:"actionId"`
	Arguments json.RawMessage `json:"arguments"`
}

type agentPathReference struct {
	RootID       string `json:"rootId"`
	RelativePath string `json:"relativePath"`
}

type agentMergePlanNode struct {
	Kind           string               `json:"kind"`
	RootID         string               `json:"rootId,omitempty"`
	RelativePath   string               `json:"relativePath,omitempty"`
	ID             string               `json:"id,omitempty"`
	Engine         string               `json:"engine,omitempty"`
	Name           string               `json:"name,omitempty"`
	ForwardKey     string               `json:"forwardKey,omitempty"`
	BackKey        string               `json:"backKey,omitempty"`
	IncludeVanilla bool                 `json:"includeVanilla,omitempty"`
	Children       []agentMergePlanNode `json:"children,omitempty"`
}

type agentMergeRequest struct {
	Group     agentPathReference `json:"group"`
	Placement string             `json:"placement"`
	PackName  string             `json:"packName"`
	Root      agentMergePlanNode `json:"root"`
}

type menuMakerSaveINIInput struct {
	SourceRootID            string                      `json:"sourceRootId"`
	SourceRelativePath      string                      `json:"sourceRelativePath"`
	DestinationRootID       string                      `json:"destinationRootId"`
	DestinationRelativePath string                      `json:"destinationRelativePath"`
	SourceText              string                      `json:"sourceText"`
	Slots                   []menumaker.MenuMakerSlot   `json:"slots"`
	Settings                menumaker.MenuMakerSettings `json:"settings"`
	Encoding                string                      `json:"encoding"`
	HasBOM                  bool                        `json:"hasBOM"`
	Newline                 string                      `json:"newline"`
}

type menuMakerSaveZIPInput struct {
	menuMakerSaveINIInput
	OutputININame string                              `json:"outputININame"`
	Assets        []menumaker.MenuMakerGeneratedAsset `json:"assets"`
}

type approvalProposal struct {
	ActionID  string
	Arguments json.RawMessage
	Summary   string
	Target    string
	Impact    string
	Kind      string
}

func newDesktopActionRegistry(deps desktopActionDependencies) *desktopActionRegistry {
	registry := &desktopActionRegistry{actions: make(map[string]desktopAction)}
	register := func(action desktopAction) {
		if err := registry.register(action); err != nil {
			panic(err)
		}
	}
	object := objectSchema

	if deps.mod != nil {
		register(simpleAction("mod.get_games", "List configured games.", "mod", ActionRiskRead, object(nil),
			func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.mod.GetGames(ctx)
			}))
		register(
			simpleAction("mod.get_characters", "List character groups for a configured game.", "mod", ActionRiskRead,
				object(map[string]any{"game": stringSchema()}, "game"),
				func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
					var input struct {
						Game string `json:"game"`
					}
					if err := decodeActionArguments(raw, &input); err != nil {
						return nil, err
					}
					return deps.mod.GetCharacters(ctx, input.Game, nil)
				}),
		)
		register(pathAction("mod.get_groups", "List subgroups under a configured Mods folder.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.GetSubGroups(ctx, path, nil)
			}))
		register(pathAction("mod.get_mods", "List mods in a group.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.GetMods(ctx, path)
			}))
		register(pathAction("mod.toggle", "Toggle a mod between enabled and disabled.", ActionRiskWrite,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.Toggle(ctx, path)
			}))
		register(pathAction("mod.enable", "Enable a mod.", ActionRiskWrite,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.Enable(ctx, path)
			}))
		register(pathAction("mod.disable", "Disable a mod.", ActionRiskWrite,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.Disable(ctx, path)
			}))
		register(pathAction("mod.disable_unmanaged", "Disable an unmanaged mod folder.", ActionRiskWrite,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.DisableUnmanaged(ctx, path)
			}))
		register(pathAction("mod.exclusive_toggle", "Exclusively enable one mod in its group.", ActionRiskWrite,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.ExclusiveToggle(ctx, path)
			}))
		register(desktopAction{
			definition: DesktopActionDefinition{ID: "mod.rename", Description: "Rename a mod folder.", Domain: "mod",
				Risk: ActionRiskWrite, Scopes: []string{"global", "mod"}, InputSchema: object(map[string]any{
					"rootId": stringSchema(), "relativePath": stringSchema(), "newName": stringSchema(),
				}, "rootId", "relativePath", "newName")},
			execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string `json:"rootId"`
					RelativePath string `json:"relativePath"`
					NewName      string `json:"newName"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				path, err := resolveActionPath(actionCtx.sandbox, input.RootID, input.RelativePath)
				if err != nil {
					return nil, err
				}
				return deps.mod.Rename(ctx, path, input.NewName)
			},
			describe: pathDescription("Rename a mod folder."),
		})
		register(pathAction("mod.enable_all", "Enable every mod in a group.", ActionRiskConfirm,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return actionOK(deps.mod.EnableAll(ctx, path))
			}))
		register(pathAction("mod.disable_all", "Disable every mod in a group.", ActionRiskConfirm,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return actionOK(deps.mod.DisableAll(ctx, path))
			}))
		register(simpleAction("mod.get_presets", "List local mod presets for a game.", "mod", ActionRiskRead,
			object(map[string]any{"game": stringSchema()}, "game"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Game string `json:"game"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.mod.GetPresets(ctx, input.Game)
			}))
		register(
			simpleAction(
				"mod.create_preset",
				"Create a local mod preset from current state.",
				"mod",
				ActionRiskWrite,
				object(
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
				func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
					var input struct {
						Game             string  `json:"game"`
						Name             string  `json:"name"`
						Description      *string `json:"description"`
						ResolveConflicts bool    `json:"resolveConflicts"`
					}
					if err := decodeActionArguments(raw, &input); err != nil {
						return nil, err
					}
					return deps.mod.CreatePreset(ctx, input.Game, input.Name, input.Description, input.ResolveConflicts)
				},
			),
		)
		register(idAction("mod.apply_preset", "Apply a local mod preset.", "presetId", ActionRiskConfirm,
			func(ctx context.Context, id string) (any, error) { return deps.mod.ApplyPreset(ctx, id) }))
		register(idAction("mod.delete_preset", "Delete a local mod preset.", "presetId", ActionRiskConfirm,
			func(ctx context.Context, id string) (any, error) { return actionOK(deps.mod.DeletePreset(ctx, id)) }))
		register(simpleAction("mod.rename_preset", "Rename a local mod preset.", "mod", ActionRiskWrite,
			object(map[string]any{"presetId": stringSchema(), "newName": stringSchema()}, "presetId", "newName"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					PresetID string `json:"presetId"`
					NewName  string `json:"newName"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return actionOK(deps.mod.UpdatePresetName(ctx, input.PresetID, input.NewName))
			}))
		register(simpleAction("mod.get_preset_conflicts", "Inspect conflicts before creating a preset.", "mod",
			ActionRiskRead, object(map[string]any{"game": stringSchema()}, "game"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Game string `json:"game"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.mod.GetPresetCreateConflicts(ctx, input.Game)
			}))
		register(pathAction("mod.get_manual_groups", "List manually configured subgroups.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.mod.GetManualSubGroups(ctx, path, nil)
			}))
		register(desktopAction{
			definition: DesktopActionDefinition{
				ID:          "mod.set_manual_group",
				Description: "Change manual subgroup state.",
				Domain:      "mod",
				Risk:        ActionRiskWrite,
				Scopes:      []string{"global", "mod"},
				InputSchema: object(map[string]any{
					"rootId": stringSchema(), "relativePath": stringSchema(), "enabled": booleanSchema(),
				}, "rootId", "relativePath", "enabled"),
			},
			execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string `json:"rootId"`
					RelativePath string `json:"relativePath"`
					Enabled      bool   `json:"enabled"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				path, err := resolveActionPath(actionCtx.sandbox, input.RootID, input.RelativePath)
				if err != nil {
					return nil, err
				}
				return actionOK(deps.mod.SetManualSubGroup(ctx, path, input.Enabled))
			},
			describe: pathDescription("Change manual subgroup state."),
		})
		register(twoPathAction("mod.import_folder", "Import a local folder into a mod group.", ActionRiskConfirm,
			func(ctx context.Context, source, target string, _ json.RawMessage) (any, error) {
				return deps.mod.CopyFolderToGroup(ctx, source, target)
			}))
		archiveImport := twoPathAction("mod.import_archive", "Extract a local archive into a mod group.",
			ActionRiskConfirm, func(ctx context.Context, source, target string, raw json.RawMessage) (any, error) {
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
				return deps.mod.ExtractArchiveToGroup(ctx, source, target, input.Mode)
			})
		archiveImport.definition.InputSchema = object(map[string]any{
			"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
			"targetRootId": stringSchema(), "targetRelativePath": stringSchema(),
			"mode": enumSchema("flatten_single_root", "keep_archive_root"),
		}, "sourceRootId", "sourceRelativePath", "targetRootId", "targetRelativePath", "mode")
		register(archiveImport)
		register(desktopAction{
			definition: DesktopActionDefinition{
				ID:          "mod.update_toggle_key",
				Description: "Update a mod toggle key.",
				Domain:      "mod",
				Risk:        ActionRiskConfirm,
				Scopes:      []string{"global", "mod"},
				InputSchema: object(map[string]any{
					"rootId":       stringSchema(),
					"relativePath": stringSchema(),
					"iniFileName":  stringSchema(),
					"sectionName":  stringSchema(),
					"variable":     stringSchema(),
					"value":        map[string]any{"type": "string"},
				}, "rootId", "relativePath", "iniFileName", "sectionName", "variable", "value"),
			},
			execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
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
				path, err := resolveActionPath(actionCtx.sandbox, input.RootID, input.RelativePath)
				if err != nil {
					return nil, err
				}
				return actionOK(deps.mod.UpdateToggleKey(
					ctx, path, input.INIFileName, input.SectionName, input.Variable, input.Value,
				))
			},
			describe: pathDescription("Update a mod toggle key."),
		})
		register(desktopAction{
			definition: DesktopActionDefinition{
				ID:          "mod.classify_merge",
				Description: "Classify mod packs for merging.",
				Domain:      "mod",
				Risk:        ActionRiskRead,
				Scopes:      []string{"global", "mod"},
				InputSchema: object(map[string]any{
					"paths": pathReferenceArraySchema(),
				}, "paths"),
			},
			execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Paths []agentPathReference `json:"paths"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				paths, err := resolveActionPaths(actionCtx.sandbox, input.Paths)
				if err != nil {
					return nil, err
				}
				return deps.mod.ClassifyMergePacks(ctx, paths)
			},
		})
		register(desktopAction{
			definition: DesktopActionDefinition{ID: "mod.merge", Description: "Merge local mod packs.", Domain: "mod",
				Risk: ActionRiskConfirm, Scopes: []string{"global", "mod"}, InputSchema: object(map[string]any{
					"group": pathReferenceSchema(), "placement": stringSchema(), "packName": stringSchema(),
					"root": map[string]any{"type": "object"},
				}, "group", "placement", "packName", "root")},
			execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
				request, err := decodeAgentMergeRequest(actionCtx.sandbox, raw)
				if err != nil {
					return nil, err
				}
				return deps.mod.MergeMods(ctx, request)
			},
			describe: func(actionCtx desktopActionContext, raw json.RawMessage) (string, string, error) {
				request, err := decodeAgentMergeRequest(actionCtx.sandbox, raw)
				return "Merge local mod packs.", request.GroupPath, err
			},
		})
		register(simpleAction("mod.get_compression", "Get mod compression state.", "mod", ActionRiskRead, object(nil),
			func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.mod.GetCompressionState(ctx)
			}))
		register(simpleAction("mod.set_compression", "Set mod compression configuration.", "mod", ActionRiskConfirm,
			object(map[string]any{"enabled": booleanSchema(), "method": enumSchema("zstd", "xpress4k"),
				"thresholdMiB": integerSchema(1, 64)}, "enabled", "method", "thresholdMiB"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Enabled      bool   `json:"enabled"`
					Method       string `json:"method"`
					ThresholdMiB int    `json:"thresholdMiB"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				if _, err := deps.mod.SetCompressionConfig(
					ctx,
					modservice.CompressionConfig{Method: input.Method, ThresholdMiB: input.ThresholdMiB},
				); err != nil {
					return nil, err
				}
				return deps.mod.SetCompressionEnabled(ctx, input.Enabled)
			}))
	}

	if deps.settings != nil {
		register(
			simpleAction("settings.get", "Read an allowlisted local application setting.", "settings", ActionRiskRead,
				object(map[string]any{"key": enumSchema(allowedAgentSettingKeys()...)}, "key"),
				func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
					var input struct {
						Key string `json:"key"`
					}
					if err := decodeActionArguments(raw, &input); err != nil {
						return nil, err
					}
					if !isAllowedAgentSetting(input.Key) {
						return nil, fmt.Errorf("setting %q is not available to the agent", input.Key)
					}
					value, err := deps.settings.Get(ctx, input.Key)
					return map[string]any{"key": input.Key, "value": value}, err
				}),
		)
		settingAction := simpleAction(
			"settings.set",
			"Change an allowlisted local application setting.",
			"settings",
			ActionRiskWrite,
			object(
				map[string]any{"key": enumSchema(allowedAgentSettingKeys()...), "value": map[string]any{}},
				"key",
				"value",
			),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Key   string `json:"key"`
					Value any    `json:"value"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				if !isAllowedAgentSetting(input.Key) {
					return nil, fmt.Errorf("setting %q is not available to the agent", input.Key)
				}
				if err := deps.settings.Set(ctx, input.Key, input.Value); err != nil {
					return nil, err
				}
				return map[string]any{"key": input.Key, "value": input.Value}, nil
			},
		)
		settingAction.risk = func(raw json.RawMessage) ActionRisk {
			var input struct {
				Key string `json:"key"`
			}
			if json.Unmarshal(raw, &input) == nil && agentConfirmSettingKeys[input.Key] {
				return ActionRiskConfirm
			}
			return ActionRiskWrite
		}
		settingAction.validate = func(raw json.RawMessage) error {
			var input struct {
				Key   string `json:"key"`
				Value any    `json:"value"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return err
			}
			return validateAgentSettingValue(input.Key, input.Value)
		}
		register(settingAction)
	}

	if deps.transfer != nil {
		register(
			simpleAction("transfer.list", "List local transfer queue entries.", "transfer", ActionRiskRead, object(nil),
				func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
					return deps.transfer.List(), nil
				}),
		)
		for _, item := range []struct {
			id, description string
			risk            ActionRisk
			run             func(string) error
		}{
			{"transfer.pause", "Pause a transfer.", ActionRiskWrite, deps.transfer.Pause},
			{"transfer.resume", "Resume a transfer.", ActionRiskWrite, deps.transfer.Resume},
			{"transfer.retry", "Retry a transfer.", ActionRiskWrite, deps.transfer.Retry},
			{"transfer.cancel", "Cancel a transfer.", ActionRiskConfirm, deps.transfer.Cancel},
		} {
			register(idAction(item.id, item.description, "pid", item.risk,
				func(_ context.Context, id string) (any, error) { return actionOK(item.run(id)) }))
		}
		register(
			simpleAction("transfer.pause_all", "Pause all active transfers.", "transfer", ActionRiskWrite, object(nil),
				func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
					return actionOK(deps.transfer.PauseAll())
				}),
		)
		register(
			simpleAction(
				"transfer.resume_all",
				"Resume all paused transfers.",
				"transfer",
				ActionRiskWrite,
				object(nil),
				func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
					return actionOK(deps.transfer.ResumeAll())
				},
			),
		)
		register(
			simpleAction("transfer.clear", "Remove completed transfers from the queue.", "transfer", ActionRiskConfirm,
				object(nil), func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
					return actionOK(deps.transfer.Clear())
				}),
		)
	}

	if deps.xxmi != nil {
		register(simpleAction("xxmi.get_path", "Get the configured XXMI path.", "xxmi", ActionRiskRead, object(nil),
			func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.xxmi.GetXXMIPath(ctx)
			}))
		register(
			simpleAction("xxmi.get_config", "Get the local XXMI configuration.", "xxmi", ActionRiskRead, object(nil),
				func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
					return deps.xxmi.GetXXMIConfig(ctx)
				}),
		)
		register(simpleAction("xxmi.get_data", "Get locally cached XXMI data.", "xxmi", ActionRiskRead, object(nil),
			func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.xxmi.GetXXMIData(ctx)
			}))
		register(simpleAction("xxmi.get_enabled_importers", "List enabled XXMI importers.", "xxmi", ActionRiskRead,
			object(nil), func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.xxmi.GetEnabledImporters(ctx)
			}))
		register(simpleAction("xxmi.start_game", "Start a configured game through XXMI.", "xxmi", ActionRiskConfirm,
			object(map[string]any{"importer": stringSchema()}, "importer"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Importer string `json:"importer"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return actionOK(deps.xxmi.StartGame(ctx, input.Importer))
			}))
		register(simpleAction("xxmi.install_dll", "Download and install a selected XXMI DLL package.", "xxmi",
			ActionRiskConfirm, object(map[string]any{"version": stringSchema()}, "version"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input xxmi.InstallDLLVersionInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return actionOK(deps.xxmi.InstallDLLVersion(ctx, input))
			}))
		register(simpleAction(
			"xxmi.disable_genshin_dcr",
			"Disable Genshin dynamic character resolution.",
			"xxmi",
			ActionRiskConfirm,
			object(nil),
			func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return actionOK(deps.xxmi.DisableGenshinDynamicCharacterResolution(ctx))
			},
		))
	}

	if deps.tools != nil {
		register(
			simpleAction("tools.get_scripts", "List installed local fix scripts.", "tools", ActionRiskRead, object(nil),
				func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
					return deps.tools.GetScripts(ctx)
				}),
		)
		register(
			simpleAction("tools.get_presets", "List installed local fix presets.", "tools", ActionRiskRead, object(nil),
				func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
					return deps.tools.GetPresets(ctx)
				}),
		)
		register(pathAction("tools.inspect_fixes", "Inspect a mod for applicable local fixes.", ActionRiskRead,
			func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string `json:"rootId"`
					RelativePath string `json:"relativePath"`
					Importer     string `json:"importer"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.DiagnoseModForFix(ctx, path, input.Importer)
			}))
		register(
			pathIDAction(
				"tools.run_script",
				"Run an installed fix script against a mod.",
				"scriptId",
				ActionRiskConfirm,
				func(ctx context.Context, path, id string) (any, error) {
					return actionOK(deps.tools.RunScript(ctx, id, path))
				},
			),
		)
		register(
			pathIDAction(
				"tools.run_preset",
				"Run an installed fix preset against a mod.",
				"presetId",
				ActionRiskConfirm,
				func(ctx context.Context, path, id string) (any, error) {
					return actionOK(deps.tools.RunPreset(ctx, id, path))
				},
			),
		)
		register(simpleAction("tools.bisect_state", "Get the current mod bisect state.", "tools", ActionRiskRead,
			object(nil), func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
				return deps.tools.BisectGetState(), nil
			}))
		register(simpleAction("tools.bisect_start", "Start local mod bisect.", "tools", ActionRiskConfirm,
			object(map[string]any{"game": stringSchema(), "excludePaths": pathReferenceListSchema()},
				"game", "excludePaths"),
			func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Game         string               `json:"game"`
					ExcludePaths []agentPathReference `json:"excludePaths"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				paths, err := resolveActionPaths(actionCtx.sandbox, input.ExcludePaths)
				if err != nil {
					return nil, err
				}
				return deps.tools.BisectStart(ctx, input.Game, paths)
			}))
		register(
			simpleAction("tools.bisect_respond", "Record whether the current bisect round fixed the problem.", "tools",
				ActionRiskWrite, object(map[string]any{"fixed": booleanSchema()}, "fixed"),
				func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
					var input struct {
						Fixed bool `json:"fixed"`
					}
					if err := decodeActionArguments(raw, &input); err != nil {
						return nil, err
					}
					return deps.tools.BisectRespond(ctx, input.Fixed)
				}),
		)
		register(simpleAction("tools.bisect_undo", "Undo the last mod bisect round.", "tools", ActionRiskWrite,
			object(nil), func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.tools.BisectUndoLastRound(ctx)
			}))
		register(
			simpleAction("tools.bisect_cancel", "Cancel and roll back local mod bisect.", "tools", ActionRiskConfirm,
				object(nil), func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
					return deps.tools.BisectCancel(ctx)
				}),
		)
		register(pathAction("tools.list_textures", "List textures inside a mod.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.ListTextureMod(ctx, path, nil)
			}))
		register(simpleAction("tools.get_texture_settings", "Get texture resize settings.", "tools", ActionRiskRead,
			object(nil), func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.tools.GetTextureResizeSettings(ctx)
			}))
		register(simpleAction("tools.get_texture_resize_state", "Get the active texture resize state.", "tools",
			ActionRiskRead, object(nil), func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
				return deps.tools.GetTextureResizeState(), nil
			}))
		register(simpleAction("tools.get_texture_runtime_status", "Get installed texture upscaler runtime status.",
			"tools", ActionRiskRead, object(nil),
			func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
				return deps.tools.GetTextureUpscaleRuntimeStatus(ctx)
			}))
		register(simpleAction("tools.set_texture_settings", "Update texture resize settings.", "tools", ActionRiskWrite,
			object(map[string]any{"settings": map[string]any{"type": "object"}}, "settings"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Settings tools.TextureResizeSettingsPatch `json:"settings"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.SaveTextureResizeSettings(ctx, input.Settings)
			}))
		register(desktopAction{
			definition: DesktopActionDefinition{
				ID:          "tools.resize_textures",
				Description: "Resize textures inside a mod.",
				Domain:      "tools",
				Risk:        ActionRiskConfirm,
				Scopes:      []string{"global", "mod"},
				InputSchema: object(map[string]any{
					"rootId":       stringSchema(),
					"relativePath": stringSchema(),
					"settings":     map[string]any{"type": "object"},
				}, "rootId", "relativePath", "settings"),
			},
			execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string                      `json:"rootId"`
					RelativePath string                      `json:"relativePath"`
					Settings     tools.TextureResizeSettings `json:"settings"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				path, err := resolveActionPath(actionCtx.sandbox, input.RootID, input.RelativePath)
				if err != nil {
					return nil, err
				}
				return deps.tools.ResizeTextureMod(ctx, path, tools.TextureResizeModInput{Settings: input.Settings})
			},
			describe: pathDescription("Resize textures inside a mod."),
		})
		register(
			simpleAction(
				"tools.persist_logs",
				"Read persist-toggle watcher logs.",
				"tools",
				ActionRiskRead,
				object(nil),
				func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
					return deps.tools.GetPersistLogs(), nil
				},
			),
		)
		register(
			simpleAction("tools.start_persist_watcher", "Start the persist-toggle watcher.", "tools", ActionRiskWrite,
				object(nil), func(ctx context.Context, _ desktopActionContext, _ json.RawMessage) (any, error) {
					return actionOK(deps.tools.StartPersistWatcher(ctx))
				}),
		)
		register(
			simpleAction("tools.stop_persist_watcher", "Stop the persist-toggle watcher.", "tools", ActionRiskWrite,
				object(nil), func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
					return map[string]any{"stopped": deps.tools.StopPersistWatcher()}, nil
				}),
		)
		persistState := pathAction(
			"tools.persist_toggle_state",
			"Persist model-viewer toggle variables to a local INI file.",
			ActionRiskConfirm,
			func(_ context.Context, path string, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string         `json:"rootId"`
					RelativePath string         `json:"relativePath"`
					State        map[string]any `json:"state"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.PersistModelViewerToggleState(path, input.State)
			},
		)
		persistState.definition.InputSchema = object(map[string]any{
			"rootId": stringSchema(), "relativePath": stringSchema(), "state": map[string]any{"type": "object"},
		}, "rootId", "relativePath", "state")
		register(persistState)
		register(pathAction("tools.wuwa_status", "Get the prepared Wuwa fixer status.", ActionRiskRead,
			func(ctx context.Context, _ string, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string  `json:"rootId"`
					RelativePath string  `json:"relativePath"`
					Importer     *string `json:"importer,omitempty"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.WuwaFixerGetStatus(ctx, input.Importer)
			}))
		register(pathAction("tools.wuwa_backups", "List Wuwa fixer backups for a mod.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.WuwaFixerScanBackups(ctx, path)
			}))
		register(pathAction("tools.wuwa_backup_size", "Get Wuwa fixer backup count and size.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.WuwaFixerGetBackupSize(ctx, path)
			}))
		wuwaRun := pathAction("tools.wuwa_run", "Run the prepared Wuwa fixer against a mod.", ActionRiskConfirm,
			func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string                 `json:"rootId"`
					RelativePath string                 `json:"relativePath"`
					Options      tools.WuwaFixerOptions `json:"options"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return actionOK(deps.tools.WuwaFixerRun(ctx, path, input.Options))
			})
		wuwaRun.definition.InputSchema = object(map[string]any{
			"rootId": stringSchema(), "relativePath": stringSchema(), "options": map[string]any{"type": "object"},
		}, "rootId", "relativePath", "options")
		register(wuwaRun)
		register(pathIDAction("tools.wuwa_restore", "Restore a Wuwa fixer backup group.", "groupKey",
			ActionRiskConfirm, func(ctx context.Context, path, groupKey string) (any, error) {
				return actionOK(deps.tools.WuwaFixerRollbackToGroup(ctx, path, groupKey))
			}))
		register(pathAction("tools.wuwa_delete_backups", "Delete all Wuwa fixer backups for a mod.",
			ActionRiskConfirm, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return actionOK(deps.tools.WuwaFixerCleanBackups(ctx, path))
			}))
		register(pathAction("tools.zzmi_prepare", "Inspect a mod for prepared ZZMI fixer tools.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.ZZMIFixerPrepare(ctx, path, false)
			}))
		zzmiRun := pathIDAction("tools.zzmi_run", "Run a prepared ZZMI fixer tool.", "tool", ActionRiskConfirm,
			func(ctx context.Context, path, tool string) (any, error) {
				return deps.tools.ZZMIFixerRun(ctx, tools.ZZMIFixerRunInput{Path: path, Tool: tool})
			})
		zzmiRun.definition.InputSchema["properties"].(map[string]any)["tool"] = enumSchema("hash", "jane", "dialyn")
		register(zzmiRun)
		register(pathAction("tools.zzmi_backups", "List ZZMI fixer backup sessions.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.ZZMIFixerListBackups(ctx, path)
			}))
		register(pathIDAction("tools.zzmi_backup", "Get one ZZMI fixer backup session.", "sessionId",
			ActionRiskRead, func(ctx context.Context, path, sessionID string) (any, error) {
				return deps.tools.ZZMIFixerGetBackup(ctx, path, sessionID)
			}))
		zzmiRestore := pathAction("tools.zzmi_restore", "Restore files from a ZZMI fixer backup.", ActionRiskConfirm,
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
				return deps.tools.ZZMIFixerRestore(ctx, tools.ZZMIFixerRestoreInput{
					Path: path, SessionID: input.SessionID, EntryID: input.EntryID, Force: input.Force,
				})
			})
		zzmiRestore.definition.InputSchema = object(map[string]any{
			"rootId": stringSchema(), "relativePath": stringSchema(), "sessionId": stringSchema(),
			"entryId": nullableStringSchema(), "force": booleanSchema(),
		}, "rootId", "relativePath", "sessionId", "force")
		register(zzmiRestore)
		zzmiDelete := pathAction("tools.zzmi_delete_backup", "Delete a ZZMI fixer backup.", ActionRiskConfirm,
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
				return actionOK(deps.tools.ZZMIFixerDeleteBackup(ctx, tools.ZZMIFixerDeleteBackupInput{
					Path: path, SessionID: input.SessionID, EntryID: input.EntryID,
				}))
			})
		zzmiDelete.definition.InputSchema = object(map[string]any{
			"rootId": stringSchema(), "relativePath": stringSchema(), "sessionId": stringSchema(),
			"entryId": nullableStringSchema(),
		}, "rootId", "relativePath", "sessionId")
		register(zzmiDelete)
		register(pathAction("tools.zzmi_delete_all_backups", "Delete all ZZMI fixer backups for a mod.",
			ActionRiskConfirm, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return actionOK(deps.tools.ZZMIFixerDeleteAllBackups(ctx, path))
			}))
		register(pathAction("tools.touch_prepare", "Start a touch-profile session for a mod.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.TouchProfilePrepare(ctx, tools.TouchProfileLoadInput{ModPath: path})
			}))
		register(simpleAction("tools.touch_mesh", "Get a touch-profile mesh descriptor.", "tools", ActionRiskRead,
			object(map[string]any{"sessionId": stringSchema(), "componentId": stringSchema()},
				"sessionId", "componentId"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input tools.TouchProfilePreviewInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.TouchProfileGetMeshDescriptor(ctx, input)
			}))
		register(simpleAction("tools.touch_preview", "Get a touch-profile preview descriptor.", "tools", ActionRiskRead,
			object(map[string]any{"sessionId": stringSchema(), "componentId": stringSchema()},
				"sessionId", "componentId"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input tools.TouchProfilePreviewInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.TouchProfileGetPreviewDescriptor(ctx, input)
			}))
		register(simpleAction("tools.touch_analyze", "Analyze touch-profile components.", "tools", ActionRiskWrite,
			object(map[string]any{
				"sessionId": stringSchema(), "componentIds": stringArraySchema(),
				"mode": nullableStringSchema(), "boneSelections": map[string]any{"type": "array"},
				"weightThreshold": map[string]any{"type": "array"},
			}, "sessionId", "componentIds"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input tools.TouchProfileAnalyzeInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.TouchProfileAnalyzeComponents(ctx, input)
			}))
		register(simpleAction("tools.touch_update", "Update touch-profile zone settings.", "tools", ActionRiskWrite,
			object(map[string]any{"sessionId": stringSchema(), "changes": map[string]any{"type": "array"}},
				"sessionId", "changes"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input tools.TouchProfileUpdateZoneSettingsBatchInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.TouchProfileUpdateZoneSettingsBatch(ctx, input)
			}))
		register(sessionApplyAction("tools.touch_apply", "Apply a touch profile to local mod files.",
			deps.tools.TouchProfileApply))
		register(sessionApplyAction("tools.touch_regenerate", "Regenerate local mod files from a touch profile.",
			deps.tools.TouchProfileRegenerate))
		register(idAction("tools.touch_discard", "Discard a touch-profile draft.", "sessionId", ActionRiskWrite,
			func(ctx context.Context, id string) (any, error) { return deps.tools.TouchProfileDiscardDraft(ctx, id) }))
		register(idAction("tools.touch_close", "Close a touch-profile session.", "sessionId", ActionRiskWrite,
			func(ctx context.Context, id string) (any, error) { return deps.tools.TouchProfileCloseSession(ctx, id) }))
		touchRollback := twoPathAction(
			"tools.touch_rollback",
			"Roll back touch-profile generated mod files.",
			ActionRiskConfirm,
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
				return deps.tools.TouchProfileRollback(ctx, tools.TouchProfileRollbackInput{
					SessionID: input.SessionID, OutputModRoot: outputRoot, SourceModRoot: sourceRoot,
					ReenableSourceOnRollback: input.ReenableSourceOnRollback,
				})
			},
		)
		touchRollback.definition.InputSchema = object(map[string]any{
			"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
			"targetRootId": stringSchema(), "targetRelativePath": stringSchema(), "sessionId": stringSchema(),
			"reenableSourceOnRollback": booleanSchema(),
		}, "sourceRootId", "sourceRelativePath", "targetRootId", "targetRelativePath", "sessionId",
			"reenableSourceOnRollback")
		register(touchRollback)
		register(pathAction("tools.body_shape_load", "Load a mod into a body-shape session.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.BodyShapeLoadMod(ctx, path)
			}))
		register(pathAction("tools.model_viewer_load", "Load a mod model-viewer descriptor.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.LoadModViewer(ctx, path)
			}))
		register(pathAction("tools.model_viewer_grid_preview", "Load a model-viewer grid preview descriptor.",
			ActionRiskRead, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.LoadModGridPreview(ctx, path)
			}))
		register(simpleAction("tools.body_shape_mesh", "Get a mesh descriptor from a body-shape session.", "tools",
			ActionRiskRead, object(map[string]any{"sessionId": stringSchema(), "meshId": stringSchema()},
				"sessionId", "meshId"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input tools.BodyShapeMeshInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.BodyShapeGetMesh(ctx, input)
			}))
		register(idAction("tools.body_shape_close", "Close a body-shape session.", "sessionId", ActionRiskWrite,
			func(ctx context.Context, id string) (any, error) { return deps.tools.BodyShapeCloseSession(ctx, id) }))
		register(simpleAction("tools.body_shape_begin_export", "Prepare a body-shape export upload.", "tools",
			ActionRiskWrite, object(map[string]any{
				"sessionId": stringSchema(), "meshId": stringSchema(), "includeWeights": booleanSchema(),
			}, "sessionId", "meshId", "includeWeights"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input tools.BodyShapeBeginExportInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.BodyShapeBeginExport(ctx, input)
			}))
		register(simpleAction("tools.body_shape_commit_export", "Commit uploaded body-shape changes to mod files.",
			"tools", ActionRiskConfirm, object(map[string]any{
				"sessionId": stringSchema(), "exportId": stringSchema(),
				"amount": map[string]any{}, "axisScale": map[string]any{"type": "array"},
				"writeChangeLog": map[string]any{}, "changeSummary": map[string]any{"type": "object"},
			}, "sessionId", "exportId"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input tools.BodyShapeCommitExportInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.BodyShapeCommitExport(ctx, input)
			}))
		register(idAction("tools.model_viewer_cleanup", "Close a model-viewer session.", "sessionId", ActionRiskWrite,
			func(ctx context.Context, id string) (any, error) { return deps.tools.CleanupModelViewer(ctx, id) }))
		register(simpleAction("tools.model_viewer_ineffective_values", "Inspect ineffective model-viewer values.",
			"tools", ActionRiskRead, object(map[string]any{
				"sessionId": stringSchema(), "state": map[string]any{"type": "object"},
			}, "sessionId", "state"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					SessionID string         `json:"sessionId"`
					State     map[string]any `json:"state"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.GetModelViewerIneffectiveValues(ctx, input.SessionID, input.State)
			}))
		register(simpleAction("tools.fixer_4001_state", "Get local 4001 fixer state.", "tools", ActionRiskRead,
			object(nil), func(context.Context, desktopActionContext, json.RawMessage) (any, error) {
				return deps.tools.FourThousandOneFixerGetState(), nil
			}))
		register(pathAction("tools.fixer_4001_write_access", "Check importer write access for the 4001 fixer.",
			ActionRiskRead, func(_ context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.FourThousandOneFixerCheckImporterWriteAccess(
					tools.Fixer4001PathInput{ImporterPath: &path},
				), nil
			}))
		register(pathAction("tools.fixer_4001_diversification_state", "Inspect 4001 fixer diversification state.",
			ActionRiskRead, func(_ context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.FourThousandOneFixerGetDiversificationState(
					tools.Fixer4001PathInput{ImporterPath: &path},
				)
			}))
		fixerDiversify := pathIDAction("tools.fixer_4001_diversify", "Diversify the configured importer's DLL.",
			"importerKey", ActionRiskConfirm, func(ctx context.Context, path, importerKey string) (any, error) {
				return deps.tools.FourThousandOneFixerDiversifyDllPadding(ctx, tools.Fixer4001ImporterInput{
					ImporterKey: importerKey, ImporterPath: &path,
				}), nil
			})
		register(fixerDiversify)
		register(pathAction("tools.fixer_4001_restore", "Restore a diversified importer DLL backup.",
			ActionRiskConfirm, func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.tools.FourThousandOneFixerRestoreDiversifiedDll(
					ctx, tools.Fixer4001PathInput{ImporterPath: &path},
				), nil
			}))
		fixerBuild := pathAction("tools.fixer_4001_build", "Build and apply a local 4001 fixer DLL.",
			ActionRiskConfirm, func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
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
				return deps.tools.FourThousandOneFixerBuildDll(ctx, tools.Fixer4001BuildInput{
					Provider:     input.Provider,
					Version:      input.Version,
					ImporterKey:  input.ImporterKey,
					ImporterPath: &path,
				}), nil
			})
		fixerBuild.definition.InputSchema = object(map[string]any{
			"rootId": stringSchema(), "relativePath": stringSchema(), "provider": stringSchema(),
			"version": stringSchema(), "importerKey": stringSchema(),
		}, "rootId", "relativePath", "provider", "version", "importerKey")
		register(fixerBuild)
		register(simpleAction("tools.bisect_finalize", "Finish mod bisect and keep selected paths disabled.", "tools",
			ActionRiskConfirm, object(map[string]any{"keepDisabled": pathReferenceListSchema()}, "keepDisabled"),
			func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					KeepDisabled []agentPathReference `json:"keepDisabled"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				paths, err := resolveActionPaths(actionCtx.sandbox, input.KeepDisabled)
				if err != nil {
					return nil, err
				}
				return deps.tools.BisectFinalize(ctx, paths)
			}))
		register(simpleAction("tools.bisect_recover", "Recover an interrupted mod bisect session.", "tools",
			ActionRiskConfirm, object(map[string]any{"game": stringSchema()}, "game"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Game string `json:"game"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.tools.BisectRecover(ctx, input.Game)
			}))
	}

	if deps.menuMaker != nil {
		register(simpleAction("menumaker.parse", "Parse MenuMaker source text.", "menumaker", ActionRiskRead,
			object(map[string]any{"text": stringSchema()}, "text"),
			func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Text string `json:"text"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.menuMaker.Parse(ctx, input.Text)
			}))
		register(pathAction("menumaker.load", "Load and parse a MenuMaker source file.", ActionRiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return deps.menuMaker.LoadSource(ctx, path)
			}))
		register(pathAction("menumaker.scan", "Scan a local folder for MenuMaker sources.", ActionRiskRead,
			func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string `json:"rootId"`
					RelativePath string `json:"relativePath"`
					IncludeTXT   bool   `json:"includeTxt"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.menuMaker.ScanFolder(ctx, path, input.IncludeTXT)
			}))
		generate := pathAction("menumaker.generate", "Generate MenuMaker output in memory.", ActionRiskRead,
			func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string                      `json:"rootId"`
					RelativePath string                      `json:"relativePath"`
					SourceText   string                      `json:"sourceText"`
					Slots        []menumaker.MenuMakerSlot   `json:"slots"`
					Settings     menumaker.MenuMakerSettings `json:"settings"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.menuMaker.Generate(ctx, menumaker.MenuMakerGenerateRequest{
					SourcePath: path, SourceText: input.SourceText, Slots: input.Slots, Settings: input.Settings,
				})
			})
		generate.definition.InputSchema = object(map[string]any{
			"rootId": stringSchema(), "relativePath": stringSchema(), "sourceText": stringSchema(),
			"slots": map[string]any{"type": "array"}, "settings": map[string]any{"type": "object"},
		}, "rootId", "relativePath", "sourceText", "slots", "settings")
		register(generate)
		register(menuMakerSaveAction("menumaker.save_ini", "Save generated MenuMaker INI output.",
			func(ctx context.Context, source, destination string, raw json.RawMessage) (any, error) {
				var input menuMakerSaveINIInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.menuMaker.SaveINI(ctx, menumaker.MenuMakerSaveINIRequest{
					SourcePath: source, DestinationPath: destination, SourceText: input.SourceText,
					Slots: input.Slots, Settings: input.Settings, Encoding: input.Encoding,
					HasBOM: input.HasBOM, Newline: input.Newline,
				})
			}))
		register(menuMakerSaveAction("menumaker.save_zip", "Save generated MenuMaker ZIP output.",
			func(ctx context.Context, source, destination string, raw json.RawMessage) (any, error) {
				var input menuMakerSaveZIPInput
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.menuMaker.SaveZIP(ctx, menumaker.MenuMakerSaveZIPRequest{
					SourcePath: source, DestinationPath: destination, OutputININame: input.OutputININame,
					SourceText: input.SourceText, Slots: input.Slots, Settings: input.Settings,
					Encoding: input.Encoding, HasBOM: input.HasBOM, Newline: input.Newline, Assets: input.Assets,
				})
			}))
	}

	return registry
}

func (r *desktopActionRegistry) register(action desktopAction) error {
	definition := &action.definition
	if strings.TrimSpace(definition.ID) == "" || strings.TrimSpace(definition.Domain) == "" ||
		strings.TrimSpace(definition.Description) == "" {
		return errors.New("desktop action requires id, domain, and description")
	}
	if definition.Risk != ActionRiskRead && definition.Risk != ActionRiskWrite && definition.Risk != ActionRiskConfirm {
		return fmt.Errorf("desktop action %q has invalid risk %q", definition.ID, definition.Risk)
	}
	if definition.InputSchema["type"] != "object" || definition.InputSchema["additionalProperties"] != false {
		return fmt.Errorf("desktop action %q requires a strict object input schema", definition.ID)
	}
	if action.execute == nil {
		return fmt.Errorf("desktop action %q requires a handler", definition.ID)
	}
	if _, exists := r.actions[definition.ID]; exists {
		return fmt.Errorf("duplicate desktop action %q", definition.ID)
	}
	if definition.Scopes == nil {
		definition.Scopes = []string{"global", "mod"}
	}
	r.actions[definition.ID] = action
	return nil
}

func (r *desktopActionRegistry) Definitions(scope, domain, query string) []DesktopActionDefinition {
	domain, query = strings.ToLower(strings.TrimSpace(domain)), strings.ToLower(strings.TrimSpace(query))
	definitions := make([]DesktopActionDefinition, 0, len(r.actions))
	for _, action := range r.actions {
		definition := action.definition
		if domain != "" && definition.Domain != domain {
			continue
		}
		if !containsString(definition.Scopes, scope) {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(definition.ID+" "+definition.Description), query) {
			continue
		}
		definitions = append(definitions, definition)
	}
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	return definitions
}

func (r *desktopActionRegistry) Hints(scope string) []desktopActionHint {
	definitions := r.Definitions(scope, "", "")
	hints := make([]desktopActionHint, 0, len(definitions))
	for _, definition := range definitions {
		hints = append(hints, desktopActionHint{
			ID: definition.ID, Description: definition.Description, Risk: definition.Risk,
		})
	}
	return hints
}

func (r *desktopActionRegistry) Prepare(
	actionCtx desktopActionContext,
	call desktopActionCall,
) (desktopAction, ActionRisk, json.RawMessage, *approvalProposal, error) {
	action, ok := r.actions[call.ActionID]
	if !ok {
		return desktopAction{}, "", nil, nil, fmt.Errorf("unregistered desktop action %q", call.ActionID)
	}
	if !containsString(action.definition.Scopes, actionCtx.scope.Type) {
		return desktopAction{}, "", nil, nil, fmt.Errorf(
			"desktop action %q is unavailable in %s scope",
			call.ActionID,
			actionCtx.scope.Type,
		)
	}
	arguments := call.Arguments
	if len(arguments) == 0 || bytes.Equal(bytes.TrimSpace(arguments), []byte("null")) {
		arguments = json.RawMessage(`{}`)
	}
	var objectValue map[string]any
	decoder := json.NewDecoder(bytes.NewReader(arguments))
	decoder.UseNumber()
	if err := decoder.Decode(&objectValue); err != nil {
		return desktopAction{}, "", nil, nil, fmt.Errorf("decode desktop action arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return desktopAction{}, "", nil, nil, errors.New("decode desktop action arguments: multiple values")
	}
	canonical, err := json.Marshal(objectValue)
	if err != nil {
		return desktopAction{}, "", nil, nil, err
	}
	if err := validateActionSchema(action.definition.InputSchema, objectValue, "arguments"); err != nil {
		return desktopAction{}, "", nil, nil, err
	}
	if action.validate != nil {
		if err := action.validate(canonical); err != nil {
			return desktopAction{}, "", nil, nil, err
		}
	}
	risk := action.definition.Risk
	if action.risk != nil {
		risk = action.risk(canonical)
	}
	if risk != ActionRiskConfirm {
		return action, risk, canonical, nil, nil
	}
	summary, target := action.definition.Description, ""
	if action.describe != nil {
		summary, target, err = action.describe(actionCtx, canonical)
		if err != nil {
			return desktopAction{}, "", nil, nil, err
		}
	}
	return action, risk, canonical, &approvalProposal{
		ActionID:  call.ActionID,
		Arguments: canonical,
		Summary:   summary,
		Target:    target,
		Impact:    "This action can make broad or difficult-to-reverse local changes.",
		Kind:      "desktop",
	}, nil
}

func (r *desktopActionRegistry) Execute(
	ctx context.Context,
	actionCtx desktopActionContext,
	actionID string,
	arguments json.RawMessage,
) (any, error) {
	action, _, canonical, _, err := r.Prepare(actionCtx, desktopActionCall{ActionID: actionID, Arguments: arguments})
	if err != nil {
		return nil, err
	}
	return action.execute(ctx, actionCtx, canonical)
}

func simpleAction(
	id, description, domain string,
	risk ActionRisk,
	schema map[string]any,
	execute func(context.Context, desktopActionContext, json.RawMessage) (any, error),
) desktopAction {
	return desktopAction{
		definition: DesktopActionDefinition{ID: id, Description: description, Domain: domain, Risk: risk,
			Scopes: []string{"global", "mod"}, InputSchema: schema},
		execute: execute,
	}
}

func pathAction(
	id, description string,
	risk ActionRisk,
	execute func(context.Context, string, json.RawMessage) (any, error),
) desktopAction {
	return desktopAction{
		definition: DesktopActionDefinition{ID: id, Description: description, Domain: strings.SplitN(id, ".", 2)[0],
			Risk: risk, Scopes: []string{"global", "mod"}, InputSchema: objectSchema(map[string]any{
				"rootId": stringSchema(), "relativePath": stringSchema(), "importer": stringSchema(),
			}, "rootId", "relativePath")},
		execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			path, err := resolveActionPath(actionCtx.sandbox, input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			return execute(ctx, path, raw)
		},
		describe: pathDescription(description),
	}
}

func pathIDAction(
	id, description, idField string,
	risk ActionRisk,
	execute func(context.Context, string, string) (any, error),
) desktopAction {
	action := pathAction(
		id,
		description,
		risk,
		func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input map[string]json.RawMessage
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			var value string
			if err := json.Unmarshal(input[idField], &value); err != nil || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("%s is required", idField)
			}
			return execute(ctx, path, value)
		},
	)
	action.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), idField: stringSchema(),
	}, "rootId", "relativePath", idField)
	return action
}

func twoPathAction(
	id, description string,
	risk ActionRisk,
	execute func(context.Context, string, string, json.RawMessage) (any, error),
) desktopAction {
	action := desktopAction{
		definition: DesktopActionDefinition{ID: id, Description: description, Domain: strings.SplitN(id, ".", 2)[0],
			Risk: risk, Scopes: []string{"global", "mod"}, InputSchema: objectSchema(map[string]any{
				"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
				"targetRootId": stringSchema(), "targetRelativePath": stringSchema(),
			}, "sourceRootId", "sourceRelativePath", "targetRootId", "targetRelativePath")},
		execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
			var input struct {
				SourceRootID       string `json:"sourceRootId"`
				SourceRelativePath string `json:"sourceRelativePath"`
				TargetRootID       string `json:"targetRootId"`
				TargetRelativePath string `json:"targetRelativePath"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			source, err := resolveActionPath(actionCtx.sandbox, input.SourceRootID, input.SourceRelativePath)
			if err != nil {
				return nil, err
			}
			target, err := resolveActionPath(actionCtx.sandbox, input.TargetRootID, input.TargetRelativePath)
			if err != nil {
				return nil, err
			}
			return execute(ctx, source, target, raw)
		},
		describe: func(actionCtx desktopActionContext, raw json.RawMessage) (string, string, error) {
			var input struct {
				SourceRootID       string `json:"sourceRootId"`
				SourceRelativePath string `json:"sourceRelativePath"`
				TargetRootID       string `json:"targetRootId"`
				TargetRelativePath string `json:"targetRelativePath"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return "", "", err
			}
			source, err := resolveActionPath(actionCtx.sandbox, input.SourceRootID, input.SourceRelativePath)
			if err != nil {
				return "", "", err
			}
			target, err := resolveActionPath(actionCtx.sandbox, input.TargetRootID, input.TargetRelativePath)
			return description, source + " → " + target, err
		},
	}
	return action
}

func idAction(
	id, description, idField string,
	risk ActionRisk,
	execute func(context.Context, string) (any, error),
) desktopAction {
	return simpleAction(id, description, strings.SplitN(id, ".", 2)[0], risk,
		objectSchema(map[string]any{idField: stringSchema()}, idField),
		func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
			var input map[string]json.RawMessage
			if err := json.Unmarshal(raw, &input); err != nil {
				return nil, err
			}
			var value string
			if err := json.Unmarshal(input[idField], &value); err != nil || strings.TrimSpace(value) == "" {
				return nil, fmt.Errorf("%s is required", idField)
			}
			return execute(ctx, value)
		})
}

func sessionApplyAction(
	id, description string,
	execute func(context.Context, tools.TouchProfileApplyInput) (tools.TouchApplyResult, error),
) desktopAction {
	return simpleAction(id, description, "tools", ActionRiskConfirm,
		objectSchema(map[string]any{"sessionId": stringSchema(), "force": booleanSchema()}, "sessionId"),
		func(ctx context.Context, _ desktopActionContext, raw json.RawMessage) (any, error) {
			var input tools.TouchProfileApplyInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return execute(ctx, input)
		})
}

func menuMakerSaveAction(
	id, description string,
	execute func(context.Context, string, string, json.RawMessage) (any, error),
) desktopAction {
	properties := map[string]any{
		"sourceRootId": stringSchema(), "sourceRelativePath": stringSchema(),
		"destinationRootId": stringSchema(), "destinationRelativePath": stringSchema(),
		"sourceText": stringSchema(), "slots": map[string]any{"type": "array"},
		"settings": map[string]any{"type": "object"}, "encoding": stringSchema(),
		"hasBOM": booleanSchema(), "newline": stringSchema(),
	}
	required := []string{
		"sourceRootId", "sourceRelativePath", "destinationRootId", "destinationRelativePath",
		"sourceText", "slots", "settings", "encoding", "hasBOM", "newline",
	}
	if id == "menumaker.save_zip" {
		properties["outputININame"] = stringSchema()
		properties["assets"] = map[string]any{"type": "array"}
		required = append(required, "outputININame", "assets")
	}
	return desktopAction{
		definition: DesktopActionDefinition{ID: id, Description: description, Domain: "menumaker",
			Risk: ActionRiskConfirm, Scopes: []string{"global", "mod"},
			InputSchema: objectSchema(properties, required...)},
		execute: func(ctx context.Context, actionCtx desktopActionContext, raw json.RawMessage) (any, error) {
			var paths struct {
				SourceRootID            string `json:"sourceRootId"`
				SourceRelativePath      string `json:"sourceRelativePath"`
				DestinationRootID       string `json:"destinationRootId"`
				DestinationRelativePath string `json:"destinationRelativePath"`
			}
			if err := json.Unmarshal(raw, &paths); err != nil {
				return nil, err
			}
			source, err := resolveActionPath(actionCtx.sandbox, paths.SourceRootID, paths.SourceRelativePath)
			if err != nil {
				return nil, err
			}
			destination, err := resolveActionOutputPath(
				actionCtx.sandbox,
				paths.DestinationRootID,
				paths.DestinationRelativePath,
			)
			if err != nil {
				return nil, err
			}
			return execute(ctx, source, destination, raw)
		},
		describe: func(actionCtx desktopActionContext, raw json.RawMessage) (string, string, error) {
			var paths struct {
				DestinationRootID       string `json:"destinationRootId"`
				DestinationRelativePath string `json:"destinationRelativePath"`
			}
			if err := json.Unmarshal(raw, &paths); err != nil {
				return "", "", err
			}
			destination, err := resolveActionOutputPath(
				actionCtx.sandbox,
				paths.DestinationRootID,
				paths.DestinationRelativePath,
			)
			return description, destination, err
		},
	}
}

func resolveActionPath(sandbox *Sandbox, rootID, relativePath string) (string, error) {
	if sandbox == nil {
		return "", errors.New("agent sandbox is unavailable")
	}
	root, clean, err := sandbox.resolve(rootID, relativePath)
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

func resolveActionOutputPath(sandbox *Sandbox, rootID, relativePath string) (string, error) {
	if sandbox == nil {
		return "", errors.New("agent sandbox is unavailable")
	}
	root, clean, err := sandbox.resolve(rootID, relativePath)
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

func resolveActionPaths(sandbox *Sandbox, references []agentPathReference) ([]string, error) {
	paths := make([]string, len(references))
	for index, reference := range references {
		path, err := resolveActionPath(sandbox, reference.RootID, reference.RelativePath)
		if err != nil {
			return nil, fmt.Errorf("resolve path %d: %w", index, err)
		}
		paths[index] = path
	}
	return paths, nil
}

func decodeAgentMergeRequest(sandbox *Sandbox, raw json.RawMessage) (modservice.MergeModsRequest, error) {
	var input agentMergeRequest
	if err := decodeActionArguments(raw, &input); err != nil {
		return modservice.MergeModsRequest{}, err
	}
	groupPath, err := resolveActionPath(sandbox, input.Group.RootID, input.Group.RelativePath)
	if err != nil {
		return modservice.MergeModsRequest{}, err
	}
	root, err := resolveAgentMergeNode(sandbox, input.Root)
	if err != nil {
		return modservice.MergeModsRequest{}, err
	}
	return modservice.MergeModsRequest{
		GroupPath: groupPath,
		Placement: input.Placement,
		PackName:  input.PackName,
		Root:      root,
	}, nil
}

func resolveAgentMergeNode(sandbox *Sandbox, input agentMergePlanNode) (modservice.MergePlanNode, error) {
	node := modservice.MergePlanNode{
		Kind: input.Kind, ID: input.ID, Engine: input.Engine, Name: input.Name,
		ForwardKey: input.ForwardKey, BackKey: input.BackKey, IncludeVanilla: input.IncludeVanilla,
		Children: make([]modservice.MergePlanNode, len(input.Children)),
	}
	if input.Kind == "leaf" {
		path, err := resolveActionPath(sandbox, input.RootID, input.RelativePath)
		if err != nil {
			return modservice.MergePlanNode{}, err
		}
		node.Path = path
	}
	for index, child := range input.Children {
		resolved, err := resolveAgentMergeNode(sandbox, child)
		if err != nil {
			return modservice.MergePlanNode{}, fmt.Errorf("resolve merge child %d: %w", index, err)
		}
		node.Children[index] = resolved
	}
	return node, nil
}

func pathDescription(summary string) func(desktopActionContext, json.RawMessage) (string, string, error) {
	return func(actionCtx desktopActionContext, raw json.RawMessage) (string, string, error) {
		var input struct {
			RootID       string `json:"rootId"`
			RelativePath string `json:"relativePath"`
		}
		if err := json.Unmarshal(raw, &input); err != nil {
			return "", "", err
		}
		path, err := resolveActionPath(actionCtx.sandbox, input.RootID, input.RelativePath)
		return summary, path, err
	}
}

func decodeActionArguments(raw json.RawMessage, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode action arguments: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("decode action arguments: multiple values")
	}
	return nil
}

func actionOK(err error) (any, error) { return map[string]any{"completed": err == nil}, err }

func stringSchema() map[string]any         { return map[string]any{"type": "string", "minLength": 1} }
func nullableStringSchema() map[string]any { return map[string]any{"type": []string{"string", "null"}} }
func booleanSchema() map[string]any        { return map[string]any{"type": "boolean"} }
func integerSchema(minimum, maximum int) map[string]any {
	return map[string]any{"type": "integer", "minimum": minimum, "maximum": maximum}
}
func enumSchema(values ...string) map[string]any {
	return map[string]any{"type": "string", "enum": values}
}
func stringArraySchema() map[string]any {
	return map[string]any{"type": "array", "items": stringSchema()}
}

func pathReferenceSchema() map[string]any {
	return objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(),
	}, "rootId", "relativePath")
}

func pathReferenceArraySchema() map[string]any {
	return map[string]any{"type": "array", "minItems": 1, "items": pathReferenceSchema()}
}

func pathReferenceListSchema() map[string]any {
	return map[string]any{"type": "array", "items": pathReferenceSchema()}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validateActionSchema(schema map[string]any, value any, path string) error {
	typeName, _ := schema["type"].(string)
	switch typeName {
	case "object":
		objectValue, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]string); ok {
			for _, name := range required {
				if _, exists := objectValue[name]; !exists {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		} else if required, ok := schema["required"].([]any); ok {
			for _, item := range required {
				name, _ := item.(string)
				if _, exists := objectValue[name]; name != "" && !exists {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		}
		for name, child := range objectValue {
			childSchema, exists := properties[name]
			if !exists {
				if schema["additionalProperties"] == false {
					return fmt.Errorf("%s.%s is not allowed", path, name)
				}
				continue
			}
			if typedSchema, ok := childSchema.(map[string]any); ok && len(typedSchema) > 0 {
				if err := validateActionSchema(typedSchema, child, path+"."+name); err != nil {
					return err
				}
			}
		}
	case "string":
		stringValue, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if minimum, ok := schema["minLength"].(int); ok && len(stringValue) < minimum {
			return fmt.Errorf("%s must not be empty", path)
		}
		if enum, ok := schema["enum"].([]string); ok && !containsString(enum, stringValue) {
			return fmt.Errorf("%s has unsupported value %q", path, stringValue)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		integer, err := number.Int64()
		if err != nil {
			return fmt.Errorf("%s must be an integer", path)
		}
		if minimum, ok := schema["minimum"].(int); ok && integer < int64(minimum) {
			return fmt.Errorf("%s must be at least %d", path, minimum)
		}
		if maximum, ok := schema["maximum"].(int); ok && integer > int64(maximum) {
			return fmt.Errorf("%s must be at most %d", path, maximum)
		}
	case "array":
		items, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if minimum, ok := schema["minItems"].(int); ok && len(items) < minimum {
			return fmt.Errorf("%s must contain at least %d item(s)", path, minimum)
		}
		itemSchema, _ := schema["items"].(map[string]any)
		for index, item := range items {
			if err := validateActionSchema(itemSchema, item, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case "":
		if types, ok := schema["type"].([]string); ok {
			if value == nil && containsString(types, "null") {
				return nil
			}
			if stringValue, ok := value.(string); ok && containsString(types, "string") {
				_ = stringValue
				return nil
			}
			return fmt.Errorf("%s has an unsupported type", path)
		}
	}
	return nil
}

var agentSettingKeys = map[string]bool{
	setting.KeyGeneralLanguage: true, setting.KeyGeneralRunOnStartup: true, setting.KeyGeneralRunInBackground: true,
	setting.KeyGeneralDefaultStartPage: true, setting.KeyGeneralLogLevel: true,
	setting.KeyGeneralMoveTransferPageWhenStartTransfer: true, setting.KeyGeneralPowerSaveBlockInTransfer: true,
	setting.KeyGeneralBisectPreserveD3dx: true, setting.KeyModArchiveExtractPathMode: true,
	setting.KeyModDeleteArchiveAfterExtract: true, setting.KeyModMoveFolderInsteadOfCopy: true,
	setting.KeyModSearchModPreview: true, setting.KeyModCopyShaderFixesOnEnable: true,
	setting.KeyModSidebarLayout: true, setting.KeyModCharacterSidebarWidth: true, setting.KeyModGridLayoutMode: true,
	setting.KeyModGridModelPreview: true, setting.KeyModGridResponsiveBaseWidth: true,
	setting.KeyModGridFixedCardWidth: true, setting.KeyModGridFixedColumnCount: true,
	setting.KeyModDisabledPrefixStyle: true, setting.KeyModAutoInspectFix: true,
	setting.KeyTransferDownloadConcurrency: true, setting.KeyTransferDownloadBandwidthLimitMibps: true,
	setting.KeyTransferUploadConcurrency: true, setting.KeyModelViewerToneMapping: true,
	setting.KeyModelViewerEnvironment: true, setting.KeyModelViewerExposure: true,
	setting.KeyModelViewerToonShadows: true, setting.KeyXXMIPersistToggles: true,
}

var agentConfirmSettingKeys = map[string]bool{
	setting.KeyGeneralRunOnStartup:    true,
	setting.KeyModelViewerEnvironment: true,
}

func allowedAgentSettingKeys() []string {
	keys := make([]string, 0, len(agentSettingKeys))
	for key := range agentSettingKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func isAllowedAgentSetting(key string) bool { return agentSettingKeys[key] }

func validateAgentSettingValue(key string, value any) error {
	if !isAllowedAgentSetting(key) {
		return fmt.Errorf("setting %q is not available to the agent", key)
	}
	if agentBooleanSettingKeys[key] {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("setting %q requires a boolean value", key)
		}
		return nil
	}
	if agentIntegerSettingKeys[key] {
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number {
			return fmt.Errorf("setting %q requires an integer value", key)
		}
		return nil
	}
	if key == setting.KeyModelViewerExposure {
		number, ok := value.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return fmt.Errorf("setting %q requires a finite number", key)
		}
		return nil
	}
	if stringValue, ok := value.(string); !ok || strings.TrimSpace(stringValue) == "" {
		return fmt.Errorf("setting %q requires a non-empty string value", key)
	}
	return nil
}

var agentBooleanSettingKeys = map[string]bool{
	setting.KeyGeneralRunOnStartup: true, setting.KeyGeneralRunInBackground: true,
	setting.KeyGeneralMoveTransferPageWhenStartTransfer: true,
	setting.KeyGeneralPowerSaveBlockInTransfer:          true,
	setting.KeyGeneralBisectPreserveD3dx:                true,
	setting.KeyModDeleteArchiveAfterExtract:             true,
	setting.KeyModMoveFolderInsteadOfCopy:               true,
	setting.KeyModSearchModPreview:                      true,
	setting.KeyModCopyShaderFixesOnEnable:               true,
	setting.KeyModGridModelPreview:                      true,
	setting.KeyModAutoInspectFix:                        true,
	setting.KeyModelViewerToonShadows:                   true,
	setting.KeyXXMIPersistToggles:                       true,
}

var agentIntegerSettingKeys = map[string]bool{
	setting.KeyModCharacterSidebarWidth: true, setting.KeyModGridResponsiveBaseWidth: true,
	setting.KeyModGridFixedCardWidth: true, setting.KeyModGridFixedColumnCount: true,
	setting.KeyTransferDownloadConcurrency: true, setting.KeyTransferDownloadBandwidthLimitMibps: true,
	setting.KeyTransferUploadConcurrency: true,
}
