package ui

import "testing"

// The start page setting and fn:navi events store React frontend paths.
func TestRouteOfFrontendPaths(t *testing.T) {
	t.Parallel()

	for path, want := range map[string]Route{
		"/transfer":         RouteTransfer,
		"/gamebanana":       RouteGameBanana,
		"/mod":              RouteMod,
		"/drive/drive/root": RouteDrive,
		"/drive/share/root": RouteShare,
		"/backup/snapshots": RouteBackup,
		"/setting/gen":      RouteSetting,
		"/auth":             RouteAuth,
	} {
		if got, ok := RouteOf(path); !ok || got != want {
			t.Errorf("RouteOf(%q) = %q, %v; want %q", path, got, ok, want)
		}
	}
	for _, path := range []string{"", "/", "/model", "/drive", "/modding"} {
		if got, ok := RouteOf(path); ok {
			t.Errorf("RouteOf(%q) = %q, want no route", path, got)
		}
	}
}

func TestRouterHistory(t *testing.T) {
	t.Parallel()

	r := NewRouter(RouteMod)
	r.Navigate(RouteTools)
	r.Navigate(RouteXXMI)
	if !r.Back() || r.Current() != RouteTools {
		t.Fatalf("after Back current = %q, want tools", r.Current())
	}
	r.Navigate(RouteSetting)
	if r.Forward() {
		t.Fatal("Navigate after Back kept the forward history")
	}
	r.Back()
	r.Back()
	if r.Current() != RouteMod || r.Back() {
		t.Fatalf("back history ends at %q, want mod", r.Current())
	}
	if !r.Forward() || r.Current() != RouteTools {
		t.Fatalf("after Forward current = %q, want tools", r.Current())
	}
}

func TestRouterLocationHistory(t *testing.T) {
	t.Parallel()

	r := NewRouter(RouteMod)
	r.Navigate(RouteSetting)
	r.Record("/setting/gen")
	r.Record("/setting/network")
	r.Record("/mod/elsewhere")
	if !r.Back() || r.Location() != "/setting/gen" {
		t.Fatalf("after Back location = %q, want /setting/gen", r.Location())
	}
	if !r.Back() || r.Location() != "/mod" || r.Current() != RouteMod {
		t.Fatalf("after second Back location = %q route %q, want /mod", r.Location(), r.Current())
	}
	if !r.Forward() || r.Location() != "/setting/gen" || r.Current() != RouteSetting {
		t.Fatalf("after Forward location = %q, want /setting/gen", r.Location())
	}
}
