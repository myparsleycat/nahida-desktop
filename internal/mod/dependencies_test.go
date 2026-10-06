package mod

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"unicode/utf16"
)

func TestReferencedLibrariesRequiresQualifiedPrefix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		line string
		want []string
	}{
		{`run = commandlist\rabbitfx\run`, []string{"RabbitFX"}},
		{`$\texfx\glow = 1`, []string{"TexFx"}},
		{`run = commandlist\global\orfix\orfix`, []string{"ORFix"}},
		{`ps-t0 = resource\rabbitfx\diffuse if $\global\tracking\swimming`, []string{"RabbitFX", "Tracking"}},
		{`filename = textures\rabbitfx\map.dds`, nil},
		{`filename = .\texfx\map.dds`, nil},
		{`$\rabbitfxcopy\h = 1`, nil},
	}
	for _, tt := range tests {
		mask := referencedLibraries(tt.line)
		var got []string
		for bit, library := range modLibraries {
			if mask&(1<<bit) != 0 {
				got = append(got, library.name)
			}
		}
		if !slices.Equal(got, tt.want) {
			t.Errorf("referencedLibraries(%q) = %v, want %v", tt.line, got, tt.want)
		}
	}
}

func TestINICodeDropsCommentsAndQuotedStrings(t *testing.T) {
	t.Parallel()
	tests := []struct{ line, want string }{
		{`run = CommandList\RabbitFX\Run`, `run = CommandList\RabbitFX\Run`},
		{`namespace = RabbitFX ; library`, `namespace = RabbitFX `},
		{`namespace = RabbitFX # library`, `namespace = RabbitFX `},
		{`run = CommandListLocal ; run = CommandList\RabbitFX\Run`, `run = CommandListLocal `},
		{`text = "Resource\RabbitFX\a ; b" + $x ; c`, `text =   + $x `},
		{`text = 'unterminated ; CommandList\RabbitFX\Run`, `text = `},
		{`x = #PoolName[0] ; c`, `x = #PoolName[0] `},
	}
	for _, tt := range tests {
		if got := iniCode(tt.line); got != tt.want {
			t.Errorf("iniCode(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestLibraryDependenciesIgnoreInlineComments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "importer", "Mods")
	group := filepath.Join(modsRoot, "Char")
	writeModFile(t, filepath.Join(modsRoot, "Utils", "RabbitFX"), "RabbitFX.ini", "namespace = RabbitFX ; library\n")
	writeModFile(t, filepath.Join(group, "Needs"), "mod.ini", "[Present]\nrun = CommandList\\RabbitFX\\Run ; fx\n")
	writeModFile(t, filepath.Join(group, "Commented"), "mod.ini",
		"[Present]\nrun = CommandListLocal ; run = CommandList\\RabbitFX\\Run\n")
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	scanned, err := service.GetMods(ctx, group)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]ModDependency{}
	for _, info := range scanned.Mods {
		got[info.Name] = info.Dependencies
	}
	if want := []ModDependency{{Name: "RabbitFX", Installed: true}}; !slices.Equal(got["Needs"], want) {
		t.Fatalf("Needs dependencies = %#v, want %#v", got["Needs"], want)
	}
	if len(got["Commented"]) != 0 {
		t.Fatalf("Commented dependencies = %#v, want none", got["Commented"])
	}
}

func TestLibraryDependenciesFollowGameFolderChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	needs := "[Present]\nrun = CommandList\\RabbitFX\\Run\n"
	first := filepath.Join(root, "first", "Mods")
	writeModFile(t, filepath.Join(first, "Utils", "RabbitFX"), "RabbitFX.ini", "namespace = RabbitFX\n")
	writeModFile(t, filepath.Join(first, "Char", "Needs"), "mod.ini", needs)
	second := filepath.Join(root, "second", "Mods")
	writeModFile(t, filepath.Join(second, "Char", "Needs"), "mod.ini", needs)
	if err := service.AddGame(ctx, "Game", first, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	installed := func(modsRoot string) bool {
		t.Helper()
		scanned, err := service.GetMods(ctx, filepath.Join(modsRoot, "Char"))
		if err != nil {
			t.Fatal(err)
		}
		if len(scanned.Mods) != 1 || len(scanned.Mods[0].Dependencies) != 1 {
			t.Fatalf("mods = %#v, want one mod with one dependency", scanned.Mods)
		}
		return scanned.Mods[0].Dependencies[0].Installed
	}

	if !installed(first) {
		t.Fatal("RabbitFX in the first folder is reported missing")
	}
	if err := service.UpdateGame(ctx, "Game", GameUpdates{ModFolderPath: second}); err != nil {
		t.Fatal(err)
	}
	if installed(second) {
		t.Fatal("the second folder reuses the libraries of the first")
	}
}

func TestGetModsReportsLibraryDependencies(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	importer := filepath.Join(root, "importer")
	modsRoot := filepath.Join(importer, "Mods")
	group := filepath.Join(modsRoot, "Char")
	writeModFile(t, importer, "d3dx.ini", "[Include]\ninclude_recursive = Mods\n")
	writeModFile(t, filepath.Join(importer, "Core", "Libraries"), "ORFix.ini", "; core\nnamespace = global\\ORFix\n")

	writeModFile(t, filepath.Join(group, "DISABLED Needs"), "mod.ini",
		"[TextureOverrideBody]\nrun = CommandList\\RabbitFX\\Run\nrun = CommandList\\global\\ORFix\\ORFix\n")
	bundled := filepath.Join(group, "Bundled")
	writeModFile(t, bundled, "mod.ini", "[TextureOverrideBody]\nrun = CommandList\\TexFx\\component.0\n")
	writeModFile(t, filepath.Join(bundled, "TexFx"), "Main.ini", "namespace = TexFx\n[Constants]\n")
	writeModFile(t, filepath.Join(group, "Plain"), "mod.ini",
		"[TextureOverrideBody]\nfilename = Textures\\RabbitFX\\map.dds\n")
	inactive := filepath.Join(group, "Inactive")
	writeModFile(t, inactive, "mod.ini", "[TextureOverrideBody]\nhash = 1\n")
	writeModFile(t, filepath.Join(inactive, "DISABLED alt"), "alt.ini", "[Present]\nrun = CommandList\\RabbitFX\\Run\n")
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}

	dependencies := func() map[string][]ModDependency {
		t.Helper()
		scanned, err := service.GetMods(ctx, group)
		if err != nil {
			t.Fatal(err)
		}
		result := map[string][]ModDependency{}
		for _, info := range scanned.Mods {
			result[info.Name] = info.Dependencies
		}
		return result
	}

	got := dependencies()
	want := []ModDependency{{Name: "RabbitFX"}, {Name: "ORFix", Installed: true}}
	if !slices.Equal(got["DISABLED Needs"], want) {
		t.Fatalf("dependencies = %#v, want %#v", got["DISABLED Needs"], want)
	}
	for _, name := range []string{"Bundled", "Plain", "Inactive"} {
		if len(got[name]) != 0 {
			t.Fatalf("%s dependencies = %#v, want none", name, got[name])
		}
	}

	// A library below a DISABLED folder is not loaded, so it stays missing.
	library := filepath.Join(modsRoot, "Utils", "DISABLED RabbitFX")
	units := utf16.Encode(append([]rune{0xfeff}, []rune("namespace = RabbitFX\r\n[Constants]\r\n")...))
	content := make([]byte, 0, 2*len(units))
	for _, unit := range units {
		content = append(content, byte(unit), byte(unit>>8))
	}
	writeModFile(t, library, "RabbitFX.ini", string(content))
	service.InvalidateLibraries()
	if got := dependencies()["DISABLED Needs"]; !slices.Equal(got, want) {
		t.Fatalf("dependencies with a disabled library = %#v, want %#v", got, want)
	}

	if err := os.Rename(library, filepath.Join(modsRoot, "Utils", "RabbitFX")); err != nil {
		t.Fatal(err)
	}
	if got := dependencies()["DISABLED Needs"]; !slices.Equal(got, want) {
		t.Fatalf("dependencies before invalidation = %#v, want the cached %#v", got, want)
	}
	service.InvalidateLibraries()
	want[0].Installed = true
	if got := dependencies()["DISABLED Needs"]; !slices.Equal(got, want) {
		t.Fatalf("dependencies with the library enabled = %#v, want %#v", got, want)
	}
}
