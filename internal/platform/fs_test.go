package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsValidWindowsFilename(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "ok", in: "folder", want: true},
		{name: "empty", in: "", want: false},
		{name: "too long", in: strings.Repeat("a", 256), want: false},
		{name: "max length", in: strings.Repeat("a", 255), want: true},
		{name: "255 Hangul UTF-16 units", in: strings.Repeat("가", 255), want: true},
		{name: "256 Hangul UTF-16 units", in: strings.Repeat("가", 256), want: false},
		{name: "254 emoji UTF-16 units", in: strings.Repeat("😀", 127), want: true},
		{name: "256 emoji UTF-16 units", in: strings.Repeat("😀", 128), want: false},
		{name: "invalid char", in: "a<b", want: false},
		{name: "slash", in: "a/b", want: false},
		{name: "only dots", in: "...", want: false},
		{name: "trailing space", in: "name ", want: false},
		{name: "trailing dot", in: "name.", want: false},
		{name: "reserved", in: "CON", want: false},
		{name: "reserved ext", in: "con.txt", want: false},
		{name: "com1", in: "COM1", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := fs.IsValidWindowsFilename(tt.in); got != tt.want {
				t.Fatalf("IsValidWindowsFilename(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSanitizeWindowsFilename(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	if got := fs.SanitizeWindowsFilename("a<b>c", ""); got != "a b c" {
		t.Fatalf("default sanitize = %q", got)
	}
	if got := fs.SanitizeWindowsFilename("a<b", "_"); got != "a_b" {
		t.Fatalf("custom sanitize = %q", got)
	}
	if got := fs.SanitizeWindowsFilename("name...", ""); got != "name" {
		t.Fatalf("trailing dots = %q", got)
	}
	if got := fs.SanitizeWindowsFilename("<>", ""); got != "Untitled" {
		t.Fatalf("empty after sanitize = %q", got)
	}
}

func TestSanitizePathKeepsDrive(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	in := `C:` + string(os.PathSeparator) + `foo<bar`
	got := fs.SanitizePath(in)
	want := `C:` + string(os.PathSeparator) + `foo bar`
	if got != want {
		t.Fatalf("SanitizePath = %q, want %q", got, want)
	}
}

func TestGetUniqueName(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	if got := fs.GetUniqueName("mod", []string{"other"}); got != "mod" {
		t.Fatalf("free name = %q", got)
	}
	if got := fs.GetUniqueName("mod", []string{"mod"}); got != "mod (2)" {
		t.Fatalf("first collision = %q", got)
	}
	if got := fs.GetUniqueName("mod", []string{"Mod", "mod (2)"}); got != "mod (3)" {
		t.Fatalf("case-insensitive = %q", got)
	}
}

func TestMkdir(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	parent := t.TempDir()

	got, err := fs.Mkdir(parent, "  group  ")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	want := filepath.Join(parent, "group")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("dir missing: %v", err)
	}

	if _, err := fs.Mkdir(parent, "   "); !errors.Is(err, ErrInvalidGroupName) {
		t.Fatalf("blank name: %v", err)
	}
	if _, err := fs.Mkdir(parent, "CON"); !errors.Is(err, ErrInvalidWindowsFilename) {
		t.Fatalf("invalid name: %v", err)
	}
	_, err = fs.Mkdir(parent, "group")
	if err == nil || !strings.HasPrefix(err.Error(), "ALREADY_EXISTS:group") {
		t.Fatalf("exists: %v", err)
	}
}

func TestIsPathWritableAndMetadata(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	dir := t.TempDir()
	if !fs.IsPathWritable(dir) {
		t.Fatal("temp dir should be writable")
	}
	if fs.IsPathWritable(filepath.Join(dir, "missing")) {
		t.Fatal("missing path should not be writable")
	}

	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if !fs.IsPathWritable(file) {
		t.Fatal("file should be writable")
	}

	meta, err := fs.GetPathMetadata(file)
	if err != nil {
		t.Fatalf("GetPathMetadata: %v", err)
	}
	if meta.IsDirectory || !meta.IsFile || meta.Size != 5 {
		t.Fatalf("file meta = %+v", meta)
	}
	if meta.Mtime.IsZero() || meta.Ctime.IsZero() || meta.Birthtime.IsZero() {
		t.Fatalf("zero times: %+v", meta)
	}

	dirMeta, err := fs.GetPathMetadata(dir)
	if err != nil {
		t.Fatalf("dir metadata: %v", err)
	}
	if !dirMeta.IsDirectory || dirMeta.IsFile {
		t.Fatalf("dir meta = %+v", dirMeta)
	}
}

func TestGetFileWriteAccess(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	dir := t.TempDir()
	missing := filepath.Join(dir, "new.txt")
	got := fs.GetFileWriteAccess(missing, "")
	if !got.Writable || got.Exists || got.Locked {
		t.Fatalf("missing file = %+v", got)
	}
	if got.Processes == nil {
		t.Fatal("processes should be empty slice, not nil")
	}

	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got = fs.GetFileWriteAccess(file, dir)
	if !got.Writable || !got.Exists || got.Locked {
		t.Fatalf("existing file = %+v", got)
	}

	got = fs.GetFileWriteAccess(filepath.Join(dir, "nope", "file.txt"), filepath.Join(dir, "nope"))
	if got.Writable || got.Exists {
		t.Fatalf("missing parent = %+v", got)
	}
}

func TestInaccessibleFileWriteAccessOnlyUsesDetectedProcessesForLocked(t *testing.T) {
	t.Parallel()

	got := inaccessibleFileWriteAccess(nil)
	if !got.Exists || got.Writable || got.Locked || got.Processes == nil {
		t.Fatalf("without processes = %+v", got)
	}
	processes := []ProcessInfo{{Name: "game.exe", PID: 42}}
	got = inaccessibleFileWriteAccess(processes)
	if !got.Exists || !got.Locked || len(got.Processes) != 1 || got.Processes[0] != processes[0] {
		t.Fatalf("with processes = %+v", got)
	}
}

func TestFormatProcessList(t *testing.T) {
	t.Parallel()

	fs := NewFS()
	got := fs.FormatProcessList([]ProcessInfo{{Name: "game.exe", PID: 12}, {Name: "other", PID: 3}})
	if got != "game.exe (12), other (3)" {
		t.Fatalf("FormatProcessList = %q", got)
	}
}
