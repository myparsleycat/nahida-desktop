//go:build windows

package app

import (
	"os"
	"testing"
)

func TestRelaunchProcessAliveReportsCurrentProcess(t *testing.T) {
	if !relaunchProcessAlive(os.Getpid()) {
		t.Fatal("current process")
	}
	if relaunchProcessAlive(0) || relaunchProcessAlive(-1) {
		t.Fatal("invalid pid")
	}
}

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
