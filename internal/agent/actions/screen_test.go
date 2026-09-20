package actions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"

	"nahida.live/desktop/internal/platform"
)

func TestRegistryRegistersWindowCaptureAction(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Screen: platform.NewScreen()})

	definitions := registry.Definitions("global", "screen", "")
	if len(definitions) != 1 || definitions[0].ID != "screen.capture_window" {
		t.Fatalf("screen definitions = %#v, want the window capture action", definitions)
	}
	if definitions[0].Risk != RiskRead {
		t.Fatalf("capture risk = %q, want %q", definitions[0].Risk, RiskRead)
	}
	if filtered := registry.Definitions("global", "screen", "capture"); len(filtered) != 1 ||
		filtered[0].ID != "screen.capture_window" {
		t.Fatalf("filtered screen definitions = %#v", filtered)
	}

	// Without the screen service the domain stays out of the action index, so a
	// model is never offered an action that cannot run.
	empty := NewRegistry(Dependencies{})
	if len(empty.Definitions("global", "screen", "")) != 0 {
		t.Fatal("screen actions registered without the screen service")
	}
}

func TestRegistryRejectsInvalidWindowCapture(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Screen: platform.NewScreen()})

	if _, err := registry.Prepare("global", nil, Call{
		ActionID: "screen.capture_window", Arguments: json.RawMessage(`{"process":"StarRail.exe","extra":true}`),
	}); err == nil {
		t.Fatal("unknown capture argument was accepted")
	}

	// The selector is validated when the action runs, because the schema cannot
	// express that one of its properties must be set.
	_, err := registry.Execute(
		context.Background(),
		"global",
		nil,
		"screen.capture_window",
		json.RawMessage(`{}`),
	)
	if !errors.Is(err, platform.ErrInputTargetRequired) {
		t.Fatalf("capture without a selector = %v, want %v", err, platform.ErrInputTargetRequired)
	}
}

func TestRegistryWindowCaptureExecution(t *testing.T) {
	t.Parallel()
	registry := NewRegistry(Dependencies{Screen: platform.NewScreen()})

	missing := fmt.Sprintf("nahida agent capture check %d", os.Getpid())
	_, err := registry.Execute(
		context.Background(),
		"global",
		nil,
		"screen.capture_window",
		json.RawMessage(fmt.Sprintf(`{"title":%q}`, missing)),
	)
	if !errors.Is(err, platform.ErrWindowNotFound) {
		t.Fatalf("capture of a missing window = %v, want %v", err, platform.ErrWindowNotFound)
	}
}
