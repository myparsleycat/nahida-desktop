package bodyshape

import (
	"context"
	"encoding/binary"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"nahida.live/desktop/internal/platform"
	"nahida.live/desktop/internal/tools/modmesh"
)

type bodyShapeTestDisabler struct {
	called string
}

func (d *bodyShapeTestDisabler) Disable(_ context.Context, path string) (string, error) {
	d.called = path
	target := filepath.Join(filepath.Dir(path), "DISABLED "+filepath.Base(path))
	return target, os.Rename(path, target)
}

func (d *bodyShapeTestDisabler) Enable(_ context.Context, path string) (string, error) {
	return path, nil
}

func writeTestPositions(t *testing.T, path string, positions []float32, stride int) {
	t.Helper()
	raw := make([]byte, len(positions)/3*stride)
	written, err := writeBodyPositions(raw, stride, positions)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, written, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBodyShapedFolderBaseName(t *testing.T) {
	for input, expected := range map[string]string{
		"Astral Modulator":                    "Astral Modulator (Body Shaped)",
		"DISABLED Astral Modulator":           "Astral Modulator (Body Shaped)",
		"DISABLED_Foo":                        "Foo (Body Shaped)",
		"Foo (Body Shaped)":                   "Foo (Body Shaped)",
		"Foo (Body Shaped) (2)":               "Foo (Body Shaped)",
		"DISABLED Foo (body shaped)":          "Foo (Body Shaped)",
		"Foo (Body Shaped) (Body Shaped) (3)": "Foo (Body Shaped)",
		"Foo (2)":                             "Foo (2) (Body Shaped)",
	} {
		if actual := bodyShapedFolderBaseName(input); actual != expected {
			t.Fatalf("bodyShapedFolderBaseName(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestLoadBodyShapeModMatchesBuffersAndBones(t *testing.T) {
	root := t.TempDir()
	meshes := filepath.Join(root, "Meshes")
	if err := os.Mkdir(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1}
	writeTestPositions(t, filepath.Join(meshes, "Position.buf"), positions, 12)
	indexBytes := make([]byte, 6*4)
	for index, value := range []uint32{0, 1, 2, 0, 2, 3} {
		binary.LittleEndian.PutUint32(indexBytes[index*4:], value)
	}
	if err := os.WriteFile(filepath.Join(meshes, "Index.buf"), indexBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	blend := make([]byte, 4*16)
	blend[16], blend[16+8] = 3, 255
	blend[32], blend[32+8] = 9, 128
	if err := os.WriteFile(filepath.Join(meshes, "Blend.buf"), blend, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[ResourcePositionBuffer]\nstride = 12\nfilename = Meshes/Position.buf\n\n" +
		"[ResourceIndexBuffer]\nformat = DXGI_FORMAT_R32_UINT\nfilename = Meshes/Index.buf\n\n" +
		"[ResourceBlendBuffer]\nstride = 16\nfilename = Meshes/Blend.buf\n"
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadBodyShapeMod(root, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Meshes) != 1 {
		t.Fatalf("meshes = %#v", loaded.Meshes)
	}
	mesh := loaded.Meshes[0]
	if mesh.VertexCount != 4 || len(mesh.Positions) != 12 || len(mesh.Indices) != 6 || mesh.BlendStride == nil ||
		*mesh.BlendStride != 16 {
		t.Fatalf("mesh = %#v", mesh)
	}
	if len(mesh.Bones) != 2 || mesh.Bones[0].ID != 3 || mesh.Bones[1].ID != 9 {
		t.Fatalf("bones = %#v", mesh.Bones)
	}
}

func TestBodyShapeSessionDescriptorUsesBinaryMeshTransport(t *testing.T) {
	root := t.TempDir()
	meshes := filepath.Join(root, "Meshes")
	if err := os.Mkdir(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	positions := []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}
	writeTestPositions(t, filepath.Join(meshes, "Position.buf"), positions, 12)
	indices := make([]byte, 12)
	for index, value := range []uint32{0, 1, 2} {
		binary.LittleEndian.PutUint32(indices[index*4:], value)
	}
	if err := os.WriteFile(filepath.Join(meshes, "Index.buf"), indices, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[ResourcePositionBuffer]\nstride = 12\nfilename = Meshes/Position.buf\n\n" +
		"[ResourceIndexBuffer]\nformat = DXGI_FORMAT_R32_UINT\nfilename = Meshes/Index.buf\n"
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	descriptor, err := service.BodyShapeLoadMod(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if len(descriptor.Meshes) != 1 || descriptor.Meshes[0].VertexCount != 3 {
		t.Fatalf("descriptor = %#v", descriptor)
	}
	mesh, err := service.BodyShapeGetMesh(
		context.Background(),
		BodyShapeMeshInput{SessionID: descriptor.SessionID, MeshID: descriptor.Meshes[0].ID},
	)
	if err != nil {
		t.Fatal(err)
	}
	positionResponse := httptest.NewRecorder()
	service.protocol.ServeHTTP(positionResponse, httptest.NewRequest(http.MethodGet, mesh.PositionsURL, nil))
	if positionResponse.Code != http.StatusOK ||
		!strings.EqualFold(positionResponse.Header().Get("Content-Type"), "application/octet-stream") {
		t.Fatalf("position response = %d %v", positionResponse.Code, positionResponse.Header())
	}
	if got, decodeErr := modmesh.DecodeFloat32Bytes(
		positionResponse.Body.Bytes(),
	); decodeErr != nil ||
		len(got) != len(positions) {
		t.Fatalf("positions = %#v, %v", got, decodeErr)
	}
	if _, err = service.BodyShapeCloseSession(context.Background(), descriptor.SessionID); err != nil {
		t.Fatal(err)
	}
	afterClose := httptest.NewRecorder()
	service.protocol.ServeHTTP(afterClose, httptest.NewRequest(http.MethodGet, mesh.PositionsURL, nil))
	if afterClose.Code != http.StatusNotFound {
		t.Fatalf("after close = %d", afterClose.Code)
	}
}

func TestLoadBodyShapeModMatchesNativeEFMIComponents(t *testing.T) {
	root := t.TempDir()
	meshes := filepath.Join(root, "Meshes")
	if err := os.Mkdir(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestPositions(
		t,
		filepath.Join(meshes, "Component0_VB0.buf"),
		[]float32{0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1},
		16,
	)
	indices := make([]byte, 12)
	for index, value := range []uint16{0, 1, 2, 0, 2, 3} {
		binary.LittleEndian.PutUint16(indices[index*2:], value)
	}
	if err := os.WriteFile(filepath.Join(meshes, "Component0_IB.buf"), indices, 0o600); err != nil {
		t.Fatal(err)
	}
	blend := make([]byte, 4*12)
	binary.LittleEndian.PutUint16(blend[12:], 65535)
	blend[20] = 3
	if err := os.WriteFile(filepath.Join(meshes, "Component0_VB2.buf"), blend, 0o600); err != nil {
		t.Fatal(err)
	}
	ini := "[Resource_Component0_IB]\nformat = DXGI_FORMAT_R16_UINT\nfilename = Meshes/Component0_IB.buf\n\n" +
		"[Resource_Component0_VB0]\nstride = 16\nfilename = Meshes/Component0_VB0.buf\n\n" +
		"[Resource_Component0_VB2]\nstride = 12\nfilename = Meshes/Component0_VB2.buf\n"
	if err := os.WriteFile(filepath.Join(root, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadBodyShapeMod(root, func(string) {})
	if err != nil {
		t.Fatal(err)
	}
	mesh := loaded.Meshes[0]
	if mesh.ID != "_Component0_VB0" || len(mesh.Indices) != 6 || mesh.BlendStride == nil || *mesh.BlendStride != 12 ||
		len(mesh.Bones) != 1 ||
		mesh.Bones[0].ID != 3 {
		t.Fatalf("mesh = %#v", mesh)
	}
}

func TestBodyShapeExportCopiesVariantAndDisablesUnmanagedSource(t *testing.T) {
	ctx := context.Background()
	sourceRoot := filepath.Join(t.TempDir(), "Character Mod")
	meshes := filepath.Join(sourceRoot, "Meshes")
	if err := os.MkdirAll(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	original := []float32{0, 0, 0, 1, 0, 0}
	positionPath := filepath.Join(meshes, "Position.buf")
	writeTestPositions(t, positionPath, original, 12)
	if err := os.WriteFile(filepath.Join(sourceRoot, shaderFixMarker), []byte("marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	disabler := &bodyShapeTestDisabler{}
	service := NewWithOptions(Options{Mod: disabler})
	changed := []float32{0, 0, 0, 2, 0, 0}
	result, err := service.BodyShapeExport(ctx, BodyShapeExportInput{
		ModRoot: sourceRoot,
		Meshes:  []BodyShapeExportMesh{{PositionPath: positionPath, PositionStride: 12, Positions: changed}},
		ChangeSummary: &BodyShapeChangeSummary{
			Amount:          .5,
			AxisScale:       []float64{1, 0, 0},
			MovedVertices:   1,
			MaxDisplacement: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ModRoot == nil || filepath.Base(*result.ModRoot) != "Character Mod (Body Shaped)" ||
		result.SourceModPath == nil ||
		disabler.called != sourceRoot {
		t.Fatalf("result = %#v, called = %q", result, disabler.called)
	}
	variantPosition := filepath.Join(*result.ModRoot, "Meshes", "Position.buf")
	variantRaw, err := os.ReadFile(variantPosition)
	if err != nil {
		t.Fatal(err)
	}
	variant, err := modmesh.ExtractPositions(variantRaw, 12)
	if err != nil || len(variant) != len(changed) {
		t.Fatalf("variant = %#v, %v", variant, err)
	}
	for index := range changed {
		if math.Float32bits(variant[index]) != math.Float32bits(changed[index]) {
			t.Fatalf("variant positions = %#v, want %#v", variant, changed)
		}
	}
	if _, err := os.Stat(filepath.Join(*result.ModRoot, shaderFixMarker)); !os.IsNotExist(err) {
		t.Fatalf("shader marker copied to variant: %v", err)
	}
	if result.ChangeLogPath == nil {
		t.Fatal("change log path missing")
	}
	disabledOriginal, err := os.ReadFile(filepath.Join(*result.SourceModPath, "Meshes", "Position.buf"))
	if err != nil {
		t.Fatal(err)
	}
	originalPositions, _ := modmesh.ExtractPositions(disabledOriginal, 12)
	if math.Float32bits(originalPositions[3]) != math.Float32bits(1) {
		t.Fatalf("source was modified: %#v", originalPositions)
	}
}

func TestBodyShapeExportRemovesPartialCopyOnFailure(t *testing.T) {
	parent := t.TempDir()
	sourceRoot := filepath.Join(parent, "Character Mod")
	if err := os.Mkdir(sourceRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	positionPath := filepath.Join(sourceRoot, "Position.buf")
	writeTestPositions(t, positionPath, []float32{0, 0, 0}, 12)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	disabler := &bodyShapeTestDisabler{}
	service := NewWithOptions(Options{Mod: disabler})

	_, err := service.BodyShapeExport(ctx, BodyShapeExportInput{
		ModRoot: sourceRoot,
		Meshes:  []BodyShapeExportMesh{{PositionPath: positionPath, PositionStride: 12, Positions: []float32{1, 0, 0}}},
	})
	if err == nil {
		t.Fatal("export succeeded with a cancelled context")
	}

	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "Character Mod" || disabler.called != "" {
		t.Fatalf("entries = %v, disabled = %q", entries, disabler.called)
	}
}

func loadBodyShapeTestMod(t *testing.T, ini string) (*Service, *bodyShapeTestDisabler, BodyShapeSessionDescriptor) {
	t.Helper()
	sourceRoot := filepath.Join(t.TempDir(), "Character Mod")
	meshes := filepath.Join(sourceRoot, "Meshes")
	if err := os.MkdirAll(meshes, 0o700); err != nil {
		t.Fatal(err)
	}
	writeTestPositions(t, filepath.Join(meshes, "Body.buf"), []float32{0, 0, 0, 1, 0, 0}, 12)
	writeTestPositions(t, filepath.Join(meshes, "Dress.buf"), []float32{0, 1, 0}, 12)
	if err := os.WriteFile(filepath.Join(sourceRoot, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	disabler := &bodyShapeTestDisabler{}
	service := NewWithOptions(Options{Mod: disabler})
	loaded, err := service.BodyShapeLoadMod(context.Background(), sourceRoot)
	if err != nil {
		t.Fatal(err)
	}
	return service, disabler, loaded
}

func TestBodyShapeBinaryUploadCommitExportsAllSelectedPositions(t *testing.T) {
	ctx := context.Background()
	service, disabler, loaded := loadBodyShapeTestMod(t,
		"[ResourceBodyPosition]\nstride = 12\nfilename = Meshes/Body.buf\n\n"+
			"[ResourceDressPosition]\nstride = 12\nfilename = Meshes/Dress.buf\n")
	if len(loaded.Meshes) != 2 {
		t.Fatalf("meshes = %#v", loaded.Meshes)
	}
	upload, err := service.BodyShapeBeginExport(ctx, BodyShapeBeginExportInput{
		SessionID: loaded.SessionID,
		MeshIDs:   []string{loaded.Meshes[0].ID, loaded.Meshes[1].ID},
	})
	if err != nil || len(upload.Meshes) != 2 {
		t.Fatalf("begin = %#v, %v", upload, err)
	}
	changed := map[string][]float32{
		loaded.Meshes[0].ID: {0, 0, 0, 2, 0, 0},
		loaded.Meshes[1].ID: {0, 3, 0},
	}
	for _, mesh := range upload.Meshes {
		request := httptest.NewRequest(
			http.MethodPut,
			mesh.PositionsUploadURL,
			strings.NewReader(string(modmesh.Float32Bytes(changed[mesh.MeshID]))),
		)
		request.Header.Set("Content-Type", "application/octet-stream")
		recorder := httptest.NewRecorder()
		service.protocol.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("upload %s = %d %s", mesh.MeshID, recorder.Code, recorder.Body.String())
		}
	}
	result, err := service.BodyShapeCommitExport(
		ctx,
		BodyShapeCommitExportInput{SessionID: loaded.SessionID, ExportID: upload.ExportID},
	)
	if err != nil || result.ModRoot == nil || len(result.Meshes) != 2 {
		t.Fatalf("commit = %#v, %v", result, err)
	}
	if !platform.SamePathFold(disabler.called, loaded.ModRoot) {
		t.Fatalf("disabled %q, want %q", disabler.called, loaded.ModRoot)
	}
	for file, want := range map[string][]float32{
		"Body.buf":  changed[loaded.Meshes[0].ID],
		"Dress.buf": changed[loaded.Meshes[1].ID],
	} {
		raw, readErr := os.ReadFile(filepath.Join(*result.ModRoot, "Meshes", file))
		if readErr != nil {
			t.Fatal(readErr)
		}
		positions, extractErr := modmesh.ExtractPositions(raw, 12)
		if extractErr != nil || !reflect.DeepEqual(positions, want) {
			t.Fatalf("%s positions = %#v, %v", file, positions, extractErr)
		}
	}
	siblings, err := os.ReadDir(filepath.Dir(*result.ModRoot))
	if err != nil || len(siblings) != 2 {
		t.Fatalf("mod folders = %v, %v", siblings, err)
	}
}

func TestBodyShapeCommitRejectsUnchangedPositions(t *testing.T) {
	ctx := context.Background()
	service, disabler, loaded := loadBodyShapeTestMod(
		t,
		"[ResourceBodyPosition]\nstride = 12\nfilename = Meshes/Body.buf\n",
	)
	upload, err := service.BodyShapeBeginExport(ctx, BodyShapeBeginExportInput{
		SessionID: loaded.SessionID,
		MeshIDs:   []string{loaded.Meshes[0].ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		upload.Meshes[0].PositionsUploadURL,
		strings.NewReader(string(modmesh.Float32Bytes([]float32{0, 0, 0, 1, 0, 0}))),
	)
	request.Header.Set("Content-Type", "application/octet-stream")
	recorder := httptest.NewRecorder()
	service.protocol.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("upload = %d %s", recorder.Code, recorder.Body.String())
	}

	_, err = service.BodyShapeCommitExport(
		ctx,
		BodyShapeCommitExportInput{SessionID: loaded.SessionID, ExportID: upload.ExportID},
	)
	if err == nil {
		t.Fatal("commit accepted unchanged positions")
	}

	entries, err := os.ReadDir(filepath.Dir(loaded.ModRoot))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || disabler.called != "" {
		t.Fatalf("entries = %v, disabled = %q", entries, disabler.called)
	}
}

var (
	frameTestBefore  = []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}
	frameTestRotated = []float32{0, 0, 0, 1, 0, 0, 0, 0, 1}
	frameTestIndices = []uint32{0, 1, 2}
)

func assertFrameVec(t *testing.T, label string, actual, want bodyVec3, tolerance float64) {
	t.Helper()
	for axis := range want {
		if math.Abs(actual[axis]-want[axis]) > tolerance {
			t.Fatalf("%s = %v, want %v", label, actual, want)
		}
	}
}

func TestExportRotatesInlineFrames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Position.buf")
	raw := make([]byte, 3*inlineFrameStride)
	for vertex := range 3 {
		base := vertex * inlineFrameStride
		writeBodyVec3(raw[base:], bodyVec3{
			float64(
				frameTestBefore[vertex*3],
			),
			float64(frameTestBefore[vertex*3+1]),
			float64(frameTestBefore[vertex*3+2]),
		})
		writeBodyVec3(raw[base+inlineNormalOffset:], bodyVec3{0, 0, 1})
		writeBodyVec3(raw[base+inlineTangentOffset:], bodyVec3{1, 0, 0})
		binary.LittleEndian.PutUint32(raw[base+inlineTangentOffset+12:], math.Float32bits(-1))
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := exportBodyShapeMesh(BodyShapeExportMesh{
		PositionPath:   path,
		PositionStride: inlineFrameStride,
		Positions:      frameTestRotated,
		Indices:        frameTestIndices,
	}, func(message string) { t.Fatal(message) })
	if err != nil || !result.InlineFrames {
		t.Fatalf("result = %#v, %v", result, err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for vertex := range 3 {
		base := vertex * inlineFrameStride
		assertFrameVec(t, "normal", readBodyVec3(written[base+inlineNormalOffset:]), bodyVec3{0, -1, 0}, 1e-5)
		assertFrameVec(t, "tangent", readBodyVec3(written[base+inlineTangentOffset:]), bodyVec3{1, 0, 0}, 1e-5)
		handedness := math.Float32frombits(binary.LittleEndian.Uint32(written[base+inlineTangentOffset+12:]))
		if handedness != -1 {
			t.Fatalf("handedness = %v", handedness)
		}
	}
}

func TestExportKeepsNonFrameBytesInStride40(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Position.buf")
	raw := make([]byte, 3*inlineFrameStride)
	for index := inlineNormalOffset; index < inlineFrameStride; index++ {
		for vertex := range 3 {
			raw[vertex*inlineFrameStride+index] = 0x7f
		}
	}
	for vertex := range 3 {
		writeBodyVec3(raw[vertex*inlineFrameStride:], bodyVec3{
			float64(
				frameTestBefore[vertex*3],
			),
			float64(frameTestBefore[vertex*3+1]),
			float64(frameTestBefore[vertex*3+2]),
		})
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	warnings := 0
	result, err := exportBodyShapeMesh(BodyShapeExportMesh{
		PositionPath:   path,
		PositionStride: inlineFrameStride,
		Positions:      frameTestRotated,
		Indices:        frameTestIndices,
	}, func(string) { warnings++ })
	if err != nil || result.InlineFrames || warnings != 1 {
		t.Fatalf("result = %#v, warnings = %d, err = %v", result, warnings, err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for vertex := range 3 {
		base := vertex * inlineFrameStride
		if !reflect.DeepEqual(
			written[base+inlineNormalOffset:base+inlineFrameStride],
			raw[base+inlineNormalOffset:base+inlineFrameStride],
		) {
			t.Fatalf("vertex %d attributes changed", vertex)
		}
	}
}

func TestExportRotatesSnorm8VectorBuffer(t *testing.T) {
	dir := t.TempDir()
	positionPath := filepath.Join(dir, "Position.buf")
	writeTestPositions(t, positionPath, frameTestBefore, 12)
	vectorPath := filepath.Join(dir, "Vector.buf")
	vectors := make([]byte, 3*8)
	for vertex := range 3 {
		base := vertex * 8
		copy(vectors[base:], []byte{127, 0, 0, 0x55, 0, 0, 127, 0x66})
	}
	if err := os.WriteFile(vectorPath, vectors, 0o600); err != nil {
		t.Fatal(err)
	}

	layout := "snorm8-tangent-normal"
	result, err := exportBodyShapeMesh(BodyShapeExportMesh{
		PositionPath:   positionPath,
		PositionStride: 12,
		Positions:      frameTestRotated,
		Indices:        frameTestIndices,
		VectorPath:     &vectorPath,
		VectorLayout:   &layout,
	}, func(message string) { t.Fatal(message) })
	if err != nil || result.VectorPath == nil {
		t.Fatalf("result = %#v, %v", result, err)
	}

	written, err := os.ReadFile(vectorPath)
	if err != nil {
		t.Fatal(err)
	}
	for vertex := range 3 {
		base := vertex * 8
		want := []byte{127, 0, 0, 0x55, 0, 0x81, 0, 0x66}
		if !reflect.DeepEqual(written[base:base+8], want) {
			t.Fatalf("vertex %d vectors = %v, want %v", vertex, written[base:base+8], want)
		}
	}
}

func TestBodyShapeBeginExportRejectsInvalidSelections(t *testing.T) {
	service, _, loaded := loadBodyShapeTestMod(t,
		"[ResourceBodyPosition]\nstride = 12\nfilename = Meshes/Body.buf\n\n"+
			"[ResourceBodyAliasPosition]\nstride = 12\nfilename = Meshes/Body.buf\n")
	if len(loaded.Meshes) != 2 {
		t.Fatalf("meshes = %#v", loaded.Meshes)
	}
	body, alias := loaded.Meshes[0].ID, loaded.Meshes[1].ID
	if loaded.Meshes[0].PositionFileKey != body || loaded.Meshes[1].PositionFileKey != body {
		t.Fatalf("position file keys = %q, %q, want both %q",
			loaded.Meshes[0].PositionFileKey, loaded.Meshes[1].PositionFileKey, body)
	}
	for name, ids := range map[string][]string{
		"empty":     nil,
		"duplicate": {body, body},
		"unknown":   {body, "missing"},
		"same file": {body, alias},
	} {
		upload, err := service.BodyShapeBeginExport(
			context.Background(),
			BodyShapeBeginExportInput{SessionID: loaded.SessionID, MeshIDs: ids},
		)
		if err == nil {
			t.Fatalf("%s: begin = %#v, want error", name, upload)
		}
	}
}

func TestRemapBodyShapePathUsesCanonicalPaths(t *testing.T) {
	base := t.TempDir()
	logicalRoot := filepath.Join(base, "junction", "Character Mod")
	canonicalRoot := filepath.Join(base, "real", "Character Mod")
	positionPath := filepath.Join(canonicalRoot, "Meshes", "Position.buf")
	targetRoot := filepath.Join(base, "Character Mod (Body Shaped)")

	resolve := func(path string) (string, error) {
		if platform.SamePathFold(path, logicalRoot) {
			return canonicalRoot, nil
		}
		return path, nil
	}
	got, err := remapBodyShapePathWithResolver(positionPath, logicalRoot, targetRoot, resolve)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(targetRoot, "Meshes", "Position.buf")
	if !platform.SamePathFold(got, want) {
		t.Fatalf("remapped path = %q, want %q", got, want)
	}
	outside := filepath.Join(base, "outside", "Position.buf")
	if _, err := remapBodyShapePathWithResolver(outside, logicalRoot, targetRoot, resolve); err == nil {
		t.Fatal("canonical path outside the mod root was accepted")
	}
}

func TestExportRotatesWeldedSeamVerticesTogether(t *testing.T) {
	before := []float32{
		0, 0, 0, 1, 0, 0, 0, 1, 0,
		1, 0, 0, 1, 1, 0, 0, 1, 0,
		0, 0, 0, 1, 0, 0, 0, 1, 0,
	}
	after := append([]float32(nil), before...)
	after[13], after[14] = 1, 1
	indices := []uint32{0, 1, 2, 3, 4, 5, 6, 8, 7}
	path := filepath.Join(t.TempDir(), "Position.buf")
	raw := make([]byte, 9*inlineFrameStride)
	for vertex := range 9 {
		base := vertex * inlineFrameStride
		writeBodyVec3(raw[base:], bodyVec3{
			float64(before[vertex*3]),
			float64(before[vertex*3+1]),
			float64(before[vertex*3+2]),
		})
		normal := bodyVec3{0, 0, 1}
		if vertex >= 6 {
			normal = bodyVec3{0, 0, -1}
		}
		writeBodyVec3(raw[base+inlineNormalOffset:], normal)
		writeBodyVec3(raw[base+inlineTangentOffset:], bodyVec3{1, 0, 0})
		binary.LittleEndian.PutUint32(raw[base+inlineTangentOffset+12:], math.Float32bits(1))
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := exportBodyShapeMesh(BodyShapeExportMesh{
		PositionPath:   path,
		PositionStride: inlineFrameStride,
		Positions:      after,
		Indices:        indices,
	}, func(message string) { t.Fatal(message) })
	if err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	normalAt := func(vertex int) bodyVec3 {
		return readBodyVec3(written[vertex*inlineFrameStride+inlineNormalOffset:])
	}

	seam := normalAt(1)
	assertFrameVec(t, "seam duplicate", normalAt(3), seam, 1e-5)
	if seam[2] > 0.99 {
		t.Fatalf("seam normal did not rotate: %v", seam)
	}
	assertFrameVec(t, "flipped shell", normalAt(7), bodyVec3{0, 0, -1}, 1e-5)
}

func TestExportSkipsOnlyInvalidInlineFrames(t *testing.T) {
	const triangles, invalidVertex = 150, 4
	before := make([]float32, 0, triangles*9)
	after := make([]float32, 0, triangles*9)
	indices := make([]uint32, 0, triangles*3)
	for triangle := range triangles {
		for index := range 9 {
			offset := float32(0)
			if index%3 == 0 {
				offset = float32(triangle * 2)
			}
			before = append(before, frameTestBefore[index]+offset)
			after = append(after, frameTestRotated[index]+offset)
		}
		first := uint32(triangle * 3)
		indices = append(indices, first, first+1, first+2)
	}
	path := filepath.Join(t.TempDir(), "Position.buf")
	raw := make([]byte, len(before)/3*inlineFrameStride)
	for vertex := range len(before) / 3 {
		base := vertex * inlineFrameStride
		writeBodyVec3(raw[base:], bodyVec3{
			float64(before[vertex*3]),
			float64(before[vertex*3+1]),
			float64(before[vertex*3+2]),
		})
		normal := bodyVec3{0, 0, 1}
		if vertex == invalidVertex {
			normal = bodyVec3{0, 0, 0.5}
		}
		writeBodyVec3(raw[base+inlineNormalOffset:], normal)
		writeBodyVec3(raw[base+inlineTangentOffset:], bodyVec3{1, 0, 0})
		binary.LittleEndian.PutUint32(raw[base+inlineTangentOffset+12:], math.Float32bits(1))
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := exportBodyShapeMesh(BodyShapeExportMesh{
		PositionPath:   path,
		PositionStride: inlineFrameStride,
		Positions:      after,
		Indices:        indices,
	}, func(string) {})
	if err != nil || !result.InlineFrames {
		t.Fatalf("result = %#v, %v", result, err)
	}
	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	normalAt := func(vertex int) bodyVec3 {
		return readBodyVec3(written[vertex*inlineFrameStride+inlineNormalOffset:])
	}
	assertFrameVec(t, "valid frame", normalAt(invalidVertex+1), bodyVec3{0, -1, 0}, 1e-5)
	assertFrameVec(t, "invalid frame", normalAt(invalidVertex), bodyVec3{0, 0, 0.5}, 1e-5)
}

func TestBodyNormalClustersWeldAcrossGridCells(t *testing.T) {
	positions := []float32{0, 0, 0, 2.9e-5, 0, 0, 3.1e-5, 0, 0, 1e-2, 0, 0, 1, 0, 0}
	normals := make([]bodyVec3, 5)
	for vertex := range normals {
		normals[vertex] = bodyVec3{0, 0, 1}
	}

	clusters := bodyNormalClusters(positions, normals)
	if clusters[2] != clusters[1] {
		t.Fatalf("clusters = %v, duplicates across a cell boundary stayed apart", clusters)
	}
	if clusters[3] != 3 || clusters[4] != 4 {
		t.Fatalf("clusters = %v, distinct vertices were welded", clusters)
	}
}
