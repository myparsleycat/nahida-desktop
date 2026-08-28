package tools

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestModelViewerResourceGrouping(t *testing.T) {
	if got := parseModelViewerMihoyoResourceName("BodyPosition.1"); got == nil || got.Key != "Body.1" || got.Kind != "position" {
		t.Fatalf("mihoyo = %#v", got)
	}
	if got := parseModelViewerWwmiResourceName("BodyTexCoordBuffer.2"); got == nil || got.Key != "BodyIndexBuffer.2" || got.Kind != "texcoord" {
		t.Fatalf("wwmi = %#v", got)
	}
	dir := t.TempDir()
	files := map[string][]byte{"pos.buf": {1, 2, 3, 4}, "blend.buf": {5, 6}, "uv.buf": {7, 8, 9, 10}}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	resources := []modelViewerResource{
		{Name: "BodyPosition", Filename: "pos.buf", Stride: 2},
		{Name: "BodyBlend", Filename: "blend.buf", Stride: 1},
		{Name: "BodyTexcoord", Filename: "uv.buf", Stride: 2},
	}
	groups, err := collectModelViewerMihoyoGroups(dir, resources, newModelViewerBufferCache(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 || groups[0].Stride != 5 || !bytes.Equal(groups[0].VB, []byte{1, 2, 5, 7, 8, 3, 4, 6, 9, 10}) {
		t.Fatalf("groups = %#v", groups)
	}
}
