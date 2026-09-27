package mod

import (
	"encoding/json"
	"os"
	"path/filepath"

	"nahida.live/desktop/internal/mod/metadata"
)

func writeDownloadMetadataToPaths[T any](paths []string, makeDocument func() T) error {
	dirs := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		dir := path
		if !info.IsDir() {
			dir = filepath.Dir(path)
		}
		if _, ok := seen[dir]; ok {
			continue
		}
		seen[dir] = struct{}{}
		dirs = append(dirs, dir)
	}

	entries := make([]metadata.WriteEntry, 0, len(dirs))
	for _, dir := range dirs {
		raw, err := json.MarshalIndent(makeDocument(), "", "  ")
		if err != nil {
			return err
		}
		entries = append(entries, metadata.WriteEntry{Dir: dir, Data: append(raw, '\n')})
	}
	return metadata.WriteBatch(entries)
}
