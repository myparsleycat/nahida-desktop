package actions

import (
	"context"
	"encoding/json"
	"math"

	"nahida.live/desktop/internal/platform"
)

const (
	captureImageMIMEType = "image/png"
	// captureImageBudget keeps one screenshot below the provider image limit
	// once the runtime base64-encodes it.
	captureImageBudget = 3 << 20
)

// registerScreenActions registers the window capture action.
func registerScreenActions(registry *Registry, deps Dependencies) {
	if deps.Screen == nil {
		return
	}
	registry.add(
		simpleAction(
			"screen.capture_window",
			"Capture a screenshot of a window and return the image.",
			"screen",
			RiskRead,
			objectSchema(map[string]any{
				"title": stringSchema(), "process": stringSchema(), "pid": integerSchema(1, math.MaxInt32),
			}),
			func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Title   string `json:"title"`
					Process string `json:"process"`
					PID     uint32 `json:"pid"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				captured, err := deps.Screen.CaptureWindow(ctx, platform.CaptureRequest{
					Target: platform.WindowTarget{
						Title: input.Title, Process: input.Process, PID: input.PID,
					},
					MaxBytes: captureImageBudget,
				})
				if err != nil {
					return nil, err
				}
				return CapturedImage{
					Window: captured.Window, Width: captured.Width, Height: captured.Height,
					Scale: captured.Scale, MIMEType: captureImageMIMEType, PNG: captured.PNG,
				}, nil
			},
		),
	)
}
