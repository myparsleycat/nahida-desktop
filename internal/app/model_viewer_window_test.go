package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/platform"
)

func TestModelViewerFolderPathExtendedPrefix(t *testing.T) {
	t.Parallel()

	folder := t.TempDir()
	if volume := filepath.VolumeName(folder); len(volume) != 2 || volume[1] != ':' {
		t.Skipf("temporary folder %q is not on a drive-letter volume", folder)
	}
	got, want := modelViewerFolderPath(`\\?\`+folder), modelViewerFolderPath(folder)
	if !platform.SamePathFold(got, want) {
		t.Errorf("comparison paths for %q differ: %q and %q", folder, got, want)
	}
}

func TestModelViewerFolderPathExpandsShortName(t *testing.T) {
	t.Parallel()

	folder := filepath.Join(t.TempDir(), "Model Viewer Folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	name, err := windows.UTF16PtrFromString(folder)
	if err != nil {
		t.Fatal(err)
	}
	var short string
	for buf := make([]uint16, windows.MAX_PATH); ; {
		length, err := windows.GetShortPathName(name, &buf[0], uint32(len(buf)))
		if err != nil {
			t.Fatal(err)
		}
		if int(length) < len(buf) {
			short = windows.UTF16ToString(buf[:length])
			break
		}
		buf = make([]uint16, length+1)
	}
	if platform.SamePathFold(short, folder) {
		t.Skip("the temporary volume does not provide an 8.3 alias")
	}

	got, want := modelViewerFolderPath(short), modelViewerFolderPath(folder)
	if !platform.SamePathFold(got, want) {
		t.Errorf("comparison paths of %q and %q differ: %q and %q", short, folder, got, want)
	}
}

func TestModelViewerFolderPathKeepsJunction(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	folder, junction := filepath.Join(root, "Target Folder"), filepath.Join(root, "Junction Folder")
	if err := os.Mkdir(folder, 0o700); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", junction, folder).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}

	got, want := modelViewerFolderPath(junction), modelViewerFolderPath(folder)
	if platform.SamePathFold(got, want) {
		t.Errorf("comparison paths of %q and %q collapsed to %q", junction, folder, got)
	}
}
