package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"nahida.live/desktop/internal/platform"
)

func TestRegistryRegistersWindowInputActions(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Input: platform.NewInput()})

	definitions := registry.Definitions("global", "input", "")
	if len(definitions) != 2 {
		t.Fatalf("input definitions = %#v, want window listing and key sending", definitions)
	}
	for _, definition := range definitions {
		switch definition.ID {
		case "input.list_windows":
			if definition.Risk != RiskRead {
				t.Fatalf("list windows risk = %q, want %q", definition.Risk, RiskRead)
			}
		case "input.send_keys":
			if definition.Risk != RiskConfirm {
				t.Fatalf("send keys risk = %q, want %q", definition.Risk, RiskConfirm)
			}
		default:
			t.Fatalf("unexpected input action %q", definition.ID)
		}
	}
	if filtered := registry.Definitions("global", "input", "send"); len(filtered) != 1 ||
		filtered[0].ID != "input.send_keys" {
		t.Fatalf("filtered input definitions = %#v", filtered)
	}
	// Without the input service the domain stays out of the action index, so a
	// model is never offered an action that cannot run.
	empty := NewRegistry(Dependencies{})
	if len(empty.Definitions("global", "input", "")) != 0 {
		t.Fatal("input actions registered without the input service")
	}
}

func TestRegistryPrepareWindowKeysRequestsApproval(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Input: platform.NewInput()})

	plan, err := registry.Prepare("global", nil, Call{
		ActionID:  "input.send_keys",
		Arguments: json.RawMessage(`{"process":"StarRail.exe","keys":["vk_f10"]}`),
	})
	if err != nil || plan.Risk != RiskConfirm || plan.Proposal == nil {
		t.Fatalf("prepare = risk %q proposal %#v, %v", plan.Risk, plan.Proposal, err)
	}
	if plan.Proposal.Kind != "desktop" || plan.Proposal.Target != `process "StarRail.exe"` {
		t.Fatalf("proposal = %#v", plan.Proposal)
	}
	if !strings.Contains(plan.Proposal.Summary, "vk_f10") {
		t.Fatalf("proposal summary = %q, want the requested key", plan.Proposal.Summary)
	}
}

func TestRegistryRejectsInvalidWindowInput(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Input: platform.NewInput()})

	cases := []struct {
		name      string
		arguments string
	}{
		{name: "no keys", arguments: `{"process":"StarRail.exe"}`},
		{
			name:      "too many keys",
			arguments: `{"process":"StarRail.exe","keys":["a","b","c","d","e","f","g","h","i","j","k","l","m","n","o","p","q"]}`,
		},
		{name: "unknown delivery", arguments: `{"process":"StarRail.exe","keys":["vk_f10"],"delivery":"hook"}`},
		{name: "hold above the cap", arguments: `{"process":"StarRail.exe","keys":["vk_f10"],"holdMs":2001}`},
		{name: "unknown argument", arguments: `{"process":"StarRail.exe","keys":["vk_f10"],"extra":true}`},
		{name: "pid of zero", arguments: `{"pid":0,"keys":["vk_f10"]}`},
	}

	for _, testCase := range cases {
		if _, err := registry.Prepare("global", nil, Call{
			ActionID: "input.send_keys", Arguments: json.RawMessage(testCase.arguments),
		}); err == nil {
			t.Fatalf("%s was accepted", testCase.name)
		}
	}
}

func TestRegistryWindowInputExecution(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Input: platform.NewInput()})
	ctx := context.Background()

	// Window discovery works on any host: an empty desktop lists nothing.
	result, err := registry.Execute(ctx, "global", nil, "input.list_windows", json.RawMessage(`{"limit":1}`))
	if err != nil {
		t.Fatalf("list windows = %v", err)
	}
	if windows, ok := result.([]platform.WindowInfo); !ok || len(windows) > 1 {
		t.Fatalf("list windows = %#v", result)
	}

	// A key that cannot be injected is refused before the window is even
	// resolved, so a gamepad request never reaches another application.
	_, err = registry.Execute(ctx, "global", nil, "input.send_keys",
		json.RawMessage(`{"process":"nahida-desktop","keys":["xb_a"]}`))
	if !errors.Is(err, platform.ErrInputKeyUnsupported) {
		t.Fatalf("gamepad key = %v, want %v", err, platform.ErrInputKeyUnsupported)
	}

	missing := fmt.Sprintf("nahida agent input check %d", os.Getpid())
	_, err = registry.Execute(ctx, "global", nil, "input.send_keys",
		json.RawMessage(fmt.Sprintf(`{"title":%q,"keys":["vk_f10"]}`, missing)))
	if !errors.Is(err, platform.ErrWindowNotFound) {
		t.Fatalf("send keys to a missing window = %v, want %v", err, platform.ErrWindowNotFound)
	}
}
