package togglepersist

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/windows"

	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/platform"
)

type fakeGameLink struct {
	states  []gamelink.State
	missing map[string]bool

	mu   sync.Mutex
	sent map[uint32][]gamelink.Assign
}

func TestApplyStateToGameSkipsDisabledPaths(t *testing.T) {
	for _, tc := range []struct {
		name string
		path string
	}{
		{name: "mod", path: `Mods\dIsAbLeD Foo\foo.ini`},
		{name: "parent", path: `Mods\DISABLED Group\Foo\foo.ini`},
		{name: "ini", path: `Mods\Foo\DISABLED foo.ini`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			importer := t.TempDir()
			path := filepath.Join(importer, tc.path)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(
				path,
				[]byte("namespace = Foo\n[Constants]\nglobal persist $outfit = 0\n"),
				0o600,
			); err != nil {
				t.Fatal(err)
			}
			link := &fakeGameLink{
				states: []gamelink.State{linkedGame(20, "GIMI", importer)}, sent: map[uint32][]gamelink.Assign{},
			}
			service := NewWithOptions(Options{Link: link})
			if service.GameLinked(path) {
				t.Error("disabled path reported linked")
			}
			result, err := service.ApplyStateToGame(t.Context(), path, map[string]any{"outfit": float64(2)}, true)
			if err != nil || result.Games != 0 || len(link.sent) != 0 {
				t.Fatalf("disabled INI applied to game: %+v, sent %+v, error %v", result, link.sent, err)
			}
		})
	}
}

func TestApplyStateToGamePreservesLinkedModsNamespace(t *testing.T) {
	root := t.TempDir()
	importer := filepath.Join(root, "DISABLED parent", "Importer")
	collection := filepath.Join(root, "DISABLED collection")
	modPath := filepath.Join(collection, "Foo")
	disabled := filepath.Join(collection, "DISABLED Foo")
	external := filepath.Join(root, "external")
	for _, path := range []string{importer, modPath, disabled, external} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{modPath, disabled, external} {
		if err := os.WriteFile(
			filepath.Join(path, "foo.ini"),
			[]byte("[Constants]\nglobal persist $outfit = 0\n"),
			0o600,
		); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "Importer Alias")
	for link, target := range map[string]string{
		filepath.Join(importer, "Mods"):     collection,
		filepath.Join(collection, "Linked"): external,
		alias:                               importer,
	} {
		if output, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Skipf("filesystem cannot create a junction: %v (%s)", err, output)
		}
	}

	for _, tc := range []struct {
		name  string
		path  string
		want  string
		short bool
	}{
		{name: "logical", path: filepath.Join(importer, "Mods", "Foo"), want: `$\mods\foo\foo.ini\outfit`},
		{name: "physical", path: modPath, want: `$\mods\foo\foo.ini\outfit`},
		{name: "case", path: strings.ToUpper(modPath), want: `$\mods\foo\foo.ini\outfit`},
		{name: "importer alias", path: filepath.Join(alias, "Mods", "Foo"), want: `$\mods\foo\foo.ini\outfit`},
		{name: "nested junction", path: filepath.Join(alias, "Mods", "Linked"), want: `$\mods\linked\foo.ini\outfit`},
		{name: "short physical", path: modPath, want: `$\mods\foo\foo.ini\outfit`, short: true},
		{name: "disabled logical", path: filepath.Join(importer, "Mods", "DISABLED Foo")},
		{name: "disabled physical", path: disabled},
		{name: "short disabled", path: disabled, short: true},
		{name: "unrelated", path: external},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if tc.short {
				long, err := windows.UTF16PtrFromString(path)
				if err != nil {
					t.Fatal(err)
				}
				buffer := make([]uint16, 32768)
				length, err := windows.GetShortPathName(long, &buffer[0], uint32(len(buffer)))
				if err != nil || int(length) >= len(buffer) {
					t.Fatalf("GetShortPathName(%q) = %d, %v", path, length, err)
				}
				path = windows.UTF16ToString(buffer[:length])
				if platform.SamePathFold(path, tc.path) {
					t.Skip("volume does not create 8.3 names")
				}
			}
			link := &fakeGameLink{
				states: []gamelink.State{linkedGame(20, "GIMI", importer)}, sent: map[uint32][]gamelink.Assign{},
			}
			service := NewWithOptions(Options{Link: link})
			if got := service.GameLinked(path); got != (tc.want != "") {
				t.Errorf("GameLinked(%q) = %v, want %v", path, got, tc.want != "")
			}
			result, err := service.ApplyStateToGame(
				t.Context(),
				filepath.Join(path, "foo.ini"),
				map[string]any{"outfit": float64(2)},
				false,
			)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if result.Games != 0 || len(link.sent) != 0 {
					t.Fatalf("unloaded INI applied: %+v, sent %+v", result, link.sent)
				}
				return
			}
			want := []gamelink.Assign{{Name: tc.want, Value: 2}}
			if result.Games != 1 || !reflect.DeepEqual(link.sent[20], want) {
				t.Fatalf("result %+v, sent %+v; want %+v", result, link.sent, want)
			}
		})
	}
}

func (f *fakeGameLink) States() []gamelink.State { return f.states }

func (f *fakeGameLink) Request(_ context.Context, pid uint32, msg gamelink.Message) (gamelink.Message, error) {
	set := msg.(gamelink.SetVars)
	f.mu.Lock()
	f.sent[pid] = append(f.sent[pid], set.Vars...)
	f.mu.Unlock()
	applied := gamelink.VarsApplied{Frame: 1}
	for _, v := range set.Vars {
		applied.Values = append(applied.Values, gamelink.Var{Name: v.Name, Exists: !f.missing[v.Name], Value: v.Value})
	}
	return applied, nil
}

func linkedGame(pid uint32, importer, folder string) gamelink.State {
	return gamelink.State{
		Importer: importer, PID: pid, Status: gamelink.StatusConnected,
		Info: gamelink.HelloReply{ImporterFolder: folder, Capabilities: gamelink.CapabilityVars},
	}
}

// The DLL creates no variable it does not know, so a name 3DMigoto would not give the declaration is dropped
// without an error. These names follow its rule: the INI's namespace line, or else its path below d3dx.ini.
func TestApplyStateToGameUsesTheNamesTheGameDeclares(t *testing.T) {
	importer := t.TempDir()
	plain := filepath.Join(importer, "Mods", "Foo", "foo.ini")
	named := filepath.Join(importer, "Mods", "Bar", "bar.ini")
	for path, content := range map[string]string{
		plain: "[Constants]\nglobal persist $Outfit = 0\nglobal $Hair = 0\nglobal persist $Name = 0\n",
		named: "namespace = Custom\\Bar\n[Constants]\nglobal persist $Outfit = 0\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := map[string]any{"outfit": float64(2), "Hair": "1", "name": "not a number", "absent": float64(1)}

	cases := []struct {
		name           string
		ini            string
		persistentOnly bool
		want           []gamelink.Assign
	}{
		{
			name: "path namespace",
			ini:  plain,
			want: []gamelink.Assign{
				{Name: `$\mods\foo\foo.ini\outfit`, Value: 2},
				{Name: `$\mods\foo\foo.ini\hair`, Value: 1},
			},
		},
		{
			name: "persistent only", ini: plain, persistentOnly: true,
			want: []gamelink.Assign{{Name: `$\mods\foo\foo.ini\outfit`, Value: 2}},
		},
		{
			name: "declared namespace", ini: named,
			want: []gamelink.Assign{{Name: `$\custom\bar\outfit`, Value: 2}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			link := &fakeGameLink{
				states: []gamelink.State{
					linkedGame(10, "GIMI", t.TempDir()),
					linkedGame(20, "GIMI", importer),
					linkedGame(30, "SRMI", t.TempDir()),
					{
						Importer: "ZZMI",
						Status:   gamelink.StatusBusy,
						Info:     gamelink.HelloReply{ImporterFolder: importer},
					},
				},
				missing: map[string]bool{`$\mods\foo\foo.ini\hair`: true},
				sent:    map[uint32][]gamelink.Assign{},
			}
			service := NewWithOptions(Options{Link: link})
			result, err := service.ApplyStateToGame(t.Context(), tc.ini, state, tc.persistentOnly)
			if err != nil {
				t.Fatal(err)
			}
			if result.Games != 1 || len(link.sent) != 1 || !reflect.DeepEqual(link.sent[20], tc.want) {
				t.Fatalf("result %+v, sent %+v; want %+v to GIMI", result, link.sent, tc.want)
			}
			if slices.Contains(result.Applied, `$\mods\foo\foo.ini\hair`) {
				t.Fatalf("a variable the game lacks was reported applied: %+v", result)
			}
		})
	}
}
