package mod

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/infra"
)

func nteBootstrapZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for name, content := range files {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func writePak(t *testing.T, dirPath, fileName string) {
	t.Helper()
	if err := os.MkdirAll(dirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dirPath, fileName), []byte("pak"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func testNteRoots(modRoot string) nteRoots {
	return nteRoots{modRoot: modRoot}
}

func modNames(group FolderGroup) []string {
	names := make([]string, len(group.Mods))
	for i, info := range group.Mods {
		names[i] = info.Name
	}
	return names
}

func groupNames(groups []FolderGroup) []string {
	names := make([]string, len(groups))
	for i, group := range groups {
		names[i] = group.Name
	}
	return names
}

func TestConfigureAndCleanupNteModFolderReconcilesJunction(t *testing.T) {
	root := t.TempDir()
	firstTarget := filepath.Join(root, "custom-mods-a")
	secondTarget := filepath.Join(root, "custom-mods-b")
	linkPath := filepath.Join(root, "game", "Content", "Paks", "Mods")
	if err := os.MkdirAll(linkPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writePak(t, filepath.Join(linkPath, "Character", "Existing"), "existing.pak")

	if err := configureNteModFolder(firstTarget, &linkPath); err != nil {
		t.Fatal(err)
	}
	resolved, err := resolveNteLinkTarget(linkPath)
	if err != nil || !samePath(resolved, firstTarget) {
		info, statErr := os.Lstat(linkPath)
		readTarget, readErr := os.Readlink(linkPath)
		t.Fatalf("first junction target = %q, %v; lstat=%#v/%v; readlink=%q/%v", resolved, err, info, statErr, readTarget, readErr)
	}
	if !fileExists(filepath.Join(firstTarget, "Character", "Existing", "existing.pak")) {
		t.Fatal("existing game mod was not migrated into the custom folder")
	}

	if err := configureNteModFolder(secondTarget, &linkPath); err != nil {
		t.Fatal(err)
	}
	resolved, err = resolveNteLinkTarget(linkPath)
	if err != nil || !samePath(resolved, secondTarget) {
		t.Fatalf("updated junction target = %q, %v", resolved, err)
	}
	writePak(t, filepath.Join(secondTarget, "UI", "New"), "new.pak")

	if err := cleanupNteModFolder(secondTarget, &linkPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(linkPath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("cleaned link path = %#v, %v", info, err)
	}
	if !fileExists(filepath.Join(linkPath, "UI", "New", "new.pak")) {
		t.Fatal("custom mod was not moved back into the game folder during cleanup")
	}
}

func TestResolveNteInstallPathExistingJunctionDoesNotRequireElevation(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	executable := filepath.Join(
		installRoot, "Neverness To Everness", "Client", "WindowsNoEditor",
		"HT", "Binaries", "Win64", nteExecutableName,
	)
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}

	htRoot := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", ".."))
	linkedRoot := filepath.Join(htRoot, filepath.FromSlash(nteModsRelative))
	modRoot := filepath.Join(root, "custom-mods")
	if err := configureNteModFolder(modRoot, &linkedRoot); err != nil {
		t.Fatal(err)
	}

	resolution, err := (&Mod{}).ResolveNteInstallPath(context.Background(), installRoot)
	if err != nil {
		t.Fatal(err)
	}
	if resolution == nil {
		t.Fatal("NTE install was not resolved")
	}
	if !samePath(resolution.ModFolderPath, modRoot) || !samePath(resolution.LinkedModFolderPath, linkedRoot) {
		t.Fatalf("resolution paths = %#v, want mod root %q and linked root %q", resolution, modRoot, linkedRoot)
	}
	if resolution.RequiresElevation {
		t.Fatalf("existing junction unexpectedly requires elevation: %#v", resolution)
	}
}

func TestResolveNteInstallPathUsesInjectedAppDataFallback(t *testing.T) {
	root := t.TempDir()
	installRoot := filepath.Join(root, "install")
	executable := filepath.Join(
		installRoot, "Neverness To Everness", "Client", "WindowsNoEditor",
		"HT", "Binaries", "Win64", nteExecutableName,
	)
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(executable, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	htRoot := filepath.Clean(filepath.Join(filepath.Dir(executable), "..", ".."))
	contentDir := filepath.Join(htRoot, "Content")
	if err := os.MkdirAll(contentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(contentDir, "Paks"), []byte("blocks directory creation"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := appdata.Open(filepath.Join(root, "home"))
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithOptions(Options{AppData: data})
	resolution, err := service.ResolveNteInstallPath(context.Background(), installRoot)
	if err != nil {
		t.Fatal(err)
	}
	want, err := data.Resolve(appdata.NTEModsDir)
	if err != nil {
		t.Fatal(err)
	}
	if resolution == nil || !resolution.RequiresElevation || !samePath(resolution.ModFolderPath, want) {
		t.Fatalf("resolution = %#v, want fallback %q", resolution, want)
	}
}

func TestEnsureNteBootstrapFilesInstallsEmitsAndRollsBack(t *testing.T) {
	sigArchive := nteBootstrapZip(t, map[string]string{
		"release/dsound.dll":                      "new-dsound",
		"release/UniversalSigBypasser.asi":        "sig-bypasser",
		"release/UniversalSigBypasser.asi.sha512": "sig-hash",
	})
	loaderArchive := nteBootstrapZip(t, map[string]string{
		"loader/winhttp.dll":        "asi-loader",
		"loader/winhttp.dll.sha512": "loader-hash",
	})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/sig.zip":
			_, _ = response.Write(sigArchive)
		case "/loader.zip":
			_, _ = response.Write(loaderArchive)
		default:
			http.NotFound(response, request)
		}
	}))
	t.Cleanup(server.Close)

	var progress []NteBootstrapProgress
	service := NewWithOptions(Options{
		Archive: infra.NewArchive(),
		HTTP:    infra.NewClientWithOptions(infra.ClientOptions{HTTPClient: server.Client()}),
		EventEmit: func(name string, data ...any) {
			if name == nteBootstrapEvent && len(data) == 1 {
				progress = append(progress, data[0].(NteBootstrapProgress))
			}
		},
	})
	service.nteSigBypasserURL = server.URL + "/sig.zip"
	service.nteASILoaderURL = server.URL + "/loader.zip"
	targetDir := t.TempDir()
	executable := filepath.Join(targetDir, nteExecutableName)
	if err := os.WriteFile(executable, []byte("exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "dsound.dll"), []byte("old-dsound"), 0o644); err != nil {
		t.Fatal(err)
	}

	install, err := service.ensureNteBootstrapFiles(context.Background(), executable)
	if err != nil || install == nil {
		t.Fatalf("ensureNteBootstrapFiles = %#v, %v", install, err)
	}
	for name, content := range map[string]string{
		"dsound.dll": "new-dsound", "UniversalSigBypasser.asi": "sig-bypasser", "winhttp.dll": "asi-loader",
		"UniversalSigBypasser.asi.sha512": "sig-hash", "winhttp.dll.sha512": "loader-hash",
	} {
		got, readErr := os.ReadFile(filepath.Join(targetDir, name))
		if readErr != nil || string(got) != content {
			t.Fatalf("installed %s = %q, %v", name, got, readErr)
		}
	}
	phases := make([]string, len(progress))
	for index, event := range progress {
		phases[index] = event.Phase
	}
	if got := strings.Join(phases, ","); got != "fetching-release,downloading,extracting,fetching-release,downloading,extracting,installing,completed" {
		t.Fatalf("progress phases = %s (%#v)", got, progress)
	}

	if err := install.Rollback(); err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(filepath.Join(targetDir, "dsound.dll"))
	if err != nil || string(restored) != "old-dsound" {
		t.Fatalf("restored dsound.dll = %q, %v", restored, err)
	}
	for _, name := range []string{"UniversalSigBypasser.asi", "winhttp.dll", "UniversalSigBypasser.asi.sha512", "winhttp.dll.sha512"} {
		if _, err := os.Stat(filepath.Join(targetDir, name)); !os.IsNotExist(err) {
			t.Fatalf("rollback left %s: %v", name, err)
		}
	}
}

func TestNteWrappedPakFolders(t *testing.T) {
	t.Parallel()

	t.Run("flattens a wrapper with inner pak folders into the parent mod list", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		shinku := filepath.Join(modRoot, "Character", "Shinku")
		sparkle := filepath.Join(shinku, "Shinku Natsu Sparkle 1.3")
		disabledShinku := filepath.Join(shinku, "DISABLED Shinku")
		inner := filepath.Join(sparkle, "shinku")
		burst := filepath.Join(sparkle, "shinku burst")

		writePak(t, disabledShinku, "mod_P.pak.disabled")
		writePak(t, inner, "Shinku Natsu Sparkle_P.pak")
		writePak(t, burst, "Shinku Natsu Sparkle burst_P.pak")
		if err := os.WriteFile(filepath.Join(sparkle, "preview.png"), []byte("preview"), 0o644); err != nil {
			t.Fatal(err)
		}

		roots := testNteRoots(modRoot)
		group := nteScanGroup(roots, shinku, false)
		if len(group.Mods) != 3 {
			t.Fatalf("mods = %#v", modNames(group))
		}
		want := []struct {
			name      string
			isEnabled bool
		}{
			{"DISABLED Shinku", false},
			{"Shinku Natsu Sparkle 1.3 / shinku", true},
			{"Shinku Natsu Sparkle 1.3 / shinku burst", true},
		}
		for i, entry := range want {
			if group.Mods[i].Name != entry.name || group.Mods[i].IsEnabled != entry.isEnabled {
				t.Fatalf("mod[%d] = {%q, %v}, want %+v", i, group.Mods[i].Name, group.Mods[i].IsEnabled, entry)
			}
		}
		if group.ModCount != 3 || group.EnabledModCount != 2 {
			t.Fatalf("counts = %d/%d", group.ModCount, group.EnabledModCount)
		}
		previewEqual(t, group.Mods[1].Preview, filepath.Join(sparkle, "preview.png"))
		gotPaths := listNteModPaths(roots, shinku)
		wantPaths := []string{disabledShinku, inner, burst}
		if len(gotPaths) != len(wantPaths) {
			t.Fatalf("listNteModPaths = %#v", gotPaths)
		}
		for i, path := range wantPaths {
			if gotPaths[i] != path {
				t.Fatalf("listNteModPaths[%d] = %q, want %q", i, gotPaths[i], path)
			}
		}
		if got := nteListingGroupPath(roots, inner); got != shinku {
			t.Fatalf("listing path inner = %q", got)
		}
		if got := nteListingGroupPath(roots, disabledShinku); got != shinku {
			t.Fatalf("listing path disabled = %q", got)
		}
	})

	t.Run("does not list a pak wrapper as a subgroup", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		shinku := filepath.Join(modRoot, "Character", "Shinku")
		writePak(t, filepath.Join(shinku, "DISABLED Shinku"), "mod_P.pak.disabled")
		writePak(t, filepath.Join(shinku, "Shinku Natsu Sparkle 1.3", "shinku"), "Shinku Natsu Sparkle_P.pak")

		groups := nteListGroups(testNteRoots(modRoot), shinku, false)
		if len(groups) != 0 {
			t.Fatalf("groups = %#v", groupNames(groups))
		}
	})

	t.Run("keeps a character folder as a group when it contains both mods and wrappers", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		character := filepath.Join(modRoot, "Character")
		shinku := filepath.Join(character, "Shinku")
		writePak(t, filepath.Join(shinku, "DISABLED Shinku"), "mod_P.pak.disabled")
		writePak(t, filepath.Join(shinku, "Shinku Natsu Sparkle 1.3", "shinku"), "Shinku Natsu Sparkle_P.pak")

		roots := testNteRoots(modRoot)
		characterMods := nteScanGroup(roots, character, false)
		characterGroups := nteListGroups(roots, character, false)
		if len(characterMods.Mods) != 0 {
			t.Fatalf("character mods = %#v", modNames(characterMods))
		}
		if len(characterGroups) != 1 || characterGroups[0].Name != "Shinku" {
			t.Fatalf("character groups = %#v", groupNames(characterGroups))
		}
		if characterGroups[0].ModCount != 2 || characterGroups[0].EnabledModCount != 1 {
			t.Fatalf("shinku counts = %d/%d", characterGroups[0].ModCount, characterGroups[0].EnabledModCount)
		}
		if characterGroups[0].HasSubGroups {
			t.Fatal("expected no subgroups")
		}
	})

	t.Run("still lists NPC mods that have a pak in the folder itself", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		npc := filepath.Join(modRoot, "NPC")
		shopGirl := filepath.Join(npc, "NPC_Shop Girl")
		writePak(t, shopGirl, "NPC_023_NSFW_P.pak")
		writePak(t, filepath.Join(modRoot, "Character", "Shinku", "DISABLED Shinku"), "mod_P.pak.disabled")

		roots := testNteRoots(modRoot)
		group := nteScanGroup(roots, npc, false)
		npcGroups := nteListGroups(roots, npc, false)
		rootGroups := nteListGroups(roots, modRoot, false)

		if len(group.Mods) != 1 || group.Mods[0].Name != "NPC_Shop Girl" || !group.Mods[0].IsEnabled {
			t.Fatalf("npc mods = %#v", group.Mods)
		}
		if len(npcGroups) != 0 {
			t.Fatalf("npc groups = %#v", groupNames(npcGroups))
		}
		if got := groupNames(rootGroups); len(got) != 2 || got[0] != "Character" || got[1] != "NPC" {
			t.Fatalf("root groups = %#v", got)
		}
		if got := nteListingGroupPath(roots, shopGirl); got != npc {
			t.Fatalf("listing path shop girl = %q", got)
		}
	})

	t.Run("keeps a character folder as a group when every child is a direct pak mod", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		character := filepath.Join(modRoot, "Character")
		shinku := filepath.Join(character, "Shinku")
		writePak(t, filepath.Join(shinku, "DISABLED Shinku"), "mod_P.pak.disabled")
		writePak(t, filepath.Join(shinku, "DISABLED ShinkuSwim"), "mod_P.pak.disabled")

		roots := testNteRoots(modRoot)
		characterMods := nteScanGroup(roots, character, false)
		characterGroups := nteListGroups(roots, character, false)
		if len(characterMods.Mods) != 0 {
			t.Fatalf("character mods = %#v", modNames(characterMods))
		}
		if len(characterGroups) != 1 || characterGroups[0].Name != "Shinku" || characterGroups[0].ModCount != 2 {
			t.Fatalf("character groups = %#v count=%d", groupNames(characterGroups), characterGroups[0].ModCount)
		}
	})

	t.Run("flattens variant wrappers like longhairdaff without collapsing them into one toggle", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		daffodil := filepath.Join(modRoot, "Character", "Daffodil")
		writePak(t, filepath.Join(daffodil, "longhairdaff", "Alt Skin"), "mod_P.pak")
		writePak(t, filepath.Join(daffodil, "longhairdaff", "Default Skin"), "mod_P.pak")

		group := nteScanGroup(testNteRoots(modRoot), daffodil, false)
		want := []string{"longhairdaff / Alt Skin", "longhairdaff / Default Skin"}
		if got := modNames(group); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
			t.Fatalf("mods = %#v", got)
		}
		for _, info := range group.Mods {
			if !info.IsEnabled {
				t.Fatalf("%q should be enabled", info.Name)
			}
		}
	})
}

func TestNtePreviewDiscovery(t *testing.T) {
	t.Parallel()

	t.Run("uses a generic media file in the mod folder as the preview", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		shopGirl := filepath.Join(modRoot, "NPC", "NPC_Shop Girl")
		writePak(t, shopGirl, "NPC_023_NSFW_P.pak")
		cover := filepath.Join(shopGirl, "cover.jpg")
		if err := os.WriteFile(cover, []byte("preview"), 0o644); err != nil {
			t.Fatal(err)
		}

		group := nteScanGroup(testNteRoots(modRoot), filepath.Join(modRoot, "NPC"), false)
		if len(group.Mods) == 0 {
			t.Fatal("expected a mod")
		}
		previewEqual(t, group.Mods[0].Preview, cover)
	})

	t.Run("finds a nested preview inside the mod folder", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		shopGirl := filepath.Join(modRoot, "NPC", "NPC_Shop Girl")
		writePak(t, shopGirl, "NPC_023_NSFW_P.pak")
		nested := filepath.Join(shopGirl, "images", "shot.png")
		if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(nested, []byte("preview"), 0o644); err != nil {
			t.Fatal(err)
		}

		group := nteScanGroup(testNteRoots(modRoot), filepath.Join(modRoot, "NPC"), false)
		if len(group.Mods) == 0 {
			t.Fatal("expected a mod")
		}
		previewEqual(t, group.Mods[0].Preview, nested)
	})

	t.Run("falls back to a non-preview filename on a wrapper folder", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		sparkle := filepath.Join(modRoot, "Character", "Shinku", "Shinku Natsu Sparkle 1.3")
		writePak(t, filepath.Join(sparkle, "shinku"), "Shinku Natsu Sparkle_P.pak")
		cover := filepath.Join(sparkle, "cover.jpg")
		if err := os.WriteFile(cover, []byte("preview"), 0o644); err != nil {
			t.Fatal(err)
		}

		group := nteScanGroup(testNteRoots(modRoot), filepath.Join(modRoot, "Character", "Shinku"), false)
		var preview *string
		for _, info := range group.Mods {
			if info.Name == "Shinku Natsu Sparkle 1.3 / shinku" {
				preview = info.Preview
				break
			}
		}
		previewEqual(t, preview, cover)
	})

	t.Run("searches child folders for a group preview only when enabled", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		shinku := filepath.Join(modRoot, "Character", "Shinku")
		nestedPreview := filepath.Join(shinku, "Shinku Mod", "preview.png")
		writePak(t, filepath.Join(shinku, "Shinku Mod"), "mod_P.pak")
		if err := os.WriteFile(nestedPreview, []byte("preview"), 0o644); err != nil {
			t.Fatal(err)
		}

		roots := testNteRoots(modRoot)
		character := filepath.Join(modRoot, "Character")
		withSearch := nteListGroups(roots, character, true)
		withoutSearch := nteListGroups(roots, character, false)
		var with, without *string
		for _, group := range withSearch {
			if group.Name == "Shinku" {
				with = group.Preview
			}
		}
		for _, group := range withoutSearch {
			if group.Name == "Shinku" {
				without = group.Preview
			}
		}
		previewEqual(t, with, nestedPreview)
		previewEqual(t, without, "")
	})

	t.Run("uses the search setting for the group preview when listing mods", func(t *testing.T) {
		t.Parallel()
		modRoot := t.TempDir()
		npc := filepath.Join(modRoot, "NPC")
		nestedPreview := filepath.Join(npc, "NPC_Shop Girl", "preview.png")
		writePak(t, filepath.Join(npc, "NPC_Shop Girl"), "NPC_023_NSFW_P.pak")
		if err := os.WriteFile(nestedPreview, []byte("preview"), 0o644); err != nil {
			t.Fatal(err)
		}

		roots := testNteRoots(modRoot)
		withSearch := nteScanGroup(roots, npc, true)
		withoutSearch := nteScanGroup(roots, npc, false)
		previewEqual(t, withSearch.Preview, nestedPreview)
		previewEqual(t, withoutSearch.Preview, "")
	})
}
