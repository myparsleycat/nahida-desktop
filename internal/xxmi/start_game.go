package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (x *XXMI) StartGame(ctx context.Context, importer string) error {
	importer = strings.TrimSpace(importer)
	if importer == "" {
		return errors.New("importer is required")
	}
	x.mu.Lock()
	if x.busy {
		x.mu.Unlock()
		return errors.New("XXMI is busy")
	}
	x.busy = true
	x.mu.Unlock()
	defer func() {
		x.mu.Lock()
		x.busy = false
		x.mu.Unlock()
	}()
	if err := x.load(ctx); err != nil {
		return err
	}
	x.mu.RLock()
	if x.path == nil || x.config == nil {
		x.mu.RUnlock()
		return errors.New("XXMI is not configured")
	}
	root := *x.path
	importerConfig, ok := x.parsed.Importers[importer]
	timeoutSeconds := x.parsed.Launcher.StartTimeout
	x.mu.RUnlock()
	if !ok {
		return fmt.Errorf("importer %s not found", importer)
	}
	executable := filepath.Join(root, "Resources", "Bin", "XXMI Launcher.exe")
	if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("XXMI Launcher not found at %s", executable)
	}
	processName := gameProcessName(importer, importerConfig.Importer.GameEXENames)
	if processName == "" {
		return fmt.Errorf("game process is not configured for importer %s", importer)
	}
	if x.log != nil {
		x.log.Info("Starting game "+importer+" via XXMI Launcher", "XXMI.startGame")
	}
	if err := startLauncher(ctx, executable, importer); err != nil {
		return err
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}
	pid, err := waitForVisibleProcess(ctx, processName, time.Duration(timeoutSeconds*float64(time.Second)))
	if err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Detected %s (PID: %d)", processName, pid), "XXMI.startGame")
	}
	timer := time.NewTimer(time.Second)
	select {
	case <-ctx.Done():
		timer.Stop()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func waitForVisibleProcess(ctx context.Context, processName string, timeout time.Duration) (int, error) {
	return waitForVisibleProcessWith(ctx, processName, timeout, 100*time.Millisecond, findProcessPID, processHasVisibleWindow)
}

func waitForVisibleProcessWith(
	ctx context.Context,
	processName string,
	timeout, pollInterval time.Duration,
	find func(context.Context, string) (int, error),
	hasVisibleWindow func(int) bool,
) (int, error) {
	deadline := time.Now().Add(timeout)
	for {
		pid, err := find(ctx, processName)
		if err != nil {
			return 0, err
		}
		if pid > 0 && hasVisibleWindow(pid) {
			return pid, nil
		}
		if timeout > 0 && !time.Now().Before(deadline) {
			return 0, fmt.Errorf("failed to detect game process %s after starting launcher", processName)
		}
		timer := time.NewTimer(pollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return 0, ctx.Err()
		case <-timer.C:
		}
	}
}

func gameProcessName(importer string, configured []string) string {
	switch strings.ToUpper(importer) {
	case "SRMI":
		return "StarRail.exe"
	case "WWMI":
		return "Client-Win64-Shipping.exe"
	case "ZZMI":
		return "ZenlessZoneZero.exe"
	default:
		if len(configured) == 0 {
			return ""
		}
		return configured[0]
	}
}
