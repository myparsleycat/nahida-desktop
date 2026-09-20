package tools

import (
	"context"

	"nahida.live/desktop/internal/tools/menumaker"
)

type (
	MenuMakerScanStats       = menumaker.MenuMakerScanStats
	MenuMakerScanFile        = menumaker.MenuMakerScanFile
	MenuMakerScanResult      = menumaker.MenuMakerScanResult
	MenuMakerSource          = menumaker.MenuMakerSource
	MenuMakerGeneratedAsset  = menumaker.MenuMakerGeneratedAsset
	MenuMakerApplyRequest    = menumaker.MenuMakerApplyRequest
	MenuMakerSaveINIRequest  = menumaker.MenuMakerSaveINIRequest
	MenuMakerSaveZIPRequest  = menumaker.MenuMakerSaveZIPRequest
	MenuMakerWriteResult     = menumaker.MenuMakerWriteResult
	MenuMakerEntry           = menumaker.MenuMakerEntry
	MenuMakerSection         = menumaker.MenuMakerSection
	MenuMakerHandler         = menumaker.MenuMakerHandler
	MenuMakerSlot            = menumaker.MenuMakerSlot
	MenuMakerDocument        = menumaker.MenuMakerDocument
	MenuMakerPalette         = menumaker.MenuMakerPalette
	MenuMakerSettings        = menumaker.MenuMakerSettings
	MenuMakerSlotPosition    = menumaker.MenuMakerSlotPosition
	MenuMakerGeometry        = menumaker.MenuMakerGeometry
	MenuMakerSlotValueState  = menumaker.MenuMakerSlotValueState
	MenuMakerSlotStateGroup  = menumaker.MenuMakerSlotStateGroup
	MenuMakerGenerateRequest = menumaker.MenuMakerGenerateRequest
	MenuMakerGenerateResult  = menumaker.MenuMakerGenerateResult
)

func (t *Tools) MenuMakerScanFolder(
	ctx context.Context,
	rootPath string,
	includeTXT bool,
) (MenuMakerScanResult, error) {
	return t.menuMaker.ScanFolder(ctx, rootPath, includeTXT)
}

func (t *Tools) MenuMakerLoadSource(ctx context.Context, filePath string) (MenuMakerSource, error) {
	return t.menuMaker.LoadSource(ctx, filePath)
}

func (t *Tools) MenuMakerParse(ctx context.Context, text string) (MenuMakerDocument, error) {
	return t.menuMaker.Parse(ctx, text)
}

func (t *Tools) MenuMakerGenerate(
	ctx context.Context,
	req MenuMakerGenerateRequest,
) (MenuMakerGenerateResult, error) {
	return t.menuMaker.Generate(ctx, req)
}

func (t *Tools) MenuMakerApplyBundle(
	ctx context.Context,
	req MenuMakerApplyRequest,
) (MenuMakerWriteResult, error) {
	return t.menuMaker.ApplyBundle(ctx, req)
}

func (t *Tools) MenuMakerSaveINI(
	ctx context.Context,
	req MenuMakerSaveINIRequest,
) (MenuMakerWriteResult, error) {
	return t.menuMaker.SaveINI(ctx, req)
}

func (t *Tools) MenuMakerSaveZIP(
	ctx context.Context,
	req MenuMakerSaveZIPRequest,
) (MenuMakerWriteResult, error) {
	return t.menuMaker.SaveZIP(ctx, req)
}
