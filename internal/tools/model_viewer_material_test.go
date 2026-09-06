package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/myparsleycat/ddsutil"
)

func TestModelViewerTextureBindingAndPreparation(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
ps-t0 = ResourceBodyDiffuse

[ResourceBodyDiffuse]
filename = body.png`)
	bindings := collectModelViewerTextureBindings(sections, nil)
	if len(bindings) != 1 || bindings[0].IBResourceName != "BodyIB" || bindings[0].DiffuseResourceName != "BodyDiffuse" {
		t.Fatalf("bindings = %#v", bindings)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "body.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	input := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	input.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	input.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 0})
	if err = png.Encode(file, input); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareModelViewerTexture(context.Background(), path, "BodyDiffuse", "jpeg-safe", 85)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.mimeType != "image/png" || prepared.alphaMode != "MASK" || len(prepared.bytes) == 0 {
		t.Fatalf("prepared = %#v", prepared)
	}
}

func TestModelViewerRabbitFXTextureBindingPreservesSemanticRoles(t *testing.T) {
	sections := parseModINI(`[TextureOverrideBody]
ib = ResourceBodyIB
Resource\RabbitFX\Diffuse = ref ResourceTexture15
Resource\RabbitFX\NormalMap = ref ResourceTexture17
Resource\RabbitFX\LightMap = ref ResourceTexture16
Resource\RabbitFX\MaterialMap = ref ResourceTexture18

[ResourceTexture15]
filename = diffuse.dds

[ResourceTexture16]
filename = light.dds

[ResourceTexture17]
filename = normal.dds

[ResourceTexture18]
filename = material.dds`)
	bindings := collectModelViewerTextureBindings(sections, nil)
	if len(bindings) != 1 {
		t.Fatalf("bindings = %#v", bindings)
	}
	binding := bindings[0]
	if binding.DiffuseResourceName != "Texture15" {
		t.Fatalf("diffuse = %q, want Texture15", binding.DiffuseResourceName)
	}
	want := map[string]string{
		"texture15": "diffuse",
		"texture16": "light_map",
		"texture17": "normal_map",
		"texture18": "material_map",
	}
	for key, role := range want {
		if binding.TextureRoles[key] != role {
			t.Fatalf("role[%q] = %q, want %q; roles=%#v", key, binding.TextureRoles[key], role, binding.TextureRoles)
		}
	}
}

func TestDetectModelViewerMaterialProfile(t *testing.T) {
	tests := []struct {
		name, ini, want string
	}{
		{
			name: "WWMI and RabbitFX",
			ini: `[Constants]
global $required_wwmi_version = 0.70
[CommandListTextures]
Resource\RabbitFX\NormalMap = ref ResourceTexture17
run = CommandList\RabbitFX\SetTextures`,
			want: "wuwa:rabbitfx",
		},
		{
			name: "case insensitive WWMI marker",
			ini: `[CommandListTextures]
$\wwmiv1\shapekey_id = 3
resource\rabbitfx\diffuse = ref ResourceTexture15`,
			want: "wuwa:rabbitfx",
		},
		{name: "RabbitFX without WWMI", ini: `[CommandListTextures]
Resource\RabbitFX\Diffuse = ref ResourceTexture15`, want: ""},
		{name: "existing ZZMI", ini: `[CommandListTextures]
Resource\ZZMI\Diffuse = ref ResourceTexture15`, want: "zzmi"},
		{
			name: "strong WWMI RabbitFX wins stray ZZMI resource",
			ini: `[Constants]
global $required_wwmi_version = 0.70
[CommandListTextures]
Resource\RabbitFX\Diffuse = ref ResourceTexture15
Resource\ZZMI\NormalMap = ref ResourceUnused`,
			want: "wuwa:rabbitfx",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := detectModelViewerMaterialProfile(parseModINI(test.ini)); got != test.want {
				t.Fatalf("profile = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPrepareModelViewerTextureDecodesUncompressedDDS(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.dds")
	if err := os.WriteFile(path, encodeUncompressedDDS(color.NRGBA{R: 12, G: 34, B: 56, A: 255}), 0o600); err != nil {
		t.Fatal(err)
	}
	prepared, err := prepareModelViewerTexture(context.Background(), path, "BodyDiffuse", "jpeg-safe", 85)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.mimeType != "image/jpeg" || len(prepared.bytes) == 0 {
		t.Fatalf("prepared = %#v", prepared)
	}
}

func encodeUncompressedDDS(pixel color.NRGBA) []byte {
	header := make([]byte, 128)
	copy(header[:4], "DDS ")
	binary.LittleEndian.PutUint32(header[4:8], 124)
	binary.LittleEndian.PutUint32(header[8:12], 0x100f)
	binary.LittleEndian.PutUint32(header[12:16], 1)
	binary.LittleEndian.PutUint32(header[16:20], 1)
	binary.LittleEndian.PutUint32(header[20:24], 4)
	binary.LittleEndian.PutUint32(header[76:80], 32)
	binary.LittleEndian.PutUint32(header[80:84], 0x41)
	binary.LittleEndian.PutUint32(header[88:92], 32)
	binary.LittleEndian.PutUint32(header[92:96], 0x00ff0000)
	binary.LittleEndian.PutUint32(header[96:100], 0x0000ff00)
	binary.LittleEndian.PutUint32(header[100:104], 0x000000ff)
	binary.LittleEndian.PutUint32(header[104:108], 0xff000000)
	binary.LittleEndian.PutUint32(header[108:112], 0x1000)
	return append(header, pixel.B, pixel.G, pixel.R, pixel.A)
}

func TestModelViewerTextureRejectsDeclaredOversizedPNG(t *testing.T) {
	raw := make([]byte, 24)
	copy(raw, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	copy(raw[12:16], "IHDR")
	binary.BigEndian.PutUint32(raw[16:20], 100_000)
	binary.BigEndian.PutUint32(raw[20:24], 100_000)
	path := filepath.Join(t.TempDir(), "oversized.png")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prepareModelViewerTexture(context.Background(), path, "Diffuse", "png", 85)
	if err == nil || !strings.Contains(err.Error(), "input safety limit") {
		t.Fatalf("err = %v", err)
	}
}

func TestViewerPreviewTextureSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width, height, wantW, wantH int
	}{
		{8192, 8192, 2048, 2048},
		{2048, 8192, 1024, 4096},
		{4096, 4096, 2048, 2048},
		{2048, 2048, 2048, 2048},
	}
	for _, test := range cases {
		gotW, gotH := viewerPreviewTextureSize(test.width, test.height)
		if gotW != test.wantW || gotH != test.wantH {
			t.Fatalf("viewerPreviewTextureSize(%d, %d) = %d×%d, want %d×%d",
				test.width, test.height, gotW, gotH, test.wantW, test.wantH)
		}
	}
}

func TestModelViewerPreviewMipmap(t *testing.T) {
	t.Parallel()
	cases := []struct {
		width, height uint32
		mipmaps       uint32
		wantMip       uint32
		wantW, wantH  int
	}{
		{8192, 8192, 14, 2, 2048, 2048},
		{2048, 8192, 14, 1, 1024, 4096},
		{8192, 8192, 1, 0, 2048, 2048},
		{8192, 8192, 2, 1, 2048, 2048},
		{4096, 4096, 2, 1, 2048, 2048},
		{2048, 2048, 1, 0, 2048, 2048},
	}
	for _, test := range cases {
		mip, width, height := modelViewerPreviewMipmap(test.width, test.height, test.mipmaps)
		if mip != test.wantMip || width != test.wantW || height != test.wantH {
			t.Fatalf("modelViewerPreviewMipmap(%d, %d, %d) = (%d, %d, %d), want (%d, %d, %d)",
				test.width, test.height, test.mipmaps, mip, width, height, test.wantMip, test.wantW, test.wantH)
		}
	}
}

func TestModelViewerTextureConcurrencyRemainsEight(t *testing.T) {
	if modelViewerTextureConcurrency != 8 {
		t.Fatalf("modelViewerTextureConcurrency = %d, want 8", modelViewerTextureConcurrency)
	}
}

func TestReconstructModelViewerNormalZ(t *testing.T) {
	input := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	input.SetNRGBA(0, 0, color.NRGBA{R: 128, G: 128, B: 17, A: 77})
	input.SetNRGBA(1, 0, color.NRGBA{R: 255, G: 128, B: 231, A: 19})

	if err := reconstructModelViewerNormalZ(context.Background(), input); err != nil {
		t.Fatal(err)
	}

	center := input.NRGBAAt(0, 0)
	if center != (color.NRGBA{R: 128, G: 128, B: 255, A: 77}) {
		t.Fatalf("center pixel = %#v, want reconstructed Z with R/G/A preserved", center)
	}
	clamped := input.NRGBAAt(1, 0)
	if clamped != (color.NRGBA{R: 255, G: 128, B: 128, A: 19}) {
		t.Fatalf("clamped pixel = %#v, want zero Z with R/G/A preserved", clamped)
	}
}

func TestModelViewerTextureTransformForProfileAndRole(t *testing.T) {
	tests := []struct {
		name, profile, role string
		want                modelViewerTextureTransform
	}{
		{name: "zzmi normal", profile: "zzmi", role: "normal_map", want: modelViewerTextureTransformNormalXYReconstruct},
		{name: "zzmi diffuse", profile: "zzmi", role: "diffuse", want: modelViewerTextureTransformPassthrough},
		{name: "zzmi light map", profile: "zzmi", role: "light_map", want: modelViewerTextureTransformPassthrough},
		{name: "zzmi material map", profile: "zzmi", role: "material_map", want: modelViewerTextureTransformPassthrough},
		{name: "RabbitFX normal", profile: "wuwa:rabbitfx", role: "normal_map", want: modelViewerTextureTransformNormalXYReconstruct},
		{name: "RabbitFX light map", profile: "wuwa:rabbitfx", role: "light_map", want: modelViewerTextureTransformPassthrough},
		{name: "gimi normal", profile: "gimi", role: "normal_map", want: modelViewerTextureTransformPassthrough},
		{name: "unprofiled normal", role: "normal_map", want: modelViewerTextureTransformPassthrough},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := modelViewerTextureTransformFor(test.profile, test.role); got != test.want {
				t.Fatalf("modelViewerTextureTransformFor(%q, %q) = %q, want %q", test.profile, test.role, got, test.want)
			}
		})
	}
}

func TestModelViewerTextureFormatForProfileAndRole(t *testing.T) {
	tests := []struct {
		profile, role, requested, want string
	}{
		{profile: "wuwa:rabbitfx", role: "normal_map", requested: "jpeg-safe", want: "png"},
		{profile: "wuwa:rabbitfx", role: "light_map", requested: "jpeg-force", want: "png"},
		{profile: "wuwa:rabbitfx", role: "material_map", requested: "jpeg-safe", want: "png"},
		{profile: "wuwa:rabbitfx", role: "diffuse", requested: "jpeg-safe", want: "jpeg-safe"},
		{profile: "zzmi", role: "light_map", requested: "jpeg-safe", want: "jpeg-safe"},
	}
	for _, test := range tests {
		if got := modelViewerTextureFormatFor(test.profile, test.role, test.requested); got != test.want {
			t.Errorf("formatFor(%q, %q, %q) = %q, want %q", test.profile, test.role, test.requested, got, test.want)
		}
	}
}

func writeModelViewerTestPNG(t *testing.T, path string, pixel color.NRGBA) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	input := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	input.SetNRGBA(0, 0, pixel)
	input.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 0})
	if err = png.Encode(file, input); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRunModelViewerTextureJobsDeduplicatesIdenticalFilesAcrossBatches(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "body.png")
	second := filepath.Join(dir, "body-copy.png")
	writeModelViewerTestPNG(t, first, color.NRGBA{R: 255, A: 255})
	if err := os.WriteFile(second, mustReadFile(t, first), 0o600); err != nil {
		t.Fatal(err)
	}
	output, stats, err := runModelViewerTextureJobs(context.Background(), modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85}, 2, []modelViewerTextureJob{
		{batchIndex: 0, path: first, resourceName: "BodyDiffuse", keys: []string{"body"}, role: "diffuse", canonicalKey: "body"},
		{batchIndex: 1, path: second, resourceName: "BodyDiffuseCopy", keys: []string{"body-copy"}, role: "diffuse", canonicalKey: "body-copy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(output) != 2 || !bytes.Equal(output[0]["body"].Bytes, output[1]["body-copy"].Bytes) || len(output[0]["body"].Bytes) == 0 {
		t.Fatalf("deduped outputs = %#v", output)
	}
	if stats.Jobs != 2 || stats.UniquePaths != 2 || stats.UniqueContents != 1 || stats.Decodes != 1 || stats.Encodes != 1 || stats.LogicalTextures != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if stats.HashBytes != int64(len(mustReadFile(t, first))*2) {
		t.Fatalf("hash bytes = %d", stats.HashBytes)
	}
}

func TestRunModelViewerTextureJobsHashesSamePathOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "body.png")
	writeModelViewerTestPNG(t, path, color.NRGBA{R: 255, A: 255})
	output, stats, err := runModelViewerTextureJobs(context.Background(), modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85}, 1, []modelViewerTextureJob{
		{path: path, resourceName: "BodyDiffuse", keys: []string{"body"}, role: "diffuse", canonicalKey: "body"},
		{path: path, resourceName: "BodyDiffuseCopy", keys: []string{"body-copy"}, role: "diffuse", canonicalKey: "body-copy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.UniquePaths != 1 || stats.UniqueContents != 1 || stats.Decodes != 1 || stats.Encodes != 1 || stats.LogicalTextures != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if !bytes.Equal(output[0]["body"].Bytes, output[0]["body-copy"].Bytes) || len(output[0]["body"].Bytes) == 0 {
		t.Fatalf("deduped payloads diverged: %#v", output)
	}
	if stats.HashBytes != int64(len(mustReadFile(t, path))) {
		t.Fatalf("hash bytes = %d", stats.HashBytes)
	}
}

func TestRunModelViewerTextureJobsKeepsInvertAlphaVariant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mask.png")
	writeModelViewerTestPNG(t, path, color.NRGBA{R: 255, A: 255})
	output, stats, err := runModelViewerTextureJobs(context.Background(), modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85}, 1, []modelViewerTextureJob{
		{path: path, resourceName: "BodyDiffuse", keys: []string{"body"}, role: "diffuse", canonicalKey: "body"},
		{path: path, resourceName: "BodyDiffuseInvertAlpha", keys: []string{"body-invert"}, role: "diffuse", canonicalKey: "body-invert"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Decodes != 1 || stats.Encodes != 2 || stats.LogicalTextures != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if bytes.Equal(output[0]["body"].Bytes, output[0]["body-invert"].Bytes) || len(output[0]["body"].Bytes) == 0 {
		t.Fatalf("invert-alpha variant reused the uninverted payload: %#v", output)
	}
}

func TestRunModelViewerTextureJobsKeepsZZMINormalTransformVariant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packed-normal.png")
	writeModelViewerTestPNG(t, path, color.NRGBA{R: 128, G: 128, B: 17, A: 255})
	settings := modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85, MaterialProfile: "zzmi"}
	output, stats, err := runModelViewerTextureJobs(context.Background(), settings, 1, []modelViewerTextureJob{
		{path: path, resourceName: "BodyNormalMap", keys: []string{"normal"}, role: "normal_map", canonicalKey: "normal"},
		{path: path, resourceName: "BodyDiffuse", keys: []string{"diffuse"}, role: "diffuse", canonicalKey: "diffuse"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Decodes != 1 || stats.Encodes != 2 || stats.LogicalTextures != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	diffuse := output[0]["diffuse"]
	normal := output[0]["normal"]
	if len(diffuse.Bytes) == 0 || bytes.Equal(diffuse.Bytes, normal.Bytes) {
		t.Fatalf("ZZMI normal transform reused passthrough payload: %#v", output)
	}
	diffusePixel := decodeModelViewerTestPNGPixel(t, diffuse.Bytes, 0, 0)
	if diffusePixel != (color.NRGBA{R: 128, G: 128, B: 17, A: 255}) {
		t.Fatalf("diffuse pixel = %#v, want passthrough source", diffusePixel)
	}
	normalPixel := decodeModelViewerTestPNGPixel(t, normal.Bytes, 0, 0)
	if normalPixel != (color.NRGBA{R: 128, G: 128, B: 255, A: 255}) {
		t.Fatalf("normal pixel = %#v, want reconstructed Z", normalPixel)
	}
}

func TestRunModelViewerTextureJobsUsesLosslessRabbitFXPackedMaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "packed.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	input := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	input.SetNRGBA(0, 0, color.NRGBA{R: 128, G: 128, B: 17, A: 255})
	if err = png.Encode(file, input); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}

	settings := modelViewerTextureSettings{TextureFormat: "jpeg-force", JPEGQuality: 85, MaterialProfile: "wuwa:rabbitfx"}
	output, stats, err := runModelViewerTextureJobs(context.Background(), settings, 1, []modelViewerTextureJob{
		{path: path, resourceName: "Texture15", keys: []string{"diffuse"}, role: "diffuse", canonicalKey: "diffuse"},
		{path: path, resourceName: "Texture17", keys: []string{"normal"}, role: "normal_map", canonicalKey: "normal"},
		{path: path, resourceName: "Texture16", keys: []string{"light"}, role: "light_map", canonicalKey: "light"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Decodes != 1 || stats.Encodes != 3 || stats.LogicalTextures != 3 {
		t.Fatalf("stats = %#v", stats)
	}
	if output[0]["diffuse"].MIMEType != "image/jpeg" || output[0]["normal"].MIMEType != "image/png" || output[0]["light"].MIMEType != "image/png" {
		t.Fatalf("payload formats = diffuse:%q normal:%q light:%q", output[0]["diffuse"].MIMEType, output[0]["normal"].MIMEType, output[0]["light"].MIMEType)
	}
	if pixel := decodeModelViewerTestPNGPixel(t, output[0]["normal"].Bytes, 0, 0); pixel != (color.NRGBA{R: 128, G: 128, B: 255, A: 255}) {
		t.Fatalf("normal pixel = %#v", pixel)
	}
}

func TestRunModelViewerTextureJobsDeduplicatesZZMINormalTransform(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packed-normal.png")
	writeModelViewerTestPNG(t, path, color.NRGBA{R: 128, G: 128, B: 17, A: 255})
	settings := modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85, MaterialProfile: "zzmi"}
	output, stats, err := runModelViewerTextureJobs(context.Background(), settings, 1, []modelViewerTextureJob{
		{path: path, resourceName: "BodyNormalMap", keys: []string{"normal"}, role: "normal_map", canonicalKey: "normal"},
		{path: path, resourceName: "BodyNormalMapCopy", keys: []string{"normal-copy"}, role: "normal_map", canonicalKey: "normal-copy"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.Decodes != 1 || stats.Encodes != 1 || stats.LogicalTextures != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if len(output[0]["normal"].Bytes) == 0 || !bytes.Equal(output[0]["normal"].Bytes, output[0]["normal-copy"].Bytes) {
		t.Fatalf("reconstructed normal payloads were not shared: %#v", output)
	}
	if pixel := decodeModelViewerTestPNGPixel(t, output[0]["normal"].Bytes, 0, 0); pixel.B != 255 {
		t.Fatalf("normal pixel = %#v, want reconstructed B=255", pixel)
	}
}

func TestRunModelViewerTextureJobsScopesZZMINormalTransform(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "texture.png")
	writeModelViewerTestPNG(t, path, color.NRGBA{R: 128, G: 128, B: 17, A: 255})
	tests := []struct {
		name     string
		profile  string
		roles    []string
		encodes  int
		wantBlue uint8
	}{
		{name: "zzmi non-normal roles", profile: "zzmi", roles: []string{"diffuse", "light_map", "material_map"}, encodes: 3, wantBlue: 17},
		{name: "non-zzmi normal", profile: "gimi", roles: []string{"diffuse", "normal_map"}, encodes: 2, wantBlue: 17},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			jobs := make([]modelViewerTextureJob, 0, len(test.roles))
			for _, role := range test.roles {
				jobs = append(jobs, modelViewerTextureJob{path: path, resourceName: role, keys: []string{role}, role: role, canonicalKey: role})
			}
			settings := modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85, MaterialProfile: test.profile}
			output, stats, err := runModelViewerTextureJobs(context.Background(), settings, 1, jobs)
			if err != nil {
				t.Fatal(err)
			}
			if stats.Decodes != 1 || stats.Encodes != test.encodes || stats.LogicalTextures != len(test.roles) {
				t.Fatalf("stats = %#v", stats)
			}
			for _, role := range test.roles {
				if pixel := decodeModelViewerTestPNGPixel(t, output[0][role].Bytes, 0, 0); pixel.B != test.wantBlue {
					t.Fatalf("%s pixel = %#v, want passthrough B=%d", role, pixel, test.wantBlue)
				}
			}
		})
	}
}

func TestRunModelViewerTextureJobsKeepsDistinctContentsAndBatchScopes(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeModelViewerTestPNG(t, first, color.NRGBA{R: 255, A: 255})
	writeModelViewerTestPNG(t, second, color.NRGBA{B: 255, A: 255})
	output, stats, err := runModelViewerTextureJobs(context.Background(), modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85}, 2, []modelViewerTextureJob{
		{batchIndex: 0, path: first, resourceName: "SharedResource", keys: []string{"shared"}, role: "diffuse", canonicalKey: "first"},
		{batchIndex: 1, path: second, resourceName: "SharedResource", keys: []string{"shared"}, role: "diffuse", canonicalKey: "second"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stats.UniqueContents != 2 || stats.Decodes != 2 || stats.Encodes != 2 || stats.LogicalTextures != 2 {
		t.Fatalf("stats = %#v", stats)
	}
	if bytes.Equal(output[0]["shared"].Bytes, output[1]["shared"].Bytes) || output[0]["shared"].Key != "first" || output[1]["shared"].Key != "second" {
		t.Fatalf("batch outputs = %#v", output)
	}
}

func TestRunModelViewerTextureJobsIsolatesFailedContent(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.png")
	writeModelViewerTestPNG(t, good, color.NRGBA{R: 255, A: 255})
	missing := filepath.Join(dir, "missing.png")
	output, stats, err := runModelViewerTextureJobs(context.Background(), modelViewerTextureSettings{TextureFormat: "png", JPEGQuality: 85}, 1, []modelViewerTextureJob{
		{path: missing, resourceName: "Missing", keys: []string{"missing"}, role: "diffuse", canonicalKey: "missing"},
		{path: good, resourceName: "Good", keys: []string{"good"}, role: "diffuse", canonicalKey: "good"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := output[0]["missing"]; ok {
		t.Fatalf("missing texture was prepared: %#v", output)
	}
	if len(output[0]["good"].Bytes) == 0 || stats.UniqueContents != 2 || stats.Decodes != 2 || stats.Encodes != 1 || stats.LogicalTextures != 1 {
		t.Fatalf("output = %#v stats = %#v", output, stats)
	}
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodeModelViewerTestPNGPixel(t *testing.T, raw []byte, x, y int) color.NRGBA {
	t.Helper()
	decoded, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
}

func TestDecodeModelViewerDDSFileUsesEmbeddedPreviewMip(t *testing.T) {
	raw := encodeModelViewerBC1DDS(t, 4096, 4096, 2, 1)
	path := filepath.Join(t.TempDir(), "embedded_mip.dds")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeModelViewerDDSFile(path, int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 2048 || decoded.Bounds().Dy() != 2048 {
		t.Fatalf("decoded bounds = %v", decoded.Bounds())
	}
	if decoded.Pix[0] < 248 || decoded.Pix[1] < 248 || decoded.Pix[2] < 248 || decoded.Pix[3] != 255 {
		t.Fatalf("decoded first pixel = %#v; expected white data from mip 1", decoded.Pix[:4])
	}
}

func TestDecodeModelViewerDDSFileStreamsLastAvailableMip(t *testing.T) {
	raw := encodeModelViewerBC1DDS(t, 4096, 4096, 1, 0)
	path := filepath.Join(t.TempDir(), "single_mip.dds")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeModelViewerDDSFile(path, int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Bounds().Dx() != 2048 || decoded.Bounds().Dy() != 2048 {
		t.Fatalf("decoded bounds = %v", decoded.Bounds())
	}
	if decoded.Pix[0] < 248 || decoded.Pix[1] < 248 || decoded.Pix[2] < 248 || decoded.Pix[3] != 255 {
		t.Fatalf("decoded first pixel = %#v", decoded.Pix[:4])
	}
}

func TestDecodeModelViewerDDSFileDoesNotFallbackFromTruncatedSelectedMip(t *testing.T) {
	raw := encodeModelViewerBC1DDS(t, 4096, 4096, 2, 1)
	raw = raw[:len(raw)-1]
	path := filepath.Join(t.TempDir(), "truncated_selected_mip.dds")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := decodeModelViewerDDSFile(path, int64(len(raw))); err == nil {
		t.Fatal("truncated selected mip unexpectedly decoded or fell back to mip 0")
	}
}

func encodeModelViewerBC1DDS(t *testing.T, width, height, mipmaps, whiteFromMip uint32) []byte {
	t.Helper()
	dds, err := ddsutil.NewDXGI(ddsutil.NewDxgiParams{
		Height:            height,
		Width:             width,
		Format:            ddsutil.DxgiFormatBC1_UNorm,
		MipmapLevels:      &mipmaps,
		ResourceDimension: ddsutil.D3D10ResourceDimensionTexture2D,
		AlphaMode:         ddsutil.AlphaModeStraight,
	})
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 0)
	for mipmap := range mipmaps {
		mipWidth := ddsutil.MipDimension(width, mipmap)
		mipHeight := ddsutil.MipDimension(height, mipmap)
		length := int(((mipWidth + 3) / 4) * ((mipHeight + 3) / 4) * 8)
		start := len(data)
		data = append(data, make([]byte, length)...)
		if mipmap >= whiteFromMip {
			for offset := start; offset < len(data); offset += 8 {
				data[offset], data[offset+1] = 0xff, 0xff
			}
		}
	}
	dds.Data = data
	var output bytes.Buffer
	if err = dds.Write(&output); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func TestModelViewerTextureDownscalesToPreviewBudget(t *testing.T) {
	input := image.NewNRGBA(image.Rect(0, 0, 3000, 2000))
	output := downscaleModelViewerTexture(input, maxModelViewerTextureOutputPixels)
	if int64(output.Bounds().Dx())*int64(output.Bounds().Dy()) > maxModelViewerTextureOutputPixels || output.Bounds().Dx() >= input.Bounds().Dx() {
		t.Fatalf("input=%v output=%v", input.Bounds(), output.Bounds())
	}
}
