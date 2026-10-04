package diskio

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// newTestLimiter treats the first path element as the volume and limits the
// volumes named in rotational.
func newTestLimiter(limit int64, rotational ...string) *limiter {
	return &limiter{
		resolve: func(dir string) string {
			return strings.SplitN(filepath.ToSlash(dir), "/", 2)[0]
		},
		classify: func(volume string) (bool, error) {
			for _, name := range rotational {
				if name == volume {
					return true, nil
				}
			}
			return false, nil
		},
		limit: limit,
	}
}

func mustAcquire(t *testing.T, l *limiter, dirs ...string) func() {
	t.Helper()
	release, err := l.acquire(context.Background(), dirs)
	if err != nil {
		t.Fatalf("acquire %v: %v", dirs, err)
	}
	return release
}

// assertFull probes the gate without waiting. A canceled context cannot stand
// in for that: Acquire reports the cancellation even when a slot is free.
func assertFull(t *testing.T, l *limiter, dir string) {
	t.Helper()
	gates := l.gatesFor([]string{dir})
	if len(gates) != 1 {
		t.Fatalf("%q is limited by %d gates, want 1", dir, len(gates))
	}
	if gates[0].TryAcquire(1) {
		gates[0].Release(1)
		t.Fatalf("volume of %q has a free slot, want it full", dir)
	}
}

func TestRotationalVolumeAdmitsOnlyItsLimit(t *testing.T) {
	t.Parallel()
	l := newTestLimiter(2, "hdd")

	first := mustAcquire(t, l, "hdd/a")
	second := mustAcquire(t, l, "hdd/b")
	assertFull(t, l, "hdd/c")

	first()
	third := mustAcquire(t, l, "hdd/c")
	assertFull(t, l, "hdd/d")
	second()
	third()
}

func TestWaiterProceedsWhenASlotIsReleased(t *testing.T) {
	t.Parallel()
	l := newTestLimiter(1, "hdd")
	held := mustAcquire(t, l, "hdd/a")

	acquired := make(chan func())
	go func() {
		release, err := l.acquire(context.Background(), []string{"hdd/b"})
		if err != nil {
			t.Errorf("waiting acquire: %v", err)
		}
		acquired <- release
	}()
	held()
	(<-acquired)()
}

func TestOtherVolumesAreNotLimited(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		classify func(string) (bool, error)
	}{
		{name: "solid state", classify: func(string) (bool, error) { return false, nil }},
		{name: "unclassifiable", classify: func(string) (bool, error) { return false, errors.New("no such device") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			l := newTestLimiter(1)
			l.classify = test.classify

			var releases []func()
			for range 16 {
				releases = append(releases, mustAcquire(t, l, "ssd/a"))
			}
			for _, release := range releases {
				release()
			}
		})
	}
}

func TestVolumeIsClassifiedAndDirectoryResolvedOnce(t *testing.T) {
	t.Parallel()
	l := newTestLimiter(4, "hdd")
	var mu sync.Mutex
	resolved, classified := 0, 0
	resolve, classify := l.resolve, l.classify
	l.resolve = func(dir string) string {
		mu.Lock()
		resolved++
		mu.Unlock()
		return resolve(dir)
	}
	l.classify = func(volume string) (bool, error) {
		mu.Lock()
		classified++
		mu.Unlock()
		return classify(volume)
	}

	for _, dir := range []string{"hdd/a", "HDD/A", "hdd/a/", "hdd/b"} {
		mustAcquire(t, l, dir)()
	}
	if resolved != 2 || classified != 1 {
		t.Fatalf("resolved %d directories and classified %d volumes, want 2 and 1", resolved, classified)
	}
}

func TestPathsOnOneVolumeTakeOneSlot(t *testing.T) {
	t.Parallel()
	l := newTestLimiter(1, "hdd")

	release := mustAcquire(t, l, "hdd/source", "hdd/target")
	assertFull(t, l, "hdd/other")
	release()
	mustAcquire(t, l, "hdd/other")()
}

func TestPathsOnTwoVolumesHoldBothWithoutDeadlock(t *testing.T) {
	t.Parallel()
	l := newTestLimiter(1, "hdd1", "hdd2")

	release := mustAcquire(t, l, "hdd2/target", "hdd1/source")
	assertFull(t, l, "hdd1/x")
	assertFull(t, l, "hdd2/x")
	release()

	// Opposite argument orders must lock the volumes in the same order.
	var group sync.WaitGroup
	for _, dirs := range [][]string{{"hdd1/a", "hdd2/a"}, {"hdd2/b", "hdd1/b"}} {
		group.Go(func() {
			for range 200 {
				release, err := l.acquire(context.Background(), dirs)
				if err != nil {
					t.Errorf("acquire %v: %v", dirs, err)
					return
				}
				release()
			}
		})
	}
	group.Wait()
}

func TestFailedAcquireReleasesTheSlotsItTook(t *testing.T) {
	t.Parallel()
	l := newTestLimiter(1, "hdd1", "hdd2")
	held := mustAcquire(t, l, "hdd2/a")
	defer held()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	release, err := l.acquire(ctx, []string{"hdd1/a", "hdd2/b"})
	if !errors.Is(err, context.DeadlineExceeded) {
		release()
		t.Fatalf("acquire behind a held volume = %v, want context.DeadlineExceeded", err)
	}
	release()
	mustAcquire(t, l, "hdd1/b")()
}

func TestAcquireUsesTheParentOfAFileAndAcquireDirTheDirectoryItself(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	want := resolveVolume(dir)

	for name, acquire := range map[string]func() (func(), error){
		"file":      func() (func(), error) { return Acquire(context.Background(), filepath.Join(dir, "missing.bin")) },
		"directory": func() (func(), error) { return AcquireDir(context.Background(), dir) },
	} {
		release, err := acquire()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		release()
	}
	shared.mu.Lock()
	got, ok := shared.dirs[strings.ToLower(filepath.Clean(dir))]
	shared.mu.Unlock()
	if !ok || got != want {
		t.Fatalf("cached volume of %q = %q, %v, want %q", dir, got, ok, want)
	}
}
