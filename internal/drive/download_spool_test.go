package drive

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/samber/lo"

	"nahida.live/desktop/internal/appdata"
	"nahida.live/desktop/internal/transfer"
)

func spoolTestPlan(t *testing.T, files []transfer.DownloadFile) *downloadPlan {
	t.Helper()
	writer, err := newSpoolWriter(filepath.Join(t.TempDir(), "test"+downloadSpoolExt))
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if err := writer.add(file); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := writer.finish()
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func collectSpool(t *testing.T, plan *downloadPlan, scheduled bool) []spooledFile {
	t.Helper()
	sequence := plan.files()
	if scheduled {
		sequence = plan.scheduled()
	}
	var out []spooledFile
	for item, err := range sequence {
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, item)
	}
	return out
}

func TestDownloadSpoolRoundTripsFilesAcrossReads(t *testing.T) {
	t.Parallel()

	parent := "parent"
	algorithm := "zstd"
	uncompressed := int64(90)
	files := []transfer.DownloadFile{
		{ID: "a", FileID: "a", ParentID: &parent, Name: "a.bin", Size: 10, URL: "https://download.invalid/a"},
		{ID: "b", FileID: "b", Name: "b.bin", Size: 20, UncompSize: &uncompressed, CompAlg: &algorithm},
		{ID: "c", FileID: "c", ParentID: &parent, Name: "c.bin", URLOrigin: "presign"},
	}
	plan := spoolTestPlan(t, files)
	if plan.fileCount != len(files) || len(plan.large) != 0 {
		t.Fatalf("plan = %+v", plan)
	}

	for range 2 {
		got := collectSpool(t, plan, false)
		if len(got) != len(files) {
			t.Fatalf("read %d files, want %d", len(got), len(files))
		}
		for index, item := range got {
			want := files[index]
			if item.index != index || item.file.ID != want.ID || item.file.Name != want.Name ||
				item.file.Size != want.Size || item.file.URL != want.URL || item.file.URLOrigin != want.URLOrigin ||
				!reflectPointerEqual(item.file.ParentID, want.ParentID) ||
				!reflectPointerEqual(item.file.CompAlg, want.CompAlg) ||
				!reflectPointerEqual(item.file.UncompSize, want.UncompSize) {
				t.Fatalf("file %d = %+v, want %+v", index, item.file, want)
			}
		}
	}
}

func reflectPointerEqual[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func TestDownloadSpoolEmptyYieldsNothing(t *testing.T) {
	t.Parallel()

	plan := spoolTestPlan(t, nil)
	if got := collectSpool(t, plan, true); len(got) != 0 {
		t.Fatalf("empty spool yielded %+v", got)
	}
}

func TestDownloadSpoolReleasesFileWhenIterationStopsEarly(t *testing.T) {
	t.Parallel()

	plan := spoolTestPlan(t, []transfer.DownloadFile{{ID: "a"}, {ID: "b"}})
	for range plan.files() {
		break
	}
	// Windows refuses to delete a file that is still open.
	if err := plan.remove(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(plan.spoolPath); !os.IsNotExist(err) {
		t.Fatalf("spool still exists: %v", err)
	}
}

func TestDownloadSpoolSchedulesLargeFilesAmongSmallOnes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		large []bool
	}{
		{name: "no large files", large: []bool{false, false, false}},
		{name: "only large files", large: []bool{true, true}},
		{name: "large first", large: []bool{true, true, false, false, false, false, false}},
		{name: "large last", large: []bool{false, false, false, false, true}},
		{name: "more large than small", large: []bool{true, false, true, true, false, true}},
		{name: "uneven interval", large: []bool{false, true, false, false, false, true, false, false, true, false}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			files := make([]transfer.DownloadFile, len(test.large))
			for index, large := range test.large {
				files[index] = transfer.DownloadFile{ID: fmt.Sprintf("file-%d", index), Size: 1}
				if large {
					files[index].Size = largeDownloadThreshold
				}
			}
			plan := spoolTestPlan(t, files)

			got := lo.Map(collectSpool(t, plan, true), func(item spooledFile, _ int) string {
				if files[item.index].ID != item.file.ID {
					t.Fatalf("index %d carries %q", item.index, item.file.ID)
				}
				return item.file.ID
			})
			if want := interleaveLargeDownloads(files); !slices.Equal(got, want) {
				t.Fatalf("scheduled = %v, want %v", got, want)
			}
		})
	}
}

// interleaveLargeDownloads is the in-memory ordering the spool replaced: each
// group of small files is followed by one large file.
func interleaveLargeDownloads(files []transfer.DownloadFile) []string {
	large, small := lo.FilterReject(files, func(file transfer.DownloadFile, _ int) bool {
		return file.Size >= largeDownloadThreshold
	})
	ordered := files
	if len(large) > 0 && len(small) > 0 {
		interval := max(1, len(small)/len(large))
		ordered = make([]transfer.DownloadFile, 0, len(files))
		for len(small) > 0 || len(large) > 0 {
			count := min(interval, len(small))
			ordered = append(ordered, small[:count]...)
			small = small[count:]
			if len(large) > 0 {
				ordered = append(ordered, large[0])
				large = large[1:]
			}
		}
	}
	return lo.Map(ordered, func(file transfer.DownloadFile, _ int) string { return file.ID })
}

func TestSweepDownloadSpoolsRemovesOnlySpoolsWithoutATransfer(t *testing.T) {
	t.Parallel()

	store, err := appdata.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	transfers := transfer.New()
	if _, err := transfers.Create(transfer.CreateParams{
		PID: "live", Type: "download", Name: "live", InitialStatus: transfer.StatusPaused,
	}); err != nil {
		t.Fatal(err)
	}
	drive := NewWithOptions(Options{Transfer: transfers})
	drive.UseAppData(store)

	paths := make(map[string]string)
	for _, name := range []string{"live" + downloadSpoolExt, "orphan" + downloadSpoolExt, "unrelated.txt"} {
		if err := store.WriteFile(filepath.Join(appdata.DownloadSpoolDir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths[name], err = store.Resolve(filepath.Join(appdata.DownloadSpoolDir, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	drive.sweepDownloadSpools()

	for name, wantExists := range map[string]bool{
		"live" + downloadSpoolExt: true, "orphan" + downloadSpoolExt: false, "unrelated.txt": true,
	} {
		_, statErr := os.Stat(paths[name])
		if exists := statErr == nil; exists != wantExists {
			t.Fatalf("%s exists = %v, want %v (%v)", name, exists, wantExists, statErr)
		}
	}
}
