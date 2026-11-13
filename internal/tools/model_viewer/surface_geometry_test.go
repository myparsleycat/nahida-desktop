package modelviewer

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"nahida.live/desktop/internal/infra"
)

func TestSurfaceGeometryRejectsChangesAfterDecode(t *testing.T) {
	for _, name := range []string{"pos.buf", "body.ib", "second.buf"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeViewerGeometry(t, root)
			ini := "[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n" +
				"drawindexed = 3, 0, 0\n" + viewerBodyResources
			if name == "second.buf" {
				writeTextureFile(t, root, name, mustReadFile(t, filepath.Join(root, "pos.buf")))
				writeTextureFile(t, root, "tc.buf", make([]byte, 6*20))
				ini = "[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePrevious\nvb1 = ResourceTc\n" +
					"drawindexed = 3, 0, 3\n" + viewerBodyResources + viewerStreamReplayINI
			}
			iniPath := filepath.Join(root, "mod.ini")
			writeTextureFile(t, root, "mod.ini", []byte(ini))
			ctx := context.Background()
			service := NewWithOptions(Options{Log: infra.NewLogWithOptions(infra.LogOptions{DisableFile: true})})
			budget, err := newModelViewerLoadBudget(root)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := service.prepareModelViewerGeometry(ctx, root, []string{iniPath}, budget)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(prepared.cache.releaseAll)
			prepared.cache.releaseGeometryScratch()
			transport := ModelViewerTransport{Textures: make(map[string]ModelViewerTextureTransport)}
			payloads, _, _, err := prepareModelViewerPayload(ctx, prepared, &transport, &infra.DiagnosticBatch{})
			if err != nil {
				t.Fatal(err)
			}
			prepared.cache.releaseAll()
			if len(transport.Meshes) != 1 {
				t.Fatalf("meshes = %d, want 1", len(transport.Meshes))
			}

			path := filepath.Join(root, name)
			original := mustReadFile(t, path)
			changed := slices.Clone(original)
			changed[0] ^= 1
			writeTextureFile(t, root, name, changed)
			if err = prepareSurfaceSnapshots(ctx, prepared.drawSources, transport.Meshes, payloads, nil); err != nil {
				t.Fatal(err)
			}
			service.modelViewerSessions["session"] = &modelViewerSession{drawSources: prepared.drawSources}
			meshID := transport.Meshes[0].ID
			if _, err = service.SurfaceGeometry(ctx, "session", meshID); err == nil {
				t.Error("accepted source changed between geometry decoding and snapshot preparation")
			}
			writeTextureFile(t, root, name, original)
			geometry, err := service.SurfaceGeometry(ctx, "session", meshID)
			if err != nil || !slices.Equal(geometry.Positions, payloads[0].Positions) {
				t.Fatalf("original surface positions = %v, err = %v", geometry.Positions, err)
			}
		})
	}
}

func TestLoadModGridPreviewOmitsSurfaceSnapshot(t *testing.T) {
	root := t.TempDir()
	fixture := loadViewerFixture(t, root, "[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\n"+
		"vb1 = ResourceTc\ndrawindexed = 3, 0, 0\n"+viewerBodyResources)
	preview, err := fixture.service.LoadModGridPreview(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = fixture.service.CleanupModelViewer(context.Background(), preview.MemorySessionID) })
	if len(preview.Meshes) != 1 {
		t.Fatalf("preview meshes = %d, want 1", len(preview.Meshes))
	}
	if _, err = fixture.service.SurfaceGeometry(
		context.Background(), preview.MemorySessionID, preview.Meshes[0].ID,
	); err == nil {
		t.Fatal("grid preview retained a surface snapshot")
	}
	if _, err = fixture.service.SurfaceGeometry(
		context.Background(), fixture.result.MemorySessionID, fixture.result.Meshes[0].ID,
	); err != nil {
		t.Fatalf("full viewer surface unavailable: %v", err)
	}
}

func TestSurfaceGeometryUsesOriginalRecoveredDrawAndChecksSource(t *testing.T) {
	for _, format := range []string{"DXGI_FORMAT_R16_UINT", "DXGI_FORMAT_R32_UINT"} {
		t.Run(format, func(t *testing.T) {
			root := t.TempDir()
			ib := filepath.Join(root, "body.ib")
			position := filepath.Join(root, "position.buf")
			width := 4
			if format == "DXGI_FORMAT_R16_UINT" {
				width = 2
			}
			indices := []uint32{99, 99, 99, 0, 1, 2, 1, 2, 3}
			raw := make([]byte, len(indices)*width)
			for i, v := range indices {
				if width == 2 {
					binary.LittleEndian.PutUint16(raw[i*width:], uint16(v))
				} else {
					binary.LittleEndian.PutUint32(raw[i*width:], v)
				}
			}
			if err := os.WriteFile(ib, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(position, []byte("snapshot"), 0o600); err != nil {
				t.Fatal(err)
			}
			source := DrawSource{
				INIPath:      filepath.Join(root, "mod.ini"),
				IBPath:       ib,
				IBFormat:     format,
				StartIndex:   3,
				IndexCount:   6,
				BaseVertex:   2,
				Reversed:     true,
				vertexBuffer: "position.buf",
				sourceFaces:  []uint32{1},
				surfaceFiles: map[string][32]byte{
					ib: sha256.Sum256(raw), position: sha256.Sum256([]byte("snapshot")),
				},
			}
			sources := map[string]DrawSource{"mesh": source}
			meshes := []ModelViewerMeshTransport{{ID: "mesh"}}
			payload := []modelViewerMeshPayload{
				{Positions: []float32{0, 0, 0, 1, 0, 0, 0, 1, 0}, SourceIndices: []uint32{3, 4, 5}},
			}
			ctx := context.Background()
			spool, err := newModelViewerSpool(root, "fixture")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = spool.close() })
			if err = prepareSurfaceSnapshots(ctx, sources, meshes, payload, spool); err != nil {
				t.Fatal(err)
			}
			service := New()
			service.modelViewerSessions["session"] = &modelViewerSession{drawSources: sources}
			geometry, err := service.SurfaceGeometry(ctx, "session", "mesh")
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(geometry.Indices, []uint32{1, 2, 3}) ||
				!slices.Equal(geometry.VertexIDs, []uint32{3, 4, 5}) ||
				geometry.Draw.SourceVertexOffset() != 2 {
				t.Fatalf("incorrect original geometry: %+v", geometry)
			}
			if err = os.WriteFile(position, []byte("modified"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err = service.SurfaceGeometry(ctx, "session", "mesh"); err == nil {
				t.Fatal("accepted changed source")
			}
			delete(service.modelViewerSessions, "session")
			if _, err = service.SurfaceGeometry(ctx, "session", "mesh"); err == nil {
				t.Fatal("accepted closed session")
			}
		})
	}
}
