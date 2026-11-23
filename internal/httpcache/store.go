package httpcache

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	tempSuffix = ".ntmp"
	// staleTempAge is how old a temporary file must be before indexing removes it, so a write in progress at
	// startup is left alone.
	staleTempAge = time.Minute
	// evictionMarginPercent is the share of the limit eviction stops at, so one more entry does not evict again.
	evictionMarginPercent = 90
)

type indexEntry struct {
	size int64
	used time.Time
	// requested is when the stored response was requested; it is zero for entries left by earlier runs.
	requested time.Time
}

// SetMaxBytes changes the size limit and evicts the least recently used entries over it.
func (c *Cache) SetMaxBytes(n int64) {
	c.mu.Lock()
	c.maxBytes = n
	victims := c.evictLocked()
	c.mu.Unlock()
	c.removeVictims(victims)
}

// Usage is the size of the indexed entries. Entries already on disk are counted once indexing finishes.
func (c *Cache) Usage() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.total
}

// Clear removes every entry. One that is being read or written is removed when that finishes.
func (c *Cache) Clear(ctx context.Context) error {
	if c.dir == "" {
		return nil
	}
	release, err := diskio.AcquireDir(ctx, c.dir)
	if err != nil {
		return err
	}
	defer release()
	files, err := os.ReadDir(c.dir)
	if err != nil {
		return err
	}

	var failures []error
	for _, file := range files {
		name := file.Name()
		if !isEntryName(name) {
			continue
		}
		c.mu.Lock()
		busy := c.busy[name] > 0
		if busy {
			c.clearPending[name] = struct{}{}
		}
		c.mu.Unlock()
		if busy {
			continue
		}
		err := os.Remove(filepath.Join(c.dir, name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			failures = append(failures, err)
			continue
		}
		c.mu.Lock()
		c.forgetLocked(name)
		c.mu.Unlock()
	}
	return errors.Join(failures...)
}

func (c *Cache) path(name string) string {
	return filepath.Join(c.dir, name)
}

// hold marks name as in use so eviction and Clear skip it.
func (c *Cache) hold(name string) {
	c.mu.Lock()
	c.busy[name]++
	c.mu.Unlock()
}

func (c *Cache) release(name string) {
	c.mu.Lock()
	_, cleared := c.clearPending[name]
	var victims []string
	if c.busy[name]--; c.busy[name] > 0 {
		cleared = false
	} else {
		delete(c.busy, name)
		delete(c.clearPending, name)
		if !cleared {
			// Eviction skipped name while it was busy.
			victims = c.evictLocked()
		}
	}
	c.mu.Unlock()
	if cleared {
		c.remove(name)
	}
	c.removeVictims(victims)
}

// read returns the entry stored for key, or nil when there is none or it is unreadable. A damaged entry is
// removed.
func (c *Cache) read(ctx context.Context, name, key string) *entry {
	release, err := diskio.Acquire(ctx, c.path(name))
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(c.path(name))
	release()
	if err != nil {
		return nil
	}
	e, err := decodeEntry(data, key)
	if err != nil {
		c.remove(name)
		return nil
	}
	return e
}

// write stores e under name through a temporary file, unless the stored entry answers a later request. A
// failure is logged and leaves any earlier entry.
func (c *Cache) write(ctx context.Context, name string, e *entry) {
	data, err := e.encode()
	if err == nil {
		err = c.writeFile(ctx, name, data, e.RequestTime)
	}
	if err != nil {
		if !infra.IsCancellationError(err) {
			c.storeFailures.Report(c.log, err, "HTTPCache", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "http-cache", Stage: "store",
				Fields: map[string]any{"path": c.dir, "url": infra.SanitizeLogURL(e.URL)},
			})
		}
		return
	}

	c.mu.Lock()
	victims := c.evictLocked()
	c.mu.Unlock()
	c.removeVictims(victims)
}

func (c *Cache) writeFile(ctx context.Context, name string, data []byte, requested time.Time) error {
	release, err := diskio.Acquire(ctx, c.path(name))
	if err != nil {
		return err
	}
	defer release()
	temp, err := os.CreateTemp(c.dir, name+".*"+tempSuffix)
	if err != nil {
		return err
	}
	_, err = temp.Write(data)
	err = errors.Join(err, temp.Close())
	replaced := false
	if err == nil {
		replaced, err = c.commit(temp.Name(), name, int64(len(data)), requested)
	}
	if !replaced {
		_ = os.Remove(temp.Name())
	}
	return err
}

// commit replaces the entry file name with temp and indexes it, unless the indexed entry was requested later.
func (c *Cache) commit(temp, name string, size int64, requested time.Time) (bool, error) {
	c.commitMu.Lock()
	defer c.commitMu.Unlock()

	c.mu.Lock()
	stored, ok := c.index[name]
	c.mu.Unlock()
	if ok && requested.Before(stored.requested) {
		return false, nil
	}
	if err := platform.ReplaceAtomic(temp, c.path(name)); err != nil {
		return false, err
	}

	c.mu.Lock()
	c.forgetLocked(name)
	c.index[name] = indexEntry{size: size, used: c.now(), requested: requested}
	c.total += size
	c.mu.Unlock()
	return true, nil
}

// touch records a use of name for eviction. The modification time carries it across restarts.
func (c *Cache) touch(name string) {
	now := c.now()
	_ = os.Chtimes(c.path(name), now, now)
	c.mu.Lock()
	if e, ok := c.index[name]; ok {
		e.used = now
		c.index[name] = e
	}
	c.mu.Unlock()
}

func (c *Cache) remove(name string) {
	err := os.Remove(c.path(name))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return
	}
	c.mu.Lock()
	c.forgetLocked(name)
	c.mu.Unlock()
}

func (c *Cache) forgetLocked(name string) {
	if !c.scanned {
		c.changedBeforeScan[name] = struct{}{}
	}
	if e, ok := c.index[name]; ok {
		c.total -= e.size
		delete(c.index, name)
	}
}

// scan indexes the entries left by earlier runs and removes abandoned temporary files. When the folder cannot
// be listed, the index holds only the entries this run writes.
func (c *Cache) scan() {
	found, _ := c.listEntries()
	c.indexFound(found)
}

func (c *Cache) listEntries() (map[string]indexEntry, bool) {
	release, err := diskio.AcquireDir(c.ctx, c.dir)
	if err != nil {
		return nil, false
	}
	files, err := os.ReadDir(c.dir)
	release()
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			_ = infra.ReportError(c.log, err, "HTTPCache", infra.Diagnostic{
				Severity:  infra.DiagnosticWarn,
				Operation: "http-cache",
				Stage:     "scan",
				Fields:    map[string]any{"path": c.dir},
			})
		}
		return nil, false
	}

	found := map[string]indexEntry{}
	for _, file := range files {
		info, err := file.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		switch name := file.Name(); {
		case strings.HasSuffix(name, tempSuffix):
			if c.now().Sub(info.ModTime()) > staleTempAge {
				_ = os.Remove(c.path(name))
			}
		case isEntryName(name):
			found[name] = indexEntry{size: info.Size(), used: info.ModTime()}
		}
	}
	return found, true
}

// indexFound adds the listed entries to the index, except those written or removed since the listing, which
// the index already describes.
func (c *Cache) indexFound(found map[string]indexEntry) {
	c.mu.Lock()
	for name, e := range found {
		_, changed := c.changedBeforeScan[name]
		if _, indexed := c.index[name]; !indexed && !changed {
			c.index[name] = e
			c.total += e.size
		}
	}
	c.scanned = true
	c.changedBeforeScan = nil
	victims := c.evictLocked()
	c.mu.Unlock()
	c.removeVictims(victims)
}

// evictLocked takes the least recently used entries out of the index until the rest fit in the margin below
// the limit, and returns them for removeVictims. It waits for the first scan, which knows the real total.
func (c *Cache) evictLocked() []string {
	if !c.scanned || c.maxBytes <= 0 || c.total <= c.maxBytes {
		return nil
	}
	type candidate struct {
		name string
		indexEntry
	}
	candidates := make([]candidate, 0, len(c.index))
	for name, e := range c.index {
		if c.busy[name] == 0 {
			candidates = append(candidates, candidate{name, e})
		}
	}
	slices.SortFunc(candidates, func(a, b candidate) int { return a.used.Compare(b.used) })

	target := c.maxBytes / 100 * evictionMarginPercent
	var victims []string
	for _, v := range candidates {
		if c.total <= target {
			break
		}
		c.forgetLocked(v.name)
		victims = append(victims, v.name)
	}
	return victims
}

// removeVictims deletes evicted entry files. One that cannot be deleted, such as a file another process
// holds open, is indexed again so a later eviction retries it.
func (c *Cache) removeVictims(names []string) {
	for _, name := range names {
		err := os.Remove(c.path(name))
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if info, statErr := os.Stat(c.path(name)); statErr == nil {
			c.mu.Lock()
			if _, ok := c.index[name]; !ok {
				c.index[name] = indexEntry{size: info.Size(), used: info.ModTime()}
				c.total += info.Size()
			}
			c.mu.Unlock()
		}
	}
}
