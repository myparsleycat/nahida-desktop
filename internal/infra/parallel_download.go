package infra

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"nahida.live/desktop/internal/transfer"
)

const (
	rangeSupportCacheTTL   = 5 * time.Minute
	parallelMinSegmentSize = 4 * 1024 * 1024
	parallelRequestLimit   = 32
	rangeProbeLimit        = 4
	parallelErrorRetries   = 2
)

type rangeCacheEntry struct {
	expiresAt time.Time
	value     bool
}

type chunkMeta struct {
	Resource string  `json:"resource"`
	FileSize float64 `json:"fileSize"`
}

type UnexpectedContentRangeError struct {
	Message string
}

func (e *UnexpectedContentRangeError) Error() string { return e.Message }

type ParallelDownloadOptions struct {
	URL              string
	SavePath         string
	FileSize         int64
	MaxChunks        int
	Adaptive         bool
	Header           http.Header
	OnProgress       func(bytes int64)
	BandwidthLimiter DownloadLimiter
	SlowChunkMonitor *transfer.SlowChunkMonitor
	FileID           string
	CohortKey        string
}

type ParallelDownloader struct {
	GetHeaders func(string) (map[string]string, error)
	Client     *http.Client
	Remove     func(string) error
	now        func() time.Time

	mu       sync.Mutex
	cache    map[string]rangeCacheEntry
	requests chan struct{}
	probes   chan struct{}
}

func NewParallelDownloader() *ParallelDownloader {
	return &ParallelDownloader{
		cache:    map[string]rangeCacheEntry{},
		requests: make(chan struct{}, parallelRequestLimit),
		probes:   make(chan struct{}, rangeProbeLimit),
	}
}

func (d *ParallelDownloader) client() *http.Client {
	if d != nil && d.Client != nil {
		return d.Client
	}
	return http.DefaultClient
}

func (d *ParallelDownloader) nowFn() time.Time {
	if d != nil && d.now != nil {
		return d.now()
	}
	return time.Now()
}

func (d *ParallelDownloader) removePath(path string) error {
	if d != nil && d.Remove != nil {
		return d.Remove(path)
	}
	return os.RemoveAll(path)
}

func (d *ParallelDownloader) headers(rawURL string) (http.Header, error) {
	header := make(http.Header)
	if d == nil || d.GetHeaders == nil {
		return header, nil
	}
	values, err := d.GetHeaders(rawURL)
	if err != nil {
		return nil, err
	}
	for key, value := range values {
		header.Set(key, value)
	}
	return header, nil
}

func (d *ParallelDownloader) SetRequestConcurrency(concurrency int) {
	d.mu.Lock()
	d.requests = make(chan struct{}, max(1, concurrency))
	d.mu.Unlock()
}

func (d *ParallelDownloader) acquireRequest(ctx context.Context) (chan struct{}, error) {
	d.mu.Lock()
	if d.requests == nil {
		d.requests = make(chan struct{}, parallelRequestLimit)
	}
	requests := d.requests
	d.mu.Unlock()
	select {
	case requests <- struct{}{}:
		return requests, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *ParallelDownloader) releaseRequest(requests chan struct{}) {
	<-requests
}

func (d *ParallelDownloader) CheckRangeSupport(ctx context.Context, rawURL string) (bool, error) {
	return d.CheckRangeSupportWithHeader(ctx, rawURL, nil)
}

func (d *ParallelDownloader) CheckRangeSupportWithHeader(ctx context.Context, rawURL string, caller http.Header) (bool, error) {
	if d.cache == nil {
		d.cache = map[string]rangeCacheEntry{}
	}
	key := rangeSupportCacheKey(rawURL)
	d.mu.Lock()
	if cached, ok := d.cache[key]; ok && cached.expiresAt.After(d.nowFn()) {
		d.mu.Unlock()
		return cached.value, nil
	}
	d.mu.Unlock()
	supported, err := d.requestRangeSupport(ctx, rawURL, caller)
	if err != nil {
		if ctx.Err() != nil {
			return false, err
		}
		return false, nil
	}
	if supported {
		d.mu.Lock()
		d.cache[key] = rangeCacheEntry{expiresAt: d.nowFn().Add(rangeSupportCacheTTL), value: true}
		d.mu.Unlock()
	}
	return supported, nil
}

func (d *ParallelDownloader) requestRangeSupport(ctx context.Context, rawURL string, caller http.Header) (bool, error) {
	d.mu.Lock()
	if d.probes == nil {
		d.probes = make(chan struct{}, rangeProbeLimit)
	}
	probes := d.probes
	d.mu.Unlock()
	select {
	case probes <- struct{}{}:
		defer func() { <-probes }()
	case <-ctx.Done():
		return false, ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return false, err
	}
	header, err := d.headers(rawURL)
	if err != nil {
		return false, err
	}
	for key, values := range caller {
		header.Del(key)
		for _, value := range values {
			header.Add(key, value)
		}
	}
	req.Header = header
	response, err := d.client().Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	// Electron compares Accept-Ranges with === "bytes", not case-insensitive.
	return response.Header.Get("Accept-Ranges") == "bytes", nil
}

type parallelSegment struct {
	id, start, end int64
	chunkPath      string
	status         string
	transferred    atomic.Int64
	splitRequested atomic.Bool
	attemptMu      sync.Mutex
	attemptCancel  context.CancelFunc
}

var errParallelSegmentSplit = errors.New("parallel segment split requested")

type parallelScheduler struct {
	mu       sync.Mutex
	segments []*parallelSegment
	nextID   int64
	adaptive bool
	savePath string
	notify   chan struct{}
}

func newParallelScheduler(segments []*parallelSegment, adaptive bool, savePath string) *parallelScheduler {
	return &parallelScheduler{
		segments: segments, nextID: int64(len(segments)), adaptive: adaptive,
		savePath: savePath, notify: make(chan struct{}),
	}
}

func (s *parallelScheduler) signalLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

func (s *parallelScheduler) acquire(ctx context.Context) *parallelSegment {
	for {
		s.mu.Lock()
		for _, segment := range s.segments {
			if segment.status == "pending" {
				segment.status = "running"
				segment.splitRequested.Store(false)
				s.mu.Unlock()
				return segment
			}
		}
		running := false
		var candidate *parallelSegment
		var candidateRemaining int64
		for _, segment := range s.segments {
			if segment.status != "running" {
				continue
			}
			running = true
			remaining := segment.end - (segment.start + segment.transferred.Load()) + 1
			if s.adaptive && remaining >= int64(parallelMinSegmentSize*2) &&
				!segment.splitRequested.Load() && remaining > candidateRemaining {
				candidate, candidateRemaining = segment, remaining
			}
		}
		if !running {
			s.mu.Unlock()
			return nil
		}
		var cancel context.CancelFunc
		if candidate != nil && candidate.splitRequested.CompareAndSwap(false, true) {
			candidate.attemptMu.Lock()
			cancel = candidate.attemptCancel
			candidate.attemptMu.Unlock()
			if cancel == nil {
				candidate.splitRequested.Store(false)
			}
		}
		notify := s.notify
		s.mu.Unlock()
		if cancel != nil {
			cancel()
		}
		select {
		case <-ctx.Done():
			return nil
		case <-notify:
		}
	}
}

func (s *parallelScheduler) complete(segment *parallelSegment) {
	s.mu.Lock()
	segment.status = "completed"
	segment.splitRequested.Store(false)
	s.signalLocked()
	s.mu.Unlock()
}

func (s *parallelScheduler) split(segment *parallelSegment, remove func(string) error) error {
	completed := min(fileSize(segment.chunkPath), segment.end-segment.start+1)
	originalEnd := segment.end
	remainingStart := segment.start + completed
	remaining := originalEnd - remainingStart + 1
	if completed == 0 {
		if err := remove(segment.chunkPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	segment.transferred.Store(completed)
	segment.end = remainingStart - 1
	segment.status = "completed"
	segment.splitRequested.Store(false)
	if remaining > 0 {
		midpoint := originalEnd
		if remaining >= int64(parallelMinSegmentSize*2) {
			midpoint = remainingStart + (remaining+1)/2 - 1
		}
		for _, bounds := range [][2]int64{{remainingStart, midpoint}, {midpoint + 1, originalEnd}} {
			if bounds[0] > bounds[1] {
				continue
			}
			id := s.nextID
			s.nextID++
			s.segments = append(s.segments, &parallelSegment{
				id: id, start: bounds[0], end: bounds[1],
				chunkPath: fmt.Sprintf("%s.chunk%d", s.savePath, id), status: "pending",
			})
		}
	}
	s.signalLocked()
	return nil
}

func (s *parallelScheduler) ordered() []*parallelSegment {
	s.mu.Lock()
	result := append([]*parallelSegment(nil), s.segments...)
	s.mu.Unlock()
	sort.Slice(result, func(i, j int) bool { return result[i].start < result[j].start })
	return result
}

func (s *parallelSegment) setAttemptCancel(cancel context.CancelFunc) {
	s.attemptMu.Lock()
	s.attemptCancel = cancel
	s.attemptMu.Unlock()
}

func (s *parallelSegment) clearAttemptCancel() {
	s.attemptMu.Lock()
	s.attemptCancel = nil
	s.attemptMu.Unlock()
}

func (d *ParallelDownloader) Download(ctx context.Context, options ParallelDownloadOptions) (returnErr error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if options.URL == "" {
		return errors.New("parallel download URL is required")
	}
	if options.SavePath == "" {
		return errors.New("parallel download destination is required")
	}
	if options.FileSize <= 0 {
		return errors.New("parallel download size must be positive")
	}
	if err := os.MkdirAll(filepath.Dir(options.SavePath), 0o755); err != nil {
		return err
	}

	concurrency := options.MaxChunks
	if concurrency <= 0 {
		concurrency = calculateParallelConcurrency(options.FileSize)
	}
	concurrency = max(1, concurrency)
	segmentSize := max(int64(parallelMinSegmentSize), (options.FileSize+int64(concurrency*4)-1)/int64(concurrency*4))
	chunkCount := int((options.FileSize + segmentSize - 1) / segmentSize)
	concurrency = min(concurrency, chunkCount)
	segments := make([]*parallelSegment, 0, chunkCount)
	for index := range chunkCount {
		start := int64(index) * segmentSize
		end := min(start+segmentSize-1, options.FileSize-1)
		segments = append(segments, &parallelSegment{
			id: int64(index), start: start, end: end,
			chunkPath: fmt.Sprintf("%s.chunk%d", options.SavePath, index), status: "pending",
		})
	}

	chunkMetaPath := options.SavePath + ".chunk-meta.json"
	expected := chunkMeta{Resource: SafeURLResource(options.URL), FileSize: float64(options.FileSize)}
	existing := readChunkMeta(chunkMetaPath)
	if existing == nil || existing.Resource != expected.Resource || existing.FileSize != expected.FileSize {
		_ = d.removeChunkArtifacts(options.SavePath)
		if len(listChunkFiles(options.SavePath)) > 0 {
			return errors.New("failed to discard leftover download chunks")
		}
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	if err := os.WriteFile(chunkMetaPath, raw, 0o600); err != nil {
		return err
	}

	targetPath := options.SavePath + ".ntmp"
	defer func() {
		if returnErr != nil {
			_ = d.removeChunkArtifacts(options.SavePath)
		}
		_ = d.removePath(targetPath)
	}()

	var progressMu sync.Mutex
	reportProgress := func(bytes int64) {
		if bytes == 0 || options.OnProgress == nil {
			return
		}
		progressMu.Lock()
		options.OnProgress(bytes)
		progressMu.Unlock()
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	scheduler := newParallelScheduler(segments, options.Adaptive, options.SavePath)
	errCh := make(chan error, 1)
	var workers sync.WaitGroup
	workers.Add(concurrency)
	for range concurrency {
		go func() {
			defer workers.Done()
			for {
				segment := scheduler.acquire(runCtx)
				if segment == nil {
					return
				}
				if err := d.downloadSegment(runCtx, options, segment, reportProgress); err != nil {
					if errors.Is(err, errParallelSegmentSplit) {
						if splitErr := scheduler.split(segment, d.removePath); splitErr == nil {
							continue
						} else {
							err = splitErr
						}
					}
					select {
					case errCh <- err:
						cancel()
					default:
					}
					return
				}
				scheduler.complete(segment)
			}
		}()
	}
	workers.Wait()
	select {
	case err := <-errCh:
		return err
	default:
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	output, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	segments = scheduler.ordered()
	for _, segment := range segments {
		if segment.end < segment.start {
			continue
		}
		input, openErr := os.Open(segment.chunkPath)
		if openErr != nil {
			_ = output.Close()
			return openErr
		}
		_, copyErr := io.Copy(output, input)
		_ = input.Close()
		if copyErr != nil {
			_ = output.Close()
			return copyErr
		}
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := replaceFile(targetPath, options.SavePath); err != nil {
		return err
	}
	_ = d.removeChunkArtifacts(options.SavePath)
	return nil
}

func calculateParallelConcurrency(fileSize int64) int {
	sizeMiB := fileSize / (1024 * 1024)
	if sizeMiB < 1 {
		return 1
	}
	divisor := int64(1)
	for sizeMiB/divisor >= 10 {
		divisor *= 10
	}
	return max(2, int(sizeMiB/divisor))
}

func (d *ParallelDownloader) downloadSegment(
	ctx context.Context,
	options ParallelDownloadOptions,
	segment *parallelSegment,
	reportProgress func(int64),
) error {
	chunkSize := segment.end - segment.start + 1
	reported := int64(0)
	slowReconnects := 0
	errorRetries := 0
	ignoreStored := false
	fileID := options.FileID
	if fileID == "" {
		fileID = options.SavePath
	}
	cohort := options.CohortKey
	if cohort == "" {
		cohort = "parallel"
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		stored := fileSize(segment.chunkPath)
		resumeBytes := stored
		if ignoreStored || stored > chunkSize {
			resumeBytes = 0
		}
		ignoreStored = false
		if stored > chunkSize {
			_ = d.removePath(segment.chunkPath)
		}
		if delta := resumeBytes - reported; delta != 0 {
			reportProgress(delta)
			reported = resumeBytes
		}
		segment.transferred.Store(resumeBytes)
		if resumeBytes == chunkSize {
			return nil
		}

		attemptCtx, attemptCancel := context.WithCancel(ctx)
		segment.setAttemptCancel(attemptCancel)
		var monitorKey string
		if options.SlowChunkMonitor != nil {
			registered := options.SlowChunkMonitor.Register(transfer.SlowChunkRegistration{
				FileID: fileID, ChunkIndex: int(segment.id), ChunkSize: chunkSize,
				CohortKey: cohort, InitialTransferredBytes: resumeBytes,
				AttemptContext: attemptCtx, AttemptCancel: attemptCancel, SlowReconnects: slowReconnects,
			})
			monitorKey = registered.Key
		}

		err := d.downloadChunk(attemptCtx, downloadChunkArgs{
			URL: options.URL, Header: options.Header, Start: segment.start, End: segment.end,
			FileSize: options.FileSize, ResumeBytes: resumeBytes, ChunkPath: segment.chunkPath,
			Limiter: options.BandwidthLimiter,
			OnPhaseChange: func(phase transfer.SlowChunkPhase) {
				if monitorKey != "" {
					options.SlowChunkMonitor.SetPhase(monitorKey, phase)
				}
			},
			OnProgress: func(transferred, incremental int64) {
				if monitorKey != "" {
					options.SlowChunkMonitor.RecordSample(monitorKey, transferred)
				}
				if transferred > reported {
					reportProgress(transferred - reported)
					reported = transferred
				}
			},
		})
		attemptCancel()
		segment.clearAttemptCancel()
		var snapshot transfer.SlowChunkSnapshot
		var monitored bool
		if monitorKey != "" {
			snapshot, monitored = options.SlowChunkMonitor.Get(monitorKey)
			options.SlowChunkMonitor.Unregister(monitorKey)
		}
		if err == nil {
			segment.transferred.Store(chunkSize)
			return nil
		}

		persisted := min(fileSize(segment.chunkPath), chunkSize)
		if delta := persisted - reported; delta != 0 {
			reportProgress(delta)
			reported = persisted
		}
		segment.transferred.Store(persisted)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if segment.splitRequested.Load() && errors.Is(err, context.Canceled) {
			return errParallelSegmentSplit
		}
		var rangeErr *UnexpectedContentRangeError
		if errors.As(err, &rangeErr) && resumeBytes > 0 {
			if removeErr := d.removePath(segment.chunkPath); removeErr != nil {
				return err
			}
			if reported > 0 {
				reportProgress(-reported)
				reported = 0
			}
			ignoreStored = true
			if errorRetries < parallelErrorRetries {
				errorRetries++
				continue
			}
			return err
		}
		if monitored && snapshot.AbortedSlowChunk && slowReconnects < transfer.SlowChunkMaxReconnects {
			slowReconnects++
			if waitErr := transfer.SleepWithAbort(ctx, transfer.SlowReconnectDelay()); waitErr != nil {
				return waitErr
			}
			continue
		}
		if !errors.Is(err, context.Canceled) && errorRetries < parallelErrorRetries {
			errorRetries++
			if waitErr := transfer.SleepWithAbort(ctx, time.Duration(1<<errorRetries)*time.Second); waitErr != nil {
				return waitErr
			}
			continue
		}
		return err
	}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0
	}
	return info.Size()
}

type downloadChunkArgs struct {
	URL           string
	Header        http.Header
	Start         int64
	End           int64
	FileSize      int64
	ResumeBytes   int64
	ChunkPath     string
	Limiter       DownloadLimiter
	OnProgress    func(transferred, incremental int64)
	OnPhaseChange func(transfer.SlowChunkPhase)
}

func (d *ParallelDownloader) downloadChunk(ctx context.Context, args downloadChunkArgs) error {
	requests, err := d.acquireRequest(ctx)
	if err != nil {
		return err
	}
	defer d.releaseRequest(requests)
	rangeStart := args.Start + args.ResumeBytes
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, args.URL, nil)
	if err != nil {
		return err
	}
	header, err := d.headers(args.URL)
	if err != nil {
		return err
	}
	for key, values := range args.Header {
		header.Del(key)
		for _, value := range values {
			header.Add(key, value)
		}
	}
	header.Set("Range", fmt.Sprintf("bytes=%d-%d", rangeStart, args.End))
	req.Header = header
	response, err := d.client().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusPartialContent {
		_, _ = io.Copy(io.Discard, response.Body)
		return fmt.Errorf("chunk download failed: expected 206 Partial Content, got %s (%d)", response.Status, response.StatusCode)
	}
	if !isExpectedContentRange(response.Header.Get("Content-Range"), rangeStart, args.End, args.FileSize) {
		_, _ = io.Copy(io.Discard, response.Body)
		return &UnexpectedContentRangeError{
			Message: fmt.Sprintf("chunk download returned an unexpected Content-Range for bytes=%d-%d", rangeStart, args.End),
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	if args.ResumeBytes > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	if err := os.MkdirAll(filepath.Dir(args.ChunkPath), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(args.ChunkPath, flags, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	transferred := args.ResumeBytes
	buf := make([]byte, 64*1024)
	for {
		n, readErr := response.Body.Read(buf)
		if n > 0 {
			if args.Limiter != nil {
				if limitErr := args.Limiter.Take(ctx, int64(n), func() {
					if args.OnPhaseChange != nil {
						args.OnPhaseChange(transfer.SlowChunkPhaseBandwidthWait)
					}
				}); limitErr != nil {
					return limitErr
				}
			}
			if args.OnPhaseChange != nil {
				args.OnPhaseChange(transfer.SlowChunkPhaseDiskWrite)
			}
			if _, writeErr := file.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			transferred += int64(n)
			if args.OnProgress != nil {
				args.OnProgress(transferred, int64(n))
			}
			if args.OnPhaseChange != nil {
				args.OnPhaseChange(transfer.SlowChunkPhaseNetwork)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	expected := args.End - args.Start + 1
	if transferred != expected {
		return fmt.Errorf("chunk download ended early: expected %d bytes, got %d", expected, transferred)
	}
	return file.Sync()
}

func (d *ParallelDownloader) removeChunkArtifacts(savePath string) error {
	var first error
	for _, path := range listChunkArtifacts(savePath) {
		if err := d.removePath(path); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func isExpectedContentRange(value string, start, end, fileSize int64) bool {
	match := regexp.MustCompile(`(?i)^bytes\s+(\d+)-(\d+)/(\d+)$`).FindStringSubmatch(value)
	if match == nil {
		return false
	}
	gotStart, _ := strconv.ParseInt(match[1], 10, 64)
	gotEnd, _ := strconv.ParseInt(match[2], 10, 64)
	gotSize, _ := strconv.ParseInt(match[3], 10, 64)
	return gotStart == start && gotEnd == end && gotSize == fileSize
}

func rangeSupportCacheKey(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}

func SafeURLResource(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "invalid-url"
	}
	return parsed.Scheme + "://" + parsed.Host + parsed.Path
}

func readChunkMeta(path string) *chunkMeta {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var parsed any
	if json.Unmarshal(raw, &parsed) != nil {
		return nil
	}
	record, ok := parsed.(map[string]any)
	if !ok {
		return nil
	}
	resource, _ := record["resource"].(string)
	size, sizeOK := record["fileSize"].(float64)
	if resource == "" || !sizeOK {
		return nil
	}
	return &chunkMeta{Resource: resource, FileSize: size}
}

func listDirectoryNames(savePath string) (dir, base string, names []string) {
	dir = filepath.Dir(savePath)
	base = filepath.Base(savePath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return dir, base, nil
	}
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return
}

func isChunkFileName(saveBase, name string) bool {
	prefix := saveBase + ".chunk"
	if !strings.HasPrefix(name, prefix) {
		return false
	}
	rest := name[len(prefix):]
	if rest == "" {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isChunkArtifactName(saveBase, name string) bool {
	return name == saveBase+".chunk-meta.json" || isChunkFileName(saveBase, name)
}

func listChunkFiles(savePath string) []string {
	dir, base, names := listDirectoryNames(savePath)
	var paths []string
	for _, name := range names {
		if isChunkFileName(base, name) {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}

func listChunkArtifacts(savePath string) []string {
	dir, base, names := listDirectoryNames(savePath)
	var paths []string
	for _, name := range names {
		if isChunkArtifactName(base, name) {
			paths = append(paths, filepath.Join(dir, name))
		}
	}
	return paths
}
