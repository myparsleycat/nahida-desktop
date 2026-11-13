package liquidemitter

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestValidateBodyPath(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "runtime")
	for _, test := range []struct {
		name      string
		path      string
		wantError bool
	}{
		{name: "missing parents", path: filepath.Join(root, "bodymesh", "data", "new.ib")},
		{name: "root itself", path: root, wantError: true},
		{name: "parent", path: filepath.Dir(root), wantError: true},
		{name: "sibling prefix", path: root + "-other/file.ib", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := validateBodyPath(root, test.path); (err != nil) != test.wantError {
				t.Fatalf("validateBodyPath(%q) = %v, want error=%v", test.path, err, test.wantError)
			}
		})
	}
}

func TestValidateBodyPathLinkedRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "runtime")
	target := t.TempDir()
	if output, err := exec.Command("cmd", "/c", "mklink", "/J", root, target).CombinedOutput(); err != nil {
		t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
	}
	if err := validateBodyPath(root, filepath.Join(root, "bodymesh", "data", "new.ib")); err != nil {
		t.Fatalf("rejected a contained path through a linked runtime root: %v", err)
	}
}

func TestValidateBodyPathFileLinks(t *testing.T) {
	t.Parallel()
	for _, parentOutside := range []bool{false, true} {
		name := "external file"
		if parentOutside {
			name = "external parent with internal target"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root, outside := t.TempDir(), t.TempDir()
			target, link := filepath.Join(outside, "file.ib"), filepath.Join(root, "file.ib")
			if parentOutside {
				dir := filepath.Join(root, "data")
				if output, err := exec.Command("cmd", "/c", "mklink", "/J", dir, outside).CombinedOutput(); err != nil {
					t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
				}
				target, link = filepath.Join(root, "file.ib"), filepath.Join(dir, "file.ib")
			}
			if err := os.WriteFile(target, []byte("fixture"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("filesystem cannot create a file symlink: %v", err)
			}
			if err := validateBodyPath(root, link); err == nil {
				t.Fatal("accepted file or atomic replacement parent outside runtime root")
			}
		})
	}
}
