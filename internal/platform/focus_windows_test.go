//go:build windows

package platform

import (
	"os"
	"path/filepath"
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
