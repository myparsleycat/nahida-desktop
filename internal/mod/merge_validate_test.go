package mod

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateMergeRequestRejectsInvalidPayloads(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "CharA")
	leafA := filepath.Join(group, "A")
	leafB := filepath.Join(group, "B")
	if err := os.MkdirAll(leafA, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(leafB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	valid := func() MergeModsRequest {
		return MergeModsRequest{
			GroupPath: group, Placement: "new_folder", PackName: "Merged",
			Root: MergePlanNode{
				Kind: "group", ID: "root", Engine: "classic", Name: "Merged",
				ForwardKey: "vk_right", BackKey: "",
				Children: []MergePlanNode{{Kind: "leaf", Path: leafA}, {Kind: "leaf", Path: leafB}},
			},
		}
	}
	if err := service.validateMergeRequest(ctx, valid()); err != nil {
		t.Fatal(err)
	}
	invalid := []MergeModsRequest{
		func() MergeModsRequest { r := valid(); r.Placement = "elsewhere"; return r }(),
		func() MergeModsRequest { r := valid(); r.PackName = `..\escape`; return r }(),
		func() MergeModsRequest { r := valid(); r.PackName = "Merged/evil"; return r }(),
		func() MergeModsRequest { r := valid(); r.PackName = "Bad]Name"; return r }(),
		func() MergeModsRequest { r := valid(); r.PackName = "A=B"; return r }(),
		func() MergeModsRequest { r := valid(); r.PackName = "Line\nBreak"; return r }(),
		func() MergeModsRequest { r := valid(); r.GroupPath = "relative-group"; return r }(),
		func() MergeModsRequest { r := valid(); r.Root = MergePlanNode{Kind: "leaf", Path: leafA}; return r }(),
		func() MergeModsRequest {
			r := valid()
			r.Root.Children = []MergePlanNode{{Kind: "leaf", Path: leafA}}
			return r
		}(),
		func() MergeModsRequest {
			r := valid()
			r.Root.Children = []MergePlanNode{{Kind: "leaf", Path: leafA}, {Kind: "leaf", Path: leafA}}
			return r
		}(),
		func() MergeModsRequest {
			r := valid()
			r.Root.Engine = "namespace"
			r.Root.BackKey = ""
			return r
		}(),
		func() MergeModsRequest { r := valid(); r.Root.ForwardKey = "vk_right\n"; return r }(),
		func() MergeModsRequest { r := valid(); r.Root.BackKey = "vk_left\n"; return r }(),
		func() MergeModsRequest { r := valid(); r.Root.Name = `..\outside`; return r }(),
		func() MergeModsRequest { r := valid(); r.Root.Name = "Bad]Name"; return r }(),
	}
	for i, request := range invalid {
		if err := service.validateMergeRequest(ctx, request); err == nil || err.Error() != invalidMergeRequestMessage {
			t.Fatalf("case %d error = %v, want %q", i, err, invalidMergeRequestMessage)
		}
	}
	okKeys := valid()
	okKeys.Root.ForwardKey = "ctrl alt no_shift vk_up"
	okKeys.Root.BackKey = "VK_OEM_4"
	if err := service.validateMergeRequest(ctx, okKeys); err != nil {
		t.Fatal(err)
	}
	korean := valid()
	korean.PackName = "나히다"
	korean.Root.Name = "나히다"
	if err := service.validateMergeRequest(ctx, korean); err != nil {
		t.Fatal(err)
	}
}

func TestValidateMergeRequestRejectsPathsOutsideManagedRootOrGroup(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "CharA")
	first := filepath.Join(group, "A")
	second := filepath.Join(group, "B")
	sibling := filepath.Join(modsRoot, "Other", "C")
	outside := filepath.Join(root, "outside")
	for _, path := range []string{first, second, sibling, outside} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	request := MergeModsRequest{
		GroupPath: group, Placement: "new_folder", PackName: "Merged",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "classic", Name: "Merged",
			ForwardKey: "vk_right",
			Children:   []MergePlanNode{{Kind: "leaf", Path: first}, {Kind: "leaf", Path: sibling}},
		},
	}
	if err := service.validateMergeRequest(ctx, request); err == nil {
		t.Fatal("sibling outside group accepted")
	} else if err.Error() != outsideMergeGroupMessage {
		t.Fatalf("sibling error = %q", err)
	}
	request.Root.Children = []MergePlanNode{{Kind: "leaf", Path: first}, {Kind: "leaf", Path: group}}
	if err := service.validateMergeRequest(ctx, request); err == nil {
		t.Fatal("group-as-leaf accepted")
	}
	request.Root.Children = []MergePlanNode{{Kind: "leaf", Path: first}, {Kind: "leaf", Path: outside}}
	if err := service.validateMergeRequest(ctx, request); err == nil ||
		!strings.Contains(err.Error(), outsideManagedModsMessage) {
		t.Fatalf("outside err = %v", err)
	}
}

func TestValidateMergeRequestFollowsGroupSymlinks(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	realRoot := filepath.Join(root, "real")
	linkParent := filepath.Join(root, "links")
	group := filepath.Join(realRoot, "CharA")
	first := filepath.Join(group, "A")
	second := filepath.Join(group, "B")
	for _, path := range []string{first, second, linkParent} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", realRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	linkGroup := filepath.Join(linkParent, "LinkedCharA")
	if err := os.Symlink(group, linkGroup); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	request := MergeModsRequest{
		GroupPath: linkGroup, Placement: "new_folder", PackName: "Merged",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "classic", Name: "Merged",
			ForwardKey: "vk_right",
			Children: []MergePlanNode{
				{Kind: "leaf", Path: filepath.Join(linkGroup, "A")},
				{Kind: "leaf", Path: filepath.Join(linkGroup, "B")},
			},
		},
	}
	if err := service.validateMergeRequest(ctx, request); err != nil {
		t.Fatal(err)
	}

	outsideTarget := filepath.Join(root, "outside", "OutsideCharA")
	if err := os.MkdirAll(filepath.Join(outsideTarget, "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(outsideTarget, "B"), 0o755); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(realRoot, "EscapeLink")
	if err := os.Symlink(outsideTarget, escape); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	request.GroupPath = escape
	request.Root.Children = []MergePlanNode{
		{Kind: "leaf", Path: filepath.Join(escape, "A")},
		{Kind: "leaf", Path: filepath.Join(escape, "B")},
	}
	if err := service.validateMergeRequest(ctx, request); err == nil {
		t.Fatal("escaped symlink group accepted")
	}
}

func TestValidateMergeRequestTreatsDistinctSymlinkAliasesAsLexicallyUnique(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	group := filepath.Join(modsRoot, "CharA")
	target := filepath.Join(group, "Target")
	other := filepath.Join(group, "Other")
	for _, path := range []string{target, other} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	firstAlias := filepath.Join(group, "AliasA")
	secondAlias := filepath.Join(group, "AliasB")
	if err := os.Symlink(target, firstAlias); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	if err := os.Symlink(target, secondAlias); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	request := MergeModsRequest{
		GroupPath: group, Placement: "new_folder", PackName: "Merged",
		Root: MergePlanNode{
			Kind: "group", ID: "root", Engine: "classic", Name: "Merged", ForwardKey: "vk_right",
			Children: []MergePlanNode{{Kind: "leaf", Path: firstAlias}, {Kind: "leaf", Path: secondAlias}},
		},
	}
	if err := service.validateMergeRequest(ctx, request); err != nil {
		t.Fatalf("distinct lexical aliases resolving to one target were rejected: %v", err)
	}
}

func TestUniqueLexicalMergeLeavesNormalizesCaseAndDotsWithoutResolvingSymlinks(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "Group")
	if uniqueLexicalMergeLeaves([]string{filepath.Join(root, "A"), filepath.Join(root, ".", "a")}) {
		t.Fatal("case/dot-equivalent lexical paths were accepted")
	}
	if !uniqueLexicalMergeLeaves([]string{filepath.Join(root, "AliasA"), filepath.Join(root, "AliasB")}) {
		t.Fatal("distinct lexical paths were rejected")
	}
}

func TestOwnedPathRejectsEscapingAndCyclicSymlinks(t *testing.T) {
	ctx := context.Background()
	service, root := newTestMod(t, testSettings{})
	modsRoot := filepath.Join(root, "mods")
	outside := filepath.Join(root, "outside", "secret")
	if err := os.MkdirAll(modsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := service.AddGame(ctx, "Game", modsRoot, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	escape := filepath.Join(modsRoot, "escape")
	if err := os.Symlink(outside, escape); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	if err := os.RemoveAll(outside); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ownedPath(ctx, escape); err == nil {
		t.Fatal("dangling outside symlink accepted")
	}
	if _, err := service.ownedPath(ctx, filepath.Join(escape, "child")); err == nil {
		t.Fatal("dangling outside child accepted")
	}

	insideTarget := filepath.Join(modsRoot, "missing-dest")
	if err := os.MkdirAll(insideTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(modsRoot, "alias")
	if err := os.Symlink(insideTarget, alias); err != nil {
		t.Skipf("directory symlink is unavailable: %v", err)
	}
	if err := os.RemoveAll(insideTarget); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ownedPath(ctx, alias); err != nil {
		t.Fatalf("inside dangling alias rejected: %v", err)
	}
	if _, err := service.ownedPath(ctx, filepath.Join(alias, "child")); err != nil {
		t.Fatalf("inside dangling child rejected: %v", err)
	}

	loop := filepath.Join(modsRoot, "loop")
	if err := os.Symlink(loop, loop); err != nil {
		t.Skipf("self symlink is unavailable: %v", err)
	}
	if _, err := service.ownedPath(ctx, loop); err == nil {
		t.Fatal("self-referential symlink accepted")
	}
}
