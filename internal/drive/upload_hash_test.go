package drive

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"nahida.live/desktop/internal/platform"
)

func TestHashUploadFilesReusesPendingCacheAndRetainsPartialResults(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	files := []UploadFile{
		{FID: "one", Name: "one.ini", FullPath: filepath.Join(root, "one.ini"), Size: 1 << 20},
		{FID: "two", Name: "two.ini", FullPath: filepath.Join(root, "two.ini"), Size: 1 << 20},
	}
	for _, file := range files {
		writeUploadFile(t, file.FullPath, string(bytes.Repeat([]byte("x"), int(file.Size))))
	}
	cache := make(map[string]uploadHashEntry)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := hashUploadFiles(ctx, files, 1, uploadHashOptions{
		verifySource: true,
		onFile:       func(file UploadFile, entry uploadHashEntry) { cache[file.FID] = entry; cancel() },
	})
	if !errors.Is(err, context.Canceled) || len(cache) != 1 || cache["one"].SHA256 == "" {
		t.Fatalf("partial cache = %v, error = %v", cache, err)
	}
	var credits []int64
	_, err = hashUploadFiles(t.Context(), files, 1, uploadHashOptions{
		cache: cache, verifySource: true,
		onBytes: func(n int64) { credits = append(credits, n) },
		onFile:  func(file UploadFile, entry uploadHashEntry) { cache[file.FID] = entry },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cache) != 2 || len(credits) < 2 || credits[0] != files[0].Size {
		t.Fatalf("resume credits = %v, cache = %v", credits, cache)
	}
	credits = nil
	_, err = hashUploadFiles(t.Context(), files[1:], 2, uploadHashOptions{
		cache: cache, verifySource: true, onBytes: func(n int64) { credits = append(credits, n) },
	})
	if err != nil || !slices.Equal(credits, []int64{files[1].Size}) {
		t.Fatalf("pending cached file was read in chunks: credits = %v, error = %v", credits, err)
	}
}

func TestHashUploadFilesInvalidatesReplacedFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "mod.ini")
	writeUploadFile(t, path, "before")
	file := UploadFile{FID: "file", Name: "mod.ini", FullPath: path, Size: 6}
	cache := make(map[string]uploadHashEntry)
	if _, err := hashUploadFiles(t.Context(), []UploadFile{file}, 1, uploadHashOptions{
		verifySource: true, onFile: func(file UploadFile, entry uploadHashEntry) { cache[file.FID] = entry },
	}); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(filepath.Dir(path), "replacement.ini")
	writeUploadFile(t, replacement, "after!")
	modified := cache[file.FID].Info.ModTime()
	if err := os.Chtimes(replacement, modified, modified); err != nil {
		t.Fatal(err)
	}
	if err := platform.ReplaceAtomic(replacement, path); err != nil {
		t.Fatal(err)
	}
	invalidated := false
	hashed, err := hashUploadFiles(t.Context(), []UploadFile{file}, 1, uploadHashOptions{
		cache: cache, verifySource: true, onInvalid: func(UploadFile) { invalidated = true },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !invalidated || hashed[0].SHA256 != fmt.Sprintf("%x", sha256.Sum256([]byte("after!"))) {
		t.Fatalf("replacement reused old identity: %+v, invalidated = %v", hashed, invalidated)
	}
}

func TestUploadHashRejectsChangedSources(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"size-before", "during-read", "deleted"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "mod.ini")
			data := bytes.Repeat([]byte("x"), 2*uploadHashBufferSize)
			writeUploadFile(t, path, string(data))
			file := UploadFile{FID: "file", Name: "mod.ini", FullPath: path, Size: int64(len(data))}
			var cached uploadHashEntry
			if mode == "size-before" {
				writeUploadFile(t, path, "short")
			}
			changed := false
			_, err := hashUploadFiles(t.Context(), []UploadFile{file}, 1, uploadHashOptions{
				verifySource: true,
				onBytes: func(int64) {
					if mode != "during-read" || changed {
						return
					}
					changed = true
					modified := time.Now().Add(time.Hour)
					if err := os.Chtimes(path, modified, modified); err != nil {
						t.Error(err)
					}
				},
				onFile: func(_ UploadFile, entry uploadHashEntry) { cached = entry },
			})
			if mode == "deleted" {
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				_, err = hashUploadFiles(t.Context(), []UploadFile{file}, 1, uploadHashOptions{
					cache: map[string]uploadHashEntry{file.FID: cached}, verifySource: true,
				})
				if !errors.Is(err, ErrBackupSourceRead) {
					t.Fatalf("deleted source error = %v", err)
				}
				return
			}
			var changedErr *UploadV2Error
			if !errors.As(err, &changedErr) || changedErr.Code != "upload_source_changed" {
				t.Fatalf("changed source error = %v", err)
			}
			if cached.SHA256 != "" {
				t.Fatal("unstable hash entered cache")
			}
		})
	}
}
