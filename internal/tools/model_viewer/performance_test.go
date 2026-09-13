package modelviewer

import (
	"context"
	"errors"
	"math/rand/v2"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelViewerGeometryConcurrentRequestsShareResult(t *testing.T) {
	cache := newModelViewerBufferCache()
	var calls atomic.Int32
	want := &modelViewerGeometry{Position: []float32{1, 2, 3}}
	var workers sync.WaitGroup
	start := make(chan struct{})
	for range 20 {
		workers.Go(func() {
			<-start
			got, err := cache.geometry("same", func() (*modelViewerGeometry, error) {
				calls.Add(1)
				return want, nil
			})
			if err != nil || got != want {
				t.Errorf("geometry=%p err=%v", got, err)
			}
		})
	}
	close(start)
	workers.Wait()
	if calls.Load() != 1 {
		t.Fatalf("builds=%d", calls.Load())
	}
	if _, err := cache.geometry(
		"retry",
		func() (*modelViewerGeometry, error) { return nil, errors.New("failed") },
	); err == nil {
		t.Fatal("missing build error")
	}
	if got, err := cache.geometry(
		"retry",
		func() (*modelViewerGeometry, error) { return want, nil },
	); err != nil ||
		got != want {
		t.Fatal("failed build retained")
	}
}

func TestModelViewerCacheHitAndCancellationDuringDecode(t *testing.T) {
	c := &modelViewerPositionCache{limit: 1024}
	if _, err := c.load(
		context.Background(),
		"hit",
		func(context.Context) ([]byte, error) { return []byte{7}, nil },
	); err != nil {
		t.Fatal(err)
	}
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := c.load(
			context.Background(),
			"slow",
			func(context.Context) ([]byte, error) { close(started); <-release; return []byte{1}, nil },
		)
		finished <- err
	}()
	<-started
	defer func() {
		close(release)
		if err := <-finished; err != nil {
			t.Error(err)
		}
	}()
	hit := make(chan error, 1)
	go func() {
		data, err := c.load(
			context.Background(),
			"hit",
			func(context.Context) ([]byte, error) { return nil, errors.New("unexpected miss") },
		)
		if err == nil && !slices.Equal(data, []byte{7}) {
			err = errors.New("wrong cached bytes")
		}
		hit <- err
	}()
	select {
	case err := <-hit:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cache hit blocked by decode")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.load(
		ctx,
		"cancelled",
		func(context.Context) ([]byte, error) { return nil, errors.New("cancelled request decoded") },
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatal(err)
	}
}

func TestModelViewerDenseCompactMatchesSparse(t *testing.T) {
	random := rand.New(rand.NewPCG(1, 2))
	for count := 1; count < 256; count++ {
		indices := make([]uint32, count*3)
		for i := range indices {
			indices[i] = uint32(random.IntN(count))
		}
		dense, sources, ok := modelViewerCompactIndices(indices, count, nil)
		sparse, sparseSources, sparseOK := modelViewerCompactIndices(indices, 1<<22, nil)
		if !ok || !sparseOK || !slices.Equal(dense, sparse) || !slices.Equal(sources, sparseSources) {
			t.Fatalf("count=%d", count)
		}
	}
}

func TestModelViewerSmallDNFMatchesMapPath(t *testing.T) {
	random := rand.New(rand.NewPCG(3, 4))
	for range 2000 {
		group := make([]ModelViewerDNFClause, 1+random.IntN(8))
		for i := range group {
			group[i] = ModelViewerDNFClause{
				Var:    []string{"A", "b", "a_", "c"}[random.IntN(4)],
				Value:  []string{"", "0", "1"}[random.IntN(3)],
				Negate: random.IntN(2) == 0,
			}
		}
		// Duplicate clauses force the map fallback without changing semantics.
		large := slices.Clone(group)
		for len(large) <= 8 {
			large = append(large, group...)
		}
		want, possible := simplifyModelViewerDNFGroup(large)
		got, actual := simplifyModelViewerDNFGroup(group)
		if modelViewerDNFGroupsCompatible(group[:len(group)/2], group[len(group)/2:]) != possible {
			t.Fatalf("intersection changed: %+v", group)
		}
		if possible != actual || !reflect.DeepEqual(want, got) {
			t.Fatalf("group=%+v got=%+v want=%+v", group, got, want)
		}
	}
}

func BenchmarkModelViewerCompactIndices(b *testing.B) {
	indices := make([]uint32, 300000)
	for i := range indices {
		indices[i] = uint32(i % 100000)
	}
	for _, tc := range []struct {
		name     string
		vertices int
	}{{"dense", 100000}, {"sparse", 1 << 22}} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				modelViewerCompactIndices(indices, tc.vertices, nil)
			}
		})
	}
}
