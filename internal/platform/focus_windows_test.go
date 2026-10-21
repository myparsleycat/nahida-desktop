//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestProcessNameUsesWin32PathFormat(t *testing.T) {
	t.Parallel()
	if processNameWin32 != 0 {
		t.Fatalf("PROCESS_NAME_WIN32 = %d, want 0", processNameWin32)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got := (&Native{}).ProcessName(uint32(os.Getpid()))
	if !strings.EqualFold(got, filepath.Base(executable)) {
		t.Fatalf("ProcessName(current) = %q, want %q", got, filepath.Base(executable))
	}
}

func TestPreviousPIDsReturnsRecentUniqueProcesses(t *testing.T) {
	t.Parallel()
	n := &Native{}
	for _, pid := range []uint32{10, 20, 20, 30, 10, 40} {
		n.focus.push(pid)
	}
	got := n.PreviousPIDs(40)
	want := []uint32{10, 30, 20}
	if len(got) != len(want) {
		t.Fatalf("PreviousPIDs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("PreviousPIDs = %v, want %v", got, want)
		}
	}
}

func TestPreviousPIDsKeepsProcessesAcrossRepeatedSwitches(t *testing.T) {
	t.Parallel()
	n := &Native{}
	n.focus.push(1)
	for range 100 {
		n.focus.push(2)
		n.focus.push(3)
	}
	if got := n.PreviousPIDs(3); !slices.Equal(got, []uint32{2, 1}) {
		t.Fatalf("PreviousPIDs after switching = %v, want [2 1]", got)
	}

	for pid := range uint32(focusHistoryLimit) {
		n.focus.push(100 + pid)
	}
	if got := n.PreviousPIDs(0); len(got) != focusHistoryLimit || slices.Contains(got, 1) {
		t.Fatalf("PreviousPIDs kept %d processes including the oldest: %v", len(got), got)
	}
}
