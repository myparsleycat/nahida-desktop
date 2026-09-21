package actions

import (
	"context"
	"encoding/json"

	"nahida.live/desktop/internal/xxmi"
)

// registerXXMIActions registers the XXMI actions.
func registerXXMIActions(registry *Registry, deps Dependencies) {
	if deps.XXMI == nil {
		return
	}
	registry.add(simpleAction("xxmi.get_path", "Get the configured XXMI path.", "xxmi", RiskRead, objectSchema(nil),
		func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.XXMI.GetXXMIPath(ctx)
		}))
	registry.add(
		simpleAction("xxmi.get_config", "Get the local XXMI configuration.", "xxmi", RiskRead, objectSchema(nil),
			func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
				return deps.XXMI.GetXXMIConfig(ctx)
			}),
	)
	registry.add(simpleAction("xxmi.get_data", "Get locally cached XXMI data.", "xxmi", RiskRead, objectSchema(nil),
		func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.XXMI.GetXXMIData(ctx)
		}))
	registry.add(simpleAction("xxmi.get_enabled_importers", "List enabled XXMI importers.", "xxmi", RiskRead,
		objectSchema(nil), func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return deps.XXMI.GetEnabledImporters(ctx)
		}))
	registry.add(simpleAction("xxmi.start_game", "Start a configured game through XXMI.", "xxmi", RiskConfirm,
		objectSchema(map[string]any{"importer": stringSchema()}, "importer"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Importer string `json:"importer"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return actionOK(deps.XXMI.StartGame(ctx, input.Importer))
		}))
	registry.add(simpleAction("xxmi.install_dll", "Download and install a selected XXMI DLL package.", "xxmi",
		RiskConfirm, objectSchema(map[string]any{"version": stringSchema()}, "version"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input xxmi.InstallDLLVersionInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return actionOK(deps.XXMI.InstallDLLVersion(ctx, input))
		}))
	registry.add(simpleAction(
		"xxmi.install_importer_package",
		"Download and install a selected XXMI importer package version.",
		"xxmi",
		RiskConfirm,
		objectSchema(map[string]any{"importer": stringSchema(), "version": stringSchema()}, "importer", "version"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input xxmi.InstallImporterPackageInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return actionOK(deps.XXMI.InstallImporterPackage(ctx, input))
		},
	))
	registry.add(simpleAction(
		"xxmi.disable_genshin_dcr",
		"Disable Genshin dynamic character resolution.",
		"xxmi",
		RiskConfirm,
		objectSchema(nil),
		func(ctx context.Context, _ actionContext, _ json.RawMessage) (any, error) {
			return actionOK(deps.XXMI.DisableGenshinDynamicCharacterResolution(ctx))
		},
	))
	registry.add(simpleAction(
		"xxmi.clear_launch_blockers",
		"Turn off Genshin dynamic character resolution and NVIDIA Smooth Motion when they would block launching the game.",
		"xxmi",
		RiskConfirm,
		objectSchema(map[string]any{"importer": stringSchema()}, "importer"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Importer string `json:"importer"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return actionOK(deps.XXMI.ClearLaunchBlockers(ctx, input.Importer))
		},
	))
}
