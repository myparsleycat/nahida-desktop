package actions

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"

	"nahida.live/desktop/internal/tools/texture"
)

func registerTextureDiagnosticActions(registry *Registry) {
	registry.add(
		pathAction(
			"tools.inspect_dds",
			"Inspect DDS format, hash, stored RGBA histograms and conditional sRGB/8-bit transparency loss.",
			RiskRead,
			func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
				return texture.InspectDDS(ctx, path)
			},
		),
	)
	registry.add(
		simpleAction(
			"tools.preview_dds",
			"View a DDS base mip or raw channel as a bounded image; no files written.",
			"tools",
			RiskRead,
			objectSchema(map[string]any{
				"rootId": stringSchema(), "relativePath": stringSchema(),
				"channel": map[string]any{"type": "string", "enum": []string{"rgb", "r", "g", "b", "a"}},
			}, "rootId", "relativePath", "channel"),
			func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
				var input struct {
					RootID       string `json:"rootId"`
					RelativePath string `json:"relativePath"`
					Channel      string `json:"channel"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				path, err := actionCtx.resolve(input.RootID, input.RelativePath)
				if err != nil {
					return nil, err
				}
				data, err := texture.PreviewDDS(ctx, path, input.Channel)
				if err != nil {
					return nil, err
				}
				config, err := png.DecodeConfig(bytes.NewReader(data))
				if err != nil {
					return nil, err
				}
				return CapturedImage{
					Width:    config.Width,
					Height:   config.Height,
					Scale:    1,
					MIMEType: "image/png",
					PNG:      data,
				}, nil
			},
		),
	)

	repair := simpleAction(
		"tools.repair_dds",
		"Preview or apply one DDS trial with expected SHA256, unique verified backup and atomic replacement. reinterpret_linear preserves compressed bytes; linearize_mask bakes sRGB RGB values into RGBA8_UNORM; minimum_red requires an explicit whole-texture choice or binary PNG UV selection. Pixel edits require one mip.",
		"tools",
		RiskConfirm,
		objectSchema(map[string]any{
			"rootId":         stringSchema(),
			"relativePath":   stringSchema(),
			"expectedSHA256": stringSchema(),
			"operation": map[string]any{
				"type": "string",
				"enum": []string{"reinterpret_linear", "linearize_mask", "minimum_red"},
			},
			"apply":            booleanSchema(),
			"sourceColorSpace": map[string]any{"type": "string", "enum": []string{"srgb"}},
			"minimumRed":       integerSchema(1, 254),
			"wholeTexture":     booleanSchema(),
			"selectionSHA256":  stringSchema(),
			"selection": objectSchema(
				map[string]any{"rootId": stringSchema(), "relativePath": stringSchema()},
				"rootId",
				"relativePath",
			),
		}, "rootId", "relativePath", "expectedSHA256", "operation", "apply"),
		func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID           string         `json:"rootId"`
				RelativePath     string         `json:"relativePath"`
				ExpectedSHA256   string         `json:"expectedSHA256"`
				Operation        string         `json:"operation"`
				Apply            bool           `json:"apply"`
				SourceColorSpace string         `json:"sourceColorSpace"`
				MinimumRed       int            `json:"minimumRed"`
				WholeTexture     bool           `json:"wholeTexture"`
				Selection        *pathReference `json:"selection"`
				SelectionSHA256  string         `json:"selectionSHA256"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			path, err := actionCtx.resolve(input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			selection := ""
			if input.Selection != nil {
				selection, err = actionCtx.resolve(input.Selection.RootID, input.Selection.RelativePath)
				if err != nil {
					return nil, err
				}
			}
			return texture.RepairDDS(ctx, path, texture.DDSRepairRequest{
				Operation: input.Operation, ExpectedSHA256: input.ExpectedSHA256, Apply: input.Apply,
				SourceColorSpace: input.SourceColorSpace, MinimumRed: input.MinimumRed,
				WholeTexture: input.WholeTexture, SelectionPath: selection, SelectionSHA256: input.SelectionSHA256,
			})
		},
	)
	repair.risk = textureTrialRisk
	repair.describe = pathDescription(
		"Apply one DDS repair; preserve a verified sibling backup. Pixel operations may change transparency or lighting appearance.",
	)
	registry.add(repair)

	restore := simpleAction(
		"tools.restore_dds",
		"Preview or restore an exact DDS backup, verifying current and backup SHA256 and retaining the replaced state.",
		"tools",
		RiskConfirm,
		objectSchema(map[string]any{
			"rootId":         stringSchema(),
			"relativePath":   stringSchema(),
			"expectedSHA256": stringSchema(),
			"backup": objectSchema(
				map[string]any{"rootId": stringSchema(), "relativePath": stringSchema()},
				"rootId",
				"relativePath",
			),
			"backupSHA256": stringSchema(),
			"apply":        booleanSchema(),
		}, "rootId", "relativePath", "expectedSHA256", "backup", "backupSHA256", "apply"),
		func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				RootID         string        `json:"rootId"`
				RelativePath   string        `json:"relativePath"`
				ExpectedSHA256 string        `json:"expectedSHA256"`
				Backup         pathReference `json:"backup"`
				BackupSHA256   string        `json:"backupSHA256"`
				Apply          bool          `json:"apply"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			path, err := actionCtx.resolve(input.RootID, input.RelativePath)
			if err != nil {
				return nil, err
			}
			backup, err := actionCtx.resolve(input.Backup.RootID, input.Backup.RelativePath)
			if err != nil {
				return nil, err
			}
			return texture.RestoreDDS(ctx, path, backup, input.ExpectedSHA256, input.BackupSHA256, input.Apply)
		},
	)
	restore.risk = textureTrialRisk
	restore.describe = pathDescription("Restore a hash-verified DDS backup and retain a backup of the replaced state.")
	registry.add(restore)
}

func textureTrialRisk(raw json.RawMessage) Risk {
	var input struct {
		Apply bool `json:"apply"`
	}
	if json.Unmarshal(raw, &input) == nil && !input.Apply {
		return RiskRead
	}
	return RiskConfirm
}
