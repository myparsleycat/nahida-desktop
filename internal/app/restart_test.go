package app

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func restoreRelaunchHooks() {
	executablePath = os.Executable
	currentPID = os.Getpid
	spawnRelaunchProcess = spawnDetachedRelaunch
	relaunchParentAlive = relaunchProcessAlive
}

func TestRelaunchChildEnvReplacesWaitPID(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		environ []string
		want    []string
	}{
		{
			name:    "replaces inherited pid",
			environ: []string{"PATH=x", relaunchWaitPIDEnv + "=9", "FOO=1"},
			want:    []string{"PATH=x", "FOO=1", relaunchWaitPIDEnv + "=42"},
		},
		{name: "empty environment", want: []string{relaunchWaitPIDEnv + "=42"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := relaunchChildEnv(tc.environ, 42); !slices.Equal(got, tc.want) {
				t.Fatalf("env = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestWaitPendingRelaunchNoopsWithoutEnv(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	t.Setenv(relaunchWaitPIDEnv, "1")
	if err := os.Unsetenv(relaunchWaitPIDEnv); err != nil {
		t.Fatal(err)
	}
	called := false
	relaunchParentAlive = func(int) bool {
		called = true
		return true
	}
	waitPendingRelaunchFor(time.Millisecond)
	if called {
		t.Fatal("waited without parent pid")
	}
}

func TestWaitPendingRelaunchClearsEnvAfterParentExits(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	t.Setenv(relaunchWaitPIDEnv, "123")
	var seen []int
	relaunchParentAlive = func(pid int) bool {
		seen = append(seen, pid)
		return false
	}
	waitPendingRelaunchFor(time.Second)
	if len(seen) != 1 || seen[0] != 123 {
		t.Fatalf("alive checks = %#v", seen)
	}
	if _, ok := os.LookupEnv(relaunchWaitPIDEnv); ok {
		t.Fatal("wait pid env still set")
	}
}

func TestWaitPendingRelaunchContinuesAfterTimeout(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	t.Setenv(relaunchWaitPIDEnv, "7")
	relaunchParentAlive = func(int) bool { return true }
	waitPendingRelaunchFor(20 * time.Millisecond)
	if _, ok := os.LookupEnv(relaunchWaitPIDEnv); ok {
		t.Fatal("wait pid env still set")
	}
}

func TestRestartSpawnsThenQuits(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	executablePath = func() (string, error) { return `C:\nahida-desktop.exe`, nil }
	currentPID = func() int { return 4242 }
	var spawnedExe string
	var spawnedPID int
	spawnRelaunchProcess = func(exe string, pid int) error {
		spawnedExe, spawnedPID = exe, pid
		return nil
	}
	quits := 0
	if err := restartApp(nil, func() { quits++ }); err != nil {
		t.Fatal(err)
	}
	if spawnedExe != `C:\nahida-desktop.exe` || spawnedPID != 4242 {
		t.Fatalf("spawned %q pid %d", spawnedExe, spawnedPID)
	}
	if quits != 1 {
		t.Fatalf("quits = %d", quits)
	}
}

func TestRestartKeepsProcessWhenSpawnFails(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	executablePath = func() (string, error) { return `C:\nahida-desktop.exe`, nil }
	currentPID = func() int { return 1 }
	spawnRelaunchProcess = func(string, int) error { return errors.New("spawn blocked") }
	quits := 0
	if err := restartApp(nil, func() { quits++ }); err == nil || !strings.Contains(err.Error(), "restart.failed") {
		t.Fatalf("restartApp = %v", err)
	}
	if quits != 0 {
		t.Fatalf("quits = %d", quits)
	}
}

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
