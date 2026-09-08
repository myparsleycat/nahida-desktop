//go:build windows

package app

import "testing"

func TestRelaunchSysProcAttrDoesNotHideWindow(t *testing.T) {
	t.Parallel()
	attr := relaunchSysProcAttr()
	if attr == nil {
		t.Fatal("nil SysProcAttr")
	}
	if attr.HideWindow {
		t.Fatal("HideWindow swallows the first ShowWindow")
	}
	if attr.CreationFlags&createNoWindow != 0 {
		t.Fatal("CREATE_NO_WINDOW")
	}
	want := uint32(detachedProcess | createNewProcGroup)
	if attr.CreationFlags&want != want {
		t.Fatalf("CreationFlags = %#x", attr.CreationFlags)
	}
}
