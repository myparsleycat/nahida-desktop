package app

import "testing"

type fakeAutostartController struct {
	enabled  [][]string
	disabled int
}

func (f *fakeAutostartController) Enable(args ...string) error {
	f.enabled = append(f.enabled, args)
	return nil
}

func (f *fakeAutostartController) Disable() error {
	f.disabled++
	return nil
}

func TestSyncAutostartUsesHiddenLaunchArgument(t *testing.T) {
	controller := &fakeAutostartController{}
	if err := syncAutostartState(controller, true, true, true); err != nil {
		t.Fatal(err)
	}
	if len(controller.enabled) != 1 || len(controller.enabled[0]) != 1 || controller.enabled[0][0] != "--hidden" {
		t.Fatalf("enable options = %#v", controller.enabled)
	}
	if err := syncAutostartState(controller, false, true, true); err != nil {
		t.Fatal(err)
	}
	if controller.disabled != 1 {
		t.Fatalf("disable calls = %d", controller.disabled)
	}
}

func TestSyncAutostartSkipsUnpackagedBuilds(t *testing.T) {
	controller := &fakeAutostartController{}
	if err := syncAutostartState(controller, true, false, false); err != nil {
		t.Fatal(err)
	}
	if len(controller.enabled) != 0 || controller.disabled != 0 {
		t.Fatalf("unpackaged sync mutated autostart: enabled=%#v disabled=%d", controller.enabled, controller.disabled)
	}
}

func TestSyncAutostartDisablesPortablePackagedBuilds(t *testing.T) {
	controller := &fakeAutostartController{}
	if err := syncAutostartState(controller, true, true, false); err != nil {
		t.Fatal(err)
	}
	if len(controller.enabled) != 0 || controller.disabled != 1 {
		t.Fatalf("portable sync = enabled=%#v disabled=%d", controller.enabled, controller.disabled)
	}
}

func TestShouldStartHiddenRequiresExactArgument(t *testing.T) {
	if !shouldStartHidden([]string{"nahida-desktop", "--hidden"}) {
		t.Fatal("--hidden was not detected")
	}
	for _, args := range [][]string{nil, {"--hidden"}, {"nahida-desktop", "--hidden=true"}} {
		if shouldStartHidden(args) {
			t.Fatalf("unexpected hidden launch for %#v", args)
		}
	}
}
