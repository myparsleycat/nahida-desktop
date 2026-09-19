package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/setting"
	"nahida.live/desktop/internal/transfer"
)

func TestDesktopActionRegistryRejectsInvalidRegistration(t *testing.T) {
	t.Parallel()
	registry := &desktopActionRegistry{actions: map[string]desktopAction{}}
	action := simpleAction(
		"test.action",
		"Test action.",
		"test",
		ActionRiskRead,
		objectSchema(nil),
		func(context.Context, desktopActionContext, json.RawMessage) (any, error) { return nil, nil },
	)
	if err := registry.register(action); err != nil {
		t.Fatal(err)
	}
	if err := registry.register(action); err == nil {
		t.Fatal("duplicate action unexpectedly registered")
	}
	action.definition.ID = "test.invalid_schema"
	action.definition.InputSchema = map[string]any{"type": "object"}
	if err := registry.register(action); err == nil {
		t.Fatal("non-strict schema unexpectedly registered")
	}
}

func TestDesktopActionRegistryDefinitions(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{
		settings: setting.New(nil),
		transfer: transfer.New(),
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
		filtered[0].ID != "transfer.cancel" || filtered[0].Risk != ActionRiskConfirm {
		t.Fatalf("filtered definitions = %#v", filtered)
	}
}

func TestDesktopActionRegistryHintsAreCompactAndSorted(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{
		settings: setting.New(nil),
		transfer: transfer.New(),
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

func TestDesktopActionSchemasUseRequiredArray(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{
		settings: setting.New(nil),
		transfer: transfer.New(),
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

func TestDesktopActionRegistryPrepareSettingRisk(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{settings: setting.New(nil)})
	actionCtx := desktopActionContext{scope: AgentScope{Type: "global"}}
	tests := []struct {
		name  string
		key   string
		value any
		risk  ActionRisk
	}{
		{name: "ordinary setting", key: setting.KeyGeneralLanguage, value: "ko", risk: ActionRiskWrite},
		{name: "startup setting", key: setting.KeyGeneralRunOnStartup, value: true, risk: ActionRiskConfirm},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			raw, _ := json.Marshal(map[string]any{"key": test.key, "value": test.value})
			_, risk, _, approval, err := registry.Prepare(actionCtx, desktopActionCall{
				ActionID: "settings.set", Arguments: raw,
			})
			if err != nil || risk != test.risk || (approval != nil) != (risk == ActionRiskConfirm) {
				t.Fatalf("prepare = risk %q approval %#v, %v", risk, approval, err)
			}
		})
	}
}

func TestDesktopActionRegistryRejectsWrongSettingType(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{settings: setting.New(nil)})
	_, _, _, _, err := registry.Prepare(
		desktopActionContext{scope: AgentScope{Type: "global"}},
		desktopActionCall{
			ActionID:  "settings.set",
			Arguments: json.RawMessage(`{"key":"general.runInBackground","value":"true"}`),
		},
	)
	if err == nil {
		t.Fatal("wrong setting value type unexpectedly accepted")
	}
}

func TestDesktopActionRegistryRejectsInvalidArguments(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{transfer: transfer.New()})
	_, _, _, _, err := registry.Prepare(desktopActionContext{scope: AgentScope{Type: "global"}}, desktopActionCall{
		ActionID: "transfer.cancel", Arguments: json.RawMessage(`{"pid":"","extra":true}`),
	})
	if err == nil {
		t.Fatal("invalid arguments unexpectedly accepted")
	}
}

func TestResolveActionPathStaysInsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	directory := filepath.Join(root, "mod")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()
	resolved, err := resolveActionPath(sandbox, "root", "mod")
	if err != nil || resolved != directory {
		t.Fatalf("resolved = %q, %v", resolved, err)
	}
	if _, err := resolveActionPath(sandbox, "root", "../outside"); err == nil {
		t.Fatal("path escape unexpectedly accepted")
	}
	outside := t.TempDir()
	link := filepath.Join(root, "outside-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Logf("symlink test skipped: %v", err)
		return
	}
	if _, err := resolveActionPath(sandbox, "root", "outside-link"); err == nil {
		t.Fatal("symlink escape unexpectedly accepted")
	}
}

func TestResolveActionOutputPathAllowsMissingFileInsideRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "output"), 0o755); err != nil {
		t.Fatal(err)
	}
	sandbox, err := NewSandbox([]SandboxRoot{{ID: "root", Name: "Root", Path: root}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sandbox.Close() }()

	resolved, err := resolveActionOutputPath(sandbox, "root", "output/generated.ini")
	if err != nil || resolved != filepath.Join(root, "output", "generated.ini") {
		t.Fatalf("resolved = %q, %v", resolved, err)
	}
	if _, err := resolveActionOutputPath(sandbox, "root", "../outside.ini"); err == nil {
		t.Fatal("output path escape unexpectedly accepted")
	}
}

func TestDesktopActionRegistersWindowInputActions(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{input: platform.NewInput()})

	definitions := registry.Definitions("global", "input", "")
	if len(definitions) != 2 {
		t.Fatalf("input definitions = %#v, want window listing and key sending", definitions)
	}
	for _, definition := range definitions {
		switch definition.ID {
		case "input.list_windows":
			if definition.Risk != ActionRiskRead {
				t.Fatalf("list windows risk = %q, want %q", definition.Risk, ActionRiskRead)
			}
		case "input.send_keys":
			if definition.Risk != ActionRiskConfirm {
				t.Fatalf("send keys risk = %q, want %q", definition.Risk, ActionRiskConfirm)
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
	empty := newDesktopActionRegistry(desktopActionDependencies{})
	if len(empty.Definitions("global", "input", "")) != 0 {
		t.Fatal("input actions registered without the input service")
	}
}

func TestDesktopActionPrepareWindowKeysRequestsApproval(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{input: platform.NewInput()})
	actionCtx := desktopActionContext{scope: AgentScope{Type: "global"}}

	_, risk, _, approval, err := registry.Prepare(actionCtx, desktopActionCall{
		ActionID:  "input.send_keys",
		Arguments: json.RawMessage(`{"process":"StarRail.exe","keys":["vk_f10"]}`),
	})
	if err != nil || risk != ActionRiskConfirm || approval == nil {
		t.Fatalf("prepare = risk %q approval %#v, %v", risk, approval, err)
	}
	if approval.Kind != "desktop" || approval.Target != `process "StarRail.exe"` {
		t.Fatalf("approval = %#v", approval)
	}
	if !strings.Contains(approval.Summary, "vk_f10") {
		t.Fatalf("approval summary = %q, want the requested key", approval.Summary)
	}
}

func TestDesktopActionRejectsInvalidWindowInput(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{input: platform.NewInput()})
	actionCtx := desktopActionContext{scope: AgentScope{Type: "global"}}

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
		if _, _, _, _, err := registry.Prepare(actionCtx, desktopActionCall{
			ActionID: "input.send_keys", Arguments: json.RawMessage(testCase.arguments),
		}); err == nil {
			t.Fatalf("%s was accepted", testCase.name)
		}
	}
}

func TestDesktopActionWindowInputExecution(t *testing.T) {
	t.Parallel()
	registry := newDesktopActionRegistry(desktopActionDependencies{input: platform.NewInput()})
	actionCtx := desktopActionContext{scope: AgentScope{Type: "global"}}
	ctx := context.Background()

	// Window discovery works on any host: an empty desktop lists nothing.
	result, err := registry.Execute(ctx, actionCtx, "input.list_windows", json.RawMessage(`{"limit":1}`))
	if err != nil {
		t.Fatalf("list windows = %v", err)
	}
	if windows, ok := result.([]platform.WindowInfo); !ok || len(windows) > 1 {
		t.Fatalf("list windows = %#v", result)
	}

	// A key that cannot be injected is refused before the window is even
	// resolved, so a gamepad request never reaches another application.
	_, err = registry.Execute(ctx, actionCtx, "input.send_keys",
		json.RawMessage(`{"process":"nahida-desktop","keys":["xb_a"]}`))
	if !errors.Is(err, platform.ErrInputKeyUnsupported) {
		t.Fatalf("gamepad key = %v, want %v", err, platform.ErrInputKeyUnsupported)
	}

	missing := fmt.Sprintf("nahida agent input check %d", os.Getpid())
	_, err = registry.Execute(ctx, actionCtx, "input.send_keys",
		json.RawMessage(fmt.Sprintf(`{"title":%q,"keys":["vk_f10"]}`, missing)))
	if !errors.Is(err, platform.ErrWindowNotFound) {
		t.Fatalf("send keys to a missing window = %v, want %v", err, platform.ErrWindowNotFound)
	}
}
