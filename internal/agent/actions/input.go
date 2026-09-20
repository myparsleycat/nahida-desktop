package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"nahida.live/desktop/internal/platform"
)

// registerInputActions registers the window listing and key injection actions.
func registerInputActions(registry *Registry, deps Dependencies) {
	if deps.Input == nil {
		return
	}
	registry.add(
		simpleAction(
			"input.list_windows",
			"List visible top-level windows with their title, class, pid, and process name.",
			"input",
			RiskRead,
			objectSchema(map[string]any{
				"title": stringSchema(), "process": stringSchema(), "pid": integerSchema(1, math.MaxInt32),
				"limit": integerSchema(1, 200),
			}),
			func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
				var input struct {
					Title   string `json:"title"`
					Process string `json:"process"`
					PID     uint32 `json:"pid"`
					Limit   int    `json:"limit"`
				}
				if err := decodeActionArguments(raw, &input); err != nil {
					return nil, err
				}
				return deps.Input.ListWindows(ctx, platform.WindowFilter{
					Title: input.Title, Process: input.Process, PID: input.PID, Limit: input.Limit,
				})
			},
		),
	)
	registry.add(action{
		definition: Definition{
			ID: "input.send_keys", Description: "Send keyboard keys to another window.", Domain: "input",
			Risk: RiskWrite, Scopes: []string{"global", "mod"}, InputSchema: objectSchema(map[string]any{
				"title": stringSchema(), "process": stringSchema(), "pid": integerSchema(1, math.MaxInt32),
				"keys": map[string]any{
					"type": "array", "minItems": 1, "maxItems": 16, "items": stringSchema(),
				},
				"delivery":     enumSchema("foreground", "message"),
				"holdMs":       integerSchema(0, 2000),
				"intervalMs":   integerSchema(0, 5000),
				"restoreFocus": booleanSchema(),
			}, "keys")},
		execute: func(ctx context.Context, _ actionContext, raw json.RawMessage) (any, error) {
			var input struct {
				Title        string               `json:"title"`
				Process      string               `json:"process"`
				PID          uint32               `json:"pid"`
				Keys         []string             `json:"keys"`
				Delivery     platform.KeyDelivery `json:"delivery"`
				HoldMs       int                  `json:"holdMs"`
				IntervalMs   int                  `json:"intervalMs"`
				RestoreFocus *bool                `json:"restoreFocus"`
			}
			if err := decodeActionArguments(raw, &input); err != nil {
				return nil, err
			}
			return deps.Input.SendKeys(ctx, platform.KeyRequest{
				Target: platform.WindowTarget{Title: input.Title, Process: input.Process, PID: input.PID},
				Keys:   input.Keys, Delivery: input.Delivery, HoldMs: input.HoldMs, IntervalMs: input.IntervalMs,
				RestoreFocus: input.RestoreFocus,
			})
		},
		describe: func(_ actionContext, raw json.RawMessage) (string, string, error) {
			var input struct {
				Title   string   `json:"title"`
				Process string   `json:"process"`
				PID     uint32   `json:"pid"`
				Keys    []string `json:"keys"`
			}
			if err := json.Unmarshal(raw, &input); err != nil {
				return "", "", err
			}
			summary := fmt.Sprintf("Send %s to a window.", strings.Join(input.Keys, ", "))
			return summary, windowTargetLabel(input.Title, input.Process, input.PID), nil
		},
	})
}
