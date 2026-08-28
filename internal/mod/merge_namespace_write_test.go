package mod

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

const namespaceChildINI = `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`

func writeNamespaceChild(t *testing.T, dir, name, contents string) string {
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

func mustWriteNamespaceMerge(t *testing.T, options namespaceMergeOptions) string {
	t.Helper()
	path, err := writeNamespaceMerge(options)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWriteNamespaceMergeWrapsHashedSectionsAndWritesMasterStub(t *testing.T) {
	root := t.TempDir()
	childPath := writeNamespaceChild(t, root, "Klee.ini", namespaceChildINI)
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources:    []namespaceMergeSource{{iniPath: childPath, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`namespace = Klee\\Master\n; Constants ---------------------------`).Match(master) {
		t.Fatalf("master = %s", master)
	}
	if !regexp.MustCompile(`; Overrides ---------------------------\n\n\[TextureOverrideKleePosition\]\nhash = abcdef01\n\$active = 1`).Match(master) {
		t.Fatalf("master overlay = %s", master)
	}
	if !regexp.MustCompile(`hash = abcdef01\nmatch_priority = 0\nif \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition\nendif`).Match(child) {
		t.Fatalf("child = %s", child)
	}
	if _, err := os.Stat(filepath.Join(root, "DISABLED_BACKUP_Klee.ini")); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
}

func TestWriteNamespaceMergeDoesNotTreatUnrelatedDisabledIniAsBackup(t *testing.T) {
	root := t.TempDir()
	childPath := writeNamespaceChild(t, root, "Klee.ini", namespaceChildINI)
	disabled := filepath.Join(root, "DISABLEDKlee.ini")
	if err := os.WriteFile(disabled, []byte("user-disabled"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources:    []namespaceMergeSource{{iniPath: childPath, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	got, err := os.ReadFile(disabled)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "user-disabled" {
		t.Fatalf("disabled overwritten: %s", got)
	}
	backup, err := os.ReadFile(filepath.Join(root, "DISABLED_BACKUP_Klee.ini"))
	if err != nil {
		t.Fatal(err)
	}
	if string(backup) != namespaceChildINI {
		t.Fatalf("backup = %s", backup)
	}
}

func TestUnwrapNamespaceRemovesExistingWrapBeforeRemastering(t *testing.T) {
	wrapped := wrapNamespaceHashes(namespaceChildINI, "Old", 3)
	unwrapped, err := unwrapNamespace(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`\$\\Old\\Master\\swapvar`).MatchString(unwrapped) {
		t.Fatalf("swapvar leftover: %s", unwrapped)
	}
	if !strings.Contains(unwrapped, "hash = abcdef01") || !strings.Contains(unwrapped, "vb0 = ResourcePosition") {
		t.Fatalf("unwrapped = %s", unwrapped)
	}
}

func TestUnwrapNamespaceTwoBranchIfElseIfEndifChain(t *testing.T) {
	twoBranchINI := `[TextureOverrideKleePosition]
hash = abcdef01
match_priority = 0
if $\Klee\Master\swapvar == 0
	vb0 = ResourcePosition0
else if $\Klee\Master\swapvar == 1
	vb0 = ResourcePosition1
endif
ps-t0 = ResourceTexture
`
	unwrapped, err := unwrapNamespace(twoBranchINI)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`\$\\Klee\\Master\\swapvar`).MatchString(unwrapped) {
		t.Fatalf("swapvar leftover: %s", unwrapped)
	}
	if regexp.MustCompile(`(?i)else if`).MatchString(unwrapped) {
		t.Fatalf("else if leftover: %s", unwrapped)
	}
	if regexp.MustCompile(`(?i)endif`).MatchString(unwrapped) {
		t.Fatalf("endif leftover: %s", unwrapped)
	}
	for _, want := range []string{"hash = abcdef01", "vb0 = ResourcePosition0", "vb0 = ResourcePosition1", "ps-t0 = ResourceTexture"} {
		if !strings.Contains(unwrapped, want) {
			t.Fatalf("missing %q in %s", want, unwrapped)
		}
	}
}

func TestWrapThenUnwrapKeepsEFMIMatchIndexCountAndDropsMatchPriority(t *testing.T) {
	input := `[TextureOverride_Component0]
hash = 79a0cd6f
match_priority = 0
match_index_count = 48909
$object_detected = 1
$lod_level = 0
if $mod_enabled && DRAW_TYPE == 4
    handling = skip
    run = CommandList_Draw_Component0
endif
`
	wrapped := wrapNamespaceHashes(input, "Liino", 1)
	unwrapped, err := unwrapNamespace(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unwrapped, "match_priority") {
		t.Fatalf("match_priority leftover: %s", unwrapped)
	}
	if regexp.MustCompile(`\$\\Liino\\Master\\swapvar`).MatchString(unwrapped) {
		t.Fatalf("swapvar leftover: %s", unwrapped)
	}
	if !strings.Contains(unwrapped, "match_index_count = 48909") || !strings.Contains(unwrapped, "$object_detected = 1") {
		t.Fatalf("unwrapped = %s", unwrapped)
	}
}

func TestWriteNamespaceMergeCopiesMatchIndexCountOntoMasterActiveOverlay(t *testing.T) {
	root := t.TempDir()
	childPath := writeNamespaceChild(t, root, "Liino.ini", `[TextureOverride_Component0]
hash = 79a0cd6f
match_index_count = 48909
$object_detected = 1
`)
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Liino",
		sources:    []namespaceMergeSource{{iniPath: childPath, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`\[TextureOverrideLiinoComponent0\]\nhash = 79a0cd6f\nmatch_index_count = 48909\n\$active = 1`).Match(master) {
		t.Fatalf("master = %s", master)
	}
	if regexp.MustCompile(`\[TextureOverrideLiinoComponent0\]\nhash = 79a0cd6f\n\$active = 1`).Match(master) {
		t.Fatalf("match_index_count omitted: %s", master)
	}
}

func TestWriteNamespaceMergeDoesNotInventSwapvarFromMultipleVB0(t *testing.T) {
	root := t.TempDir()
	multiVb0INI := `[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition0
vb0 = ResourcePosition1
`
	childPath := writeNamespaceChild(t, root, "Klee.ini", multiVb0INI)
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources:    []namespaceMergeSource{{iniPath: childPath, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	child, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`\$swapvar = 0\n`).Match(master) {
		t.Fatalf("master = %s", master)
	}
	if regexp.MustCompile(`\$swapvar = 0,1`).Match(master) {
		t.Fatalf("invented extra swap index: %s", master)
	}
	if !regexp.MustCompile(`if \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition0\n\tvb0 = ResourcePosition1\nendif`).Match(child) {
		t.Fatalf("child = %s", child)
	}
	if regexp.MustCompile(`else if \$\\Klee\\Master\\swapvar==1`).Match(child) {
		t.Fatalf("extra branch: %s", child)
	}
}

func TestWriteNamespaceMergeRemastersTwoNamespacedChildren(t *testing.T) {
	root := t.TempDir()
	alphaDir := filepath.Join(root, "Alpha")
	betaDir := filepath.Join(root, "Beta")
	alphaPath := writeNamespaceChild(t, alphaDir, "A.ini", namespaceChildINI)
	betaPath := writeNamespaceChild(t, betaDir, "B.ini", namespaceChildINI)
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: alphaDir, name: "Alpha",
		sources:    []namespaceMergeSource{{iniPath: alphaPath, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: betaDir, name: "Beta",
		sources:    []namespaceMergeSource{{iniPath: betaPath, index: 0}},
		forwardKey: "]", backKey: "[",
	})
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: alphaPath, index: 0},
			{iniPath: betaPath, index: 1},
		},
		forwardKey: "]", backKey: "[",
	})
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	alpha, err := os.ReadFile(alphaPath)
	if err != nil {
		t.Fatal(err)
	}
	beta, err := os.ReadFile(betaPath)
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`namespace = Klee\\Master\n; Constants ---------------------------`).Match(master) {
		t.Fatalf("master = %s", master)
	}
	if !regexp.MustCompile(`\$swapvar = 0,1\n`).Match(master) {
		t.Fatalf("master cycle = %s", master)
	}
	if !regexp.MustCompile(`; Overrides ---------------------------\n\n\[TextureOverrideKleePosition\]\nhash = abcdef01\n\$active = 1`).Match(master) {
		t.Fatalf("master overlay = %s", master)
	}
	if !regexp.MustCompile(`hash = abcdef01\nmatch_priority = 0\nif \$\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition\nendif`).Match(alpha) {
		t.Fatalf("alpha = %s", alpha)
	}
	if !regexp.MustCompile(`hash = abcdef01\nmatch_priority = 1\nif \$\\Klee\\Master\\swapvar==1\n\tvb0 = ResourcePosition\nendif`).Match(beta) {
		t.Fatalf("beta = %s", beta)
	}
	if regexp.MustCompile(`\$\\Alpha\\Master\\swapvar`).Match(alpha) || regexp.MustCompile(`\$\\Beta\\Master\\swapvar`).Match(beta) {
		t.Fatalf("old namespace leftover")
	}
	if regexp.MustCompile(`else if \$\\Klee\\Master\\swapvar`).Match(alpha) || regexp.MustCompile(`else if \$\\Klee\\Master\\swapvar`).Match(beta) {
		t.Fatalf("else-if leftover")
	}
}

func TestExtractMergedModPathsHandlesLineJSONAndLegacyLists(t *testing.T) {
	multiLine := strings.Join([]string{
		`; Merged Mod: C:\Mods\Klee, (Red Dress)\Klee.ini`,
		`; Merged Mod: C:\Mods\Klee, (Blue Dress)\Klee.ini`,
		`namespace = Klee\Master`,
	}, "\n")
	if got := extractMergedModPaths(multiLine); !mergeStringSlicesEqual(got, []string{
		`C:\Mods\Klee, (Red Dress)\Klee.ini`,
		`C:\Mods\Klee, (Blue Dress)\Klee.ini`,
	}) {
		t.Fatalf("multiLine = %#v", got)
	}
	jsonArray := `; Merged Mod: ["C:\\Mods\\Klee, (Red)\\Klee.ini", "D:\\Other.ini"]`
	if got := extractMergedModPaths(jsonArray); !mergeStringSlicesEqual(got, []string{
		`C:\Mods\Klee, (Red)\Klee.ini`,
		`D:\Other.ini`,
	}) {
		t.Fatalf("jsonArray = %#v", got)
	}
	mixedJSONArray := `; Merged Mod: ["A.ini", 7, null, " ", "B.ini"]`
	if got := extractMergedModPaths(mixedJSONArray); !mergeStringSlicesEqual(got, []string{"A.ini", "B.ini"}) {
		t.Fatalf("mixedJSONArray = %#v", got)
	}
	singleCommaPath := `; Merged Mod: C:\Mods\Klee, (Red Dress)\Klee.ini`
	if got := extractMergedModPaths(singleCommaPath); !mergeStringSlicesEqual(got, []string{
		`C:\Mods\Klee, (Red Dress)\Klee.ini`,
	}) {
		t.Fatalf("singleCommaPath = %#v", got)
	}
	legacyList := `; Merged Mod: a.ini, b.ini, c.ini`
	if got := extractMergedModPaths(legacyList); !mergeStringSlicesEqual(got, []string{"a.ini", "b.ini", "c.ini"}) {
		t.Fatalf("legacyList = %#v", got)
	}
	commaInDirLegacyList := `; Merged Mods: C:\Mods\Klee, (Red Dress)\Klee.ini, D:\Mods\Klee, (Blue Dress)\Klee.ini`
	if got := extractMergedModPaths(commaInDirLegacyList); !mergeStringSlicesEqual(got, []string{
		`C:\Mods\Klee, (Red Dress)\Klee.ini`,
		`D:\Mods\Klee, (Blue Dress)\Klee.ini`,
	}) {
		t.Fatalf("commaInDirLegacyList = %#v", got)
	}
}

func TestWriteNamespaceMergeRoundTripsCommaPathsAndRediscoversChildren(t *testing.T) {
	root := t.TempDir()
	folder1 := filepath.Join(root, "Klee, (Red Dress)")
	folder2 := filepath.Join(root, "Klee, (Blue Dress)")
	folder3 := filepath.Join(root, "Klee, (Green Dress)")
	child1 := writeNamespaceChild(t, folder1, "Klee.ini", namespaceChildINI)
	child2 := writeNamespaceChild(t, folder2, "Klee.ini", namespaceChildINI)
	child3 := writeNamespaceChild(t, folder3, "Klee.ini", namespaceChildINI)
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: child1, index: 0},
			{iniPath: child2, index: 1},
		},
		forwardKey: "]", backKey: "[",
	})
	masterContent, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	rel1, err := filepath.Rel(root, child1)
	if err != nil {
		t.Fatal(err)
	}
	rel2, err := filepath.Rel(root, child2)
	if err != nil {
		t.Fatal(err)
	}
	headerLines := []string{}
	for _, line := range strings.Split(string(masterContent), "\n") {
		if strings.HasPrefix(line, "; Merged Mod:") {
			headerLines = append(headerLines, line)
		}
	}
	if len(headerLines) != 1 {
		t.Fatalf("header lines = %#v", headerLines)
	}
	if !strings.Contains(headerLines[0], rel1) || !strings.Contains(headerLines[0], rel2) {
		t.Fatalf("header = %s", headerLines[0])
	}
	if got := extractMergedModPaths(string(masterContent)); !mergeStringSlicesEqual(got, []string{`.\` + rel1, `.\` + rel2}) {
		t.Fatalf("extracted = %#v", got)
	}
	discovered, err := collectNamespaceChildren(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !mergeResolvedPathSetsEqual(discovered, []string{child1, child2}) {
		t.Fatalf("discovered = %#v", discovered)
	}
	updatedMasterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: child1, index: 0},
			{iniPath: child2, index: 1},
			{iniPath: child3, index: 2},
		},
		forwardKey: "]", backKey: "[",
		existingMasterPath: masterPath,
	})
	rediscovered, err := collectNamespaceChildren(updatedMasterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !mergeResolvedPathSetsEqual(rediscovered, []string{child1, child2, child3}) {
		t.Fatalf("rediscovered = %#v", rediscovered)
	}
}

func TestUnwrapNamespaceParenthesizedSimpleMasterCondition(t *testing.T) {
	wrapped := `[TextureOverrideKleePosition]
hash = abcdef01
match_priority = 0
if ($\Klee\Master\swapvar == 0)
	vb0 = ResourcePosition
endif
`
	unwrapped, err := unwrapNamespace(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`\$\\Klee\\Master\\swapvar`).MatchString(unwrapped) {
		t.Fatalf("swapvar leftover: %s", unwrapped)
	}
	if !strings.Contains(unwrapped, "vb0 = ResourcePosition") {
		t.Fatalf("unwrapped = %s", unwrapped)
	}
}

func TestUnwrapNamespaceRejectsCompoundLeftoverMasterConditions(t *testing.T) {
	compound := `[TextureOverrideKleePosition]
hash = abcdef01
if ($\Klee\Master\swapvar == 0 && $foo == 1)
	vb0 = ResourcePosition
endif
`
	if _, err := unwrapNamespace(compound); err == nil || !strings.Contains(err.Error(), "NAMESPACE_UNWRAP_INCOMPLETE") {
		t.Fatalf("unwrap err = %v", err)
	}
	root := t.TempDir()
	childPath := writeNamespaceChild(t, root, "Klee.ini", compound)
	if _, err := writeNamespaceMerge(namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources:    []namespaceMergeSource{{iniPath: childPath, index: 0}},
		forwardKey: "]", backKey: "[",
	}); err == nil || !strings.Contains(err.Error(), "NAMESPACE_UNWRAP_INCOMPLETE") {
		t.Fatalf("write err = %v", err)
	}
}

func TestUnwrapNamespaceDropsTopLevelMasterElse(t *testing.T) {
	withElse := `[TextureOverrideKleePosition]
hash = abcdef01
if $\Klee\Master\swapvar == 0
	vb0 = ResourcePosition0
else
	vb0 = ResourcePosition1
endif
`
	unwrapped, err := unwrapNamespace(withElse)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`\$\\Klee\\Master\\swapvar`).MatchString(unwrapped) {
		t.Fatalf("swapvar leftover: %s", unwrapped)
	}
	if regexp.MustCompile(`(?m)^else$`).MatchString(unwrapped) {
		t.Fatalf("else leftover: %s", unwrapped)
	}
	if !strings.Contains(unwrapped, "vb0 = ResourcePosition0") || !strings.Contains(unwrapped, "vb0 = ResourcePosition1") {
		t.Fatalf("unwrapped = %s", unwrapped)
	}
}

func TestCollectNamespaceChildrenScansWhenListedPathsAreMissing(t *testing.T) {
	root := t.TempDir()
	childPath := writeNamespaceChild(t, root, "Klee.ini", namespaceChildINI+
		"if $\\Klee\\Master\\swapvar==0\n\tvb0 = ResourcePosition\nendif\n")
	masterPath := filepath.Join(root, "MasterKlee.ini")
	if err := os.WriteFile(masterPath, []byte("; Merged Mod: "+filepath.Join(root, "missing", "gone.ini")+"\nnamespace = Klee\\Master\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	discovered, err := collectNamespaceChildren(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !mergeResolvedPathSetsEqual(discovered, []string{childPath}) {
		t.Fatalf("discovered = %#v", discovered)
	}
}

func TestCollectNamespaceChildrenUnionsListedAndScanned(t *testing.T) {
	root := t.TempDir()
	listedChild := writeNamespaceChild(t, root, "Listed.ini", namespaceChildINI)
	scannedChild := writeNamespaceChild(t, root, "Scanned.ini", namespaceChildINI+
		"if $\\Klee\\Master\\swapvar==1\n\tvb0 = ResourcePosition\nendif\n")
	masterPath := filepath.Join(root, "MasterKlee.ini")
	if err := os.WriteFile(masterPath, []byte("; Merged Mod: "+listedChild+"\n; Merged Mod: "+filepath.Join(root, "missing.ini")+"\nnamespace = Klee\\Master\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	discovered, err := collectNamespaceChildren(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !mergeResolvedPathSetsEqual(discovered, []string{listedChild, scannedChild}) {
		t.Fatalf("discovered = %#v", discovered)
	}
}

func TestUnwrapNamespacePreservesNestedIfElifElseEndif(t *testing.T) {
	complexINI := `[TextureOverrideBlackSwanHairBlend]
hash = e2770c9a
match_priority = 0
if $\BlackswanMerge\Master\swapvarZ==0
	handling = skip
	vb2 = ResourceBlackSwanHairBlend
	if DRAW_TYPE == 1
		vb0 = ResourceBlackSwanHairPosition
		draw = 5376, 0
	endif
	ResourceBlackSwanHairDrawCS = copy ResourceBlackSwanHairDrawCS
	if DRAW_TYPE == 8
		Resource\SRMI\PositionBuffer = ref ResourceBlackSwanHairPositionCS
		Resource\SRMI\BlendBuffer = ref ResourceBlackSwanHairBlendCS
		Resource\SRMI\DrawBuffer = ref ResourceBlackSwanHairDrawCS
		$\SRMI\vertcount = 5376
	elif DRAW_TYPE != 1
		$_blend_ = 2
	endif
endif
`
	unwrapped, err := unwrapNamespace(complexINI)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`\$\\BlackswanMerge\\Master\\swapvarZ`).MatchString(unwrapped) {
		t.Fatalf("swapvar leftover: %s", unwrapped)
	}
	if !strings.Contains(unwrapped, "handling = skip") {
		t.Fatalf("unwrapped = %s", unwrapped)
	}
	if !regexp.MustCompile(`if DRAW_TYPE == 1\n\tvb0 = ResourceBlackSwanHairPosition\n\tdraw = 5376, 0\nendif`).MatchString(unwrapped) {
		t.Fatalf("draw type 1 = %s", unwrapped)
	}
	if !regexp.MustCompile(`elif DRAW_TYPE != 1\n\t\$_blend_ = 2\nendif`).MatchString(unwrapped) {
		t.Fatalf("elif = %s", unwrapped)
	}
}

func TestWriteNamespaceMergeIgnoresHelperFilesWhenPickingRepresentative(t *testing.T) {
	root := t.TempDir()
	orfixPath := writeNamespaceChild(t, root, "ORFix.ini", `[TextureOverrideORFix]
hash = helper01
run = CommandList\global\ORFix
`)
	childPath := writeNamespaceChild(t, root, "Klee.ini", namespaceChildINI)
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: orfixPath, index: 0},
			{iniPath: childPath, index: 0},
		},
		forwardKey: "]", backKey: "[",
	})
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(master), "[TextureOverrideKleePosition]") || !strings.Contains(string(master), "hash = abcdef01") {
		t.Fatalf("master = %s", master)
	}
	if strings.Contains(string(master), "hash = helper01") {
		t.Fatalf("helper hash used: %s", master)
	}
}

func TestWriteNamespaceMergeSelectsWWMIMarkBoneDataCBOverHelper(t *testing.T) {
	root := t.TempDir()
	orfixPath := writeNamespaceChild(t, root, "ORFix.ini", `[TextureOverrideORFix]
hash = helper01
run = CommandList\global\ORFix
`)
	roverPath := writeNamespaceChild(t, root, "Rover.ini", `; WWMI
[TextureOverrideRoverMarkBoneDataCB]
hash = 98765432
vb0 = ResourceRoverPosition
`)
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Rover",
		sources: []namespaceMergeSource{
			{iniPath: orfixPath, index: 0},
			{iniPath: roverPath, index: 0},
		},
		forwardKey: "]", backKey: "[",
	})
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(master), "[TextureOverrideRoverMarkBoneDataCB]") || !strings.Contains(string(master), "hash = 98765432") {
		t.Fatalf("master = %s", master)
	}
	if strings.Contains(string(master), "hash = helper01") {
		t.Fatalf("helper hash used: %s", master)
	}
}

func TestExtractPositionSectionHashReadsHashAfterFilenameOnlyResource(t *testing.T) {
	text := `[ResourcePosition]
filename = KleePosition.buf

[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`
	if got := extractPositionSectionHash(text); got != "abcdef01" {
		t.Fatalf("hash = %q", got)
	}
}

func TestWriteNamespaceMergeKeepsPositionHashWhenHairblendSourcePresent(t *testing.T) {
	root := t.TempDir()
	bodyPath := writeNamespaceChild(t, root, "Klee.ini", `[ResourcePosition]
filename = KleePosition.buf

[TextureOverrideKleePosition]
hash = abcdef01
vb0 = ResourcePosition
`)
	hairPath := writeNamespaceChild(t, root, "KleeHair.ini", `[TextureOverrideKleeHairBlend]
hash = hairblend01
vb2 = ResourceHairBlend
`)
	masterPath := mustWriteNamespaceMerge(t, namespaceMergeOptions{
		masterDir: root, name: "Klee",
		sources: []namespaceMergeSource{
			{iniPath: bodyPath, index: 0},
			{iniPath: hairPath, index: 0},
		},
		forwardKey: "]", backKey: "[",
	})
	master, err := os.ReadFile(masterPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(master), "[TextureOverrideKleePosition]") || !strings.Contains(string(master), "hash = abcdef01") {
		t.Fatalf("master = %s", master)
	}
	if strings.Contains(string(master), "hash = hairblend01") {
		t.Fatalf("hairblend hash used: %s", master)
	}
}

func mergeStringSlicesEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func mergeResolvedPathSetsEqual(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	normalize := func(paths []string) []string {
		out := make([]string, len(paths))
		for i, path := range paths {
			abs, err := filepath.Abs(path)
			if err != nil {
				abs = path
			}
			out[i] = abs
		}
		sort.Strings(out)
		return out
	}
	left, right := normalize(got), normalize(want)
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
