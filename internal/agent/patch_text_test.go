package agent

import (
	"reflect"
	"strings"
	"testing"
)

func TestParsePatchTextReadsFileSections(t *testing.T) {
	t.Parallel()
	operations, err := parsePatchText(strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: mod.ini",
		"@@ [TextureOverrideBody]",
		" hash = 12345678",
		"-\trun = CommandListOld",
		"+\trun = CommandListNew",
		"",
		"@@",
		"-stale = 1",
		"*** End of File",
		"*** Add File: notes/readme.txt",
		"+first",
		"+",
		"+third",
		"*** Delete File: stale.ini",
		"*** End Patch",
	}, "\r\n"))
	if err != nil {
		t.Fatal(err)
	}

	want := []PatchOperation{
		{Type: "update", Path: "mod.ini", Hunks: []PatchHunk{
			{
				Context:  "[TextureOverrideBody]",
				OldLines: []string{"hash = 12345678", "\trun = CommandListOld", ""},
				NewLines: []string{"hash = 12345678", "\trun = CommandListNew", ""},
			},
			{OldLines: []string{"stale = 1"}, EOF: true},
		}},
		{Type: "create", Path: "notes/readme.txt", Content: "first\n\nthird\n"},
		{Type: "delete", Path: "stale.ini"},
	}
	if !reflect.DeepEqual(operations, want) {
		t.Fatalf("operations = %#v", operations)
	}
}

// A chunk may omit "@@", and a chunk that only adds lines without context appends to the file.
func TestParsePatchTextAcceptsImplicitChunks(t *testing.T) {
	t.Parallel()
	operations, err := parsePatchText(
		"*** Begin Patch\n*** Update File: mod.ini\n\n-key=1\n+key=2\n@@\n+[New]\n+value=1\n*** End Patch\n",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []PatchHunk{
		{OldLines: []string{"key=1"}, NewLines: []string{"key=2"}},
		{NewLines: []string{"[New]", "value=1"}, EOF: true},
	}
	if len(operations) != 1 || !reflect.DeepEqual(operations[0].Hunks, want) {
		t.Fatalf("operations = %#v", operations)
	}

	updated, _, err := applyPatchHunks("key=1\r\n", operations[0].Hunks, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	if updated != "key=2\r\n[New]\r\nvalue=1\r\n" {
		t.Fatalf("updated = %q", updated)
	}
}

func TestParsePatchTextRejectsMalformedPatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, text, errMsg string
	}{
		{name: "no envelope", text: "*** Update File: mod.ini\n-a\n+b", errMsg: `must start with "*** Begin Patch"`},
		{
			name:   "no end marker",
			text:   "*** Begin Patch\n*** Update File: mod.ini\n-a\n+b",
			errMsg: `must end with "*** End Patch"`,
		},
		{name: "empty", text: "*** Begin Patch\n*** End Patch", errMsg: "contains no file sections"},
		{
			name:   "stray line",
			text:   "*** Begin Patch\nkey=1\n*** End Patch",
			errMsg: `patch line 2: expected "*** Update File:"`,
		},
		{
			name:   "unprefixed chunk line",
			text:   "*** Begin Patch\n*** Update File: mod.ini\n@@ [A]\nkey=1\n*** End Patch",
			errMsg: `patch line 4: a chunk line starts with a space, -, or +, got "key=1"`,
		},
		{
			name:   "update without changes",
			text:   "*** Begin Patch\n*** Update File: mod.ini\n*** End Patch",
			errMsg: `patch line 2: update of "mod.ini" has no changes`,
		},
		{
			name:   "move",
			text:   "*** Begin Patch\n*** Update File: a.ini\n*** Move to: b.ini\n@@\n-a\n+b\n*** End Patch",
			errMsg: "rename with move_path",
		},
		{
			name:   "added file line without plus",
			text:   "*** Begin Patch\n*** Add File: a.ini\nkey=1\n*** End Patch",
			errMsg: "patch line 3: every line of an added file starts with +",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			_, err := parsePatchText(testCase.text)
			if err == nil || !strings.Contains(err.Error(), testCase.errMsg) {
				t.Fatalf("err = %v, want message containing %q", err, testCase.errMsg)
			}
		})
	}
}

// The patch a model wrote for a mod whose texture block repeats under two overrides: chunks in file
// order without context, relaxed indentation, and a blank separator line before the next section.
func TestPatchTextResolvesRepeatedBlocksInFileOrder(t *testing.T) {
	t.Parallel()
	text := strings.Join([]string{
		"[TextureOverrideComponent3]",
		"\tif $draw",
		"\t\tdrawindexed = 444, 0, 0",
		"\tendif",
		"        Resource\\RabbitFX\\Diffuse = ref ResourceTextureBodyD",
		"        run = Commandlist\\RabbitFX\\SetTextures",
		"    run = CommandListCleanup",
		"",
		"[TextureOverrideComponent7]",
		"        Resource\\RabbitFX\\Diffuse = ref ResourceTextureBodyD",
		"        run = Commandlist\\RabbitFX\\SetTextures",
		"    run = CommandListCleanup",
		"",
	}, "\r\n")
	operations, err := parsePatchText(strings.Join([]string{
		"*** Begin Patch",
		"*** Update File: mod.ini",
		"@@",
		"-        Resource\\RabbitFX\\Diffuse = ref ResourceTextureBodyD",
		"-        run = Commandlist\\RabbitFX\\SetTextures",
		"+        ps-t3 = ref ResourceTextureBodyD",
		"@@ [TextureOverrideComponent7]",
		"-    Resource\\RabbitFX\\Diffuse = ref ResourceTextureBodyD",
		"-    run = Commandlist\\RabbitFX\\SetTextures",
		"+        ps-t3 = ref ResourceTextureBodyD",
		"     run = CommandListCleanup",
		"",
		"*** End Patch",
	}, "\n"))
	if err != nil {
		t.Fatal(err)
	}

	updated, warnings, err := applyPatchHunks(text, operations[0].Hunks, "mod.ini", "\r\n")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"[TextureOverrideComponent3]",
		"\tif $draw",
		"\t\tdrawindexed = 444, 0, 0",
		"\tendif",
		"        ps-t3 = ref ResourceTextureBodyD",
		"    run = CommandListCleanup",
		"",
		"[TextureOverrideComponent7]",
		"        ps-t3 = ref ResourceTextureBodyD",
		"    run = CommandListCleanup",
		"",
	}, "\r\n")
	if updated != want {
		t.Fatalf("updated = %q", updated)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "hunk 2: matched at line 10 with indentation ignored") {
		t.Fatalf("warnings = %v", warnings)
	}
}
