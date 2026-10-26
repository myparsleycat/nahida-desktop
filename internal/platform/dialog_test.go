package platform

import (
	"errors"
	"fmt"
	"testing"
)

func TestSaveFileCanceledAndPath(t *testing.T) {
	t.Parallel()

	d := NewDialog(DialogBackend{})
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

func TestSelectDirectory(t *testing.T) {
	t.Parallel()

	d := NewDialog(DialogBackend{})
	d.selectDirectory = func(string) (string, error) {
		return "", nil
	}
	got, err := d.SelectDirectory("")
	if err != nil {
		t.Fatalf("SelectDirectory: %v", err)
	}
	if !got.Canceled {
		t.Fatalf("canceled = %+v", got)
	}

	d.selectDirectory = func(string) (string, error) {
		return `D:\mods`, nil
	}
	got, err = d.SelectDirectory("")
	if err != nil {
		t.Fatalf("SelectDirectory path: %v", err)
	}
	if got.Canceled || got.FilePath != `D:\mods` {
		t.Fatalf("path = %+v", got)
	}

	d.selectDirectory = func(string) (string, error) {
		return "", ErrMainWindowNotFound
	}
	if _, err := d.SelectDirectory(""); err == nil {
		t.Fatal("expected window error")
	}
}

func TestDialogCancellationSentinel(t *testing.T) {
	t.Parallel()

	t.Run("save file", func(t *testing.T) {
		t.Parallel()
		d := NewDialog(DialogBackend{})
		d.saveFile = func(SaveFileOptions) (string, error) {
			return "", fmt.Errorf("native dialog: %w", ErrDialogCancelled)
		}
		got, err := d.SaveFile(SaveFileOptions{})
		if err != nil || !got.Canceled || got.FilePath != "" {
			t.Fatalf("SaveFile() = %+v, %v", got, err)
		}
	})

	t.Run("directory", func(t *testing.T) {
		t.Parallel()
		d := NewDialog(DialogBackend{})
		d.selectDirectory = func(string) (string, error) {
			return "", ErrDialogCancelled
		}
		got, err := d.SelectDirectory("")
		if err != nil || !got.Canceled || got.FilePath != "" {
			t.Fatalf("SelectDirectory() = %+v, %v", got, err)
		}
	})

	for _, properties := range [][]string{{"openFile"}, {"openFile", "multiSelections"}} {
		name := "single open"
		if len(properties) > 1 {
			name = "multiple open"
		}
		t.Run(name, func(t *testing.T) {
			d := NewDialog(DialogBackend{})
			d.openDialog = func(OpenDialogOptions) ([]string, error) {
				return nil, ErrDialogCancelled
			}
			got, err := d.ShowOpenDialog(OpenDialogOptions{Properties: properties})
			if err != nil || !got.Canceled || got.FilePaths == nil || len(got.FilePaths) != 0 {
				t.Fatalf("ShowOpenDialog() = %+v, %v", got, err)
			}
		})
	}
}

func TestDialogPreservesNonCancellationErrors(t *testing.T) {
	t.Parallel()

	dialogErr := errors.New("native dialog failed")
	d := NewDialog(DialogBackend{})
	d.saveFile = func(SaveFileOptions) (string, error) { return "", dialogErr }
	d.selectDirectory = func(string) (string, error) { return "", dialogErr }
	d.openDialog = func(OpenDialogOptions) ([]string, error) { return nil, dialogErr }

	if _, err := d.SaveFile(SaveFileOptions{}); !errors.Is(err, dialogErr) {
		t.Fatalf("SaveFile error = %v, want %v", err, dialogErr)
	}
	if _, err := d.SelectDirectory(""); !errors.Is(err, dialogErr) {
		t.Fatalf("SelectDirectory error = %v, want %v", err, dialogErr)
	}
	if _, err := d.ShowOpenDialog(OpenDialogOptions{}); !errors.Is(err, dialogErr) {
		t.Fatalf("ShowOpenDialog error = %v, want %v", err, dialogErr)
	}
}
