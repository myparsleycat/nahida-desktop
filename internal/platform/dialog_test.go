package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveFileCanceledAndPath(t *testing.T) {
	t.Parallel()

	d := NewDialog()
	d.saveFile = func(opts SaveFileOptions) (string, error) {
		if opts.SuggestedName != "out.txt" {
			t.Fatalf("suggested = %q", opts.SuggestedName)
		}
		if len(opts.Filters) != 1 || opts.Filters[0].Name != "Text" {
			t.Fatalf("filters = %#v", opts.Filters)
		}
		return "", nil
	}
	got, err := d.SaveFile(SaveFileOptions{
		SuggestedName: "out.txt",
		Filters:       []FileFilter{{Name: "Text", Extensions: []string{"txt"}}},
	})
	if err != nil {
		t.Fatalf("SaveFile: %v", err)
	}
	if !got.Canceled || got.FilePath != "" {
		t.Fatalf("canceled = %+v", got)
	}

	d.saveFile = func(SaveFileOptions) (string, error) {
		return `D:\out.txt`, nil
	}
	got, err = d.SaveFile(SaveFileOptions{})
	if err != nil {
		t.Fatalf("SaveFile path: %v", err)
	}
	if got.Canceled || got.FilePath != `D:\out.txt` {
		t.Fatalf("path = %+v", got)
	}
}

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

func TestSelectDirectory(t *testing.T) {
	t.Parallel()

	d := NewDialog()
	d.selectDirectory = func() (string, error) {
		return "", nil
	}
	got, err := d.SelectDirectory()
	if err != nil {
		t.Fatalf("SelectDirectory: %v", err)
	}
	if !got.Canceled {
		t.Fatalf("canceled = %+v", got)
	}

	d.selectDirectory = func() (string, error) {
		return `D:\mods`, nil
	}
	got, err = d.SelectDirectory()
	if err != nil {
		t.Fatalf("SelectDirectory path: %v", err)
	}
	if got.Canceled || got.FilePath != `D:\mods` {
		t.Fatalf("path = %+v", got)
	}

	d.selectDirectory = func() (string, error) {
		return "", ErrMainWindowNotFound
	}
	if _, err := d.SelectDirectory(); err == nil {
		t.Fatal("expected window error")
	}
}

func TestFileFilterPattern(t *testing.T) {
	t.Parallel()

	if got := fileFilterPattern([]string{"txt", ".png", "*.jpg", ""}); got != "*.txt;*.png;*.jpg" {
		t.Fatalf("pattern = %q", got)
	}
}
