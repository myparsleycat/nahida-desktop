package togglepersist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPersistWatcherResolvesNamespaceDeclarations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string
		state     string
		want      map[string]string
		ambiguous bool
	}{
		{
			name: "ordinary copies remain independent",
			files: map[string]string{
				"Mods/Example - 1/mod.ini": "[Constants]\nglobal persist $Toggle = 0\n",
				"Mods/Example - 2/mod.ini": "[Constants]\nglobal persist $Toggle = 0\n",
			},
			state: `$\Mods\Example - 1\mod.ini\Toggle = 1` + "\n" + `$\Mods\Example - 2\mod.ini\Toggle = 2`,
			want: map[string]string{
				"Mods/Example - 1/mod.ini": "$Toggle = 1",
				"Mods/Example - 2/mod.ini": "$Toggle = 2",
			},
		},
		{
			name: "explicit namespaces without ini suffix",
			files: map[string]string{
				"Mods/Example - 1/mod.ini": "namespace = Creator\\One\n[Constants]\nglobal persist $Toggle = 0\n",
				"Mods/Example - 2/mod.ini": "namespace = Creator\\Two\n[Constants]\nglobal persist $Toggle = 0\n",
			},
			state: `$\Creator\One\Toggle = 1` + "\n" + `$\Creator\Two\Toggle = 2`,
			want: map[string]string{
				"Mods/Example - 1/mod.ini": "$Toggle = 1",
				"Mods/Example - 2/mod.ini": "$Toggle = 2",
			},
		},
		{
			name: "path shaped namespace cannot hijack another declaration",
			files: map[string]string{
				"Mods/Copy/mod.ini": "namespace = Mods\\Example\\mod.ini\n[Constants]\nglobal persist $Toggle = 0\n",
			},
			state:     `$\Mods\Example\mod.ini\Toggle = 1`,
			want:      map[string]string{"Mods/Copy/mod.ini": "$Toggle = 0", "Mods/Example/mod.ini": "$Toggle = 0"},
			ambiguous: true,
		},
		{
			name: "path shaped namespace does not require file to exist",
			files: map[string]string{
				"Mods/Copy/mod.ini": "namespace = Missing\\Original.ini\n[Constants]\nglobal persist $Toggle = 0\n",
			},
			state: `$\Missing\Original.ini\Toggle = 2`,
			want:  map[string]string{"Mods/Copy/mod.ini": "$Toggle = 2", "Mods/Example/mod.ini": "$Toggle = 0"},
		},
		{
			name: "helper ini shares namespace with distinct variables",
			files: map[string]string{
				"Mods/Copy/mod.ini": "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 0\n",
				"Mods/Copy/help.ini": "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Help = 0\n" +
					"[Present]\n$\\Creator\\Outfit\\Toggle = $Help\n",
			},
			state: `$\Creator\Outfit\Toggle = 2` + "\n" + `$\Creator\Outfit\Help = 1`,
			want: map[string]string{
				"Mods/Copy/mod.ini":  "$Toggle = 2",
				"Mods/Copy/help.ini": "global persist $Help = 1",
			},
		},
		{
			name: "duplicate explicit namespaces do not overwrite either copy",
			files: map[string]string{
				"Mods/One/mod.ini": "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 1\n",
				"Mods/Two/mod.ini": "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 2\n",
			},
			state: `$\Creator\Outfit\Toggle = 9`,
			want: map[string]string{
				"Mods/One/mod.ini": "$Toggle = 1",
				"Mods/Two/mod.ini": "$Toggle = 2",
			},
			ambiguous: true,
		},
		{
			name: "disabled copy is not a duplicate",
			files: map[string]string{
				"Mods/DISABLED One/mod.ini": "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 1\n",
				"Mods/Two/mod.ini":          "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 2\n",
			},
			state: `$\Creator\Outfit\Toggle = 9`,
			want: map[string]string{
				"Mods/DISABLED One/mod.ini": "$Toggle = 1",
				"Mods/Two/mod.ini":          "$Toggle = 9",
			},
		},
		{
			name: "unambiguous variables save alongside a conflict",
			files: map[string]string{
				"Mods/One/mod.ini": "namespace = Shared\n[Constants]\nglobal persist $Toggle = 1\nglobal persist $Unique = 0\n",
				"Mods/Two/mod.ini": "namespace = Shared\n[Constants]\nglobal persist $Toggle = 2\n",
			},
			state: `$\Shared\Toggle = 9` + "\n" + `$\Shared\Unique = 3`,
			want: map[string]string{
				"Mods/One/mod.ini": "$Toggle = 1\nglobal persist $Unique = 3",
				"Mods/Two/mod.ini": "$Toggle = 2",
			},
			ambiguous: true,
		},
		{
			name: "nonpersistent redeclaration is also ambiguous",
			files: map[string]string{
				"Mods/Copy/mod.ini":  "namespace = Shared\n[Constants]\nglobal persist $Toggle = 0\n",
				"Mods/Copy/help.ini": "namespace = Shared\n[Constants]\nglobal $Toggle = 2\n",
			},
			state:     `$\Shared\Toggle = 9`,
			want:      map[string]string{"Mods/Copy/mod.ini": "$Toggle = 0", "Mods/Copy/help.ini": "$Toggle = 2"},
			ambiguous: true,
		},
		{
			name: "case insensitive namespace and bom",
			files: map[string]string{
				"Mods/Copy/mod.ini": "\uFEFF; header\r\nNaMeSpAcE = Creator\\Outfit ; original namespace\r\n[constants]\r\nglobal\tpersist\t$Toggle = 0\r\n",
			},
			state: `$\CREATOR\OUTFIT\toggle = 2`,
			want:  map[string]string{"Mods/Copy/mod.ini": "$Toggle = 2"},
		},
		{
			name: "bom before constants",
			files: map[string]string{
				"Mods/Copy/mod.ini": "\uFEFF[Constants]\r\nglobal persist $Toggle = 0\r\n",
			},
			state: `$\Mods\Copy\mod.ini\Toggle = 2`,
			want:  map[string]string{"Mods/Copy/mod.ini": "$Toggle = 2"},
		},
		{
			name: "declaration without initializer",
			files: map[string]string{
				"Mods/Copy/mod.ini": "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle\n",
			},
			state: `$\Creator\Outfit\Toggle = 2`,
			want:  map[string]string{"Mods/Copy/mod.ini": "$Toggle = 2"},
		},
		{
			name: "namespace looking like traversal remains a label",
			files: map[string]string{
				"Mods/Copy/mod.ini": "namespace = ..\\outside.ini\n[Constants]\nglobal persist $Toggle = 0\n",
			},
			state: `$\..\outside.ini\Toggle = 2`,
			want:  map[string]string{"Mods/Copy/mod.ini": "$Toggle = 2"},
		},
		{
			name: "disabled ini backup is excluded",
			files: map[string]string{
				"Mods/Copy/mod.ini":                 "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 0\n",
				"Mods/Copy/DISABLED_BACKUP_mod.ini": "namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 1\n",
			},
			state: `$\Creator\Outfit\Toggle = 2`,
			want: map[string]string{
				"Mods/Copy/mod.ini":                 "$Toggle = 2",
				"Mods/Copy/DISABLED_BACKUP_mod.ini": "$Toggle = 1",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
			for path, content := range test.files {
				writePersistFixture(t, harness, path, content)
			}
			if err := harness.start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(harness.engine.Stop)

			triggerPersistContent(t, harness, test.state)
			harness.engine.Advance(3_000)
			for path, expected := range test.want {
				raw, err := os.ReadFile(filepath.Join(filepath.Dir(harness.d3dxPath), filepath.FromSlash(path)))
				if err != nil || !strings.Contains(string(raw), expected) {
					t.Errorf("%s = %q, %v; want %q", path, raw, err, expected)
				}
			}
			if test.ambiguous != (countContains(harness.errors, "ambiguous persist variable") > 0) {
				t.Errorf("ambiguity diagnostics = %v", harness.errors)
			}
		})
	}
}

func TestPersistWatcherRevalidatesQueuedOwnership(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*testing.T, *persistHarness)
	}{
		{
			name: "new duplicate",
			change: func(t *testing.T, harness *persistHarness) {
				t.Helper()
				writePersistFixture(
					t,
					harness,
					"Mods/Copy/mod.ini",
					"namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 7\n",
				)
			},
		},
		{
			name: "changed namespace",
			change: func(t *testing.T, harness *persistHarness) {
				t.Helper()
				writePersistFixture(
					t,
					harness,
					"Mods/Example/mod.ini",
					"namespace = Other\n[Constants]\nglobal persist $Toggle = 0\n",
				)
			},
		},
		{
			name: "file replaced by identical copy",
			change: func(t *testing.T, harness *persistHarness) {
				t.Helper()
				raw, err := os.ReadFile(harness.targetINIPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(harness.targetINIPath, harness.targetINIPath+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(harness.targetINIPath, raw, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "disabled during quiet window",
			change: func(t *testing.T, harness *persistHarness) {
				t.Helper()
				oldFolder := filepath.Dir(harness.targetINIPath)
				newFolder := filepath.Join(filepath.Dir(oldFolder), "DISABLED Example")
				if err := os.Rename(oldFolder, newFolder); err != nil {
					t.Fatal(err)
				}
				harness.targetINIPath = filepath.Join(newFolder, "mod.ini")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
			writePersistFixture(
				t,
				harness,
				"Mods/Example/mod.ini",
				"namespace = Creator\\Outfit\n[Constants]\nglobal persist $Toggle = 0\n",
			)
			if err := harness.start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(harness.engine.Stop)
			triggerPersistContent(t, harness, `$\Creator\Outfit\Toggle = 9`)

			test.change(t, harness)
			harness.engine.Advance(3_000)
			raw, err := os.ReadFile(harness.targetINIPath)
			if err != nil || !strings.Contains(string(raw), "$Toggle = 0") {
				t.Fatalf("queued update changed replaced target: %q, %v", raw, err)
			}
		})
	}
}

func TestPersistWatcherKeepsLaterUpdatesAfterAtomicWrite(t *testing.T) {
	t.Parallel()
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}, {"Amount", "0"}})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.engine.Stop)
	harness.trigger([][2]string{{"Toggle", "1"}, {"Amount", "0"}})
	harness.engine.Advance(1_000)
	harness.trigger([][2]string{{"Toggle", "1"}, {"Amount", "2"}})
	harness.engine.Advance(2_000)
	harness.engine.Advance(1_000)
	raw, err := os.ReadFile(harness.targetINIPath)
	if err != nil || !strings.Contains(string(raw), "$Toggle = 1") || !strings.Contains(string(raw), "$Amount = 2") {
		t.Fatalf("staggered updates = %q, %v", raw, err)
	}
}

func TestPersistWatcherDoesNotOverwriteAnExplicitSave(t *testing.T) {
	t.Parallel()
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.engine.Stop)
	harness.trigger([][2]string{{"Toggle", "1"}})
	if _, err := harness.engine.PersistStateToINI(harness.targetINIPath, map[string]any{"Toggle": 2}); err != nil {
		t.Fatal(err)
	}
	harness.engine.Advance(3_000)
	raw, err := os.ReadFile(harness.targetINIPath)
	if err != nil || !strings.Contains(string(raw), "$Toggle = 2") {
		t.Fatalf("explicit save was overwritten by old queued state: %q, %v", raw, err)
	}
}

func TestPersistWatcherSupportsLinkedModsRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	external := t.TempDir()
	if err := os.Symlink(external, filepath.Join(root, "Mods")); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	path := filepath.Join(external, "mod.ini")
	if err := os.WriteFile(path, []byte("[Constants]\nglobal persist $Toggle = 0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	index, err := indexPersistTargets(root)
	if err != nil {
		t.Fatal(err)
	}
	target, err := index.resolve(`$\Mods\mod.ini\Toggle`)
	if err != nil || target == nil {
		t.Fatalf("linked target = %#v, %v", target, err)
	}
	if _, err := applyPersistUpdatesChecked(target.iniPath, map[string]string{"toggle": "2"}, target); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(raw), "$Toggle = 2") {
		t.Fatalf("linked target content = %q, %v", raw, err)
	}
}

func TestPersistWatcherIgnoresNestedLinks(t *testing.T) {
	t.Parallel()
	harness := createPersistHarness(t, [][2]string{{"Toggle", "0"}})
	external := t.TempDir()
	path := filepath.Join(external, "mod.ini")
	input := "namespace = Shared\n[Constants]\nglobal persist $Toggle = 7\n"
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(filepath.Dir(harness.targetINIPath), "escape")); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	if err := harness.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(harness.engine.Stop)
	triggerPersistContent(t, harness, `$\Shared\Toggle = 9`)
	harness.engine.Advance(3_000)
	raw, err := os.ReadFile(path)
	if err != nil || string(raw) != input {
		t.Fatalf("linked INI changed: %q, %v", raw, err)
	}
}

func writePersistFixture(t *testing.T, harness *persistHarness, relative, content string) {
	t.Helper()
	path := filepath.Join(filepath.Dir(harness.d3dxPath), filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func triggerPersistContent(t *testing.T, harness *persistHarness, constants string) {
	t.Helper()
	if err := os.WriteFile(harness.d3dxPath, []byte("[Constants]\n"+constants+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	harness.onModify()
}
