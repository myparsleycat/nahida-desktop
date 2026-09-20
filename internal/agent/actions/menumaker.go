package actions

import (
	"context"
	"encoding/json"

	"nahida.live/desktop/internal/menumaker"
)

// registerMenuMakerActions registers the MenuMaker actions.
func registerMenuMakerActions(registry *Registry, deps Dependencies) {
	if deps.MenuMaker == nil {
		return
	}
	registry.add(simpleAction("menumaker.parse", "Parse MenuMaker source text.", "menumaker", RiskRead,
		objectSchema(map[string]any{"text": stringSchema()}, "text"),
		func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Text string `json:"text"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.MenuMaker.Parse(ctx, input.Text)
		}))
	registry.add(pathAction("menumaker.load", "Load and parse a MenuMaker source file.", RiskRead,
		func(ctx context.Context, path string, _ json.RawMessage) (any, error) {
			return deps.MenuMaker.LoadSource(ctx, path)
		}))
	registry.add(pathAction("menumaker.scan", "Scan a local folder for MenuMaker sources.", RiskRead,
		func(ctx context.Context, path string, raw json.RawMessage) (any, error) {
			var input struct {
				RootID       string `json:"rootId"`
				RelativePath string `json:"relativePath"`
				IncludeTXT   bool   `json:"includeTxt"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.MenuMaker.ScanFolder(ctx, path, input.IncludeTXT)
		}))
	generate := pathAction("menumaker.generate", "Generate MenuMaker output in memory.", RiskRead,
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
			return deps.MenuMaker.Generate(ctx, menumaker.MenuMakerGenerateRequest{
				SourcePath: path, SourceText: input.SourceText, Slots: input.Slots, Settings: input.Settings,
			})
		})
	generate.definition.InputSchema = objectSchema(map[string]any{
		"rootId": stringSchema(), "relativePath": stringSchema(), "sourceText": stringSchema(),
		"slots": map[string]any{"type": "array"}, "settings": map[string]any{"type": "object"},
	}, "rootId", "relativePath", "sourceText", "slots", "settings")
	registry.add(generate)
	registry.add(menuMakerSaveAction("menumaker.save_ini", "Save generated MenuMaker INI output.",
		func(ctx context.Context, source, destination string, raw json.RawMessage) (any, error) {
			var input menuMakerSaveINIInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.MenuMaker.SaveINI(ctx, menumaker.MenuMakerSaveINIRequest{
				SourcePath: source, DestinationPath: destination, SourceText: input.SourceText,
				Slots: input.Slots, Settings: input.Settings, Encoding: input.Encoding,
				HasBOM: input.HasBOM, Newline: input.Newline,
			})
		}))
	registry.add(menuMakerSaveAction("menumaker.save_zip", "Save generated MenuMaker ZIP output.",
		func(ctx context.Context, source, destination string, raw json.RawMessage) (any, error) {
			var input menuMakerSaveZIPInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.MenuMaker.SaveZIP(ctx, menumaker.MenuMakerSaveZIPRequest{
				SourcePath: source, DestinationPath: destination,
				SourceText: input.SourceText, Slots: input.Slots, Settings: input.Settings,
				Encoding: input.Encoding, HasBOM: input.HasBOM, Newline: input.Newline, Assets: input.Assets,
			})
		}))
}

// menuMakerSaveINIInput is the shared save argument of the MenuMaker save actions.
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

// menuMakerSaveZIPInput adds the generated assets of a ZIP save.
type menuMakerSaveZIPInput struct {
	menuMakerSaveINIInput
	Assets []menumaker.MenuMakerGeneratedAsset `json:"assets"`
}
