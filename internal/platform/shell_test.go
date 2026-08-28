package platform

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestGetAppStatus(t *testing.T) {
	s := NewShell()
	s.version = "0.1.0"

	t.Setenv("NAHIDA_DEV", "1")
	got := s.GetAppStatus()
	if got.Version != "0.1.0" {
		t.Fatalf("version = %q", got.Version)
	}
	if got.IsPackaged || !got.IsDev {
		t.Fatalf("dev status = %+v", got)
	}
	wantPlatform := "win32"
	if got.Platform != wantPlatform {
		t.Fatalf("platform = %q, want %q", got.Platform, wantPlatform)
	}

	t.Setenv("NAHIDA_DEV", "")
	got = s.GetAppStatus()
	if !got.IsPackaged || got.IsDev {
		t.Fatalf("packaged status = %+v", got)
	}
}

func TestOpenExternalChoosesURLOrPath(t *testing.T) {
	t.Parallel()

	var urls, paths []string
	s := NewShell()
	s.openURL = func(target string) error {
		urls = append(urls, target)
		return nil
	}
	s.openPath = func(target string) error {
		paths = append(paths, target)
		return nil
	}

	if err := s.OpenExternal("https://nahida.live/path"); err != nil {
		t.Fatalf("url: %v", err)
	}
	if err := s.OpenExternal(`C:\mods\folder`); err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := s.OpenPath(`D:\file.txt`); err != nil {
		t.Fatalf("OpenPath: %v", err)
	}
	if len(urls) != 1 || urls[0] != "https://nahida.live/path" {
		t.Fatalf("urls = %#v", urls)
	}
	if len(paths) != 2 || paths[0] != `C:\mods\folder` || paths[1] != `D:\file.txt` {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestOpenExternalFallsBackToPathWhenURLHandlerFails(t *testing.T) {
	t.Parallel()

	var path string
	s := NewShell()
	s.openURL = func(string) error { return errors.New("no protocol handler") }
	s.openPath = func(target string) error {
		path = target
		return nil
	}
	const target = "custom://missing-handler"
	if err := s.OpenExternal(target); err != nil {
		t.Fatal(err)
	}
	if path != target {
		t.Fatalf("fallback path = %q, want %q", path, target)
	}
}

func TestParseExternalURL(t *testing.T) {
	t.Parallel()

	if _, ok := parseExternalURL("https://example.com"); !ok {
		t.Fatal("https should parse")
	}
	if _, ok := parseExternalURL("C:\\mods"); ok {
		t.Fatal("windows path must not be a URL")
	}
	if _, ok := parseExternalURL("/tmp/file"); ok {
		t.Fatal("posix path must not be a URL")
	}
}

func TestTrashDelegates(t *testing.T) {
	t.Parallel()

	var got string
	s := NewShell()
	s.trashItem = func(path string) error {
		got = path
		return nil
	}
	if err := s.Trash(`D:\gone.txt`); err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if got != `D:\gone.txt` {
		t.Fatalf("trashed = %q", got)
	}
}

func TestTrashMissingPath(t *testing.T) {
	s := NewShell()
	missing := filepath.Join(t.TempDir(), "missing.txt")
	if err := s.Trash(missing); err == nil {
		t.Fatal("missing path should fail")
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("stat missing: %v", err)
	}
}
