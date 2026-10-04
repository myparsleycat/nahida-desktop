package actions

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/tools"
	"nahida.live/desktop/internal/transfer"
)

func TestRegistryRejectsInvalidRegistration(t *testing.T) {
	t.Parallel()
	registry := &Registry{actions: map[string]action{}}
	registered := simpleAction(
		"test.action",
		"Test action.",
		"test",
		RiskRead,
		objectSchema(nil),
		func(context.Context, actionContext, json.RawMessage) (any, error) { return nil, nil },
	)
	if err := registry.register(registered); err != nil {
		t.Fatal(err)
	}
	if err := registry.register(registered); err == nil {
		t.Fatal("duplicate action unexpectedly registered")
	}
	registered.definition.ID = "test.invalid_schema"
	registered.definition.InputSchema = map[string]any{"type": "object"}
	if err := registry.register(registered); err == nil {
		t.Fatal("non-strict schema unexpectedly registered")
	}
}

func TestRegistryDefinitions(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{
		Settings: setting.New(nil),
		Transfer: transfer.New(),
	})
	definitions := registry.Definitions("global", "transfer", "")
	if len(definitions) == 0 {
		t.Fatal("transfer definitions are empty")
	}
	for _, definition := range definitions {
		if definition.Domain != "transfer" {
			t.Fatalf("unexpected definition: %#v", definition)
		}
	}
	if filtered := registry.Definitions("global", "transfer", "cancel"); len(filtered) != 1 ||
		filtered[0].ID != "transfer.cancel" || filtered[0].Risk != RiskConfirm {
		t.Fatalf("filtered definitions = %#v", filtered)
	}
}

func TestRegistryHintsAreCompactAndSorted(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{
		Settings: setting.New(nil),
		Transfer: transfer.New(),
	})
	hints := registry.Hints("global")
	if len(hints) == 0 {
		t.Fatal("desktop action hints are empty")
	}
	for index, hint := range hints {
		if hint.ID == "" || hint.Description == "" || hint.Risk == "" {
			t.Fatalf("incomplete hint: %#v", hint)
		}
		if index > 0 && hints[index-1].ID >= hint.ID {
			t.Fatalf("hints are not sorted: %q before %q", hints[index-1].ID, hint.ID)
		}
	}
}

func TestRegistrySchemasUseRequiredArray(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{
		Settings: setting.New(nil),
		Transfer: transfer.New(),
	})
	definitions := registry.Definitions("global", "", "")
	if len(definitions) == 0 {
		t.Fatal("desktop action definitions are empty")
	}
	for _, definition := range definitions {
		if _, ok := definition.InputSchema["required"].([]string); !ok {
			t.Errorf("action %s schema required = %#v, want a string array",
				definition.ID, definition.InputSchema["required"])
		}
	}
}

func TestRegistryRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Transfer: transfer.New()})
	_, err := registry.Prepare("global", nil, Call{
		ActionID: "transfer.cancel", Arguments: json.RawMessage(`{"pid":"","extra":true}`),
	})
	if err == nil {
		t.Fatal("invalid arguments unexpectedly accepted")
	}
}

// guardedResolver stands in for a sandbox that holds a read-only location: its writable view
// refuses every path.
type guardedResolver struct{}

func (guardedResolver) ResolveExisting(_, relativePath string) (string, error) {
	return relativePath, nil
}

func (guardedResolver) ResolveTarget(_, relativePath string) (string, error) {
	return relativePath, nil
}

func (guardedResolver) Writable() Resolver {
	return refusingResolver{}
}

type refusingResolver struct{}

func (refusingResolver) ResolveExisting(string, string) (string, error) {
	return "", errors.New("read-only")
}

func (refusingResolver) ResolveTarget(string, string) (string, error) {
	return "", errors.New("read-only")
}

func (refusingResolver) CheckWritable(string) error {
	return errors.New("read-only")
}

// A session is opened by a read action, so the action that later changes its folder never resolves
// a path. It still has to vet the stored folder, before asking for approval and again before running.
func TestRegistryVetsStoredSessionPaths(t *testing.T) {
	t.Parallel()
	registry := &Registry{actions: map[string]action{}}
	executed := false
	if err := registry.register(sessionApplyAction(
		"tools.touch_apply", "Apply a touch profile.",
		func(sessionID string) ([]string, error) { return []string{"Core/" + sessionID}, nil },
		func(context.Context, tools.TouchProfileApplyInput) (tools.TouchApplyResult, error) {
			executed = true
			return tools.TouchApplyResult{}, nil
		},
	)); err != nil {
		t.Fatal(err)
	}
	call := Call{ActionID: "tools.touch_apply", Arguments: json.RawMessage(`{"sessionId":"session"}`)}

	if _, err := registry.Prepare("global", guardedResolver{}, call); err == nil {
		t.Fatal("session action proposed a change to a read-only location")
	}
	// refusingResolver stands in for the writable view an approved action is replayed against.
	if _, err := registry.Execute(
		t.Context(), "global", refusingResolver{}, call.ActionID, call.Arguments,
	); err == nil || executed {
		t.Fatalf("session action reached a read-only location: executed = %v, err = %v", executed, err)
	}

	plan, err := registry.Prepare("global", permissiveResolver{}, call)
	if err != nil || plan.Proposal == nil || plan.Proposal.Target != "Core/session" {
		t.Fatalf("plan = %#v, %v", plan, err)
	}
	if _, err := plan.Run(t.Context()); err != nil || !executed {
		t.Fatalf("writable session action: executed = %v, err = %v", executed, err)
	}
}

// permissiveResolver is a writable view with no read-only location.
type permissiveResolver struct{}

func (permissiveResolver) ResolveExisting(_, relativePath string) (string, error) {
	return relativePath, nil
}

func (permissiveResolver) ResolveTarget(_, relativePath string) (string, error) {
	return relativePath, nil
}

func (permissiveResolver) CheckWritable(string) error {
	return nil
}

// Only a read-risk action may resolve a path through the unguarded resolver; write and confirm
// actions bind to the writable view, including a risk decided from the arguments.
func TestRegistryBindsMutatingActionsToWritableResolver(t *testing.T) {
	t.Parallel()
	registry := &Registry{actions: map[string]action{}}
	echo := func(_ context.Context, path string, _ json.RawMessage) (any, error) { return path, nil }
	escalated := pathAction("test.escalated", "Escalated action.", RiskRead, echo)
	escalated.risk = func(json.RawMessage) Risk { return RiskWrite }
	for _, registered := range []action{
		pathAction("test.read", "Read action.", RiskRead, echo),
		pathAction("test.write", "Write action.", RiskWrite, echo),
		pathAction("test.confirm", "Confirm action.", RiskConfirm, echo),
		escalated,
	} {
		if err := registry.register(registered); err != nil {
			t.Fatal(err)
		}
	}
	arguments := json.RawMessage(`{"rootId":"core","relativePath":"main.ini"}`)

	output, err := registry.Execute(t.Context(), "global", guardedResolver{}, "test.read", arguments)
	if err != nil || output != "main.ini" {
		t.Fatalf("read output = %#v, %v", output, err)
	}
	for _, actionID := range []string{"test.write", "test.escalated"} {
		if _, err := registry.Execute(t.Context(), "global", guardedResolver{}, actionID, arguments); err == nil {
			t.Fatalf("%s reached a read-only location", actionID)
		}
	}
	if _, err := registry.Prepare(
		"global", guardedResolver{}, Call{ActionID: "test.confirm", Arguments: arguments},
	); err == nil {
		t.Fatal("confirm action proposed a change to a read-only location")
	}
}
