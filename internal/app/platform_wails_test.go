package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"

	"nahida.live/desktop/internal/platform"
)

func TestOpenDialogDefaultDirectory(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "preview.png")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := openDialogDefaultDirectory(root); got != root {
		t.Fatalf("directory = %q", got)
	}
	if got := openDialogDefaultDirectory(file); got != root {
		t.Fatalf("file parent = %q", got)
	}
	if got := openDialogDefaultDirectory(filepath.Join(root, "future.png")); got != root {
		t.Fatalf("missing file parent = %q", got)
	}
	if got := openDialogDefaultDirectory("  "); got != "" {
		t.Fatalf("empty = %q", got)
	}
}

func TestFileFilterPattern(t *testing.T) {
	t.Parallel()

	if got := fileFilterPattern([]string{"txt", ".png", "*.jpg", ""}); got != "*.txt;*.png;*.jpg" {
		t.Fatalf("pattern = %q", got)
	}
}

func TestWailsDialogCancellationReadsAsCancelled(t *testing.T) {
	t.Parallel()

	dialog := platform.NewDialog(platform.DialogBackend{
		SaveFile: func(platform.SaveFileOptions) (string, error) {
			return "", dialogError(fmt.Errorf("native dialog: %w", application.ErrDialogCancelled))
		},
	})
	got, err := dialog.SaveFile(platform.SaveFileOptions{})
	if err != nil || !got.Canceled {
		t.Fatalf("SaveFile() = %+v, %v", got, err)
	}

	failure := errors.New("native dialog failed")
	if err := dialogError(failure); !errors.Is(err, failure) || errors.Is(err, platform.ErrDialogCancelled) {
		t.Fatalf("dialogError(%v) = %v", failure, err)
	}
}
