package xxmi

import (
	"context"
	"io/fs"
	"path/filepath"
	"sync"
)

func findFileAcrossRoots(ctx context.Context, roots []string, targetName string, excludedDirs map[string]struct{}) (*string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	searchCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan string, 1)
	done := make(chan struct{})
	var wg sync.WaitGroup
	for _, root := range roots {
		root := filepath.Clean(root)
		if root == "." || root == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if searchCtx.Err() != nil {
					return fs.SkipAll
				}
				if walkErr != nil {
					if entry != nil && entry.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				if entry.IsDir() {
					if _, excluded := excludedDirs[entry.Name()]; excluded {
						return fs.SkipDir
					}
					return nil
				}
				if entry.Name() != targetName {
					return nil
				}
				select {
				case results <- path:
					cancel()
				default:
				}
				return fs.SkipAll
			})
		}()
	}
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case result := <-results:
		cancel()
		<-done
		return &result, nil
	case <-done:
		select {
		case result := <-results:
			return &result, nil
		default:
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, nil
	case <-ctx.Done():
		cancel()
		<-done
		return nil, ctx.Err()
	}
}
