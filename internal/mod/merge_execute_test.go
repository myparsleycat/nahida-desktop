package mod

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func ordinaryMergeINI(hash string) string {
	if hash == "" {
		hash = "abcdef01"
	}
	return "[TextureOverrideKleePosition]\nhash = " + hash + "\nvb0 = ResourcePosition\n\n[ResourcePosition]\nfilename = KleePosition.buf\n"
}

func withDrawTypeMergeINI(hash string) string {
	return "[TextureOverrideKleePosition]\nhash = " + hash + "\nif DRAW_TYPE == 1\n\tvb0 = ResourcePosition\nendif\n\n[ResourcePosition]\nfilename = KleePosition.buf\n"
}

func setupMergeGame(t *testing.T, root string) *Mod {
	t.Helper()
	ctx := context.Background()
	service, _ := newTestMod(t, testSettings{})
	if err := service.AddGame(ctx, "Game", root, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	return service
}

func writeMergePackINI(t *testing.T, dir, name, contents string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteNamespaceMergeNamespacesOrdinaryPackOntoExistingMaster(t *testing.T) {
	root := t.TempDir()
	host := filepath.Join(root, "Host")
	extra := filepath.Join(root, "Extra")
	child := writeMergePackINI(t, host, "child.ini", ordinaryMergeINI(""))
	extraIni := writeMergePackINI(t, extra, "Klee.ini", ordinaryMergeINI(""))
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: host, name: "Klee",
		sources:    []namespaceMergeSource{{iniPath: child, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: host, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: child, index: 0},
			{iniPath: extraIni, index: 1},
		},
		forwardKey: "]", backKey: "[",
		existingMasterPath: masterPath,
	})
	extraText, err := os.ReadFile(extraIni)
	if err != nil {
		t.Fatal(err)
	}
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==1`).Match(extraText) {
		t.Fatalf("extra = %s", extraText)
	}
	if !regexp.MustCompile(`\$swapvar = 0,1`).Match(master) {
		t.Fatalf("master = %s", master)
	}
}

func TestMergeModsRemastersTwoNamespacePacksIntoOneSwapSpace(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "A")
	bDir := filepath.Join(root, "B")
	aIni := writeMergePackINI(t, aDir, "a.ini", ordinaryMergeINI(""))
	bIni := writeMergePackINI(t, bDir, "b.ini", ordinaryMergeINI(""))
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: aDir, name: "Alpha",
		sources:    []namespaceMergeSource{{iniPath: aIni, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: bDir, name: "Beta",
		sources:    []namespaceMergeSource{{iniPath: bIni, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	service := setupMergeGame(t, root)
	if _, err := service.MergeMods(context.Background(), namespaceMergeRequest(root, false, aDir, bDir)); err != nil {
		t.Fatal(err)
	}
	aText, err := os.ReadFile(aIni)
	if err != nil {
		t.Fatal(err)
	}
	bText, err := os.ReadFile(bIni)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==0`).Match(aText) {
		t.Fatalf("a.ini = %s", aText)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==1`).Match(bText) {
		t.Fatalf("b.ini = %s", bText)
	}
	if regexp.MustCompile(`\$\\Alpha\\Master\\swapvar`).Match(aText) {
		t.Fatalf("old Alpha namespace leftover: %s", aText)
	}
}

func TestWriteNamespaceMergeNamespacesClassicMergedINI(t *testing.T) {
	root := t.TempDir()
	classicDir := filepath.Join(root, "Classic")
	ordinaryDir := filepath.Join(root, "Ordinary")
	aIni := writeMergePackINI(t, classicDir, "A.ini", ordinaryMergeINI(""))
	bIni := writeMergePackINI(t, classicDir, "B.ini", ordinaryMergeINI(""))
	ordinaryIni := writeMergePackINI(t, ordinaryDir, "C.ini", ordinaryMergeINI(""))
	created := []mergeRollback{}
	if err := writeClassicMergedINI(classicDir, []classicSource{
		{path: aIni, index: 0}, {path: bIni, index: 1},
	}, "vk_right", "", &created); err != nil {
		t.Fatal(err)
	}
	merged := filepath.Join(classicDir, "merged.ini")
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: ordinaryDir, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: merged, index: 0},
			{iniPath: ordinaryIni, index: 1},
		},
		forwardKey: "]", backKey: "[",
	})
	mergedText, err := os.ReadFile(merged)
	if err != nil {
		t.Fatal(err)
	}
	ordinaryText, err := os.ReadFile(ordinaryIni)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==0`).Match(mergedText) {
		t.Fatalf("merged = %s", mergedText)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==1`).Match(ordinaryText) {
		t.Fatalf("ordinary = %s", ordinaryText)
	}
}

func namespaceMergeRequest(group string, includeVanilla bool, children ...string) MergeModsRequest {
	leaves := make([]MergePlanNode, len(children))
	for i, path := range children {
		leaves[i] = MergePlanNode{Kind: "leaf", Path: path}
	}
	return MergeModsRequest{
		GroupPath: group, Placement: "in_place", PackName: "Klee",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Klee",
			ForwardKey: "]", BackKey: "[", IncludeVanilla: includeVanilla,
			Children: leaves,
		},
	}
}

func TestMergeModsDisablesLeftoverMastersWhenRemastering(t *testing.T) {
	root := t.TempDir()
	aDir := filepath.Join(root, "A")
	bDir := filepath.Join(root, "B")
	aIni := writeMergePackINI(t, aDir, "a.ini", ordinaryMergeINI(""))
	bIni := writeMergePackINI(t, bDir, "b.ini", ordinaryMergeINI(""))
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: aDir, name: "Alpha",
		sources:    []namespaceMergeSource{{iniPath: aIni, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: bDir, name: "Beta",
		sources:    []namespaceMergeSource{{iniPath: bIni, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	service := setupMergeGame(t, root)
	if _, err := service.MergeMods(context.Background(), namespaceMergeRequest(root, false, aDir, bDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(aDir, "MasterKlee.ini")); err != nil {
		t.Fatalf("MasterKlee missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(aDir, "MasterAlpha.ini")); !os.IsNotExist(err) {
		t.Fatalf("MasterAlpha should be gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bDir, "MasterBeta.ini")); !os.IsNotExist(err) {
		t.Fatalf("MasterBeta should be gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(bDir, "DISABLED_BACKUP_MasterBeta.ini")); err != nil {
		t.Fatalf("MasterBeta backup missing: %v", err)
	}
	aText, _ := os.ReadFile(aIni)
	bText, _ := os.ReadFile(bIni)
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==0`).Match(aText) {
		t.Fatalf("a.ini = %s", aText)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==1`).Match(bText) {
		t.Fatalf("b.ini = %s", bText)
	}
}

func TestMergeModsFlattensTwoMultiChildNamespacePacks(t *testing.T) {
	root := t.TempDir()
	alphaDir := filepath.Join(root, "Alpha")
	betaDir := filepath.Join(root, "Beta")
	alphaA := writeMergePackINI(t, alphaDir, "A.ini", withDrawTypeMergeINI("abcdef01"))
	alphaB := writeMergePackINI(t, alphaDir, "B.ini", withDrawTypeMergeINI("abcdef02"))
	betaC := writeMergePackINI(t, betaDir, "C.ini", withDrawTypeMergeINI("abcdef03"))
	betaD := writeMergePackINI(t, betaDir, "D.ini", withDrawTypeMergeINI("abcdef04"))
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: alphaDir, name: "Alpha",
		sources: []namespaceMergeSource{
			{iniPath: alphaA, index: 0}, {iniPath: alphaB, index: 1},
		},
		forwardKey: "]", backKey: "[",
	})
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: betaDir, name: "Beta",
		sources: []namespaceMergeSource{
			{iniPath: betaC, index: 0}, {iniPath: betaD, index: 1},
		},
		forwardKey: "]", backKey: "[",
	})
	service := setupMergeGame(t, root)
	if _, err := service.MergeMods(context.Background(), namespaceMergeRequest(root, false, alphaDir, betaDir)); err != nil {
		t.Fatal(err)
	}
	master, err := os.ReadFile(filepath.Join(alphaDir, "MasterKlee.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(alphaDir, "MasterAlpha.ini")); !os.IsNotExist(err) {
		t.Fatal("MasterAlpha remains")
	}
	if _, err := os.Stat(filepath.Join(betaDir, "MasterBeta.ini")); !os.IsNotExist(err) {
		t.Fatal("MasterBeta remains")
	}
	if !regexp.MustCompile(`\$swapvar = 0,1,2,3\n`).Match(master) {
		t.Fatalf("master = %s", master)
	}
	if !regexp.MustCompile(`\[TextureOverrideKleePosition\]\nhash = abcdef01\n\$active = 1`).Match(master) {
		t.Fatalf("overlay = %s", master)
	}
	for _, want := range []string{"A.ini", "B.ini", "C.ini", "D.ini"} {
		if !strings.Contains(string(master), want) {
			t.Fatalf("master missing %s: %s", want, master)
		}
	}
	cases := []struct {
		path  string
		hash  string
		index int
	}{
		{alphaA, "abcdef01", 0}, {alphaB, "abcdef02", 1},
		{betaC, "abcdef03", 2}, {betaD, "abcdef04", 3},
	}
	for _, tc := range cases {
		text, err := os.ReadFile(tc.path)
		if err != nil {
			t.Fatal(err)
		}
		want := regexp.MustCompile(
			`hash = ` + tc.hash + `\nmatch_priority = ` + strconv.Itoa(tc.index) +
				`\nif \$\\Klee\\Master\\swapvar==` + strconv.Itoa(tc.index) +
				`\n\tif DRAW_TYPE == 1\n\t\tvb0 = ResourcePosition\n\tendif\nendif`,
		)
		if !want.Match(text) {
			t.Fatalf("%s = %s", tc.path, text)
		}
		if regexp.MustCompile(`else if\s+\$\\`).Match(text) ||
			regexp.MustCompile(`\$\\(?:Alpha|Beta)\\Master\\swapvar`).Match(text) {
			t.Fatalf("leftover wrap in %s: %s", tc.path, text)
		}
	}
}

func TestMergeModsInsertsMultiChildPackOntoExistingMaster(t *testing.T) {
	root := t.TempDir()
	hostDir := filepath.Join(root, "Host")
	extraDir := filepath.Join(root, "Extra")
	hostA := writeMergePackINI(t, hostDir, "A.ini", ordinaryMergeINI("abcdef01"))
	hostB := writeMergePackINI(t, hostDir, "B.ini", ordinaryMergeINI("abcdef02"))
	extraC := writeMergePackINI(t, extraDir, "C.ini", ordinaryMergeINI("abcdef03"))
	extraD := writeMergePackINI(t, extraDir, "D.ini", ordinaryMergeINI("abcdef04"))
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: hostDir, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: hostA, index: 0}, {iniPath: hostB, index: 1},
		},
		forwardKey: "]", backKey: "[",
	})
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: extraDir, name: "Beta",
		sources: []namespaceMergeSource{
			{iniPath: extraC, index: 0}, {iniPath: extraD, index: 1},
		},
		forwardKey: "]", backKey: "[",
	})
	service := setupMergeGame(t, root)
	if _, err := service.MergeMods(context.Background(), namespaceMergeRequest(root, false, hostDir, extraDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(extraDir, "MasterBeta.ini")); !os.IsNotExist(err) {
		t.Fatal("MasterBeta remains")
	}
	master, _ := os.ReadFile(filepath.Join(hostDir, "MasterKlee.ini"))
	if !regexp.MustCompile(`\$swapvar = 0,1,2,3\n`).Match(master) {
		t.Fatalf("master = %s", master)
	}
	for _, tc := range []struct {
		path  string
		index int
		hash  string
	}{
		{hostA, 0, "abcdef01"}, {hostB, 1, "abcdef02"},
		{extraC, 2, "abcdef03"}, {extraD, 3, "abcdef04"},
	} {
		text, _ := os.ReadFile(tc.path)
		pattern := regexp.MustCompile(`hash = ` + tc.hash + `\nmatch_priority = ` + strconv.Itoa(tc.index) +
			`\nif \$\\Klee\\Master\\swapvar==` + strconv.Itoa(tc.index))
		if !pattern.Match(text) {
			t.Fatalf("%s = %s", tc.path, text)
		}
	}
}

func TestMergeModsSharesOneSwapvarIndexAcrossINIsFromSamePack(t *testing.T) {
	root := t.TempDir()
	packA := filepath.Join(root, "PackA")
	packB := filepath.Join(root, "PackB")
	mainIni := writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI(""))
	helperIni := writeMergePackINI(t, packA, "ORFix.ini", ordinaryMergeINI("abcdef02"))
	extraIni := writeMergePackINI(t, packB, "Klee.ini", ordinaryMergeINI(""))
	service := setupMergeGame(t, root)
	if _, err := service.MergeMods(context.Background(), namespaceMergeRequest(root, false, packA, packB)); err != nil {
		t.Fatal(err)
	}
	mainText, _ := os.ReadFile(mainIni)
	helperText, _ := os.ReadFile(helperIni)
	extraText, _ := os.ReadFile(extraIni)
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==0`).Match(mainText) {
		t.Fatalf("main = %s", mainText)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==0`).Match(helperText) {
		t.Fatalf("helper = %s", helperText)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==1`).Match(extraText) {
		t.Fatalf("extra = %s", extraText)
	}
	master, _ := os.ReadFile(filepath.Join(packA, "MasterKlee.ini"))
	if !regexp.MustCompile(`\$swapvar = 0,1\n`).Match(master) {
		t.Fatalf("master = %s", master)
	}
}

func TestMergeModsKeepsOneSwapvarIndexPerCopiedPackInNewFolder(t *testing.T) {
	root := t.TempDir()
	packA := filepath.Join(root, "PackA")
	packB := filepath.Join(root, "PackB")
	writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI(""))
	writeMergePackINI(t, packA, "ORFix.ini", ordinaryMergeINI("abcdef02"))
	writeMergePackINI(t, packB, "Klee.ini", ordinaryMergeINI(""))
	service := setupMergeGame(t, root)
	result, err := service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "new_folder", PackName: "Klee",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Klee",
			ForwardKey: "]", BackKey: "[", IncludeVanilla: true,
			Children: []MergePlanNode{
				{Kind: "leaf", Path: packA}, {Kind: "leaf", Path: packB},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	copiedA := filepath.Join(result.OutputPath, "PackA")
	copiedB := filepath.Join(result.OutputPath, "PackB")
	kleeA, _ := os.ReadFile(filepath.Join(copiedA, "Klee.ini"))
	orfixA, _ := os.ReadFile(filepath.Join(copiedA, "ORFix.ini"))
	kleeB, _ := os.ReadFile(filepath.Join(copiedB, "Klee.ini"))
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==1`).Match(kleeA) {
		t.Fatalf("copied A Klee = %s", kleeA)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==1`).Match(orfixA) {
		t.Fatalf("copied A ORFix = %s", orfixA)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==2`).Match(kleeB) {
		t.Fatalf("copied B = %s", kleeB)
	}
	master, _ := os.ReadFile(filepath.Join(result.OutputPath, "MasterKlee.ini"))
	if !regexp.MustCompile(`\$swapvar = 0,1,2\n`).Match(master) {
		t.Fatalf("master = %s", master)
	}
}

func TestMergeModsDropsDisabledPrefixWhenCopyingIntoNewFolder(t *testing.T) {
	root := t.TempDir()
	disabled := filepath.Join(root, "DISABLED Aino Nude toggle - 복사본")
	extra := filepath.Join(root, "Extra")
	writeMergePackINI(t, disabled, "Aino.ini", ordinaryMergeINI(""))
	writeMergePackINI(t, extra, "Aino.ini", ordinaryMergeINI(""))
	service := setupMergeGame(t, root)
	result, err := service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "new_folder", PackName: "AinoNudetoggle",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "AinoNudetoggle",
			ForwardKey: "]", BackKey: "[",
			Children: []MergePlanNode{
				{Kind: "leaf", Path: disabled}, {Kind: "leaf", Path: extra},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.OutputPath, "Aino Nude toggle - 복사본")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.OutputPath, "DISABLED Aino Nude toggle - 복사본")); !os.IsNotExist(err) {
		t.Fatal("disabled prefix copied")
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatal("original disabled pack missing")
	}
}

func TestMergeModsDoesNotReDisableAlreadyDisabledPacks(t *testing.T) {
	root := t.TempDir()
	disabled := filepath.Join(root, "disableddisabled PackA")
	extra := filepath.Join(root, "Extra")
	writeMergePackINI(t, disabled, "Aino.ini", ordinaryMergeINI(""))
	writeMergePackINI(t, extra, "Aino.ini", ordinaryMergeINI(""))
	service := setupMergeGame(t, root)
	result, err := service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "new_folder", PackName: "AinoNudetoggle",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "AinoNudetoggle",
			ForwardKey: "]", BackKey: "[",
			Children: []MergePlanNode{
				{Kind: "leaf", Path: disabled}, {Kind: "leaf", Path: extra},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(result.OutputPath, "PackA")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(disabled); err != nil {
		t.Fatal("original missing")
	}
}

func TestMergeModsDisablesOriginalUnderFreeNameWhenDisabledExists(t *testing.T) {
	root := t.TempDir()
	packA := filepath.Join(root, "PackA")
	packB := filepath.Join(root, "PackB")
	existingDisabled := filepath.Join(root, "DISABLED PackA")
	writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI(""))
	writeMergePackINI(t, packB, "Klee.ini", ordinaryMergeINI(""))
	writeMergePackINI(t, existingDisabled, "keep.ini", "user-disabled")
	service := setupMergeGame(t, root)
	if _, err := service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "new_folder", PackName: "Klee",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Klee",
			ForwardKey: "]", BackKey: "[",
			Children: []MergePlanNode{
				{Kind: "leaf", Path: packA}, {Kind: "leaf", Path: packB},
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packA); !os.IsNotExist(err) {
		t.Fatal("PackA should be moved")
	}
	if _, err := os.Stat(filepath.Join(root, "DISABLED PackA (2)")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(existingDisabled, "keep.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user-disabled" {
		t.Fatalf("keep.ini = %s", got)
	}
}

func TestMergeModsRestoresWrappedChildrenWhenNestedMergeFails(t *testing.T) {
	root := t.TempDir()
	packA := filepath.Join(root, "PackA")
	packB := filepath.Join(root, "PackB")
	empty1 := filepath.Join(root, "Empty1")
	empty2 := filepath.Join(root, "Empty2")
	aIni := writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI(""))
	bIni := writeMergePackINI(t, packB, "Klee.ini", ordinaryMergeINI(""))
	if err := os.MkdirAll(empty1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(empty2, 0o755); err != nil {
		t.Fatal(err)
	}
	service := setupMergeGame(t, root)
	_, err := service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "in_place", PackName: "Klee",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Klee",
			ForwardKey: "]", BackKey: "[",
			Children: []MergePlanNode{
				{
					Kind: "group", ID: "inner", Engine: "namespace", Name: "Inner",
					ForwardKey: "]", BackKey: "[",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: packA}, {Kind: "leaf", Path: packB},
					},
				},
				{
					Kind: "group", ID: "fail", Engine: "namespace", Name: "Fail",
					ForwardKey: "]", BackKey: "[",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: empty1}, {Kind: "leaf", Path: empty2},
					},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "NAMESPACE_MERGE_NEEDS_CHILD") {
		t.Fatalf("err = %v", err)
	}
	aText, _ := os.ReadFile(aIni)
	bText, _ := os.ReadFile(bIni)
	if string(aText) != ordinaryMergeINI("") || string(bText) != ordinaryMergeINI("") {
		t.Fatalf("children not restored: %s / %s", aText, bText)
	}
	if _, err := os.Stat(filepath.Join(packA, "DISABLED_BACKUP_Klee.ini")); !os.IsNotExist(err) {
		t.Fatal("backup remains")
	}
	if _, err := os.Stat(filepath.Join(packA, "MasterInner.ini")); !os.IsNotExist(err) {
		t.Fatal("MasterInner remains")
	}
}

func TestMergeModsRestoresLeftoverMasterWhenDisabledBackupExists(t *testing.T) {
	root := t.TempDir()
	packA := filepath.Join(root, "PackA")
	packB := filepath.Join(root, "PackB")
	empty1 := filepath.Join(root, "Empty1")
	empty2 := filepath.Join(root, "Empty2")
	aIni := writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI(""))
	bIni := writeMergePackINI(t, packB, "Klee.ini", ordinaryMergeINI(""))
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: packA, name: "Alpha",
		sources:    []namespaceMergeSource{{iniPath: aIni, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: packB, name: "Beta",
		sources:    []namespaceMergeSource{{iniPath: bIni, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	masterBeta := filepath.Join(packB, "MasterBeta.ini")
	disabledBeta := filepath.Join(packB, "DISABLEDMasterBeta.ini")
	masterBetaText, err := os.ReadFile(masterBeta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(disabledBeta, []byte("stale-master-backup"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(empty1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(empty2, 0o755); err != nil {
		t.Fatal(err)
	}
	service := setupMergeGame(t, root)
	_, err = service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "in_place", PackName: "Klee",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Klee",
			ForwardKey: "]", BackKey: "[",
			Children: []MergePlanNode{
				{
					Kind: "group", ID: "inner", Engine: "namespace", Name: "Inner",
					ForwardKey: "]", BackKey: "[",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: packA}, {Kind: "leaf", Path: packB},
					},
				},
				{
					Kind: "group", ID: "fail", Engine: "namespace", Name: "Fail",
					ForwardKey: "]", BackKey: "[",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: empty1}, {Kind: "leaf", Path: empty2},
					},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "NAMESPACE_MERGE_NEEDS_CHILD") {
		t.Fatalf("err = %v", err)
	}
	gotMaster, _ := os.ReadFile(masterBeta)
	gotDisabled, _ := os.ReadFile(disabledBeta)
	if string(gotMaster) != string(masterBetaText) {
		t.Fatalf("master restored badly: %s", gotMaster)
	}
	if string(gotDisabled) != "stale-master-backup" {
		t.Fatalf("disabled overwritten: %s", gotDisabled)
	}
}

func TestMergeModsReenablesClassicSourcesWhenLaterNestedMergeFails(t *testing.T) {
	root := t.TempDir()
	packA := filepath.Join(root, "PackA")
	packB := filepath.Join(root, "PackB")
	wwmi := filepath.Join(root, "WWMI")
	extra := filepath.Join(root, "Extra")
	aIni := writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI(""))
	bIni := writeMergePackINI(t, packB, "Klee.ini", ordinaryMergeINI(""))
	writeMergePackINI(t, wwmi, "mod.ini", "; WWMI ALPHA-2 INI\n[Constants]\nglobal $object_guid = 170961\n[TextureOverrideComponent0]\nhash = 7748c1d8\n")
	writeMergePackINI(t, extra, "Klee.ini", ordinaryMergeINI(""))
	service := setupMergeGame(t, root)
	_, err := service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "in_place", PackName: "Klee",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Klee",
			ForwardKey: "]", BackKey: "[",
			Children: []MergePlanNode{
				{
					Kind: "group", ID: "classic", Engine: "classic", Name: "Classic",
					ForwardKey: "vk_right", BackKey: "vk_left",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: packA}, {Kind: "leaf", Path: packB},
					},
				},
				{
					Kind: "group", ID: "locked", Engine: "classic", Name: "Locked",
					ForwardKey: "vk_right", BackKey: "vk_left",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: wwmi}, {Kind: "leaf", Path: extra},
					},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "CLASSIC_LOCKED") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(packA); err != nil {
		t.Fatal(err)
	}
	aText, _ := os.ReadFile(aIni)
	bText, _ := os.ReadFile(bIni)
	if string(aText) != ordinaryMergeINI("") || string(bText) != ordinaryMergeINI("") {
		t.Fatalf("classic sources not restored")
	}
	if _, err := os.Stat(filepath.Join(root, "Classic")); !os.IsNotExist(err) {
		t.Fatal("Classic folder remains")
	}
}

func TestMergeModsStagesPacksThroughTemporaryPathsWhenInPlaceDestinationsCollide(t *testing.T) {
	root := t.TempDir()
	disabledPackA := filepath.Join(root, "DISABLED PackA")
	packA := filepath.Join(root, "PackA")
	writeMergePackINI(t, disabledPackA, "Klee.ini", ordinaryMergeINI("abcdef01"))
	writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI("abcdef02"))
	service := setupMergeGame(t, root)
	if _, err := service.MergeMods(context.Background(), namespaceMergeRequest(root, false, disabledPackA, packA)); err != nil {
		t.Fatal(err)
	}
	finalPackA := filepath.Join(root, "PackA")
	finalPackA2 := filepath.Join(root, "PackA (2)")
	if _, err := os.Stat(disabledPackA); !os.IsNotExist(err) {
		t.Fatal("DISABLED PackA remains")
	}
	if _, err := os.Stat(finalPackA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(finalPackA2); err != nil {
		t.Fatal(err)
	}
	aText, _ := os.ReadFile(filepath.Join(finalPackA, "Klee.ini"))
	bText, _ := os.ReadFile(filepath.Join(finalPackA2, "Klee.ini"))
	if !regexp.MustCompile(`(?s)hash = abcdef01.*if \$\\Klee\\Master\\swapvar==0`).Match(aText) {
		t.Fatalf("PackA = %s", aText)
	}
	if !regexp.MustCompile(`(?s)hash = abcdef02.*if \$\\Klee\\Master\\swapvar==1`).Match(bText) {
		t.Fatalf("PackA (2) = %s", bText)
	}
}

func TestMergeModsRestoresStagedPacksWhenInPlaceCollisionMergeFails(t *testing.T) {
	root := t.TempDir()
	disabledPackA := filepath.Join(root, "DISABLED PackA")
	packA := filepath.Join(root, "PackA")
	empty1 := filepath.Join(root, "Empty1")
	empty2 := filepath.Join(root, "Empty2")
	disabledIni := writeMergePackINI(t, disabledPackA, "Klee.ini", ordinaryMergeINI("abcdef01"))
	packAIni := writeMergePackINI(t, packA, "Klee.ini", ordinaryMergeINI("abcdef02"))
	if err := os.MkdirAll(empty1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(empty2, 0o755); err != nil {
		t.Fatal(err)
	}
	service := setupMergeGame(t, root)
	_, err := service.MergeMods(context.Background(), MergeModsRequest{
		GroupPath: root, Placement: "in_place", PackName: "Klee",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "namespace", Name: "Klee",
			ForwardKey: "]", BackKey: "[",
			Children: []MergePlanNode{
				{
					Kind: "group", ID: "inner", Engine: "namespace", Name: "Inner",
					ForwardKey: "]", BackKey: "[",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: disabledPackA}, {Kind: "leaf", Path: packA},
					},
				},
				{
					Kind: "group", ID: "fail", Engine: "namespace", Name: "Fail",
					ForwardKey: "]", BackKey: "[",
					Children: []MergePlanNode{
						{Kind: "leaf", Path: empty1}, {Kind: "leaf", Path: empty2},
					},
				},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "NAMESPACE_MERGE_NEEDS_CHILD") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(disabledPackA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(packA); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "PackA (2)")); !os.IsNotExist(err) {
		t.Fatal("PackA (2) remains")
	}
	disabledText, _ := os.ReadFile(disabledIni)
	packText, _ := os.ReadFile(packAIni)
	if !strings.Contains(string(disabledText), "hash = abcdef01") {
		t.Fatalf("disabled = %s", disabledText)
	}
	if !strings.Contains(string(packText), "hash = abcdef02") {
		t.Fatalf("packA = %s", packText)
	}
}
