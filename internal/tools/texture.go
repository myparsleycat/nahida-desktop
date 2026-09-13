package tools

import (
	"context"

	"nahida.live/desktop/internal/tools/texture"
)

type (
	TextureResizeSettings         = texture.TextureResizeSettings
	TextureResizeSettingsPatch    = texture.TextureResizeSettingsPatch
	TextureResizeListItem         = texture.TextureResizeListItem
	TextureResizeRunInput         = texture.TextureResizeRunInput
	TextureResizeModInput         = texture.TextureResizeModInput
	TextureResizeFileRunInput     = texture.TextureResizeFileRunInput
	TextureResizeFileResult       = texture.TextureResizeFileResult
	TextureResizeResult           = texture.TextureResizeResult
	TextureResizeProgressEvent    = texture.TextureResizeProgressEvent
	TextureUpscaleProgressEvent   = texture.TextureUpscaleProgressEvent
	TextureUpscaleRuntimeStatus   = texture.TextureUpscaleRuntimeStatus
	TextureUpscaleRuntimeStatuses = texture.TextureUpscaleRuntimeStatuses
)

func (t *Tools) GetTextureResizeSettings(ctx context.Context) (TextureResizeSettings, error) {
	return t.texture.GetTextureResizeSettings(ctx)
}

func (t *Tools) SaveTextureResizeSettings(
	ctx context.Context,
	patch TextureResizeSettingsPatch,
) (TextureResizeSettings, error) {
	return t.texture.SaveTextureResizeSettings(ctx, patch)
}

func (t *Tools) ListTextureFolder(
	ctx context.Context,
	targetPath string,
	patch *TextureResizeSettingsPatch,
) ([]TextureResizeListItem, error) {
	return t.texture.ListTextureFolder(ctx, targetPath, patch)
}

func (t *Tools) ListTextureMod(
	ctx context.Context,
	modPath string,
	patch *TextureResizeSettingsPatch,
) ([]TextureResizeListItem, error) {
	return t.texture.ListTextureMod(ctx, modPath, patch)
}

func (t *Tools) GetTextureResizeState() TextureResizeProgressEvent {
	return t.texture.GetTextureResizeState()
}

func (t *Tools) ResizeTextureFolder(ctx context.Context, input TextureResizeRunInput) (TextureResizeResult, error) {
	return t.texture.ResizeTextureFolder(ctx, input)
}

func (t *Tools) ResizeTextureMod(
	ctx context.Context,
	modPath string,
	input TextureResizeModInput,
) (TextureResizeResult, error) {
	return t.texture.ResizeTextureMod(ctx, modPath, input)
}

func (t *Tools) ResizeTextureFile(ctx context.Context, input TextureResizeFileRunInput) (TextureResizeResult, error) {
	return t.texture.ResizeTextureFile(ctx, input)
}

func (t *Tools) GetTextureUpscaleRuntimeStatus(ctx context.Context) (TextureUpscaleRuntimeStatuses, error) {
	return t.texture.GetTextureUpscaleRuntimeStatus(ctx)
}
