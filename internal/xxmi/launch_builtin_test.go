package xxmi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/xxmi/inject"
)

type stubLaunchHelper struct{}

func (stubLaunchHelper) Acquire(context.Context) (func(), error) { return func() {}, nil }

func (stubLaunchHelper) LaunchXXMI(context.Context, inject.LaunchSpec) (inject.LaunchResult, error) {
	return inject.LaunchResult{}, nil
}

func (stubLaunchHelper) HelperImageName() string { return "nahida-elevated-helper-test.exe" }

func TestUpdateLaunchINIPreservesUserContentAndSetsHelper(t *testing.T) {
	folder := t.TempDir()
	path := filepath.Join(folder, "d3dx.ini")
	original := []byte("\xef\xbb\xbf; user comment\r\n[Loader]\r\ntarget = old.exe\r\ncustom = keep\r\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{Elevated: stubLaunchHelper{}})
	cfg := ImporterConfig{ImporterFolder: folder, Mode: RuntimeXXMI,
		Migoto: MigotoOptions{EnforceRendering: true, EnableHunting: true, MuteWarnings: true}}
	if err := service.updateLaunchINI(context.Background(), "GIMI", cfg, "Game.exe"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"; user comment\r\n", "target = Game.exe\r\n", "custom = keep\r\n",
		"loader = nahida-elevated-helper-test.exe\r\n", "launch = \r\n",
		"texture_hash = 0\r\n", "hunting = 2\r\n", "show_warnings = 0\r\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("updated INI is missing %q: %q", want, data)
		}
	}
}

func TestSplitLaunchOptionsKeepsQuotedArgument(t *testing.T) {
	t.Parallel()
	args, err := splitLaunchOptions(`-screen-width 1920 -data "folder with spaces"`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"-screen-width", "1920", "-data", "folder with spaces"}
	if len(args) != len(want) {
		t.Fatalf("args = %#v", args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("args = %#v", args)
		}
	}
}
