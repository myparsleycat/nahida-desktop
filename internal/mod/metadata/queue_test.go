package metadata

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestOperationsQueueInOrderAndReleasePath(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir, []byte(`{"step":"initial"}`)); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	unblock := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	})
	updated := make(chan error, 1)
	go func() {
		updated <- Update(dir, func([]byte) ([]byte, error) {
			close(entered)
			<-unblock
			return []byte(`{"step":"updated"}`), nil
		})
	}()
	<-entered

	read := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		raw, err := Read(dir)
		read <- raw
		readErr <- err
	}()
	waitForQueueRefs(t, dir, 2)
	written := make(chan error, 1)
	go func() { written <- Write(dir, []byte(`{"step":"final"}`)) }()
	waitForQueueRefs(t, dir, 3)
	close(unblock)

	if err := <-updated; err != nil {
		t.Fatal(err)
	}
	if raw, err := <-read, <-readErr; err != nil || string(raw) != `{"step":"updated"}` {
		t.Fatalf("queued read = %q, %v", raw, err)
	}
	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if raw, err := Read(dir); err != nil || string(raw) != `{"step":"final"}` {
		t.Fatalf("final metadata = %q, %v", raw, err)
	}
	key, err := queueKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	fileQueues.Lock()
	_, exists := fileQueues.byPath[key]
	fileQueues.Unlock()
	if exists {
		t.Fatal("idle path queue was not removed")
	}
}

func TestReadWaitsForBatchWrite(t *testing.T) {
	dir := t.TempDir()
	if err := Initialize(dir, []byte(`{"step":"initial"}`)); err != nil {
		t.Fatal(err)
	}

	entered := make(chan struct{})
	unblock := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	})
	realHide := hideFile
	t.Cleanup(func() { hideFile = realHide })
	hideFile = func(string) error {
		close(entered)
		<-unblock
		return nil
	}

	written := make(chan error, 1)
	go func() { written <- WriteBatch([]WriteEntry{{Dir: dir, Data: []byte(`{"step":"final"}`)}}) }()
	<-entered
	read := make(chan []byte, 1)
	readErr := make(chan error, 1)
	go func() {
		raw, err := Read(dir)
		read <- raw
		readErr <- err
	}()
	waitForQueueRefs(t, dir, 2)
	close(unblock)

	if err := <-written; err != nil {
		t.Fatal(err)
	}
	if raw, err := <-read, <-readErr; err != nil || string(raw) != `{"step":"final"}` {
		t.Fatalf("read after batch write = %q, %v", raw, err)
	}
}

func TestWriteBatchQueuesOverlappingPathsWithoutDeadlock(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first")
	second := filepath.Join(root, "second")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	var workers sync.WaitGroup
	results := make(chan error, 16)
	for i := range 16 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			entries := []WriteEntry{
				{Dir: first, Data: []byte(`{"writer":1}`)},
				{Dir: second, Data: []byte(`{"writer":1}`)},
			}
			if i%2 != 0 {
				entries[0], entries[1] = entries[1], entries[0]
			}
			results <- WriteBatch(entries)
		}()
	}
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("overlapping batches deadlocked")
	}
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestIndependentDirectoriesDoNotWait(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	for _, dir := range []string{first, second} {
		if err := Initialize(dir, []byte(`{"step":"initial"}`)); err != nil {
			t.Fatal(err)
		}
	}

	entered := make(chan struct{})
	unblock := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	})
	updated := make(chan error, 1)
	go func() {
		updated <- Update(first, func(raw []byte) ([]byte, error) {
			close(entered)
			<-unblock
			return raw, nil
		})
	}()
	<-entered

	done := make(chan error, 1)
	go func() {
		if err := Write(second, []byte(`{"step":"final"}`)); err != nil {
			done <- err
			return
		}
		raw, err := Read(second)
		if err == nil && string(raw) != `{"step":"final"}` {
			err = errors.New("read returned stale metadata")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("unrelated mod directory was blocked")
	}
	close(unblock)
	if err := <-updated; err != nil {
		t.Fatal(err)
	}
}

func TestQueueKeyResolvesAliases(t *testing.T) {
	dir := t.TempDir()
	key, err := queueKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	alias, err := queueKey(strings.ToUpper(dir))
	if err != nil || alias != key {
		t.Fatalf("queue key for case alias = %q, %v; want %q", alias, err, key)
	}
}

func waitForQueueRefs(t *testing.T, path string, want int) {
	t.Helper()
	key, err := queueKey(path)
	if err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		fileQueues.Lock()
		queue := fileQueues.byPath[key]
		refs := 0
		if queue != nil {
			refs = queue.refs
		}
		fileQueues.Unlock()
		if refs == want {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatalf("queued operations = %d, want %d", refs, want)
		}
	}
}
