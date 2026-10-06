package fixer4001

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/elevated"
)

func TestInstallFileCopiesUsesHelperOnlyWhenWriteIsDenied(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "built.dll")
	if err := os.WriteFile(source, []byte("built"), 0o600); err != nil {
		t.Fatal(err)
	}
	writable := filepath.Join(root, "writable", targetD3D11DLL)
	protected := filepath.Join(root, "protected", targetD3D11DLL)
	if err := os.MkdirAll(filepath.Dir(protected), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(protected, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	makeReadOnly(t, protected)

	helper := &recordingElevatedFiles{}
	lease := elevated.NewFileLease(helper)
	if err := installFileCopies(t.Context(), lease, []fileCopy{{Source: source, Target: writable}}, false); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(writable); err != nil || string(data) != "built" {
		t.Fatalf("writable target = %q, err = %v", data, err)
	}
	if helper.acquires != 0 {
		t.Fatal("a writable target started the helper")
	}

	copies := []fileCopy{{Source: source, Target: writable}, {Source: source, Target: protected}}
	if err := installFileCopies(t.Context(), lease, copies, false); err != nil {
		t.Fatal(err)
	}
	if got, want := helper.summary(), []string{"copy " + writable, "copy " + protected}; !slices.Equal(got, want) {
		t.Fatalf("helper operations = %q, want %q", got, want)
	}
	if data, err := os.ReadFile(protected); err != nil || string(data) != "old" {
		t.Fatalf("protected target was written without the helper: %q, err = %v", data, err)
	}
}

func TestInstallFileCopiesMarksHelperFailures(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, "built.dll")
	if err := os.WriteFile(source, []byte("built"), 0o600); err != nil {
		t.Fatal(err)
	}
	declined := errors.New("the operation was canceled by the user")
	helper := &recordingElevatedFiles{acquireErr: declined}
	lease := elevated.NewFileLease(helper)
	copies := []fileCopy{{Source: source, Target: filepath.Join(root, "importer", targetD3D11DLL)}}

	err := installFileCopies(t.Context(), lease, copies, true)
	var elevatedErr elevatedFileCopyError
	if !errors.As(err, &elevatedErr) || !errors.Is(err, declined) {
		t.Fatalf("install error = %v, want a marked elevation failure", err)
	}

	// The cleanup after a declined prompt must not ask again.
	if err := removeFilePaths(t.Context(), lease, []string{copies[0].Target}, true); !errors.Is(err, declined) {
		t.Fatalf("remove error = %v", err)
	}
	if helper.acquires != 1 || len(helper.ops) != 0 {
		t.Fatalf("acquires = %d, operations = %d; want one prompt and no request", helper.acquires, len(helper.ops))
	}
}
