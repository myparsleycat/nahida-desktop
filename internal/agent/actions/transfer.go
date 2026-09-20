package actions

import (
	"context"
	"encoding/json"
)

// registerTransferActions registers the local transfer queue actions.
func registerTransferActions(registry *Registry, deps Dependencies) {
	if deps.Transfer == nil {
		return
	}
	registry.add(
		simpleAction("transfer.list", "List local transfer queue entries.", "transfer", RiskRead, objectSchema(nil),
			func(context.Context, actionContext, json.RawMessage) (any, error) {
				return deps.Transfer.List(), nil
			}),
	)
	for _, item := range []struct {
		id, description string
		risk            Risk
		run             func(string) error
	}{
		{"transfer.pause", "Pause a transfer.", RiskWrite, deps.Transfer.Pause},
		{"transfer.resume", "Resume a transfer.", RiskWrite, deps.Transfer.Resume},
		{"transfer.retry", "Retry a transfer.", RiskWrite, deps.Transfer.Retry},
		{"transfer.cancel", "Cancel a transfer.", RiskConfirm, deps.Transfer.Cancel},
	} {
		registry.add(idAction(item.id, item.description, "pid", item.risk,
			func(_ context.Context, id string) (any, error) { return actionOK(item.run(id)) }))
	}
	registry.add(
		simpleAction("transfer.pause_all", "Pause all active transfers.", "transfer", RiskWrite, objectSchema(nil),
			func(context.Context, actionContext, json.RawMessage) (any, error) {
				return actionOK(deps.Transfer.PauseAll())
			}),
	)
	registry.add(
		simpleAction(
			"transfer.resume_all",
			"Resume all paused transfers.",
			"transfer",
			RiskWrite,
			objectSchema(nil),
			func(context.Context, actionContext, json.RawMessage) (any, error) {
				return actionOK(deps.Transfer.ResumeAll())
			},
		),
	)
	registry.add(
		simpleAction("transfer.clear", "Remove completed transfers from the queue.", "transfer", RiskConfirm,
			objectSchema(nil), func(context.Context, actionContext, json.RawMessage) (any, error) {
				return actionOK(deps.Transfer.Clear())
			}),
	)
}
