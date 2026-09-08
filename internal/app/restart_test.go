package app

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func restoreRelaunchHooks() {
	executablePath = os.Executable
	currentPID = os.Getpid
	spawnRelaunchProcess = spawnDetachedRelaunch
	relaunchParentAlive = relaunchProcessAlive
	currentApplication = application.Get
	waitForPendingRelaunch = waitPendingRelaunch
	newApplication = application.New
	quitApplication = func(app *application.App) {
		if app != nil {
			app.Quit()
		}
	}
}

func TestRelaunchChildEnvReplacesWaitPID(t *testing.T) {
	t.Parallel()
	got := relaunchChildEnv([]string{"PATH=x", relaunchWaitPIDEnv + "=9", "FOO=1"}, 42)
	if len(got) != 3 || got[0] != "PATH=x" || got[1] != "FOO=1" || got[2] != relaunchWaitPIDEnv+"=42" {
		t.Fatalf("env = %#v", got)
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

func TestWindowRestartSpawnsThenQuits(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	executablePath = func() (string, error) { return `C:\nahida-desktop.exe`, nil }
	currentPID = func() int { return 4242 }
	var spawnedExe string
	var spawnedPID int
	spawnRelaunchProcess = func(exe string, pid int) error {
		spawnedExe = exe
		spawnedPID = pid
		return nil
	}
	quits := 0
	quitApplication = func(*application.App) { quits++ }
	window := NewWindow()
	window.app = &application.App{}
	if err := window.Restart(); err != nil {
		t.Fatal(err)
	}
	if spawnedExe != `C:\nahida-desktop.exe` || spawnedPID != 4242 {
		t.Fatalf("spawned %q pid %d", spawnedExe, spawnedPID)
	}
	if quits != 1 {
		t.Fatalf("quits = %d", quits)
	}
}

func TestWindowRestartKeepsProcessWhenSpawnFails(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	executablePath = func() (string, error) { return `C:\nahida-desktop.exe`, nil }
	currentPID = func() int { return 1 }
	spawnRelaunchProcess = func(string, int) error { return errors.New("spawn blocked") }
	quits := 0
	quitApplication = func(*application.App) { quits++ }
	window := NewWindow()
	window.app = &application.App{}
	if err := window.Restart(); err == nil || !strings.Contains(err.Error(), "restart.failed") {
		t.Fatalf("Restart = %v", err)
	}
	if quits != 0 {
		t.Fatalf("quits = %d", quits)
	}
}

func TestWindowRestartDoesNotSpawnWithoutApp(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	spawned := 0
	spawnRelaunchProcess = func(string, int) error {
		spawned++
		return nil
	}
	currentApplication = func() *application.App { return nil }
	if err := NewWindow().Restart(); err == nil {
		t.Fatal("Restart succeeded")
	}
	if spawned != 0 {
		t.Fatalf("spawned = %d", spawned)
	}
}

func TestRunWaitsForRelaunchBeforeSingleInstance(t *testing.T) {
	t.Cleanup(restoreRelaunchHooks)
	var order []string
	waitForPendingRelaunch = func() { order = append(order, "wait") }
	newApplication = func(application.Options) *application.App {
		order = append(order, "new")
		return nil
	}
	if app := newLockedApplication(application.Options{}); app != nil {
		t.Fatal("fake application")
	}
	if len(order) != 2 || order[0] != "wait" || order[1] != "new" {
		t.Fatalf("order = %#v", order)
	}
}

func TestRelaunchChildEnvPIDFormat(t *testing.T) {
	t.Parallel()
	pid := os.Getpid()
	got := relaunchChildEnv(nil, pid)
	if len(got) != 1 || got[0] != relaunchWaitPIDEnv+"="+strconv.Itoa(pid) {
		t.Fatalf("env = %#v", got)
	}
}
