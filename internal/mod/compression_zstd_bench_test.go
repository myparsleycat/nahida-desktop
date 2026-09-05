package mod

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Includes streaming, file sync, metadata and replacement, but excludes fixture preparation.
func BenchmarkZstdFiles(b *testing.B) {
	for _, compressible := range []bool{true, false} {
		for _, restore := range []bool{false, true} {
			for _, workers := range []int{1, runtime.GOMAXPROCS(0)} {
				b.Run(fmt.Sprintf("compressible=%t/restore=%t/workers=%d", compressible, restore, workers), func(b *testing.B) {
					b.StopTimer()
					const fileCount, fileSize = 32, 8 << 20
					data := make([]byte, fileSize)
					rng := rand.New(rand.NewPCG(17, 29))
					for i := range data {
						if compressible && i >= 32<<10 {
							data[i] = data[i%(32<<10)]
						} else {
							data[i] = byte(rng.Uint32())
						}
					}
					folder := b.TempDir()
					var archive []byte
					if restore {
						source := filepath.Join(folder, "fixture")
						if err := os.WriteFile(source, data, 0o644); err != nil {
							b.Fatal(err)
						}
						if err := streamCompressZstd(context.Background(), source, source+".nzst"); err != nil {
							b.Fatal(err)
						}
						var err error
						archive, err = os.ReadFile(source + ".nzst")
						if err != nil {
							b.Fatal(err)
						}
					}
					b.SetBytes(fileCount * fileSize)
					b.ReportAllocs()
					for n := range b.N {
						files := make([]compressionFile, fileCount)
						for i := range files {
							path := filepath.Join(folder, fmt.Sprintf("%d-%d.bin", n, i))
							payload := data
							if restore {
								path += managedZstdExtension
								payload = archive
							}
							if err := os.WriteFile(path, payload, 0o644); err != nil {
								b.Fatal(err)
							}
							files[i] = compressionFile{path: path, size: int64(len(payload))}
						}
						b.StartTimer()
						err := runZstdWorkers(context.Background(), files, workers, func(file compressionFile) error {
							if restore {
								return restoreZstdFile(context.Background(), file.path, maxZstdRestoreSize, ignoreCompressionMutations)
							}
							return compressZstdFile(context.Background(), file.path, ignoreCompressionMutations)
						}, func(string, int64, bool) {}, func(path string, err error) { b.Errorf("%s: %v", path, err) })
						b.StopTimer()
						if err != nil {
							b.Fatal(err)
						}
						// Keep long benchmark runs from accumulating output on disk.
						for _, file := range files {
							paths := []string{file.path, file.path + managedZstdExtension}
							if restore {
								paths[1] = file.path[:len(file.path)-len(managedZstdExtension)]
							}
							for _, path := range paths {
								if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
									b.Fatal(err)
								}
							}
						}
					}
				})
			}
		}
	}
}
