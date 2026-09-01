package zzmi

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmbeddedPack(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	if pack.UpstreamTag != EmbeddedTag || pack.CommitSHA != EmbeddedCommit {
		t.Fatalf("unexpected embedded identity: %s %s", pack.UpstreamTag, pack.CommitSHA)
	}
	if len(pack.HashCommands) != 5419 {
		t.Fatalf("expected 5419 hashes, got %d", len(pack.HashCommands))
	}
	if pack.Jane.Mapping[26] != 4 || pack.Jane.Secondary[4] != 0 || pack.Dialyn.Mapping[18] != 20 {
		t.Fatal("embedded remapper mappings are incomplete")
	}
}

func TestParseCharacterModuleRejectsExecutablePython(t *testing.T) {
	t.Parallel()
	_, err := ParseCharacterModule([]byte(`
def get_hash_commands(log, **kwargs):
    return {'12345678': [(log, (danger(),))]}
`))
	if err == nil || !strings.Contains(err.Error(), "expected") && !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected executable expression rejection, got %v", err)
	}
}

func TestParseCharacterModuleSupportsAdjacentStrings(t *testing.T) {
	t.Parallel()
	commands, err := ParseCharacterModule([]byte(`
def get_hash_commands(log, **kwargs):
    return {'12345678': [(log, ('hello ' 'world',))]}
`))
	if err != nil {
		t.Fatal(err)
	}
	if got := commands["12345678"][0].Args[0]; got != "hello world" {
		t.Fatalf("unexpected adjacent string result: %v", got)
	}
}

func TestCompileZipVerifiesGitBlob(t *testing.T) {
	t.Parallel()
	module := []byte(`
def get_hash_commands(log, **kwargs):
    return {'12345678': [(log, ('ok',))]}
`)
	jane := []byte(`
HAIR_MAPPINGS={26:4}
HAND_MAPPINGS={4:0}
POSITION_TO_BLEND={'33a09cfe':'e42171df','82e7c056':'d06a9206'}
STRIDE=32
`)
	dialyn := []byte(`
BLEND_MAPPING={18:20}
POSITION_TO_BLEND={'ff36809b':'3d7e53cf'}
STRIDE=32
VALID_BLEND_HASHES={'3d7e53cf'}
`)
	files := map[string][]byte{
		"repo/Source Codes/Assets/PlayerCharacterPYData/Test.py": module,
		"repo/Source Codes/Jane.remapper.py":                     jane,
		"repo/Source Codes/Dialyn.remapper.py":                   dialyn,
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	expected := map[string]string{}
	for name, data := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
		expected[strings.TrimPrefix(name, "repo/")] = gitBlobSHA(data)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	pack, err := CompileZip(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()), "tag", strings.Repeat("a", 40), "", expected)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.HashCommands) != 1 {
		t.Fatalf("unexpected hash count: %d", len(pack.HashCommands))
	}
	expected["Source Codes/Assets/PlayerCharacterPYData/Missing.py"] = gitBlobSHA(module)
	if _, err := CompileZip(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()), "tag", strings.Repeat("a", 40), "", expected); err == nil || !strings.Contains(err.Error(), "is missing") {
		t.Fatalf("expected missing rule rejection, got %v", err)
	}
	delete(expected, "Source Codes/Assets/PlayerCharacterPYData/Missing.py")
	expected["Source Codes/Jane.remapper.py"] = strings.Repeat("0", 40)
	if _, err := CompileZip(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()), "tag", strings.Repeat("a", 40), "", expected); err == nil {
		t.Fatal("expected blob mismatch rejection")
	}
}

func TestRunJaneRemapProducesBufferChange(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ini := `[TextureOverrideJane]
hash = 33a09cfe
vb2 = ResourceHairBlend

[ResourceHairBlend]
type = Buffer
stride = 32
filename = hair.buf
`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], 26)
	if err := os.WriteFile(filepath.Join(dir, "hair.buf"), buffer, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolJane, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedBUF != 1 || len(result.Changes) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := binary.LittleEndian.Uint32(result.Changes[0].Data[16:]); got != 4 {
		t.Fatalf("expected mapped index 4, got %d", got)
	}
	original, err := os.ReadFile(filepath.Join(dir, "hair.buf"))
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(original[16:]) != 26 {
		t.Fatal("engine wrote the source buffer directly")
	}
}

func TestRunHashFixProducesINIChangeWithoutWritingSource(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	var oldHash string
	var update Command
	for hash, commands := range pack.HashCommands {
		for _, command := range commands {
			if command.Op == "update_hash" {
				oldHash = hash
				update = command
				break
			}
		}
		if oldHash != "" {
			break
		}
	}
	if oldHash == "" {
		t.Fatal("embedded rules contain no update_hash command")
	}
	pack.HashCommands = map[string][]Command{oldHash: {update}}
	newHash := update.Args[0].(string)
	dir := t.TempDir()
	original := []byte("[TextureOverrideTest]\r\nhash = " + oldHash + "\r\n")
	path := filepath.Join(dir, "mod.ini")
	if err := os.WriteFile(path, original, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolHash, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedINI != 1 || len(result.Changes) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !bytes.Contains(result.Changes[0].Data, []byte("hash = "+newHash+"\r\n")) {
		t.Fatalf("updated INI does not contain %s: %q", newHash, result.Changes[0].Data)
	}
	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(current, original) {
		t.Fatal("engine wrote the source INI directly")
	}
}

func TestRunDialynRemapProducesBufferChange(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	var source, mapped uint32
	for candidate, replacement := range pack.Dialyn.Mapping {
		if candidate != replacement {
			source, mapped = candidate, replacement
			break
		}
	}
	if source == mapped {
		t.Fatal("embedded Dialyn rules contain no effective mapping")
	}
	dir := t.TempDir()
	ini := `[TextureOverrideDialyn]
hash = ` + pack.Dialyn.ValidHashes[0] + `
vb2 = ResourceDialynBlend

[ResourceDialynBlend]
type = Buffer
stride = 32
filename = dialyn.buf
`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], source)
	if err := os.WriteFile(filepath.Join(dir, "dialyn.buf"), buffer, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolDialyn, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedBUF != 1 || len(result.Changes) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if got := binary.LittleEndian.Uint32(result.Changes[0].Data[16:]); got != mapped {
		t.Fatalf("expected mapped index %d, got %d", mapped, got)
	}
}

func TestRunDialynFallbackRemapsUnknownHashResource(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ini := `[TextureOverrideUnknown]
hash = 12345678
vb2 = resourceGenericBlend

[ResourceGenericBlend]
type = Buffer
stride = 32
filename = generic.buf
`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], 18)
	if err := os.WriteFile(filepath.Join(dir, "generic.buf"), buffer, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolDialyn, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedBUF != 1 || binary.LittleEndian.Uint32(result.Changes[0].Data[16:]) != 20 {
		t.Fatalf("Dialyn fallback did not remap the buffer: %+v", result)
	}
}

func TestRunRemapsSharedBufferOnlyOnce(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, name := range []string{"a", "b"} {
		folder := filepath.Join(dir, name)
		if err := os.Mkdir(folder, 0o755); err != nil {
			t.Fatal(err)
		}
		ini := `[TextureOverrideDialyn]
hash = 3d7e53cf
vb2 = ResourceSharedBlend

[ResourceSharedBlend]
type = Buffer
stride = 32
filename = ../shared.buf
`
		if err := os.WriteFile(filepath.Join(folder, "mod.ini"), []byte(ini), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], 18)
	if err := os.WriteFile(filepath.Join(dir, "shared.buf"), buffer, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolDialyn, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedBUF != 1 || len(result.Changes) != 1 {
		t.Fatalf("unexpected shared-buffer result: %+v", result)
	}
	if got := binary.LittleEndian.Uint32(result.Changes[0].Data[16:]); got != 20 {
		t.Fatalf("shared buffer was remapped more than once: got %d", got)
	}
}

func TestRunDiscardsBufferChangesWhenINIProcessingFails(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	const hash = "12345678"
	pack.HashCommands = map[string][]Command{
		hash: {
			{Op: "update_buffer_blend_indices", Args: []any{hash, []any{int64(18)}, []any{int64(20)}}},
			{Op: "zzz_12_shrink_texcoord_color", Args: []any{"broken-after-buffer-change"}},
		},
	}
	dir := t.TempDir()
	ini := `[TextureOverrideTest]
hash = 12345678
vb2 = ResourceBlend
vb1 = ResourceMissing

[ResourceBlend]
type = Buffer
stride = 32
filename = blend.buf
`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], 18)
	if err := os.WriteFile(filepath.Join(dir, "blend.buf"), buffer, 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolHash, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedFiles != 1 || result.ChangedBUF != 0 || len(result.Changes) != 0 {
		t.Fatalf("failed INI leaked staged buffer changes: %+v", result)
	}
}

func TestRunRejectsNon32BlendStride(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	const hash = "12345678"
	pack.HashCommands = map[string][]Command{
		hash: {{Op: "update_buffer_blend_indices", Args: []any{hash, []any{int64(18)}, []any{int64(20)}}}},
	}
	dir := t.TempDir()
	ini := `[TextureOverrideTest]
hash = 12345678
vb2 = ResourceBlend

[ResourceBlend]
type = Buffer
stride = 64
filename = blend.buf
`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "blend.buf"), make([]byte, 64), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolHash, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.SkippedFiles != 1 || result.ChangedBUF != 0 {
		t.Fatalf("non-32 blend stride was accepted: %+v", result)
	}
}

func TestTransferIndexedSectionsPreservesUnrelatedSections(t *testing.T) {
	t.Parallel()
	const hash = "0f82a13e"
	content := `[TextureOverrideGraceBodyIB]
hash = 0f82a13e
ib = ResourceGraceBodyIB

[ResourceKeep]
type = Buffer
filename = keep.buf

[TextureOverrideGraceBodyA]
hash = 0f82a13e
match_first_index = 0
ib = ResourceGraceBodyA

[CommandListKeep]
run = CommandListSomething

[TextureOverrideGraceBodyB]
hash = 0f82a13e
match_first_index = 42885
ib = ResourceGraceBodyB
`
	updated, err := transferIndexed(content, hash, map[string]any{
		"src_indices": []any{"0", "42885"},
		"trg_indices": []any{"0", "42927"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"[TextureOverrideGraceBodyIB]", "[TextureOverrideGraceBodyA]", "[TextureOverrideGraceBodyB]",
		"match_first_index = 42927", "[ResourceKeep]", "[CommandListKeep]",
	} {
		if !strings.Contains(updated, expected) {
			t.Fatalf("updated INI is missing %q:\n%s", expected, updated)
		}
	}
	if strings.Contains(updated, "GraceBodyIBIB") || strings.Contains(updated, "GraceBodyAA") {
		t.Fatalf("indexed section base title was not restored:\n%s", updated)
	}

	onlyUnindexed := "[TextureOverrideGraceBodyIB]\nhash = " + hash + "\n"
	unchanged, err := transferIndexed(onlyUnindexed, hash, map[string]any{
		"src_indices": []any{"0"}, "trg_indices": []any{"1"},
	})
	if err != nil || unchanged != onlyUnindexed {
		t.Fatalf("unindexed-only input should be unchanged: %v\n%s", err, unchanged)
	}
}

func TestReferencedResourcesStopsCommandListCycles(t *testing.T) {
	t.Parallel()
	content := `[TextureOverrideCycle]
hash = 12345678
run = CommandListA

[CommandListA]
run = CommandListB

[CommandListB]
run = CommandListA
vb1 = ResourceTexcoord
`
	resources := referencedResources(content, "12345678", "vb1")
	if len(resources) != 1 || resources[0] != "ResourceTexcoord" {
		t.Fatalf("unexpected cyclic command-list resources: %#v", resources)
	}
}

func TestRunRejectsEscapingResource(t *testing.T) {
	t.Parallel()
	pack, err := LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	parent := t.TempDir()
	dir := filepath.Join(parent, "mod")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(parent, "outside.buf")
	if err := os.WriteFile(outside, make([]byte, 32), 0o644); err != nil {
		t.Fatal(err)
	}
	ini := `[TextureOverrideJane]
hash = 33a09cfe
vb2 = ResourceHairBlend
[ResourceHairBlend]
type = Buffer
stride = 32
filename = ../outside.buf
`
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := Run(context.Background(), dir, ToolJane, pack, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedBUF != 0 || len(result.Warnings) == 0 {
		t.Fatalf("expected unsafe resource warning: %+v", result)
	}
}
