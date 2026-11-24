package drive

import (
	"cmp"
	"context"
	"fmt"
	"iter"
	"slices"
	"sync"

	"nahida.live/desktop/internal/transfer"
)

type downloadLane int

const (
	smallDownloadLane downloadLane = iota
	largeDownloadLane
)

// downloadScheduler hands a download's files to its workers in two lanes so
// that small and large files cannot crowd each other out of the connection
// budget. While large files are queued, the large lane may hold up to half the
// budget and the small lane keeps the rest; connections the large lane does
// not hold are lent to the small lane once nothing large is queued, and either
// lane gets the whole budget once the other has nothing queued or running.
// Large files are handed out largest first.
type downloadScheduler struct {
	budget int

	mu sync.Mutex
	// changed is closed and replaced whenever a lane's connections or limit
	// change, which wakes the workers waiting in take.
	changed   chan struct{}
	nextSmall func() (spooledFile, error, bool)
	stopSmall func()
	smallLeft int
	large     []spooledFile
	inUse     [2]int
	err       error
}

// scheduledDownload is a file handed to a worker, with the lane and the
// connections it was counted against.
type scheduledDownload struct {
	spooledFile
	lane        downloadLane
	connections int
}

// downloadConnections is how many connections downloadDriveFile is expected
// to open for file. A large uncompressed file is counted at its full chunk
// count even though it falls back to one stream when the server rejects
// ranges or a large partial file is resumed.
func downloadConnections(file transfer.DownloadFile) int {
	if file.CompAlg == nil && isLargeDownload(file) {
		return parallelDownloadChunks
	}
	return 1
}

func newDownloadScheduler(plan *downloadPlan, budget int) *downloadScheduler {
	next, stop := iter.Pull2(plan.smallFiles())
	large := slices.Clone(plan.large)
	slices.SortStableFunc(large, func(a, b spooledFile) int { return cmp.Compare(b.file.Size, a.file.Size) })
	return &downloadScheduler{
		budget:    max(1, budget),
		changed:   make(chan struct{}),
		nextSmall: next,
		stopSmall: stop,
		smallLeft: plan.fileCount - len(plan.large),
		large:     large,
	}
}

// take returns the next file a worker may start, waiting while neither lane
// has room. It returns false once every file has been handed out or ctx is
// done. Every file it returns must be passed back to done.
func (s *downloadScheduler) take(ctx context.Context) (scheduledDownload, bool) {
	for {
		if ctx.Err() != nil {
			return scheduledDownload{}, false
		}
		s.mu.Lock()
		if item, ok := s.nextLocked(); ok {
			s.inUse[item.lane] += item.connections
			s.mu.Unlock()
			return item, true
		}
		if s.smallLeft == 0 && len(s.large) == 0 {
			s.mu.Unlock()
			return scheduledDownload{}, false
		}
		changed := s.changed
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return scheduledDownload{}, false
		case <-changed:
		}
	}
}

func (s *downloadScheduler) done(item scheduledDownload) {
	s.mu.Lock()
	s.inUse[item.lane] -= item.connections
	s.signalLocked()
	s.mu.Unlock()
}

// spoolErr reports a failure to read the small files from the spool, after
// which no further files were handed out.
func (s *downloadScheduler) spoolErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *downloadScheduler) close() {
	s.mu.Lock()
	s.stopSmall()
	s.mu.Unlock()
}

func (s *downloadScheduler) nextLocked() (scheduledDownload, bool) {
	if len(s.large) > 0 {
		// A file larger than the lane's limit still runs, alone.
		index := 0
		if s.inUse[largeDownloadLane] > 0 {
			limit := s.largeLimitLocked()
			index = slices.IndexFunc(s.large, func(item spooledFile) bool {
				return s.inUse[largeDownloadLane]+downloadConnections(item.file) <= limit
			})
		}
		if index >= 0 {
			item := s.large[index]
			s.large = slices.Delete(s.large, index, index+1)
			if len(s.large) == 0 {
				// The small lane may now use what the large lane does not hold.
				s.signalLocked()
			}
			return scheduledDownload{
				spooledFile: item,
				lane:        largeDownloadLane,
				connections: downloadConnections(item.file),
			}, true
		}
	}
	if s.smallLeft == 0 || s.inUse[smallDownloadLane] >= s.smallLimitLocked() {
		return scheduledDownload{}, false
	}

	// The spool is a local zstd stream, so reading the next entry under the
	// lock is brief and keeps iter.Pull2's single-caller rule.
	item, err, ok := s.nextSmall()
	if err == nil && ok {
		s.smallLeft--
		return scheduledDownload{spooledFile: item, lane: smallDownloadLane, connections: 1}, true
	}
	if err == nil {
		err = fmt.Errorf("download spool ended with %d small files unread", s.smallLeft)
	}
	// The run fails once its running files finish, so nothing more is started.
	s.err = err
	s.smallLeft = 0
	s.large = nil
	s.signalLocked()
	return scheduledDownload{}, false
}

func (s *downloadScheduler) largeLimitLocked() int {
	if s.smallLeft > 0 || s.inUse[smallDownloadLane] > 0 {
		return s.budget / 2
	}
	return s.budget
}

func (s *downloadScheduler) smallLimitLocked() int {
	reserved := s.inUse[largeDownloadLane]
	if len(s.large) > 0 {
		reserved = max(reserved, s.budget/2)
	}
	return max(1, s.budget-reserved)
}

func (s *downloadScheduler) signalLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}
