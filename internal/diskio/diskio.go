// Package diskio bounds how many file operations run at once on a rotational
// disk. Seeks make a spinning disk slower the more files are read or written
// concurrently, while solid-state and network volumes gain from parallelism, so
// only volumes that report a seek penalty are limited.
//
// The limit is shared by the whole process: unrelated operations that touch the
// same disk queue behind one another instead of each bringing its own workers.
//
// Acquire around a leaf file operation only. Code that holds a slot must not
// acquire again, directly or through a callee, or two holders can wait on each
// other forever.
package diskio

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"
)

const (
	// Two keeps command queuing useful for many small files without letting
	// large sequential reads fight over the head.
	rotationalLimit = 2

	maxCachedDirs = 8192
)

type limiter struct {
	// resolve reports the volume a directory physically lives on, following
	// junctions and symbolic links.
	resolve func(dir string) string
	// classify reports whether the volume incurs a seek penalty.
	classify func(volume string) (bool, error)
	limit    int64

	mu    sync.Mutex
	log   Logger
	dirs  map[string]string
	gates map[string]*semaphore.Weighted
}

// Logger is satisfied by *infra.Log. diskio does not import infra because the
// elevated helper links diskio and must stay free of infra's dependencies.
type Logger interface {
	Info(msg any, where string)
	Warn(msg any, where string)
}

var shared = &limiter{resolve: resolveVolume, classify: incursSeekPenalty, limit: rotationalLimit}

func UseLog(log Logger) {
	shared.mu.Lock()
	shared.log = log
	shared.mu.Unlock()
}

// Acquire waits for a slot on every rotational volume that holds one of the
// files and returns the function that gives the slots back. Files on other
// volumes pass straight through. The release function is never nil.
func Acquire(ctx context.Context, files ...string) (release func(), err error) {
	dirs := make([]string, len(files))
	for i, file := range files {
		dirs[i] = filepath.Dir(file)
	}
	return shared.acquire(ctx, dirs)
}

// AcquireDir is Acquire for work that covers whole directories, such as a walk.
// A directory that is itself a junction counts as the volume it points to.
func AcquireDir(ctx context.Context, dirs ...string) (release func(), err error) {
	return shared.acquire(ctx, dirs)
}

func (l *limiter) acquire(ctx context.Context, dirs []string) (func(), error) {
	gates := l.gatesFor(dirs)
	for held, gate := range gates {
		if err := gate.Acquire(ctx, 1); err != nil {
			releaseGates(gates[:held])
			return func() {}, err
		}
	}
	return func() { releaseGates(gates) }, nil
}

func releaseGates(gates []*semaphore.Weighted) {
	for _, gate := range gates {
		gate.Release(1)
	}
}

// gatesFor returns the gates of the rotational volumes below dirs, one per
// volume and in volume order so that every caller locks them the same way.
func (l *limiter) gatesFor(dirs []string) []*semaphore.Weighted {
	volumes := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		volumes = append(volumes, l.volumeOf(dir))
	}
	slices.Sort(volumes)
	volumes = slices.Compact(volumes)

	l.mu.Lock()
	defer l.mu.Unlock()
	gates := make([]*semaphore.Weighted, 0, len(volumes))
	for _, volume := range volumes {
		if gate := l.gateLocked(volume); gate != nil {
			gates = append(gates, gate)
		}
	}
	return gates
}

func (l *limiter) volumeOf(dir string) string {
	key := strings.ToLower(filepath.Clean(dir))
	l.mu.Lock()
	volume, ok := l.dirs[key]
	l.mu.Unlock()
	if ok {
		return volume
	}

	// Resolving opens the directory, so it stays outside the lock.
	volume = l.resolve(dir)
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.dirs) >= maxCachedDirs {
		clear(l.dirs)
	}
	if l.dirs == nil {
		l.dirs = make(map[string]string)
	}
	l.dirs[key] = volume
	return volume
}

// gateLocked returns nil for a volume that is not limited. A volume is
// classified once; one that cannot be classified keeps its full concurrency.
func (l *limiter) gateLocked(volume string) *semaphore.Weighted {
	if gate, ok := l.gates[volume]; ok {
		return gate
	}
	rotational, err := l.classify(volume)
	if err != nil && l.log != nil {
		l.log.Warn(fmt.Sprintf("classify disk volume %s: %v", volume, err), "DiskIO")
	}

	var gate *semaphore.Weighted
	if rotational {
		gate = semaphore.NewWeighted(l.limit)
		if l.log != nil {
			l.log.Info(
				fmt.Sprintf("limit concurrent file operations on rotational volume %s to %d", volume, l.limit),
				"DiskIO",
			)
		}
	}
	if l.gates == nil {
		l.gates = make(map[string]*semaphore.Weighted)
	}
	l.gates[volume] = gate
	return gate
}
