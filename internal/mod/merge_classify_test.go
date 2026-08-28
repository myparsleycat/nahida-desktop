package mod

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func makeOwnedMergePack(t *testing.T, service *Mod, modsRoot, name string, files map[string]string) string {
	t.Helper()
	pack := filepath.Join(modsRoot, name)
	for relative, text := range files {
		writeMergePackINI(t, filepath.Join(pack, filepath.Dir(relative)), filepath.Base(relative), text)
	}
	return pack
}

func TestClassifyMergePacksMatchesElectronFamilies(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	if err := os.MkdirAll(modsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	ordinaryIni := ordinaryMergeINI("")
	ordinary := makeOwnedMergePack(t, service, modsRoot, "Ordinary", map[string]string{"CharA.ini": ordinaryIni})
	toggle := makeOwnedMergePack(t, service, modsRoot, "Toggle", map[string]string{
		"CharA.ini": ordinaryIni + "\n[Constants]\nglobal persist $dress = 0\n\n[KeyDress]\ntype = cycle\n$dress = 0,1\n",
	})
	classic := makeOwnedMergePack(t, service, modsRoot, "Classic", map[string]string{
		"merged.ini": "; Merged Mod: a.ini, b.ini\n[Constants]\nglobal persist $swapvar = 0\n[CommandListCharAPosition]\nif $swapvar == 0\n\tvb0 = ResourcePosition.0\nendif\n[ResourcePosition.0]\nfilename = a.buf\n",
	})
	namespaced := makeOwnedMergePack(t, service, modsRoot, "Namespaced", map[string]string{
		"MasterCharA.ini": "; Merged Mod: child.ini\nnamespace = CharA\\Master\n[Constants]\nglobal persist $swapvar = 0\n[TextureOverrideCharAPosition]\nhash = abcdef01\n$active = 1\n",
		"child.ini":       "[TextureOverrideCharAPosition]\nhash = abcdef01\nmatch_priority = 0\nif $\\CharA\\Master\\swapvar==0\n\tvb0 = ResourcePosition\nendif\n",
	})
	support := makeOwnedMergePack(t, service, modsRoot, "Support", map[string]string{
		"ORFix.ini": "run = CommandList\\global\\ORFix\n",
	})
	folderDisabled := makeOwnedMergePack(t, service, modsRoot, "DISABLED Ordinary", map[string]string{"CharA.ini": ordinaryIni})
	fileDisabled := makeOwnedMergePack(t, service, modsRoot, "FileDisabled", map[string]string{
		"DISABLEDCharA.ini": ordinaryIni,
		"help.ini":          "[KeyHelp]\n",
	})
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.ClassifyMergePacks(ctx, []string{
		ordinary, toggle, classic, namespaced, support, folderDisabled, fileDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}
	family := map[string]string{}
	allows := map[string]bool{}
	for _, pack := range result.Packs {
		family[pack.Path] = pack.Family
		allows[pack.Path] = pack.AllowsClassic
	}
	if family[ordinary] != "ordinary" || family[toggle] != "in_mod_toggle" ||
		family[classic] != "classic_merge" || family[namespaced] != "namespace_merge" ||
		family[support] != "support" || family[folderDisabled] != "ordinary" ||
		family[fileDisabled] != "support" {
		t.Fatalf("families = %#v", family)
	}
	if !allows[ordinary] || allows[toggle] {
		t.Fatalf("allowsClassic = %#v", allows)
	}
}

func TestClassifyMergePacksLocksClassicForControlFlowAndWWMI(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	conditional := makeOwnedMergePack(t, service, modsRoot, "Conditional", map[string]string{
		"CharA.ini": withDrawTypeMergeINI("abcdef01"),
	})
	wwmi := makeOwnedMergePack(t, service, modsRoot, "Camellya", map[string]string{
		"mod.ini": "; WWMI ALPHA-2 INI\n[Constants]\nglobal $object_guid = 100001\n[TextureOverrideComponent0]\nhash = beef0001\n",
	})
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	conditionalResult, err := service.ClassifyMergePacks(ctx, []string{conditional})
	if err != nil {
		t.Fatal(err)
	}
	if conditionalResult.Packs[0].Family != "ordinary" || conditionalResult.Packs[0].AllowsClassic {
		t.Fatalf("conditional = %#v", conditionalResult.Packs[0])
	}
	wwmiResult, err := service.ClassifyMergePacks(ctx, []string{wwmi})
	if err != nil {
		t.Fatal(err)
	}
	if wwmiResult.Packs[0].Dialect != "wwmi" || wwmiResult.Packs[0].AllowsClassic {
		t.Fatalf("wwmi = %#v", wwmiResult.Packs[0])
	}
}

func TestClassifyMergePacksWarnsWhenHashesDoNotOverlap(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	left := makeOwnedMergePack(t, service, modsRoot, "Left", map[string]string{"A.ini": ordinaryMergeINI("abcdef01")})
	right := makeOwnedMergePack(t, service, modsRoot, "Right", map[string]string{"B.ini": ordinaryMergeINI("12345678")})
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.ClassifyMergePacks(ctx, []string{left, right})
	if err != nil {
		t.Fatal(err)
	}
	if result.HashOverlap {
		t.Fatal("expected hash mismatch")
	}
	found := false
	for _, warning := range result.Warnings {
		if warning == "hash_mismatch" {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestClassifyMergePacksPreservesWarningInsertionOrder(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	left := makeOwnedMergePack(t, service, modsRoot, "Left", map[string]string{
		"A.ini": ordinaryMergeINI("abcdef01") + "\n[Constants]\nglobal persist $one = 0\nglobal persist $two = 0\n",
	})
	right := makeOwnedMergePack(t, service, modsRoot, "Right", map[string]string{"B.ini": ordinaryMergeINI("12345678")})
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	result, err := service.ClassifyMergePacks(ctx, []string{left, right})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"in_mod_toggle", "hash_mismatch"}
	if !reflect.DeepEqual(result.Warnings, want) {
		t.Fatalf("warnings = %#v, want %#v", result.Warnings, want)
	}
}

func TestMergeHeaderAndNamespaceGatesMatchElectronRegexes(t *testing.T) {
	t.Parallel()

	if hasMergedModHeader("; Merged Mod:\n[Constants]\n") {
		t.Fatal("empty merged-mod header matched")
	}
	if !hasMergedModHeader(" ;  Merged Mods : child.ini\n[Constants]\n") {
		t.Fatal("spaced merged-mod header did not match")
	}
	if family := detectMergeFamily("mod.ini", "[Constants]\n", `plain\master\swapvar text`); family == "namespace_merge" {
		t.Fatalf("plain text family = %q", family)
	}
	if family := detectMergeFamily("mod.ini", "[Constants]\n", `if $\CharA\Master\swapvar0 == 1`); family != "namespace_merge" {
		t.Fatalf("qualified swap ref family = %q", family)
	}
}
