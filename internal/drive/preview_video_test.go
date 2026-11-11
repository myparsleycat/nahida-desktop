package drive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPrunePreviewVideosRemovesLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	write := func(name string, size int, age time.Duration) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
			t.Fatal(err)
		}
		used := base.Add(-age)
		if err := os.Chtimes(path, used, used); err != nil {
			t.Fatal(err)
		}
		return path
	}
	oldest := write("a.mp4", 40, 4*time.Hour)
	kept := write("b.mp4", 40, 5*time.Hour)
	recent := write("c.mp4", 40, time.Hour)
	partial := write("d.mp4"+previewVideoTempExt, 100, 6*time.Hour)

	prunePreviewVideos(dir, kept, 80)

	for path, want := range map[string]bool{oldest: false, kept: true, recent: true, partial: true} {
		_, err := os.Stat(path)
		if got := err == nil; got != want {
			t.Errorf("%s exists = %v, want %v", filepath.Base(path), got, want)
		}
	}
}
