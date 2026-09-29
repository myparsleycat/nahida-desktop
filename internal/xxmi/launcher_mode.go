package xxmi

import (
	"context"
	"errors"
	"fmt"

	"nahida.live/desktop/internal/db"
)

// LauncherMode selects whether games start through the built-in importer runtime
// or through a separately installed XXMI Launcher.
type LauncherMode string

const (
	LauncherBuiltin  LauncherMode = "builtin"
	LauncherExternal LauncherMode = "external"
)

const launcherModeKey = "xxmi_launcher_mode"

func (x *XXMI) GetLauncherMode(ctx context.Context) (LauncherMode, error) {
	client, err := x.settingsClient()
	if err != nil {
		return "", err
	}
	return launcherMode(ctx, client)
}

// SetLauncherMode switches between the built-in runtime and the external XXMI Launcher.
// Both configurations are kept, so switching back restores the previous setup.
func (x *XXMI) SetLauncherMode(ctx context.Context, mode LauncherMode) error {
	if mode != LauncherBuiltin && mode != LauncherExternal {
		return fmt.Errorf("unknown XXMI launcher mode %q", mode)
	}
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	current, err := launcherMode(ctx, client)
	if err != nil {
		return err
	}
	if current == mode {
		return nil
	}

	x.mu.Lock()
	launching := len(x.busy) > 0
	x.mu.Unlock()
	if launching {
		return errors.New("XXMI_BUSY")
	}

	value := string(mode)
	if err := client.Settings.Upsert(ctx, launcherModeKey, &value); err != nil {
		return err
	}
	if x.log != nil {
		x.log.Info(fmt.Sprintf("Switched XXMI launcher mode from %s to %s", current, mode), "XXMI.setLauncherMode")
	}
	if x.eventEmit != nil {
		x.eventEmit("renderer:reload")
	}
	return nil
}

// launcherMode defaults to the external XXMI Launcher, which most users already run;
// the built-in runtime is used only after the user selects it.
func launcherMode(ctx context.Context, client *db.Client) (LauncherMode, error) {
	stored, err := client.Settings.GetValue(ctx, launcherModeKey)
	if err != nil {
		return "", err
	}
	if stored != nil && LauncherMode(*stored) == LauncherBuiltin {
		return LauncherBuiltin, nil
	}
	return LauncherExternal, nil
}

func (x *XXMI) settingsClient() (*db.Client, error) {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil, errors.New("XXMI settings store is not configured")
	}
	return client, nil
}

func (x *XXMI) usesExternalLauncher(ctx context.Context) (bool, error) {
	mode, err := x.GetLauncherMode(ctx)
	return mode == LauncherExternal, err
}
