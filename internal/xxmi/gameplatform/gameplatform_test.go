package gameplatform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const localConfigFixture = "\"UserLocalConfigStore\"\r\n{\r\n" +
	"\t\"friends\"\r\n\t{\r\n\t\t\"PersonaName\"\t\t\"Tester \\\"T\\\"\"\r\n\t}\r\n" +
	"\t\"Software\"\r\n\t{\r\n\t\t\"Valve\"\r\n\t\t{\r\n\t\t\t\"Steam\"\r\n\t\t\t{\r\n" +
	"\t\t\t\t\"apps\"\r\n\t\t\t\t{\r\n" +
	"\t\t\t\t\t\"3513350\"\r\n\t\t\t\t\t{\r\n" +
	"\t\t\t\t\t\t\"LastPlayed\"\t\t\"1700000000\"\r\n" +
	"\t\t\t\t\t\t\"LaunchOptions\"\t\t\"-old \\\"C:\\\\Path With Spaces\\\\x.exe\\\"\"\r\n" +
	"\t\t\t\t\t}\r\n" +
	"\t\t\t\t\t\"4162040\"\r\n\t\t\t\t\t{\r\n\t\t\t\t\t\t\"LastPlayed\"\t\t\"1\"\r\n\t\t\t\t\t}\r\n" +
	"\t\t\t\t}\r\n\t\t\t}\r\n\t\t}\r\n\t}\r\n}\r\n"

// steamFixture builds a Steam folder with one extra library and two signed-in accounts.
func steamFixture(t *testing.T) (Steam, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "Steam Client")
	library := filepath.Join(t.TempDir(), "Steam Library")
	account := filepath.Join(root, "userdata", "2", "config")
	files := map[string]string{
		filepath.Join(root, "steam.exe"): "",
		filepath.Join(root, "config", "loginusers.vdf"): `"users"
{
	"76561197960265729"
	{
		"AccountName"		"older"
		"Timestamp"		"100"
	}
	"76561197960265730"
	{
		"AccountName"		"recent"
		"Timestamp"		"200"
	}
}
`,
		filepath.Join(account, "localconfig.vdf"):                         localConfigFixture,
		filepath.Join(root, "userdata", "1", "config", "localconfig.vdf"): `"UserLocalConfigStore" { }`,
		filepath.Join(root, "steamapps", "libraryfolders.vdf"): `"libraryfolders"
{
	"0"
	{
		"path"		"` + strings.ReplaceAll(root, `\`, `\\`) + `"
	}
	"1"
	{
		"path"		"` + strings.ReplaceAll(library, `\`, `\\`) + `"
	}
	"2"		"` + strings.ReplaceAll(filepath.Join(root, "missing"), `\`, `\\`) + `"
}
`,
		filepath.Join(root, "steamapps", "appmanifest_10.acf"): `"AppState" { "appid" "10" "name" "Other Game" "installdir" "Other" }`,
		filepath.Join(root, "steamapps", "appmanifest_11.acf"): `"AppState" { broken`,
		filepath.Join(library, "steamapps", "appmanifest_3513350.acf"): `"AppState"
{
	"appid"		"3513350"
	"name"		"Renamed Title"
	"installdir"		"Renamed Folder"
}
`,
		filepath.Join(library, "steamapps", "appmanifest_99.acf"): `"AppState" { "appid" "99" "name" "Zenless Zone Zero" "installdir" "ZZZ" }`,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return Steam{Exe: filepath.Join(root, "steam.exe")}, library
}

func TestSteamFindsGamesAcrossLibraries(t *testing.T) {
	t.Parallel()
	steam, library := steamFixture(t)
	for _, tc := range []struct {
		importer string
		id       string
		folder   string
	}{
		// The application ID wins even when neither the folder nor the title names the game.
		{"WWMI", "3513350", filepath.Join(library, "steamapps", "common", "Renamed Folder")},
		// An unknown application ID still matches through the store title.
		{"ZZMI", "99", filepath.Join(library, "steamapps", "common", "ZZZ")},
	} {
		game, _ := GameFor(tc.importer)
		app, found, err := steam.FindGame(game)
		if err != nil || !found || app.ID != tc.id || app.InstallDir != tc.folder {
			t.Errorf("%s: app = %+v, found = %t, err = %v", tc.importer, app, found, err)
		}
	}
	game, _ := GameFor("GIMI")
	if _, found, err := steam.FindGame(game); err != nil || found {
		t.Fatalf("missing game: found = %t, err = %v", found, err)
	}
	if args := steam.LaunchArgs("3513350"); strings.Join(args, " ") != "-silent -applaunch 3513350" {
		t.Fatalf("launch args = %q", args)
	}
}

func TestSteamLaunchOptionsRewriteOnlyTheValue(t *testing.T) {
	t.Parallel()
	steam, _ := steamFixture(t)
	path, err := steam.localConfigPath()
	if err != nil || filepath.Base(filepath.Dir(filepath.Dir(path))) != "2" {
		t.Fatalf("active account config = %q, %v", path, err)
	}
	options, err := steam.LaunchOptions("3513350")
	if err != nil || options != `-old "C:\Path With Spaces\x.exe"` {
		t.Fatalf("launch options = %q, %v", options, err)
	}

	updated := `"C:\Games\ZZZ\Game.exe" && %command% -dx11`
	if err := steam.SetLaunchOptions("3513350", updated); err != nil {
		t.Fatal(err)
	}
	if options, err = steam.LaunchOptions("3513350"); err != nil || options != updated {
		t.Fatalf("updated launch options = %q, %v", options, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(
		localConfigFixture,
		`"-old \"C:\\Path With Spaces\\x.exe\""`,
		`"\"C:\\Games\\ZZZ\\Game.exe\" && %command% -dx11"`,
		1,
	)
	if string(data) != want {
		t.Fatalf("localconfig.vdf = %q\nwant %q", data, want)
	}

	// An application without launch options gains the key and nothing else changes.
	if err := steam.SetLaunchOptions("4162040", "-a"); err != nil {
		t.Fatal(err)
	}
	if options, err = steam.LaunchOptions("4162040"); err != nil || options != "-a" {
		t.Fatalf("new launch options = %q, %v", options, err)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	inserted := "\"4162040\"\r\n\t\t\t\t\t{\r\n\t\t\t\t\t\t\"LaunchOptions\"\t\t\"-a\"\r\n\t\t\t\t\t\t\"LastPlayed\"\t\t\"1\""
	if !strings.Contains(string(data), inserted) ||
		len(data) != len(want)+len("\r\n\t\t\t\t\t\t\"LaunchOptions\"\t\t\"-a\"") {
		t.Fatalf("localconfig.vdf after insert = %q", data)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), "*.tmp")); len(leftovers) != 0 {
		t.Fatalf("temporary files left behind: %q", leftovers)
	}

	if _, err := steam.LaunchOptions("1"); !errors.Is(err, ErrSteamAppNotConfigured) {
		t.Fatalf("unknown application error = %v", err)
	}
	if err := steam.SetLaunchOptions("1", "-a"); !errors.Is(err, ErrSteamAppNotConfigured) {
		t.Fatalf("unknown application write error = %v", err)
	}
}

func TestSteamWithoutSignedInUser(t *testing.T) {
	t.Parallel()
	steam := Steam{Exe: filepath.Join(t.TempDir(), "steam.exe")}
	if _, err := steam.LaunchOptions("10"); !errors.Is(err, ErrSteamUserNotFound) {
		t.Fatalf("error = %v", err)
	}
	game, _ := GameFor("WWMI")
	if _, found, err := steam.FindGame(game); err != nil || found {
		t.Fatalf("empty client: found = %t, err = %v", found, err)
	}
}

func TestParseVDFRejectsMalformedDocuments(t *testing.T) {
	t.Parallel()
	for _, document := range []string{`"a" {`, `"a" "b" }`, `"a"`, `"a" "unterminated`} {
		if _, err := parseVDF([]byte(document)); err == nil {
			t.Errorf("%q was accepted", document)
		}
	}
	root, err := parseVDF([]byte("\xef\xbb\xbf// comment\nbare value\n\"k\" \"a\\tb\\\\c\" [$WIN32]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if value, _ := root.text("BARE"); value != "value" {
		t.Fatalf("bare token = %q", value)
	}
	if value, _ := root.text("k"); value != "a\tb\\c" {
		t.Fatalf("escaped value = %q", value)
	}
}

func TestFindEpicGame(t *testing.T) {
	t.Parallel()
	manifest := filepath.Join(t.TempDir(), "Launcher Installed.dat")
	content := "\xef\xbb\xbf" + `{"InstallationList":[
		{"InstallLocation":"D:\\Games\\Other","NamespaceId":"n0","ItemId":"i0","ArtifactId":"a0"},
		{"InstallLocation":"D:\\Games\\Incomplete Wuthering","NamespaceId":"","ItemId":"i","ArtifactId":"a"},
		{"InstallLocation":"D:/Epic Games/Wuthering_Waves","NamespaceId":"ns","ItemId":"item","ArtifactId":"art"}
	]}`
	if err := os.WriteFile(manifest, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	game, _ := GameFor("wwmi")
	app, found, err := FindEpicGame(manifest, game)
	if err != nil || !found || app.InstallDir != `D:\Epic Games\Wuthering_Waves` {
		t.Fatalf("app = %+v, found = %t, err = %v", app, found, err)
	}
	want := EpicURIPrefix + `ns%3Aitem%3Aart?action=launch&silent=true&args="-dx11%20-a%26b%20%22q%22"`
	if uri := app.LaunchURI(` -dx11 -a&b "q" `); uri != want {
		t.Fatalf("launch URI = %q", uri)
	}
	if uri := app.LaunchURI(""); uri != EpicURIPrefix+"ns%3Aitem%3Aart?action=launch&silent=true" {
		t.Fatalf("launch URI without arguments = %q", uri)
	}

	other, _ := GameFor("GIMI")
	if _, found, err := FindEpicGame(manifest, other); err != nil || found {
		t.Fatalf("missing game: found = %t, err = %v", found, err)
	}
	if _, _, err := FindEpicGame(filepath.Join(t.TempDir(), "absent.dat"), game); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing launcher error = %v", err)
	}
}
