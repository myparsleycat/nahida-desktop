package mod

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/transfer"
)

type flakyActionSettings struct {
	testSettings
	disableFailures int
	enableFailures  int
}

func TestDisableUnmanagedDoesNotRequireRegisteredGame(t *testing.T) {
	source := filepath.Join(t.TempDir(), "Custom Mod")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}

	disabled, err := (&Mod{}).DisableUnmanaged(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(disabled) != "DISABLED Custom Mod" {
		t.Fatalf("disabled path = %q", disabled)
	}
	if info, statErr := os.Stat(disabled); statErr != nil || !info.IsDir() {
		t.Fatalf("disabled directory = %#v, %v", info, statErr)
	}
}

func (s *flakyActionSettings) GetDisabledPrefixStyle(context.Context) (string, error) {
	if s.disableFailures > 0 {
		s.disableFailures--
		return "", errors.New("simulated disable failure")
	}
	return "space", nil
}

func (s *flakyActionSettings) GetCopyShaderFixesOnEnable(context.Context) (bool, error) {
	if s.enableFailures > 0 {
		s.enableFailures--
		return false, errors.New("simulated enable failure")
	}
	return false, nil
}

func TestRetryExclusiveToggleOperationRetriesPermissionFailures(t *testing.T) {
	service := New()
	attempts := 0
	result, err := service.retryExclusiveToggleOperation(context.Background(), "mod", func() (string, error) {
		attempts++
		if attempts < 3 {
			return "", os.ErrPermission
		}
		return "done", nil
	})
	if err != nil || result != "done" {
		t.Fatalf("retry result = %q, %v", result, err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
}

func TestExclusiveToggleContinuesAfterSiblingFailureAndSkipsDotDirectory(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	settings := &flakyActionSettings{disableFailures: 1}
	service.settings = settings
	var logs bytes.Buffer
	service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true})

	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	for _, name := range []string{".Hidden", "A", "B", "DISABLED Target"} {
		path := filepath.Join(group, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "mod.ini"), []byte("[Constants]"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	result, err := service.ExclusiveToggle(ctx, filepath.Join(group, "DISABLED Target"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(result) != "Target" {
		t.Fatalf("exclusive result = %q", result)
	}
	if _, err := os.Stat(filepath.Join(group, "A")); err != nil {
		t.Fatalf("failed sibling should remain enabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, "DISABLED B")); err != nil {
		t.Fatalf("later sibling was not disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, ".Hidden")); err != nil {
		t.Fatalf("dot-directory should be ignored: %v", err)
	}
	if !strings.Contains(logs.String(), "Mod:exclusiveToggle:disable:") || !strings.Contains(logs.String(), "simulated disable failure") {
		t.Fatalf("exclusive logs = %s", logs.String())
	}
}

func TestDisableAllContinuesAfterFailureAndSkipsDotDirectory(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	service.settings = &flakyActionSettings{disableFailures: 1}
	var logs bytes.Buffer
	service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true})

	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	for _, name := range []string{".Hidden", "A", "B"} {
		if err := os.MkdirAll(filepath.Join(group, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := service.DisableAll(ctx, group); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(group, "A")); err != nil {
		t.Fatalf("failed mod should remain enabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, "DISABLED B")); err != nil {
		t.Fatalf("later mod was not disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, ".Hidden")); err != nil {
		t.Fatalf("dot-directory should be ignored: %v", err)
	}
	if !strings.Contains(logs.String(), "Mod:disableAll:") || !strings.Contains(logs.String(), "simulated disable failure") {
		t.Fatalf("disableAll logs = %s", logs.String())
	}
}

func TestEnableAllContinuesAfterFailure(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	service.settings = &flakyActionSettings{enableFailures: 1}
	var logs bytes.Buffer
	service.log = infra.NewLogWithOptions(infra.LogOptions{Writer: &logs, DisableFile: true})

	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	for _, name := range []string{"DISABLED A", "DISABLED B"} {
		if err := os.MkdirAll(filepath.Join(group, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := service.EnableAll(ctx, group); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(group, "DISABLED A")); err != nil {
		t.Fatalf("failed mod should remain disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, "B")); err != nil {
		t.Fatalf("later mod was not enabled: %v", err)
	}
	if !strings.Contains(logs.String(), "Mod:enableAll:") || !strings.Contains(logs.String(), "simulated enable failure") {
		t.Fatalf("enableAll logs = %s", logs.String())
	}
}

func TestActiveDownloadDestinationRejectsDirectModActions(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	active := filepath.Join(group, "Active")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	registerActiveModDownload(t, service, "active", active)

	for name, action := range map[string]func() error{
		"disable": func() error {
			_, err := service.Disable(ctx, active)
			return err
		},
		"enable": func() error {
			_, err := service.Enable(ctx, active)
			return err
		},
		"toggle": func() error {
			_, err := service.Toggle(ctx, active)
			return err
		},
		"exclusive toggle": func() error {
			_, err := service.ExclusiveToggle(ctx, active)
			return err
		},
		"rename": func() error {
			_, err := service.Rename(ctx, active, "Renamed")
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := action()
			if err == nil || err.Error() != "MOD_DOWNLOAD_IN_PROGRESS" {
				t.Fatalf("action error = %v", err)
			}
			if _, statErr := os.Stat(active); statErr != nil {
				t.Fatalf("active download folder changed: %v", statErr)
			}
		})
	}
}

func TestBulkAndExclusiveActionsSkipActiveDownloadDestinations(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "Group")
	for _, name := range []string{"Active", "Other", "DISABLED Active Disabled", "DISABLED Other Disabled", "DISABLED Target"} {
		if err := os.MkdirAll(filepath.Join(group, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	registerActiveModDownload(t, service, "active-enabled", filepath.Join(group, "Active"))
	registerActiveModDownload(t, service, "active-disabled", filepath.Join(group, "DISABLED Active Disabled"))

	if err := service.DisableAll(ctx, group); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(group, "Active")); err != nil {
		t.Fatalf("active download was disabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, "DISABLED Other")); err != nil {
		t.Fatalf("ordinary mod was not disabled: %v", err)
	}

	target := filepath.Join(group, "DISABLED Target")
	if _, err := service.ExclusiveToggle(ctx, target); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(group, "Active")); err != nil {
		t.Fatalf("exclusive toggle disabled active download: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, "Target")); err != nil {
		t.Fatalf("exclusive target was not enabled: %v", err)
	}

	if err := service.EnableAll(ctx, group); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(group, "DISABLED Active Disabled")); err != nil {
		t.Fatalf("active disabled download was enabled: %v", err)
	}
	if _, err := os.Stat(filepath.Join(group, "Other Disabled")); err != nil {
		t.Fatalf("ordinary disabled mod was not enabled: %v", err)
	}
}

func registerActiveModDownload(t *testing.T, service *Mod, pid, destinationPath string) {
	t.Helper()
	if service.transfer == nil {
		service.transfer = transfer.New()
	}
	if _, err := service.transfer.Create(transfer.CreateParams{
		PID: pid, Type: "download", Name: pid, InitialStatus: transfer.StatusProgress,
		DestinationPaths: []string{destinationPath}, ManualStart: true,
	}); err != nil {
		t.Fatal(err)
	}
}
