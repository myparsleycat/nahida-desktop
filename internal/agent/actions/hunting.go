package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"nahida.live/desktop/internal/hunting"
)

type huntingBeginInput struct {
	ImporterKey     string `json:"importerKey"`
	PID             uint32 `json:"pid"`
	InitiallyActive bool   `json:"initiallyActive"`
}

func registerHuntingActions(registry *Registry, deps Dependencies) {
	if deps.Hunting == nil {
		return
	}
	registry.add(action{
		definition: Definition{
			ID:             "hunting.inspect",
			Description:    "Inspect a running XXMI game and its live 3DMigoto hunting configuration.",
			Domain:         "hunting",
			Risk:           RiskRead,
			RequiresImages: true,
			InputSchema: objectSchema(map[string]any{
				"importerKey": stringSchema(), "pid": integerSchema(1, math.MaxInt32),
			}, "importerKey"),
		},
		execute: func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				ImporterKey string `json:"importerKey"`
				PID         uint32 `json:"pid"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			result, err := deps.Hunting.Inspect(ctx, input.ImporterKey, input.PID)
			if err != nil {
				return nil, err
			}
			return CapturedImage{
				Window: result.Window, Width: result.Width, Height: result.Height, Scale: 1,
				MIMEType: result.MIMEType, PNG: result.PNG, Result: result,
			}, nil
		},
	})

	registry.add(action{
		definition: Definition{
			ID: "hunting.begin",
			Description: "Prepare a user-controlled 3DMigoto hunting session and expose its controls on the " +
				"Agent conversation page.",
			Domain:         "hunting",
			Risk:           RiskConfirm,
			RequiresImages: true,
			InputSchema: objectSchema(map[string]any{
				"importerKey": stringSchema(), "pid": integerSchema(1, math.MaxInt32),
				"initiallyActive": booleanSchema(),
			}, "importerKey", "pid", "initiallyActive"),
		},
		impact: "Nahida will enable hunting mode for up to 15 minutes. Only your button presses on the " +
			"conversation page will focus the game and send configured next or mark keys; marking a found " +
			"resource replaces the clipboard with its hash.",
		execute: func(ctx context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input huntingBeginInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			result, err := deps.Hunting.Begin(
				ctx, actionCtx.sessionID, input.ImporterKey, input.PID, input.InitiallyActive,
			)
			return huntingActionResult(result), err
		},
		describe: func(_ actionContext, raw json.RawMessage) (string, string, error) {
			var input huntingBeginInput
			if err := decodeActionArguments(raw, &input); err != nil {
				return "", "", err
			}
			return "Prepare manual hunting controls.", fmt.Sprintf(
				"%s game pid %d",
				input.ImporterKey,
				input.PID,
			), nil
		},
	})

	registry.add(simpleAction(
		"hunting.status", "Read the current or most recently completed hunting session status.",
		"hunting", RiskRead, objectSchema(map[string]any{"sessionId": stringSchema()}, "sessionId"),
		func(_ context.Context, actionCtx actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				SessionID string `json:"sessionId"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Hunting.Status(actionCtx.sessionID, input.SessionID)
		},
	))
}

func huntingActionResult(result hunting.ImageResult) any {
	if len(result.PNG) == 0 {
		return result.Snapshot
	}
	return CapturedImage{
		Window: result.Window, Width: result.Width, Height: result.Height, Scale: 1,
		MIMEType: result.MIMEType, PNG: result.PNG, Result: result.Snapshot,
	}
}
