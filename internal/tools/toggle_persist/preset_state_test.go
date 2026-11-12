package togglepersist

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/gamelink"
)

const presetTestINI = "namespace = Custom\\Foo\n" +
	"[Constants]\n" +
	"global persist $Outfit = 0\n" +
	"global persist $Hat = 0\n" +
	"global $frame = 0\n"

func presetTestMod(t *testing.T, userINI string) (importer, modPath string) {
	t.Helper()
	importer = t.TempDir()
	modPath = filepath.Join(importer, "Mods", "Foo")
	if err := os.MkdirAll(filepath.Join(modPath, "DISABLED old"), 0o700); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		filepath.Join(importer, "d3dx.ini"):                   "[Loader]\n",
		filepath.Join(importer, "d3dx_user.ini"):              userINI,
		filepath.Join(modPath, "foo.ini"):                     presetTestINI,
		filepath.Join(modPath, "DISABLED old", "old.ini"):     presetTestINI,
		filepath.Join(modPath, "DISABLED extra.ini"):          presetTestINI,
		filepath.Join(modPath, "notes.txt"):                   "",
		filepath.Join(importer, "Mods", "Baz", "baz.ini"):     presetTestINI,
		filepath.Join(importer, "Mods", "Baz", "baz_two.ini"): presetTestINI,
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return importer, modPath
}

// 3DMigoto runs d3dx_user.ini top to bottom, so of the two names a value may be saved under, the later line is
// the value the game starts with.
func TestCaptureModStateTakesTheLastSavedValue(t *testing.T) {
	for _, tc := range []struct {
		name string
		user string
		want string
	}{
		{
			name: "declared last",
			user: "[Constants]\n$\\mods\\foo\\foo.ini\\outfit = 1\n$\\custom\\foo\\outfit = 2\n",
			want: "2",
		},
		{
			name: "path last",
			user: "[Constants]\n$\\custom\\foo\\outfit = 2\n$\\mods\\foo\\foo.ini\\outfit = 1\n",
			want: "1",
		},
		{name: "not saved", user: "[Constants]\n$\\mods\\baz\\baz.ini\\outfit = 5\n", want: "0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, modPath := presetTestMod(t, tc.user)
			captured, err := NewWithOptions(Options{}).CaptureModState(t.Context(), modPath)
			if err != nil {
				t.Fatal(err)
			}
			want := INIValues{"foo.ini": {"Outfit": tc.want, "Hat": "0"}}
			if captured.Source != CaptureSourceINI || !reflect.DeepEqual(captured.INIs, want) {
				t.Fatalf("captured %+v, want %v from the INI", captured, want)
			}
		})
	}
}

func TestCaptureModStatePrefersTheLinkedGame(t *testing.T) {
	importer, modPath := presetTestMod(t, "[Constants]\n$\\custom\\foo\\hat = 1\n")
	link := &fakeGameLink{
		states: []gamelink.State{linkedGame(20, "GIMI", importer)},
		values: map[string]float32{`$\custom\foo\outfit`: 0.5, `$\custom\foo\hat`: 3},
	}
	service := NewWithOptions(Options{Link: link})

	captured, err := service.CaptureModState(t.Context(), modPath)
	if err != nil {
		t.Fatal(err)
	}
	want := INIValues{"foo.ini": {"Outfit": "0.5", "Hat": "3"}}
	if captured.Source != CaptureSourceGame || !reflect.DeepEqual(captured.INIs, want) {
		t.Fatalf("captured %+v, want %v from the game", captured, want)
	}

	link.missing = map[string]bool{`$\custom\foo\hat`: true}
	captured, err = service.CaptureModState(t.Context(), modPath)
	if err != nil {
		t.Fatal(err)
	}
	want = INIValues{"foo.ini": {"Outfit": "0.5", "Hat": "1"}}
	if captured.Source != CaptureSourceMixed || !reflect.DeepEqual(captured.INIs, want) {
		t.Fatalf("captured %+v, want %v from the game and d3dx_user.ini", captured, want)
	}
}

// A value applied only to the mod INI would be replaced on the next launch by the one d3dx_user.ini saved.
func TestApplyModStateWritesINIUserINIAndGame(t *testing.T) {
	importer, modPath := presetTestMod(t, "[Constants]\n"+
		"$\\custom\\foo\\outfit = 1\n"+
		"$\\mods\\foo\\foo.ini\\outfit = 1\n"+
		"$\\mods\\baz\\baz.ini\\outfit = 1\n")
	link := &fakeGameLink{
		states: []gamelink.State{linkedGame(20, "GIMI", importer)}, sent: map[uint32][]gamelink.Assign{},
		values: map[string]float32{},
	}
	service := NewWithOptions(Options{Link: link})

	result, err := service.ApplyModState(t.Context(), modPath, INIValues{
		"FOO.ini": {"outfit": "3", "frame": "1", "gone": "1"},
		"bar.ini": {"outfit": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}

	content, err := os.ReadFile(filepath.Join(modPath, "foo.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "global persist $Outfit = 3\n") ||
		!strings.Contains(string(content), "global $frame = 0\n") {
		t.Fatalf("mod INI:\n%s", content)
	}
	user, err := os.ReadFile(filepath.Join(importer, "d3dx_user.ini"))
	if err != nil {
		t.Fatal(err)
	}
	wantUser := "[Constants]\n" +
		"$\\custom\\foo\\outfit = 3\n" +
		"$\\mods\\foo\\foo.ini\\outfit = 3\n" +
		"$\\mods\\baz\\baz.ini\\outfit = 1\n"
	if string(user) != wantUser {
		t.Fatalf("d3dx_user.ini:\n%q\nwant\n%q", user, wantUser)
	}
	wantSent := []gamelink.Assign{{Name: `$\custom\foo\outfit`, Value: 3}}
	if !reflect.DeepEqual(link.sent[20], wantSent) {
		t.Fatalf("sent %+v, want %+v", link.sent, wantSent)
	}
	if !reflect.DeepEqual(result.Skipped, []string{"foo.ini: frame", "foo.ini: gone"}) ||
		!reflect.DeepEqual(result.MissingINIs, []string{"bar.ini"}) || result.UserINI != 2 ||
		result.Games != 1 || result.UnlinkedGame {
		t.Fatalf("result %+v", result)
	}
}
