package tools

import (
	"context"

	fixer4001 "nahida.live/desktop/internal/tools/fixer_4001"
)

type (
	Fixer4001State            = fixer4001.Fixer4001State
	Fixer4001ProgressEvent    = fixer4001.Fixer4001ProgressEvent
	Fixer4001BuildInput       = fixer4001.Fixer4001BuildInput
	Fixer4001ImporterInput    = fixer4001.Fixer4001ImporterInput
	Fixer4001PathInput        = fixer4001.Fixer4001PathInput
	Fixer4001Result           = fixer4001.Fixer4001Result
	Fixer4001BuildToolsResult = fixer4001.Fixer4001BuildToolsResult
	DiversificationState      = fixer4001.DiversificationState
	PEDiversificationReport   = fixer4001.PEDiversificationReport
	PEDiversifierPatch        = fixer4001.PEDiversifierPatch
	ImporterWriteAccess       = fixer4001.ImporterWriteAccess
)

func (t *Tools) FourThousandOneFixerGetState() Fixer4001State {
	return t.fixer4001.FourThousandOneFixerGetState()
}

func (t *Tools) FourThousandOneFixerGetProviderReleases(ctx context.Context, provider string) ([]string, error) {
	return t.fixer4001.FourThousandOneFixerGetProviderReleases(ctx, provider)
}

func (t *Tools) FourThousandOneFixerUpdateReleases(ctx context.Context) error {
	return t.fixer4001.FourThousandOneFixerUpdateReleases(ctx)
}

func (t *Tools) FourThousandOneFixerGetBuildToolsPath(ctx context.Context) (string, error) {
	return t.fixer4001.FourThousandOneFixerGetBuildToolsPath(ctx)
}

func (t *Tools) FourThousandOneFixerSetBuildToolsPath(
	ctx context.Context,
	path string,
) (Fixer4001BuildToolsResult, error) {
	return t.fixer4001.FourThousandOneFixerSetBuildToolsPath(ctx, path)
}

func (t *Tools) FourThousandOneFixerClearBuildToolsPath(ctx context.Context) error {
	return t.fixer4001.FourThousandOneFixerClearBuildToolsPath(ctx)
}

func (t *Tools) FourThousandOneFixerGetDiversificationState(
	input Fixer4001PathInput,
) (DiversificationState, error) {
	return t.fixer4001.FourThousandOneFixerGetDiversificationState(input)
}

func (t *Tools) FourThousandOneFixerCheckImporterWriteAccess(input Fixer4001PathInput) ImporterWriteAccess {
	return t.fixer4001.FourThousandOneFixerCheckImporterWriteAccess(input)
}

func (t *Tools) FourThousandOneFixerBuildDll(
	ctx context.Context,
	input Fixer4001BuildInput,
) (result Fixer4001Result) {
	return t.fixer4001.FourThousandOneFixerBuildDll(ctx, input)
}

func (t *Tools) FourThousandOneFixerDiversifyDllPadding(
	ctx context.Context,
	input Fixer4001ImporterInput,
) (result Fixer4001Result) {
	return t.fixer4001.FourThousandOneFixerDiversifyDllPadding(ctx, input)
}

func (t *Tools) FourThousandOneFixerRestoreDiversifiedDll(
	ctx context.Context,
	input Fixer4001PathInput,
) (result Fixer4001Result) {
	return t.fixer4001.FourThousandOneFixerRestoreDiversifiedDll(ctx, input)
}

// CleanupStaleD3DBuilds removes only validated child directories of the fixed temp root.
//
//wails:ignore
func (t *Tools) CleanupStaleD3DBuilds(ctx context.Context) error {
	return t.fixer4001.CleanupStaleD3DBuilds(ctx)
}
