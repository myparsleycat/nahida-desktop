package togglepersist

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/mod/namespace"
)

func TestPersistWatcherSupportsJunctionModsRoot(t *testing.T) {
	t.Parallel()
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	mods := filepath.Join(filepath.Dir(harness.d3dxPath), "Mods")
	external := filepath.Join(t.TempDir(), "External Mods")
	if err := os.Rename(mods, external); err != nil {
		t.Fatal(err)
	}
	createPersistJunction(t, mods, external)
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.engine.Stop)

	harness.trigger([][2]string{{"Toggle", "2"}})
	harness.engine.Advance(3_000)
	raw, err := os.ReadFile(filepath.Join(external, "Example", "mod.ini"))
	if err != nil || !strings.Contains(string(raw), "$Toggle = 2") {
		t.Fatalf("junction target content = %q, %v; diagnostics = %v", raw, err, harness.errors)
	}
}

func TestPersistWatcherNamespaceTransactionsThroughJunctionModsRoot(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		blocked bool
	}{
		{name: "committed"},
		{name: "rolledback"},
		{name: "pending", blocked: true},
		{name: "invalid", blocked: true},
		{name: "journal junction", blocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
			root := filepath.Dir(harness.d3dxPath)
			mods := filepath.Join(root, "Mods")
			external := filepath.Join(t.TempDir(), "External Mods")
			if err := os.Rename(mods, external); err != nil {
				t.Fatal(err)
			}
			createPersistJunction(t, mods, external)
			physicalMod := filepath.Join(external, "Example")
			physicalINI := filepath.Join(physicalMod, "mod.ini")
			before, info, err := readPersistINI(physicalINI)
			if err != nil {
				t.Fatal(err)
			}
			rollback := errors.New("force transaction rollback")
			err = namespace.ApplyVerified(t.Context(), []namespace.Change{{
				ModPath: physicalMod, RelativePath: "mod.ini", Before: before,
				After: []byte(string(before) + "\n; namespace transaction\n"), Exists: true, Info: info,
			}}, nil, func() error {
				if test.name == "rolledback" {
					return rollback
				}
				return nil
			})
			if test.name == "rolledback" {
				if !errors.Is(err, rollback) {
					t.Fatalf("rollback = %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}

			if test.name == "journal junction" {
				transactions := filepath.Join(physicalMod, ".nhd-namespace", "transactions")
				outside := filepath.Join(t.TempDir(), "Transactions")
				if err := os.Rename(transactions, outside); err != nil {
					t.Fatal(err)
				}
				createPersistJunction(t, transactions, outside)
			} else if test.blocked {
				journals, err := filepath.Glob(
					filepath.Join(physicalMod, ".nhd-namespace", "transactions", "*", "journal.json"),
				)
				if err != nil || len(journals) != 1 {
					t.Fatalf("journals = %v, %v", journals, err)
				}
				journal, err := os.ReadFile(journals[0])
				if err != nil {
					t.Fatal(err)
				}
				content := "{"
				if test.name == "pending" {
					content = strings.Replace(string(journal), `"status":"committed"`, `"status":"pending"`, 1)
				}
				if err := os.WriteFile(journals[0], []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			pending, journalErr := namespace.HasIncompleteTransactions(physicalMod)
			if (pending || journalErr != nil) != test.blocked {
				t.Fatalf("physical transaction: pending = %v, error = %v", pending, journalErr)
			}
			index, err := indexPersistTargets(root)
			if err != nil {
				t.Fatal(err)
			}
			target, err := index.resolve(`$\Mods\Example\mod.ini\Toggle`)
			if test.blocked {
				if target != nil || err == nil {
					t.Fatalf("unfinished transaction target = %#v, %v", target, err)
				}
			} else if err != nil || target == nil || target.iniPath != harness.targetINIPath {
				t.Fatalf("finished transaction logical target = %#v, %v", target, err)
			}
			if err := harness.start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(harness.engine.Stop)
			harness.trigger([][2]string{{"Toggle", "2"}})
			harness.engine.Advance(3_000)
			raw, err := os.ReadFile(physicalINI)
			value := "$Toggle = 2"
			if test.blocked {
				value = "$Toggle = 0"
			}
			if err != nil || !strings.Contains(string(raw), value) {
				t.Fatalf("junction target content = %q, %v; diagnostics = %v", raw, err, harness.errors)
			}
		})
	}
}

func TestPersistTargetsResolveSymlinkedImporterAncestor(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	mod := filepath.Join(parent, "Importer", "Mods", "Example")
	if err := os.MkdirAll(mod, 0o700); err != nil {
		t.Fatal(err)
	}
	ini := filepath.Join(mod, "mod.ini")
	if err := os.WriteFile(ini, []byte("[Constants]\nglobal persist $Toggle = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, info, err := readPersistINI(ini)
	if err != nil {
		t.Fatal(err)
	}
	if err := namespace.Apply(t.Context(), []namespace.Change{{
		ModPath: mod, RelativePath: "mod.ini", Before: before,
		After: []byte(string(before) + "; namespace transaction\n"), Exists: true, Info: info,
	}}, nil); err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(t.TempDir(), "Linked")
	if err := os.Symlink(parent, link); err != nil {
		t.Skipf("directory symlinks are unavailable: %v", err)
	}
	index, err := indexPersistTargets(filepath.Join(link, "Importer"))
	if err != nil {
		t.Fatal(err)
	}
	if target, err := index.resolve(`$\Mods\Example\mod.ini\Toggle`); target == nil || err != nil {
		t.Fatalf("committed transaction blocked behind a symlinked ancestor: %#v, %v", target, err)
	}
}

func TestPersistWatcherIgnoresNestedJunctions(t *testing.T) {
	t.Parallel()
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	mods := filepath.Join(filepath.Dir(harness.d3dxPath), "Mods")
	collection := filepath.Join(t.TempDir(), "External Mods")
	if err := os.Rename(mods, collection); err != nil {
		t.Fatal(err)
	}
	createPersistJunction(t, mods, collection)
	external := t.TempDir()
	path := filepath.Join(external, "mod.ini")
	input := "namespace = Shared\n[Constants]\nglobal persist $Toggle = 7\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	createPersistJunction(t, filepath.Join(collection, "escape"), external)
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.engine.Stop)

	triggerPersistContent(t, harness, `$\Shared\Toggle = 9`)
	harness.engine.Advance(3_000)
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != input {
		t.Fatalf("nested junction INI changed: %q, %v", raw, err)
	}
}

func createPersistJunction(t *testing.T, link, target string) {
	t.Helper()
	output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		t.Fatalf("create junction: %v (%s)", err, output)
	}
}
