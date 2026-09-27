package metadata

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

type pathQueue struct {
	tail chan struct{}
	refs int
}

var fileQueues = struct {
	sync.Mutex
	byPath map[string]*pathQueue
}{byPath: make(map[string]*pathQueue)}

// reserve serializes in-process access per directory in arrival order. Batch callers reserve sorted paths
// to avoid deadlocks, and release in reverse order before returning to their callers.
func reserve(paths ...string) (func(), error) {
	keys := make([]string, 0, len(paths))
	seen := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		key, err := queueKey(path)
		if err != nil {
			return nil, err
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate mod metadata directory: %s", path)
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	slices.Sort(keys)

	releases := make([]func(), 0, len(keys))
	for _, key := range keys {
		releases = append(releases, enqueue(key))
	}
	return func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}, nil
}

func queueKey(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve mod metadata directory %s: %w", path, err)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		absolute = resolved
	}
	return strings.ToLower(filepath.Clean(absolute)), nil
}

func enqueue(key string) func() {
	fileQueues.Lock()
	queue := fileQueues.byPath[key]
	if queue == nil {
		queue = &pathQueue{}
		fileQueues.byPath[key] = queue
	}
	previous := queue.tail
	done := make(chan struct{})
	queue.tail = done
	queue.refs++
	fileQueues.Unlock()

	if previous != nil {
		<-previous
	}
	return func() {
		fileQueues.Lock()
		close(done)
		queue.refs--
		if queue.refs == 0 {
			delete(fileQueues.byPath, key)
		}
		fileQueues.Unlock()
	}
}
