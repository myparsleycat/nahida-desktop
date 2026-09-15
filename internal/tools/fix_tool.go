package tools

import (
	"context"

	"nahida.live/desktop/internal/db"
	fixtool "nahida.live/desktop/internal/tools/fix_tool"
)

type (
	FixToolLogEvent         = fixtool.FixToolLogEvent
	CreateScriptPresetInput = fixtool.CreateScriptPresetInput
)

func (t *Tools) GetScripts(ctx context.Context) ([]db.ScriptBasicRow, error) {
	return t.fixTool.GetScripts(ctx)
}

func (t *Tools) SaveScript(ctx context.Context, inputPath string) error {
	return t.fixTool.SaveScript(ctx, inputPath)
}

func (t *Tools) DeleteScript(ctx context.Context, scriptID string) error {
	return t.fixTool.DeleteScript(ctx, scriptID)
}

func (t *Tools) GetPresets(ctx context.Context) ([]db.ScriptPresetWithScripts, error) {
	return t.fixTool.GetPresets(ctx)
}

func (t *Tools) CreatePreset(ctx context.Context, input CreateScriptPresetInput) error {
	return t.fixTool.CreatePreset(ctx, input)
}

func (t *Tools) DeletePreset(ctx context.Context, presetID string) error {
	return t.fixTool.DeletePreset(ctx, presetID)
}

func (t *Tools) IsPythonAvailable(ctx context.Context) bool {
	return t.fixTool.IsPythonAvailable(ctx)
}

func (t *Tools) CancelRun() bool {
	return t.fixTool.CancelRun()
}

func (t *Tools) SendInput(input string) bool {
	return t.fixTool.SendInput(input)
}

func (t *Tools) RunScript(ctx context.Context, scriptID, destPath string) error {
	return t.fixTool.RunScript(ctx, scriptID, destPath)
}

func (t *Tools) RunPreset(ctx context.Context, presetID, destPath string) error {
	return t.fixTool.RunPreset(ctx, presetID, destPath)
}

func (t *Tools) shutdownFixTool() error {
	if t == nil || t.fixTool == nil {
		return nil
	}
	return t.fixTool.Shutdown()
}
