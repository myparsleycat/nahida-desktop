package drive

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkHashUploadFiles(b *testing.B) {
	for _, shape := range []struct {
		name        string
		count, size int
	}{
		{name: "tiny", count: 256, size: 4 << 10},
		{name: "medium", count: 64, size: 1 << 20},
		{name: "large", count: 16, size: 16 << 20},
		{name: "single", count: 1, size: 32 << 20},
	} {
		b.Run(shape.name, func(b *testing.B) {
			dir := b.TempDir()
			data := make([]byte, shape.size)
			for index := range data {
				data[index] = byte(index*31 + 17)
			}
			files := make([]UploadFile, shape.count)
			for index := range files {
				path := filepath.Join(dir, fmt.Sprintf("%d.bin", index))
				if err := os.WriteFile(path, data, 0o600); err != nil {
					b.Fatal(err)
				}
				files[index] = UploadFile{
					FID:      fmt.Sprint(index),
					Name:     filepath.Base(path),
					FullPath: path,
					Size:     int64(shape.size),
				}
			}
			b.SetBytes(int64(shape.count * shape.size))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := HashUploadFiles(b.Context(), files, uploadHashConcurrency(), nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
