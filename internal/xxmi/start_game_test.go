package xxmi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfiguredGameExecutableUsesAbsoluteGamePath(t *testing.T) {
	t.Parallel()
	folder := t.TempDir()
	second := filepath.Join(folder, "GameBeta.exe")
	if err := os.WriteFile(second, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	got := configuredGameExecutable(folder, []string{"Game.exe", "GameBeta.exe"})
	if got != second {
		t.Fatalf("configuredGameExecutable = %q, want %q", got, second)
	}
}

func TestConfiguredGameExecutableFallsBackToBasenameWithoutAbsoluteFolder(t *testing.T) {
	t.Parallel()
	if got := configuredGameExecutable("", []string{"Game.exe"}); got != "" {
		t.Fatalf("configuredGameExecutable = %q, want empty", got)
	}
}
