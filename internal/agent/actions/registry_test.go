package actions

import (
	"context"
	"encoding/json"
	"testing"

	"nahida.live/desktop/internal/setting"
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
