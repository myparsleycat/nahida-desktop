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

type preparedLaunch struct {
	importer       string
	exe            string
	gameExecutable string
	root           string
	timeout        float64
}

func (x *XXMI) StartGame(ctx context.Context, importer string) error {
	ready, err := x.prepareGameLaunch(ctx, importer)
	if err != nil {
		return err
	}
	if err := x.rejectLaunchBlockers(ctx, ready.importer, ready.gameExecutable); err != nil {
		return err
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
	return x.launchPrepared(ctx, ready)
}

func (x *XXMI) prepareGameLaunch(ctx context.Context, importer string) (preparedLaunch, error) {
	importer = strings.TrimSpace(importer)
	if importer == "" {
		return preparedLaunch{}, errors.New("importer is required")
	}
	if err := x.load(ctx); err != nil {
		return preparedLaunch{}, err
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	if x.path == nil || x.config == nil {
		return preparedLaunch{}, errors.New("XXMI is not configured")
	}
	importerConfig, ok := x.parsed.Importers[importer]
	if !ok {
		return preparedLaunch{}, fmt.Errorf("importer %s not found", importer)
	}
	exe := gameProcessName(importer, importerConfig.Importer.GameEXENames)
	gameExecutable := configuredGameExecutable(
		importerConfig.Importer.GameFolder,
		importerConfig.Importer.GameEXENames,
	)
	if gameExecutable == "" {
		gameExecutable = exe
	}
	return preparedLaunch{
		importer:       importer,
		exe:            exe,
		gameExecutable: gameExecutable,
		root:           *x.path,
		timeout:        x.parsed.Launcher.StartTimeout,
	}, nil
}

func (x *XXMI) launchPrepared(ctx context.Context, ready preparedLaunch) error {
	executable := filepath.Join(ready.root, "Resources", "Bin", "XXMI Launcher.exe")
	if info, err := os.Stat(executable); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("XXMI Launcher not found at %s", executable)
	}
	if ready.exe == "" {
		return fmt.Errorf("game process is not configured for importer %s", ready.importer)
	}
	if x.log != nil {
		x.log.Info("Starting game "+ready.importer+" via XXMI Launcher", "XXMI.startGame")
	}
	if err := startLauncher(ctx, executable, ready.importer); err != nil {
		return err
	}
	timeoutSeconds := ready.timeout
	if timeoutSeconds <= 0 {
		timeoutSeconds = 60
	}
	pid, err := waitForVisibleProcess(ctx, ready.exe, time.Duration(timeoutSeconds*float64(time.Second)))
	if err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Detected %s (PID: %d)", ready.exe, pid), "XXMI.startGame")
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
	return waitForVisibleProcessWith(
		ctx,
		processName,
		timeout,
		100*time.Millisecond,
		findProcessPID,
		processHasVisibleWindow,
	)
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

func configuredGameExecutable(folder string, configured []string) string {
	folder = strings.TrimSpace(folder)
	var first string
	for _, name := range configured {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}

		candidate := filepath.Clean(name)
		if !filepath.IsAbs(candidate) {
			if !filepath.IsAbs(folder) {
				continue
			}
			candidate = filepath.Join(folder, candidate)
		}
		if first == "" {
			first = candidate
		}
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
	}
	return first
}
