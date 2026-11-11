package modelviewer

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"nahida.live/desktop/internal/appdata"
)

func TestModelViewerSpoolServesGeometryAndIsRemovedOnCleanup(t *testing.T) {
	dir := t.TempDir()
	writeViewerGeometry(t, dir)
	ini := "[TextureOverrideBody]\nib = ResourceBodyIB\nvb0 = ResourcePos\nvb1 = ResourceTc\n" +
		"drawindexed = 3, 0, 0\n" + viewerBodyResources
	if err := os.WriteFile(filepath.Join(dir, "mod.ini"), []byte(ini), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	memory := New()
	want, err := memory.LoadModViewer(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = memory.CleanupModelViewer(ctx, want.MemorySessionID) })

	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spooled := New()
	spooled.UseAppData(store)
	got, err := spooled.LoadModViewer(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	spoolDir, err := store.Resolve(appdata.ModelViewerSpoolDir)
	if err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(spoolDir); len(entries) != 1 {
		t.Fatalf("spool files = %d, want 1", len(entries))
	}
	for index, mesh := range got.Meshes {
		gotBytes, err := spooled.ReadResource(ctx, mesh.GeometryURL)
		if err != nil {
			t.Fatal(err)
		}
		wantBytes, err := memory.ReadResource(ctx, want.Meshes[index].GeometryURL)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(gotBytes, wantBytes) {
			t.Fatalf("mesh %s geometry differs from the in-memory session", mesh.ID)
		}
	}

	if removed, err := spooled.CleanupModelViewer(ctx, got.MemorySessionID); err != nil || !removed {
		t.Fatalf("cleanup removed=%v err=%v", removed, err)
	}
	if entries, _ := os.ReadDir(spoolDir); len(entries) != 0 {
		t.Fatalf("spool files after cleanup = %d", len(entries))
	}
}
