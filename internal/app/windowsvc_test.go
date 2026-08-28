package app

import (
	"context"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/setting"
)

func TestWaitReadyDistinguishesWarmAndColdWindow(t *testing.T) {
	window := NewWindow()
	window.mu.Lock()
	window.ready = true
	window.mu.Unlock()
	if alreadyReady, err := window.WaitReady(context.Background()); err != nil || !alreadyReady {
		t.Fatalf("warm WaitReady = %v, %v", alreadyReady, err)
	}

	window.mu.Lock()
	window.ready = false
	window.mu.Unlock()
	go func() {
		time.Sleep(20 * time.Millisecond)
		window.mu.Lock()
		window.ready = true
		window.mu.Unlock()
	}()
	if alreadyReady, err := window.WaitReady(context.Background()); err != nil || alreadyReady {
		t.Fatalf("cold WaitReady = %v, %v", alreadyReady, err)
	}
}

func TestValidSavedBoundsRequiresTopLeftInsideWorkArea(t *testing.T) {
	screens := []*application.Screen{
		{WorkArea: application.Rect{X: 0, Y: 0, Width: 1920, Height: 1040}},
		{WorkArea: application.Rect{X: -1280, Y: -200, Width: 1280, Height: 1024}},
	}
	tests := []struct {
		name   string
		bounds *setting.Bounds
		valid  bool
	}{
		{name: "primary", bounds: &setting.Bounds{X: 100, Y: 100, Width: 1200, Height: 800}, valid: true},
		{name: "negative secondary", bounds: &setting.Bounds{X: -1280, Y: -200, Width: 1200, Height: 800}, valid: true},
		{name: "secondary inside corner", bounds: &setting.Bounds{X: -1, Y: 823, Width: 1200, Height: 800}, valid: true},
		{name: "right edge excluded", bounds: &setting.Bounds{X: 1920, Y: 0, Width: 1200, Height: 800}},
		{name: "bottom edge excluded", bounds: &setting.Bounds{X: 0, Y: 1040, Width: 1200, Height: 800}},
		{name: "off screen", bounds: &setting.Bounds{X: 5000, Y: 5000, Width: 1200, Height: 800}},
		{name: "too narrow", bounds: &setting.Bounds{X: 100, Y: 100, Width: 799, Height: 800}},
		{name: "too short", bounds: &setting.Bounds{X: 100, Y: 100, Width: 1200, Height: 599}},
		{name: "nil"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := validSavedBounds(test.bounds, screens); got != test.valid {
				t.Fatalf("validSavedBounds() = %v, want %v", got, test.valid)
			}
		})
	}
}

func TestWaitForScreens(t *testing.T) {
	calls := 0
	screen := &application.Screen{WorkArea: application.Rect{Width: 1920, Height: 1040}}
	got := waitForScreens(func() []*application.Screen {
		calls++
		if calls < 2 {
			return nil
		}
		return []*application.Screen{screen}
	}, 100*time.Millisecond)
	if len(got) != 1 || got[0] != screen {
		t.Fatalf("waitForScreens() = %#v, want configured screen", got)
	}
}

func TestSetInitialRouteKeepsColdStartRouteUntilCreate(t *testing.T) {
	window := NewWindow()
	window.SetInitialRoute("/gamebanana?mod=42")

	window.mu.Lock()
	got := window.pendingRoute
	window.mu.Unlock()
	if got != "/gamebanana?mod=42" {
		t.Fatalf("pendingRoute = %q, want cold-start route", got)
	}
}

func TestFocusAndNavigateDoesNotConsumeRouteBeforeConfigure(t *testing.T) {
	window := NewWindow()
	window.FocusAndNavigate("/gamebanana?mod=42")

	window.mu.Lock()
	got := window.pendingRoute
	window.mu.Unlock()
	if got != "/gamebanana?mod=42" {
		t.Fatalf("pendingRoute = %q, want cold-start route", got)
	}
}

func TestNormalizeWindowRoute(t *testing.T) {
	tests := map[string]string{
		"":                            "",
		"/":                           "",
		" /setting/gen?tab=advanced ": "/setting/gen?tab=advanced",
		"setting/gen":                 "",
		"/setting#nested":             "",
	}
	for input, want := range tests {
		if got := normalizeWindowRoute(input); got != want {
			t.Errorf("normalizeWindowRoute(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSetConsoleWindowEnabledRecreatesWindowAndPreservesRoute(t *testing.T) {
	applicationApp := application.New(application.Options{Name: "window-console-test"})
	window := NewWindow()
	window.Configure(applicationApp, nil, nil)
	current := window.Create()
	if current == nil {
		t.Fatal("Create returned nil")
	}
	window.SyncRoute("/setting/gen?tab=advanced")

	window.SetConsoleWindowEnabled(true)

	window.mu.Lock()
	replacement := window.window
	consoleOpen := window.consoleOpen
	route := window.currentRoute
	window.mu.Unlock()
	if replacement == nil || replacement == current {
		t.Fatal("console toggle did not replace the window")
	}
	if !consoleOpen {
		t.Fatal("replacement window did not retain enabled console state")
	}
	if route != "/setting/gen?tab=advanced" {
		t.Fatalf("currentRoute = %q, want preserved route", route)
	}
}
