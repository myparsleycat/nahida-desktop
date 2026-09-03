package tools

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/db"
	zzmiengine "nahida.live/desktop/internal/tools/zzmi"
)

func TestFixInspectorRegistry(t *testing.T) {
	registry := NewFixInspectorRegistry()
	ctx := context.Background()

	res, err := registry.Inspect(ctx, "some/path", "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsFix {
		t.Fatal("expected NeedsFix to be false for empty registry")
	}

	dummy := &dummyInspector{supported: "GIMI"}
	registry.Register(dummy)

	res, err = registry.Inspect(ctx, "some/path", "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsFix {
		t.Fatal("expected NeedsFix to be false for unsupported importer")
	}

	res, err = registry.Inspect(ctx, "some/path", "GIMI")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsFix || res.ToolName != "Dummy Fixer" {
		t.Fatalf("unexpected result from dummy inspector: %+v", res)
	}
}

type dummyInspector struct {
	supported string
}

func (d *dummyInspector) CanInspect(importer string) bool {
	return importer == d.supported
}

func (d *dummyInspector) Inspect(_ context.Context, _ string) (*FixInspectionResult, error) {
	return &FixInspectionResult{
		NeedsFix:   true,
		Importer:   d.supported,
		ToolName:   "Dummy Fixer",
		Summary:    "Dummy fix required",
		ActionTool: "dummy",
	}, nil
}

func TestZZMIFixInspector(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()

	service := New()
	service.UseClient(openToolsTestDB(t))
	useToolsTestAppData(t, service, t.TempDir())
	importer := "ZZMI"
	if err := service.client.GamePaths.Insert(ctx, db.GamePathRow{
		Game:          "ZZZ",
		ModFolderPath: root,
		Importer:      &importer,
	}); err != nil {
		t.Fatal(err)
	}

	pack, err := zzmiengine.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}

	cleanTarget := filepath.Join(root, "CleanMod")
	if err := os.Mkdir(cleanTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cleanTarget, "mod.ini"), []byte("[TextureOverrideClean]\nhash = aabbccdd\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := service.InspectModForFix(ctx, cleanTarget, "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsFix {
		t.Fatalf("expected NeedsFix: false for clean mod, got %+v", res)
	}

	janeTarget := filepath.Join(root, "JaneMod")
	if err := os.Mkdir(janeTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	ini := `[TextureOverrideJane]
hash = 33a09cfe
vb2 = ResourceHairBlend

[ResourceHairBlend]
type = Buffer
stride = 32
filename = hair.buf
`
	if err := os.WriteFile(filepath.Join(janeTarget, "mod.ini"), []byte(ini), 0o644); err != nil {
		t.Fatal(err)
	}
	bufferPath := filepath.Join(janeTarget, "hair.buf")
	buffer := make([]byte, 32)
	binary.LittleEndian.PutUint32(buffer[16:], 26) // Index 26 maps to 4 in Jane rules
	if err := os.WriteFile(bufferPath, buffer, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err = service.InspectModForFix(ctx, janeTarget, "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsFix || !slices.Contains(res.Details, "Jane Doe blend remapping required") {
		t.Fatalf("expected NeedsFix: true with Jane Doe remap in details, got %+v", res)
	}

	originalContent, err := os.ReadFile(bufferPath)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint32(originalContent[16:]) != 26 {
		t.Fatal("inspection modified the file on disk! Dry-run invariant violated")
	}

	var outdatedHash string
	for hash, cmds := range pack.HashCommands {
		for _, cmd := range cmds {
			if cmd.Op == "update_hash" {
				outdatedHash = hash
				break
			}
		}
		if outdatedHash != "" {
			break
		}
	}
	if outdatedHash != "" {
		hashTarget := filepath.Join(root, "OutdatedHashMod")
		if err := os.Mkdir(hashTarget, 0o755); err != nil {
			t.Fatal(err)
		}
		hashIni := "[TextureOverrideOld]\nhash = " + outdatedHash + "\n"
		if err := os.WriteFile(filepath.Join(hashTarget, "mod.ini"), []byte(hashIni), 0o644); err != nil {
			t.Fatal(err)
		}

		res, err = service.InspectModForFix(ctx, hashTarget, "ZZMI")
		if err != nil {
			t.Fatal(err)
		}
		if !res.NeedsFix || res.ActionTool != "hash" {
			t.Fatalf("expected NeedsFix: true with ActionTool: 'hash', got %+v", res)
		}
	}

	otherTarget := filepath.Join(root, "OtherMod")
	if err := os.Mkdir(otherTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	otherIni := `[TextureOverrideHair]
hash = 12345678
vb2 = ResourceHairBlend

[ResourceHairBlend]
type = Buffer
stride = 32
filename = hair.buf
`
	if err := os.WriteFile(filepath.Join(otherTarget, "mod.ini"), []byte(otherIni), 0o644); err != nil {
		t.Fatal(err)
	}
	otherBuf := make([]byte, 32)
	binary.LittleEndian.PutUint32(otherBuf[16:], 18) // Dialyn remapping candidate
	if err := os.WriteFile(filepath.Join(otherTarget, "hair.buf"), otherBuf, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = service.InspectModForFix(ctx, otherTarget, "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if res.NeedsFix {
		t.Fatalf("expected NeedsFix: false when ini lacks Jane/Dialyn hashes, got %+v", res)
	}

	dialynTarget := filepath.Join(root, "DialynMod")
	if err := os.Mkdir(dialynTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	dialynIni := `[TextureOverrideCustom]
hash = ff36809b
vb2 = ResourceBlend

[ResourceBlend]
type = Buffer
stride = 32
filename = body.buf
`
	if err := os.WriteFile(filepath.Join(dialynTarget, "mod.ini"), []byte(dialynIni), 0o644); err != nil {
		t.Fatal(err)
	}
	dialynBuf := make([]byte, 32)
	binary.LittleEndian.PutUint32(dialynBuf[16:], 18)
	if err := os.WriteFile(filepath.Join(dialynTarget, "body.buf"), dialynBuf, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = service.InspectModForFix(ctx, dialynTarget, "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsFix || !slices.Contains(res.Details, "Dialyn blend remapping required") {
		t.Fatalf("expected NeedsFix: true with Dialyn remap in details, got %+v", res)
	}

	legacyJaneTarget := filepath.Join(root, "LegacyJaneMod")
	if err := os.Mkdir(legacyJaneTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	legacyJaneIni := `[TextureOverrideHair]
hash = e7a3b7dc
vb2 = ResourceHairBlend

[ResourceHairBlend]
type = Buffer
stride = 32
filename = hair.buf
`
	if err := os.WriteFile(filepath.Join(legacyJaneTarget, "mod.ini"), []byte(legacyJaneIni), 0o644); err != nil {
		t.Fatal(err)
	}
	legacyBuf := make([]byte, 32)
	binary.LittleEndian.PutUint32(legacyBuf[16:], 26)
	if err := os.WriteFile(filepath.Join(legacyJaneTarget, "hair.buf"), legacyBuf, 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = service.InspectModForFix(ctx, legacyJaneTarget, "ZZMI")
	if err != nil {
		t.Fatal(err)
	}
	if !res.NeedsFix || !slices.Contains(res.Details, "Jane Doe blend remapping required") {
		t.Fatalf("expected NeedsFix: true with Jane remap for legacy hash, got %+v", res)
	}
}
