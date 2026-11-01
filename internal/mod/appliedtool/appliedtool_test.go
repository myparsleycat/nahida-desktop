package appliedtool

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDetect(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{name: "no traces"},
		{
			name:  "body shape change log",
			files: map[string]string{BodyShapeChangeLogFile: BodyShapeChangeLogHeader + "\n\n- 수정 대상 파일: a.buf\n"},
			want:  []string{BodyShape},
		},
		{
			name:  "another mod's change log",
			files: map[string]string{BodyShapeChangeLogFile: "v1.2 변경사항\n" + BodyShapeChangeLogHeader},
		},
		{
			name:  "touch manifest",
			files: map[string]string{TouchProfileManifestFile: `{"kind":"nahida-touch-profile"}`},
			want:  []string{TouchProfile},
		},
		{
			name:  "foreign touch manifest",
			files: map[string]string{TouchProfileManifestFile: `{"kind":"other"}`},
		},
		{
			name:  "broken touch manifest",
			files: map[string]string{TouchProfileManifestFile: `{`},
		},
		{
			name:  "liquid marker",
			files: map[string]string{LiquidMarkerFile: `{"version":1,"modKey":"0123456789ab"}`},
			want:  []string{LiquidEmitter},
		},
		{
			name:  "liquid marker without emitters",
			files: map[string]string{LiquidMarkerFile: `{"version":1,"modKey":"0123456789ab","empty":true}`},
		},
		{
			name:  "broken liquid marker",
			files: map[string]string{LiquidMarkerFile: `{`},
		},
		{
			name: "several tools",
			files: map[string]string{
				BodyShapeChangeLogFile:   BodyShapeChangeLogHeader,
				TouchProfileManifestFile: `{"kind":"nahida-touch-profile"}`,
				LiquidMarkerFile:         `{"modKey":"0123456789ab"}`,
			},
			want: []string{BodyShape, TouchProfile, LiquidEmitter},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range test.files {
				if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Detect(root)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("Detect = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDetectReportsUnreadableTrace(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, LiquidMarkerFile), 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := Detect(root)
	if err == nil || len(got) != 0 {
		t.Fatalf("Detect = %v, %v", got, err)
	}
}
