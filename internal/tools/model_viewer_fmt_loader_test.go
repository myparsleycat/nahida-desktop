package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadModelViewerFmtPrefersLocal(t *testing.T) {
	modDir, assetDir := t.TempDir(), t.TempDir()
	text := "stride: 12\ntopology: trianglelist\nformat: DXGI_FORMAT_R16_UINT\nelement[0]:\n SemanticName: POSITION\n Format: DXGI_FORMAT_R32G32B32_FLOAT\n AlignedByteOffset: 0"
	if err := os.WriteFile(filepath.Join(modDir, "Body.fmt"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	layout, err := loadModelViewerFmt(modDir, assetDir, modelViewerIbResource{Filename: "Body.ib", Format: "DXGI_FORMAT_R16_UINT"}, 12, "mihoyo")
	if err != nil || layout.Stride != 12 || len(layout.Elements) != 1 {
		t.Fatalf("layout=%#v err=%v", layout, err)
	}
}
