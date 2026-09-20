package texture

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func diagnosticFixture(t *testing.T, format uint32, pixels []byte) (string, []byte) {
	t.Helper()
	header := make([]byte, 148)
	copy(header, "DDS ")
	for offset, value := range map[int]uint32{
		4: 124, 8: 0x100f, 12: 1, 16: uint32(len(pixels) / 4), 20: uint32(len(pixels)),
		28: 1, 76: 32, 80: 4, 108: 0x1000, 128: format, 132: 3, 140: 1,
	} {
		binary.LittleEndian.PutUint32(header[offset:offset+4], value)
	}
	copy(header[84:88], "DX10")
	raw := append(bytes.Clone(header), pixels...)
	path := filepath.Join(t.TempDir(), "mask.dds")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, raw
}

func TestInspectDDSStoredChannelsAndConditionalRisk(t *testing.T) {
	t.Parallel()
	path, original := diagnosticFixture(
		t,
		29,
		[]byte{0, 10, 20, 200, 1, 11, 21, 201, 6, 12, 22, 202, 7, 13, 23, 203, 128, 14, 24, 204},
	)
	got, err := InspectDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 5 || !got.SRGB || got.SHA256 != digestDDS(original) || got.RedQuantizationRisk != 2 ||
		got.Channels[0].Histogram[128] != 1 ||
		got.Channels[3].Min != 200 {
		t.Fatalf("inspection = %+v", got)
	}
	preview, err := PreviewDDS(context.Background(), path, "a")
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(preview))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, a := img.At(0, 0).RGBA()
	if r != 200*257 || g != r || b != r || a != 65535 {
		t.Fatalf("alpha preview = %d %d %d %d", r, g, b, a)
	}
	if _, err := PreviewDDS(context.Background(), path, ""); err == nil {
		t.Fatal("empty channel accepted")
	}
}

func TestRepairDDSRetagBackupAndRestore(t *testing.T) {
	t.Parallel()
	path, original := diagnosticFixture(t, 29, []byte{6, 40, 90, 255, 128, 80, 0, 128})
	request := DDSRepairRequest{Operation: "reinterpret_linear", ExpectedSHA256: digestDDS(original)}
	preview, err := RepairDDS(context.Background(), path, request)
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied || len(entries) != 1 || preview.ChangedBytes != 1 {
		t.Fatalf("preview = %+v, entries %d", preview, len(entries))
	}
	request.Apply = true
	result, err := RepairDDS(context.Background(), path, request)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Applied || !bytes.Equal(original, backup) || !bytes.Equal(original[132:], changed[132:]) ||
		!bytes.Equal(original[:128], changed[:128]) ||
		changed[128] != 28 ||
		preview.AfterSHA256 != result.AfterSHA256 {
		t.Fatalf("unexpected retag: %+v", result)
	}
	if _, err := RepairDDS(context.Background(), path, request); err == nil {
		t.Fatal("stale source accepted")
	}
	if _, err := RestoreDDS(
		context.Background(),
		path,
		result.BackupPath,
		result.AfterSHA256,
		"wrong",
		true,
	); err == nil {
		t.Fatal("wrong backup hash accepted")
	}
	restored, err := RestoreDDS(
		context.Background(),
		path,
		result.BackupPath,
		result.AfterSHA256,
		result.BeforeSHA256,
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) || restored.BackupPath == result.BackupPath {
		t.Fatal("restore did not retain exact states")
	}
}

func TestRepairDDSLinearCurveAndSelectedFloor(t *testing.T) {
	t.Parallel()
	pixels := []byte{0, 128, 64, 123, 6, 128, 64, 124, 7, 128, 64, 125, 128, 128, 64, 126}
	path, raw := diagnosticFixture(t, 29, pixels)
	result, err := RepairDDS(context.Background(), path, DDSRepairRequest{
		Operation: "linearize_mask", ExpectedSHA256: digestDDS(raw), SourceColorSpace: "srgb", Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	dds, err := loadDiagnosticDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	for i, red := range []byte{0, 0, 1, 55} {
		if dds.pixels[i*4] != red || dds.pixels[i*4+1] != 55 || dds.pixels[i*4+2] != 13 ||
			dds.pixels[i*4+3] != pixels[i*4+3] {
			t.Fatalf("linearized = %v", dds.pixels)
		}
	}
	mask := image.NewGray(image.Rect(0, 0, 4, 1))
	mask.SetGray(1, 0, color.Gray{Y: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, mask); err != nil {
		t.Fatal(err)
	}
	selection := filepath.Join(filepath.Dir(path), "selection.png")
	if err := os.WriteFile(selection, encoded.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	floor, err := RepairDDS(context.Background(), path, DDSRepairRequest{
		Operation: "minimum_red", ExpectedSHA256: result.AfterSHA256, MinimumRed: 1,
		SelectionPath: selection, SelectionSHA256: digestDDS(encoded.Bytes()), Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := loadDiagnosticDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	want := bytes.Clone(dds.pixels)
	want[4] = 1
	if !bytes.Equal(updated.pixels, want) || floor.ChangedPixels != 1 || floor.BackupPath == result.BackupPath {
		t.Fatalf("floor = %+v pixels %v", floor, updated.pixels)
	}
}

func TestDDSRejectsUnsupportedAndTruncatedInputs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		offset int
		value  uint32
	}{
		{"array", 140, 2}, {"cube", 136, 4}, {"volume", 132, 4}, {"oversize", 16, 1 << 30}, {"float", 128, 2}, {"mips", 28, 99},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path, raw := diagnosticFixture(t, 29, []byte{1, 2, 3, 4})
			binary.LittleEndian.PutUint32(raw[tc.offset:tc.offset+4], tc.value)
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := InspectDDS(context.Background(), path); err == nil {
				t.Fatal("invalid DDS accepted")
			}
		})
	}
	path, raw := diagnosticFixture(t, 29, []byte{1, 2, 3, 4})
	for _, size := range []int{0, 4, 127, 147, 148, 151} {
		if err := os.WriteFile(path, raw[:size], 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := InspectDDS(context.Background(), path); err == nil {
			t.Fatalf("truncation %d accepted", size)
		}
	}
}

func TestDDSRepairRejectsUnsafeRequestsWithoutWrites(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		request DDSRepairRequest
	}{
		{"unknown", DDSRepairRequest{Operation: "unknown"}},
		{"missing color semantics", DDSRepairRequest{Operation: "linearize_mask"}},
		{"missing selection", DDSRepairRequest{Operation: "minimum_red", MinimumRed: 1}},
		{"zero floor", DDSRepairRequest{Operation: "minimum_red", WholeTexture: true}},
		{"discard floor", DDSRepairRequest{Operation: "minimum_red", WholeTexture: true, MinimumRed: 255}},
		{"missing mask", DDSRepairRequest{Operation: "minimum_red", MinimumRed: 1, SelectionPath: "missing.png"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path, raw := diagnosticFixture(t, 28, []byte{0, 2, 3, 4})
			request := tc.request
			request.ExpectedSHA256 = digestDDS(raw)
			request.Apply = true
			if _, err := RepairDDS(context.Background(), path, request); err == nil {
				t.Fatal("invalid repair accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			entries, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, raw) || len(entries) != 1 {
				t.Fatal("failed validation changed files")
			}
		})
	}
	path, raw := diagnosticFixture(t, 29, []byte{6, 2, 3, 4})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RepairDDS(
		ctx,
		path,
		DDSRepairRequest{Operation: "reinterpret_linear", ExpectedSHA256: digestDDS(raw), Apply: true},
	); err == nil {
		t.Fatal("cancellation ignored")
	}
}

func TestDDSBC7RetagPreservesCompressedMipPayload(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	pngPath, path := filepath.Join(dir, "source.png"), filepath.Join(dir, "mask.dds")
	pixels := bytes.Repeat([]byte{6, 128, 64, 200}, 16*16)
	if err := saveTexturePNG(pngPath, pixels, 16, 16); err != nil {
		t.Fatal(err)
	}
	if _, err := encodePNGToDDS(pngPath, path, "DXGI_FORMAT_BC7_UNORM_SRGB", false, true); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	inspection, err := InspectDDS(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Mipmaps <= 1 || inspection.DXGIFormat != 99 {
		t.Fatalf("fixture = %+v", inspection)
	}
	if _, err := RepairDDS(context.Background(), path, DDSRepairRequest{
		Operation: "linearize_mask", ExpectedSHA256: inspection.SHA256, SourceColorSpace: "srgb", Apply: true,
	}); err == nil {
		t.Fatal("pixel edit silently dropped mip chain")
	}
	result, err := RepairDDS(context.Background(), path, DDSRepairRequest{
		Operation: "reinterpret_linear", ExpectedSHA256: inspection.SHA256, Apply: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.ChangedBytes != 1 || !bytes.Equal(before[132:], after[132:]) || after[128] != 98 {
		t.Fatal("BC7 retag modified compressed data or mip metadata")
	}
}

func TestDDSSelectionValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		width int
		pixel color.NRGBA
	}{
		{"wrong size", 2, color.NRGBA{R: 255, G: 255, B: 255, A: 255}},
		{"transparent", 1, color.NRGBA{R: 255, G: 255, B: 255, A: 0}},
		{"gray", 1, color.NRGBA{R: 128, G: 128, B: 128, A: 255}},
		{"colored", 1, color.NRGBA{R: 255, A: 255}},
		{"empty", 1, color.NRGBA{A: 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path, raw := diagnosticFixture(t, 28, []byte{0, 80, 90, 200})
			img := image.NewNRGBA(image.Rect(0, 0, tc.width, 1))
			img.SetNRGBA(0, 0, tc.pixel)
			var encoded bytes.Buffer
			if err := png.Encode(&encoded, img); err != nil {
				t.Fatal(err)
			}
			mask := filepath.Join(filepath.Dir(path), "selection.png")
			if err := os.WriteFile(mask, encoded.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := RepairDDS(context.Background(), path, DDSRepairRequest{
				Operation: "minimum_red", ExpectedSHA256: digestDDS(raw), MinimumRed: 1,
				SelectionPath: mask, SelectionSHA256: digestDDS(encoded.Bytes()), Apply: true,
			})
			if err == nil {
				t.Fatal("invalid mask accepted")
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, raw) {
				t.Fatal("invalid selection mutated source")
			}
		})
	}
}
