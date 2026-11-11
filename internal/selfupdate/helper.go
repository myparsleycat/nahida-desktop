package selfupdate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nahida.live/desktop/internal/platform"
)

// The helper protocol travels in the environment so it never mixes with the application's own arguments.
const (
	envHelper    = "NAHIDA_UPDATE_HELPER"
	envTarget    = "NAHIDA_UPDATE_TARGET"
	envStaged    = "NAHIDA_UPDATE_STAGED"
	envParentPID = "NAHIDA_UPDATE_PARENT_PID"
	envReady     = "NAHIDA_UPDATE_READY"
	envLog       = "NAHIDA_UPDATE_LOG"

	helperReady       = "ready"
	helperErrorPrefix = "error:"

	parentExitTimeout = 30 * time.Second
	moveAttempts      = 20
	moveRetryDelay    = 500 * time.Millisecond
)

// Helper exit codes. Only exitBroken leaves no runnable executable at the target; every other code either
// installed the update or left the previous executable in place and started it again.
const (
	exitUpdated        = 0
	exitBadEnvironment = 2
	exitMissingFile    = 10
	exitParentAlive    = 11
	exitPayloadInvalid = 12
	exitSwapFailed     = 13
	exitLaunchFailed   = 14
	exitBroken         = 15
)

// HandleHelperMode returns at once unless this process was started by Restart as the swap helper. The helper
// waits for the application to exit, replaces the executable, starts the new version and exits the process
// without returning. Call it before any other startup work, which would otherwise run a second application
// against the files being replaced.
func HandleHelperMode() {
	if os.Getenv(envHelper) != "1" {
		return
	}
	os.Exit(runHelperProcess())
}

func runHelperProcess() int {
	target, staged := os.Getenv(envTarget), os.Getenv(envStaged)
	readyPath, logPath := os.Getenv(envReady), os.Getenv(envLog)
	pid, _ := strconv.Atoi(os.Getenv(envParentPID))
	// The new version and a restored one must start as the application, not as another helper.
	for _, key := range []string{envHelper, envTarget, envStaged, envParentPID, envReady, envLog} {
		_ = os.Unsetenv(key)
	}
	log := openHelperLog(logPath)
	defer log.close()

	if target == "" || staged == "" {
		_ = signalHelper(readyPath, helperErrorPrefix+" missing target or staged executable")
		return exitBadEnvironment
	}
	for _, path := range []string{target, staged} {
		if _, err := os.Stat(path); err != nil {
			log.printf("helper: %v", err)
			_ = signalHelper(readyPath, helperErrorPrefix+" "+err.Error())
			return exitMissingFile
		}
	}
	// The handle is opened before the parent learns the helper is ready, so the parent cannot exit and
	// leave its process ID to an unrelated process in between.
	parent, err := openParent(pid)
	if err != nil {
		log.printf("helper: open parent process %d: %v", pid, err)
		_ = signalHelper(readyPath, helperErrorPrefix+" open parent process: "+err.Error())
		return exitBadEnvironment
	}
	defer parent.close()
	if err := signalHelper(readyPath, helperReady); err != nil {
		log.printf("helper: signal ready: %v", err)
		return exitBadEnvironment
	}

	return swapper{
		waitParent: parent.wait,
		move:       platform.MoveNoReplace,
		launch:     launchDetached,
		retryDelay: moveRetryDelay,
		logf:       log.printf,
	}.run(target, staged)
}

func signalHelper(path, status string) error {
	if path == "" {
		return nil
	}
	return os.WriteFile(path, []byte(status), 0o600)
}

// swapper replaces the executable. Its process and file operations are fields so tests drive every branch
// without starting processes.
type swapper struct {
	waitParent func(timeout time.Duration) error
	move       func(source, destination string) error
	launch     func(path string) error
	retryDelay time.Duration
	logf       func(format string, args ...any)
}

func (s swapper) run(target, staged string) int {
	s.logf("helper: replacing %s with %s", target, staged)
	if err := s.waitParent(parentExitTimeout); err != nil {
		// The executable is untouched; starting it again beats leaving the user without the application.
		s.logf("helper: application did not exit: %v; starting the current version", err)
		s.relaunch(target)
		return exitParentAlive
	}
	if err := checkPayload(staged); err != nil {
		s.logf("helper: %v; starting the current version", err)
		s.relaunch(target)
		return exitPayloadInvalid
	}

	// Windows keeps an executable's image locked against deletion for a while after its process exits, and
	// this helper runs from the same file, but a mapped image can still be renamed. The renamed original
	// is the rollback copy.
	removeAsides(target)
	aside := fmt.Sprintf("%s.old-%d", target, time.Now().UnixNano())
	if err := s.retry(func() error { return s.move(target, aside) }); err != nil {
		s.logf("helper: move %s aside: %v; starting the current version", target, err)
		s.relaunch(target)
		return exitSwapFailed
	}
	if err := s.retry(func() error { return s.move(staged, target) }); err != nil {
		s.logf("helper: move %s into place: %v; restoring the previous version", staged, err)
		return s.restore(aside, target, exitSwapFailed)
	}
	if err := s.launch(target); err != nil {
		s.logf("helper: start %s: %v; restoring the previous version", target, err)
		if err := os.Remove(target); err != nil {
			s.logf("helper: remove the update that failed to start: %v", err)
			return exitBroken
		}
		return s.restore(aside, target, exitLaunchFailed)
	}

	if err := os.Remove(aside); err != nil {
		s.logf("helper: remove %s: %v (removed by the next update)", aside, err)
	}
	if dir := filepath.Dir(staged); strings.HasPrefix(filepath.Base(dir), stagingPrefix) {
		if err := os.RemoveAll(dir); err != nil {
			s.logf("helper: remove %s: %v", dir, err)
		}
	}
	s.logf("helper: update installed")
	return exitUpdated
}

func (s swapper) restore(aside, target string, code int) int {
	if err := s.retry(func() error { return s.move(aside, target) }); err != nil {
		s.logf("helper: restore %s from %s: %v", target, aside, err)
		return exitBroken
	}
	s.relaunch(target)
	return code
}

func (s swapper) relaunch(target string) {
	if err := s.launch(target); err != nil {
		s.logf("helper: start %s: %v", target, err)
	}
}

func (s swapper) retry(operation func() error) error {
	var err error
	for attempt := range moveAttempts {
		if err = operation(); err == nil {
			return nil
		}
		if attempt < moveAttempts-1 {
			time.Sleep(s.retryDelay)
		}
	}
	return err
}

func checkPayload(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("staged update is unreadable: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("staged update is not a non-empty regular file")
	}
	return nil
}

// removeAsides deletes the originals that earlier updates moved aside but could not delete while their image
// was still mapped.
func removeAsides(target string) {
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		return
	}
	prefix := filepath.Base(target) + ".old-"
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			_ = os.Remove(filepath.Join(filepath.Dir(target), entry.Name()))
		}
	}
}

// helperLog appends to a file of its own: the helper runs before the application configures its log.
type helperLog struct{ file *os.File }

func openHelperLog(path string) *helperLog {
	if path == "" {
		return &helperLog{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return &helperLog{}
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return &helperLog{}
	}
	return &helperLog{file: file}
}

func (l *helperLog) printf(format string, args ...any) {
	if l.file == nil {
		return
	}
	_, _ = fmt.Fprintf(l.file, "%s %s\n", time.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
}

func (l *helperLog) close() {
	if l.file != nil {
		_ = l.file.Close()
	}
}
