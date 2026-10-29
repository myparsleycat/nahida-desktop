package platform

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLockedExecutableRejectsReplacementAndReleases(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "folder with spaces")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "probe.dll")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := LockExecutable(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	for _, operation := range []string{"write", "remove", "rename", "parent"} {
		command := exec.Command(os.Args[0], "-test.run=^TestLockedExecutableMutationChild$")
		command.Env = append(os.Environ(), "NAHIDA_EXECUTABLE_MUTATION="+operation, "NAHIDA_EXECUTABLE_PATH="+path)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("%s was not blocked: %v\n%s", operation, err, output)
		}
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatalf("file remained locked: %v", err)
	}
	if err := os.Rename(dir, dir+"-moved"); err != nil {
		t.Fatalf("directory remained locked: %v", err)
	}
}

func TestLockedExecutableMutationChild(t *testing.T) {
	operation := os.Getenv("NAHIDA_EXECUTABLE_MUTATION")
	if operation == "" {
		return
	}
	path := os.Getenv("NAHIDA_EXECUTABLE_PATH")
	var err error
	switch operation {
	case "write":
		err = os.WriteFile(path, []byte("replacement"), 0o600)
	case "remove":
		err = os.Remove(path)
	case "rename":
		err = os.Rename(path, path+"-moved")
	case "parent":
		err = os.Rename(filepath.Dir(path), filepath.Dir(path)+"-moved")
	default:
		t.Fatal("unknown mutation")
	}
	if !errors.Is(err, windows.ERROR_SHARING_VIOLATION) && !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Fatalf("mutation error = %v, want sharing violation or access denied", err)
	}
}

func TestLockedExecutableResolvesAliases(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	dir := filepath.Join(root, "Actual directory with spaces")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Test library.dll")
	if err := os.WriteFile(path, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "junction")
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, dir).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	for _, alias := range []string{filepath.Join(link, "Test library.dll"), strings.ToUpper(path)} {
		file, err := LockExecutable(alias)
		if err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file.Path)
		if err != nil {
			t.Fatal(err)
		}
		original, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(info, original) {
			t.Fatalf("resolved %q to another file: %q", alias, file.Path)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("redirected junction", func(t *testing.T) {
		t.Parallel()
		other := filepath.Join(root, "replacement directory")
		if err := os.Mkdir(other, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, "Test library.dll"), []byte("replacement"), 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := LockExecutable(filepath.Join(link, "Test library.dll"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		if err := os.Remove(link); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, other).CombinedOutput(); err != nil {
			t.Fatalf("redirect fixture junction: %v (%s)", err, output)
		}
		data, err := os.ReadFile(file.Path)
		if err != nil || string(data) != "original" {
			t.Fatalf("redirected junction changed the executable: %q, %v", data, err)
		}
	})
	t.Run("short path", func(t *testing.T) {
		t.Parallel()
		pointer, err := windows.UTF16PtrFromString(path)
		if err != nil {
			t.Fatal(err)
		}
		buffer := make([]uint16, windows.MAX_LONG_PATH)
		length, err := windows.GetShortPathName(pointer, &buffer[0], uint32(len(buffer)))
		if err != nil {
			t.Fatal(err)
		}
		short := windows.UTF16ToString(buffer[:length])
		if SamePathFold(short, path) {
			t.Skip("filesystem does not provide short path aliases")
		}
		file, err := LockExecutable(short)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		original, _ := os.Stat(path)
		resolved, _ := file.File.Stat()
		if !os.SameFile(original, resolved) {
			t.Fatal("short path opened another file")
		}
	})
}
