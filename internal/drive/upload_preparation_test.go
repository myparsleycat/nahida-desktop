package drive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"nahida.live/desktop/internal/transfer"
)

func TestUploadPreparationProgressThrottlesAndStopsAfterCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	now := time.Unix(100, 0)
	var snapshots []transfer.PreparationProgress
	progress := uploadPreparationProgress{
		ctx: ctx, now: func() time.Time { return now },
		value:   transfer.PreparationProgress{TotalBytes: 1000, TotalFiles: 3},
		publish: func(value transfer.PreparationProgress) { snapshots = append(snapshots, value) },
	}
	progress.report(true)
	progress.add(100, true)
	now = now.Add(499 * time.Millisecond)
	progress.add(200, false)
	if len(snapshots) != 1 {
		t.Fatalf("unthrottled progress = %v", snapshots)
	}
	now = now.Add(time.Millisecond)
	progress.add(300, true)
	if len(snapshots) != 2 || snapshots[1].ProcessedBytes != 600 || snapshots[1].ProcessedFiles != 2 {
		t.Fatalf("byte progress = %v", snapshots)
	}
	progress.add(400, true)
	progress.report(true)
	if len(snapshots) != 3 || snapshots[2].Percent() != 100 {
		t.Fatalf("final progress = %v", snapshots)
	}
	cancel()
	now = now.Add(time.Second)
	progress.add(100, true)
	progress.report(true)
	if len(snapshots) != 3 {
		t.Fatalf("canceled progress was published = %v", snapshots)
	}
}

func TestUploadPreparationCountsMixedSourcesAndCachedFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	ordinary := filepath.Join(root, "plain.ini")
	archive := filepath.Join(root, "decoded.ini.nzst")
	empty := filepath.Join(root, "empty.ini")
	writeUploadFile(t, ordinary, string(bytes.Repeat([]byte("x"), 1<<20)))
	writeUploadFile(t, empty, "")
	decoded := bytes.Repeat([]byte("y"), 128<<10)
	writeUploadNZST(t, archive, decoded)
	prep, err := PrepareUpload([]string{ordinary, archive, empty}, nil, "", testUploadRules(), nil, false)
	if err != nil {
		t.Fatal(err)
	}
	cache := make(map[string]uploadHashEntry)
	for range 2 {
		now := time.Unix(100, 0)
		var snapshots []transfer.PreparationProgress
		progress := uploadPreparationProgress{
			ctx: t.Context(), now: func() time.Time { now = now.Add(time.Second); return now },
			value:   transfer.PreparationProgress{TotalBytes: prep.TotalSize, TotalFiles: len(prep.Files)},
			publish: func(value transfer.PreparationProgress) { snapshots = append(snapshots, value) },
		}
		progress.report(true)
		temp := t.TempDir()
		files, hashes, err := restoreUploadSources(t.Context(), prep.Files, &temp, progress.add)
		if err != nil {
			t.Fatal(err)
		}
		pending := make([]UploadFile, 0)
		for index, file := range prep.Files {
			if isUploadNZST(file.FullPath) {
				if hashes[file.FID].SHA256 != fmt.Sprintf("%x", sha256.Sum256(decoded)) ||
					files[index].FullPath == file.FullPath {
					t.Fatalf("restored hash = %+v", hashes[file.FID])
				}
				continue
			}
			pending = append(pending, file)
		}
		// The worker callbacks update a separate cache, as the upload runner does.
		nextCache := make(map[string]uploadHashEntry)
		_, err = hashUploadFiles(t.Context(), pending, 2, uploadHashOptions{
			cache:        cache,
			verifySource: true,
			onBytes:      func(n int64) { progress.add(n, false) },
			onFile:       func(file UploadFile, entry uploadHashEntry) { nextCache[file.FID] = entry; progress.add(0, true) },
		})
		if err != nil {
			t.Fatal(err)
		}
		cache = nextCache
		progress.report(true)
		last := snapshots[len(snapshots)-1]
		if last.ProcessedBytes != prep.TotalSize || last.ProcessedFiles != 3 || last.Percent() != 100 {
			t.Fatalf("mixed source progress = %+v", last)
		}
	}
}
