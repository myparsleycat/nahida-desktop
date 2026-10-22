package mod

import (
	"slices"
	"testing"

	"nahida.live/desktop/internal/db"
)

func TestRunningGamePrefersFocusedGameByImporter(t *testing.T) {
	t.Parallel()

	gimi, wwmi := "GIMI", "WWMI"
	games := []GameConfig{
		{Game: "메인", Importer: &gimi},
		{Game: "WW", Importer: &wwmi},
		{Game: "젠레스"},
	}
	running := map[uint32]string{10: "GIMI", 20: "WWMI", 30: "ZZMI"}

	cases := []struct {
		name    string
		focused []uint32
		running map[uint32]string
		want    string
	}{
		{name: "most recently focused", focused: []uint32{20, 10}, running: running, want: "WW"},
		{name: "game order without focus", running: running, want: "메인"},
		{name: "game without importer", running: map[uint32]string{30: "ZZMI"}},
		{name: "unregistered game", running: map[uint32]string{40: "SRMI"}},
	}
	for _, tc := range cases {
		got := runningGame(games, tc.focused, tc.running)
		if tc.want == "" {
			if got != nil {
				t.Fatalf("%s: runningGame = %q, want nil", tc.name, *got)
			}
			continue
		}
		if got == nil || *got != tc.want {
			t.Fatalf("%s: runningGame = %v, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNTEProcessNamesIncludesRegisteredExecutables(t *testing.T) {
	t.Parallel()

	executable, nte := `D:\Games\NTE\Client\NTEGame.exe`, db.NTEImporter
	games := []GameConfig{{Game: "이환", Importer: &nte, GameExecutablePath: &executable}}
	names := nteProcessNames(games)
	if !slices.Equal(names[db.NTEImporter], []string{nteExecutableName, "NTEGame.exe"}) {
		t.Fatalf("nteProcessNames = %v", names)
	}
	if got := runningGame(games, nil, map[uint32]string{10: db.NTEImporter}); got == nil || *got != "이환" {
		t.Fatalf("runningGame = %v, want 이환", got)
	}

	gimi := "GIMI"
	if names := nteProcessNames([]GameConfig{{Game: "원신", Importer: &gimi}}); names != nil {
		t.Fatalf("nteProcessNames without an NTE game = %v", names)
	}
}
