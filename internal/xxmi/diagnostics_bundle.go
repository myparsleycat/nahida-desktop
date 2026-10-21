package xxmi

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	errDiagnosticsEmpty    = "XXMI_DIAGNOSTICS_EMPTY"
	errDiagnosticsTooLarge = "XXMI_DIAGNOSTICS_TOO_LARGE"

	diagnosticsLogName = "d3d11_log.txt"
	// A log over head and tail together keeps its start, where the DLL lists its settings, and the end that
	// leads up to the problem. The tail shrinks while the bundle exceeds the upload bound.
	diagnosticsLogHead    = 4 << 20
	diagnosticsLogTail    = 96 << 20
	diagnosticsLogMinTail = 4 << 20
	diagnosticsMaxDumps   = 2
	// diagnosticsDumpLimit leaves out a dump no report could carry anyway.
	diagnosticsDumpLimit = 512 << 20
	// diagnosticsAnonymizeChunk bounds the text anonymized at once, which is cut at line ends.
	diagnosticsAnonymizeChunk = 1 << 20
)

// DiagnosticsBundle is the zip a collected session wrote for a report.
type DiagnosticsBundle struct {
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	LogBytes     int64  `json:"logBytes"`
	LogTruncated bool   `json:"logTruncated"`
	Dumps        int    `json:"dumps"`
	CrashEvents  int    `json:"crashEvents"`
}

type diagnosticsFile struct {
	path     string
	size     int64
	modified time.Time
}

// diagnosticsSources are the files and records of a session that go into its bundle.
type diagnosticsSources struct {
	log         *diagnosticsFile
	dumps       []diagnosticsFile
	ini         []byte
	events      string
	eventCount  int
	runtime     runtimeManifest
	dllSHA256   string
	dllSize     int64
	collectedAt time.Time
}

// CollectDiagnostics writes the log, crash dumps, d3dx.ini, and crash records of the session into a zip of
// at most maxBytes for a report, and then takes the session's options out of d3dx.ini. A collected session
// returns the bundle it already wrote.
func (x *XXMI) CollectDiagnostics(
	ctx context.Context,
	importer string,
	maxBytes int64,
) (bundle DiagnosticsBundle, returnErr error) {
	key := strings.ToUpper(strings.TrimSpace(importer))
	stage := "validate"
	cfg := ImporterConfig{}
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.CollectDiagnostics", infra.Diagnostic{
			Operation: "collect-diagnostics", Stage: stage,
			Fields: map[string]any{
				"importer": key, "maxBytes": maxBytes, "importerFolder": cfg.ImporterFolder, "bundle": bundle.Path,
			},
		})
	}()
	if maxBytes <= 0 {
		return bundle, fmt.Errorf("invalid bundle size bound %d", maxBytes)
	}
	if !x.acquireImporter(key) {
		return bundle, errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(key)

	// Holding the importer keeps its session as read here, so the stored sessions are only locked to read and
	// store it, and a long collection does not hold up the launch of another importer.
	stage = "read-session"
	x.diagnosticsMu.Lock()
	records, err := x.loadDiagnostics(ctx)
	x.diagnosticsMu.Unlock()
	if err != nil {
		return bundle, err
	}
	record := records[key]
	if record == nil {
		return bundle, errors.New(errDiagnosticsNotStarted)
	}
	if record.State == DiagnosticsCollected && record.Bundle != nil {
		if _, err := os.Stat(record.Bundle.Path); err == nil {
			return *record.Bundle, nil
		}
		return bundle, errors.New(errDiagnosticsNotStarted)
	}
	cfg, err = x.GetImporterConfig(ctx, key)
	if err != nil {
		return bundle, err
	}
	cfg = record.config(cfg)

	// The game holds its log open and writes to it until it exits.
	stage = "check-running"
	running, err := x.runningImporters(ctx, map[string][]string{key: cfg.gameProcessNames(key)})
	if err != nil {
		return bundle, err
	}
	if running[key] {
		return bundle, errors.New("XXMI_GAME_RUNNING")
	}

	stage = "collect"
	sources, err := x.diagnosticsSources(ctx, key, cfg, *record)
	if err != nil {
		return bundle, err
	}
	if sources.log == nil && len(sources.dumps) == 0 {
		return bundle, errors.New(errDiagnosticsEmpty)
	}

	stage = "write-bundle"
	folder, err := diagnosticsFolder()
	if err != nil {
		return bundle, err
	}
	name := key + "-" + sources.collectedAt.Format("20060102-150405") + ".zip"
	bundle, err = writeDiagnosticsBundle(ctx, filepath.Join(folder, name), key, *record, sources, maxBytes)
	if err != nil {
		return bundle, err
	}

	// The options come out as soon as the logs are safe, so a report that is never sent does not leave the
	// game logging.
	stage = "restore-ini"
	if err := restoreDiagnosticsINI(ctx, key, cfg, *record); err != nil {
		x.reportCleanup(os.Remove(bundle.Path), "collect-diagnostics-rollback")
		return bundle, err
	}

	stage = "store"
	record.State, record.Bundle = DiagnosticsCollected, &bundle
	if err := x.storeDiagnosticsRecord(ctx, key, record); err != nil {
		return bundle, err
	}
	x.emitDiagnosticsChanged(key)
	return bundle, nil
}

func (x *XXMI) diagnosticsSources(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	record diagnosticsRecord,
) (diagnosticsSources, error) {
	sources := diagnosticsSources{collectedAt: time.Now()}

	// d3d11_log.txt is created again by each launch that logs, so one older than the session is not its own.
	logPath := filepath.Join(cfg.ImporterFolder, diagnosticsLogName)
	if info, err := os.Stat(logPath); err == nil && info.Mode().IsRegular() && info.Size() > 0 &&
		!info.ModTime().Before(record.StartedAt) {
		sources.log = &diagnosticsFile{path: logPath, size: info.Size(), modified: info.ModTime()}
	}
	sources.dumps = sessionDumps(slices.Concat(record.DumpDirs, []string{cfg.ImporterFolder}), record.StartedAt)

	ini, err := readImporterINI(cfg.ImporterFolder)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return sources, err
	}
	sources.ini = ini
	if data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName)); err == nil {
		_ = json.Unmarshal(data, &sources.runtime)
	}
	dll := filepath.Join(cfg.ImporterFolder, customDLLName)
	if info, err := os.Stat(dll); err == nil && info.Mode().IsRegular() && info.Size() <= customDLLSizeLimit {
		if data, err := os.ReadFile(dll); err == nil {
			sources.dllSHA256, sources.dllSize = hashBytes(data), int64(len(data))
		}
	}

	// The crash records name the faulting module even when the DLL's own handler never ran, but a report
	// without them still has the log.
	if x.crashEvents != nil {
		spec, _ := lookupImporterPackage(key)
		events, count, err := x.crashEvents(ctx, record.StartedAt, slices.Concat(spec.gameExeNames, spec.processNames))
		if err != nil && infra.IsCancellationError(err) {
			return sources, err
		}
		if err != nil {
			_ = infra.ReportError(x.log, err, "XXMI.CollectDiagnostics", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "collect-diagnostics", Stage: "crash-events",
				Fields: map[string]any{"importer": key},
			})
		}
		sources.events, sources.eventCount = events, count
	}
	return sources, nil
}

// sessionDumps returns the newest 3DMigoto crash dumps written to dirs since the session started.
func sessionDumps(dirs []string, since time.Time) []diagnosticsFile {
	var dumps []diagnosticsFile
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			name := strings.ToLower(entry.Name())
			if !entry.Type().IsRegular() || !strings.HasPrefix(name, "3dm-") || !strings.HasSuffix(name, ".dmp") {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.ModTime().Before(since) || info.Size() == 0 || info.Size() > diagnosticsDumpLimit {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if slices.ContainsFunc(
				dumps,
				func(dump diagnosticsFile) bool { return platform.SamePathFold(dump.path, path) },
			) {
				continue
			}
			dumps = append(dumps, diagnosticsFile{path: path, size: info.Size(), modified: info.ModTime()})
		}
	}
	slices.SortFunc(dumps, func(a, b diagnosticsFile) int { return b.modified.Compare(a.modified) })
	return dumps[:min(len(dumps), diagnosticsMaxDumps)]
}

// writeDiagnosticsBundle writes the bundle to destination. A bundle over maxBytes is written again with less
// of the log's end, then with only the newest dump, before it is refused.
func writeDiagnosticsBundle(
	ctx context.Context,
	destination, key string,
	record diagnosticsRecord,
	sources diagnosticsSources,
	maxBytes int64,
) (DiagnosticsBundle, error) {
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return DiagnosticsBundle{}, err
	}
	paths := []string{destination}
	if sources.log != nil {
		paths = append(paths, sources.log.path)
	}
	for _, dump := range sources.dumps {
		paths = append(paths, dump.path)
	}
	release, err := diskio.Acquire(ctx, paths...)
	if err != nil {
		return DiagnosticsBundle{}, err
	}
	defer release()

	tail := int64(diagnosticsLogTail)
	for {
		temporary, bundle, err := writeDiagnosticsZip(ctx, filepath.Dir(destination), key, record, sources, tail)
		if err != nil {
			return DiagnosticsBundle{}, err
		}
		if bundle.Size <= maxBytes {
			if err := os.Rename(temporary, destination); err != nil {
				return DiagnosticsBundle{}, errors.Join(err, os.Remove(temporary))
			}
			bundle.Path = destination
			return bundle, nil
		}
		if err := os.Remove(temporary); err != nil {
			return DiagnosticsBundle{}, err
		}

		switch {
		case sources.log != nil && tail > diagnosticsLogMinTail &&
			sources.log.size > diagnosticsLogHead+diagnosticsLogMinTail:
			tail = max(min(tail, sources.log.size-diagnosticsLogHead)/2, diagnosticsLogMinTail)
		case len(sources.dumps) > 1:
			sources.dumps = sources.dumps[:1]
		default:
			return DiagnosticsBundle{}, infra.ContractError(fmt.Sprintf("%s:%d", errDiagnosticsTooLarge, bundle.Size))
		}
	}
}

func writeDiagnosticsZip(
	ctx context.Context,
	folder, key string,
	record diagnosticsRecord,
	sources diagnosticsSources,
	tail int64,
) (_ string, bundle DiagnosticsBundle, returnErr error) {
	file, err := os.CreateTemp(folder, key+"-*.zip.tmp")
	if err != nil {
		return "", bundle, err
	}
	temporary := file.Name()
	defer func() {
		if returnErr != nil {
			_ = file.Close()
			_ = os.Remove(temporary)
		}
	}()

	archive := zip.NewWriter(file)
	manifest := diagnosticsManifestFor(key, record, sources)
	if sources.log != nil {
		entry, err := archive.CreateHeader(&zip.FileHeader{
			Name: diagnosticsLogName, Method: zip.Deflate, Modified: sources.log.modified,
		})
		if err != nil {
			return "", bundle, err
		}
		written, omitted, err := copyLogWindow(ctx, entry, sources.log.path, diagnosticsLogHead, tail)
		if err != nil {
			return "", bundle, fmt.Errorf("add %s: %w", sources.log.path, err)
		}
		bundle.LogBytes, bundle.LogTruncated = written, omitted > 0
		manifest.Log = &diagnosticsManifestLog{Bytes: sources.log.size, OmittedBytes: omitted}
	}
	for _, dump := range sources.dumps {
		name := "dumps/" + filepath.Base(dump.path)
		entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: dump.modified})
		if err != nil {
			return "", bundle, err
		}
		if err := copyFile(ctx, entry, dump.path); err != nil {
			return "", bundle, fmt.Errorf("add %s: %w", dump.path, err)
		}
		manifest.Dumps = append(manifest.Dumps, diagnosticsManifestFile{
			Name: name, Size: dump.size, Modified: dump.modified.Format(time.RFC3339),
		})
	}
	bundle.Dumps = len(sources.dumps)
	if len(sources.ini) > 0 {
		if err := addZipText(archive, "d3dx.ini", infra.AnonymizeLogText(string(sources.ini))); err != nil {
			return "", bundle, err
		}
	}
	if sources.events != "" {
		if err := addZipText(archive, "windows-events.txt", infra.AnonymizeLogText(sources.events)); err != nil {
			return "", bundle, err
		}
	}
	bundle.CrashEvents = sources.eventCount
	encoded, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", bundle, err
	}
	if err := addZipText(archive, "session.json", infra.AnonymizeLogText(string(encoded))); err != nil {
		return "", bundle, err
	}

	if err := errors.Join(archive.Close(), file.Sync()); err != nil {
		return "", bundle, err
	}
	info, err := file.Stat()
	if err != nil {
		return "", bundle, err
	}
	if err := file.Close(); err != nil {
		return "", bundle, err
	}
	bundle.Size = info.Size()
	return temporary, bundle, nil
}

func addZipText(archive *zip.Writer, name, text string) error {
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.WriteString(entry, text)
	return err
}

func copyFile(ctx context.Context, w io.Writer, path string) error {
	source, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = source.Close() }()
	_, err = io.Copy(w, &contextReader{ctx: ctx, reader: source})
	return err
}

// copyLogWindow writes the anonymized log, or for one over head and tail together its first head bytes and
// last tail bytes cut at line ends, with a line marking what was left out. It returns the bytes of the log it
// read and the bytes it left out.
func copyLogWindow(ctx context.Context, w io.Writer, path string, head, tail int64) (int64, int64, error) {
	source, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = source.Close() }()
	info, err := source.Stat()
	if err != nil {
		return 0, 0, err
	}
	reader := &contextReader{ctx: ctx, reader: source}
	if info.Size() <= head+tail {
		written, err := copyAnonymized(w, reader)
		return written, 0, err
	}

	start := make([]byte, head)
	if _, err := io.ReadFull(reader, start); err != nil {
		return 0, 0, err
	}
	if end := bytes.LastIndexByte(start, '\n'); end >= 0 {
		start = start[:end+1]
	}
	if _, err := io.WriteString(w, infra.AnonymizeLogText(string(start))); err != nil {
		return 0, 0, err
	}

	if _, err := source.Seek(info.Size()-tail, io.SeekStart); err != nil {
		return 0, 0, err
	}
	rest := bufio.NewReaderSize(reader, 64<<10)
	skipped, err := rest.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, 0, err
	}
	kept := tail - int64(len(skipped))
	omitted := info.Size() - int64(len(start)) - kept
	if _, err := fmt.Fprintf(w, "\n... %d bytes of the log left out by Nahida Desktop ...\n\n", omitted); err != nil {
		return 0, 0, err
	}
	written, err := copyAnonymized(w, rest)
	return int64(len(start)) + written, omitted, err
}

// copyAnonymized anonymizes text in chunks cut at line ends, so a large log is never held whole.
func copyAnonymized(w io.Writer, r io.Reader) (int64, error) {
	reader := bufio.NewReaderSize(r, 64<<10)
	var chunk bytes.Buffer
	var read int64
	flush := func() error {
		if chunk.Len() == 0 {
			return nil
		}
		_, err := io.WriteString(w, infra.AnonymizeLogText(chunk.String()))
		chunk.Reset()
		return err
	}
	for {
		line, err := reader.ReadSlice('\n')
		read += int64(len(line))
		chunk.Write(line)
		if errors.Is(err, bufio.ErrBufferFull) {
			err = nil
		}
		if chunk.Len() >= diagnosticsAnonymizeChunk && len(line) > 0 && line[len(line)-1] == '\n' {
			if flushErr := flush(); flushErr != nil {
				return read, flushErr
			}
		}
		if errors.Is(err, io.EOF) {
			return read, flush()
		}
		if err != nil {
			return read, err
		}
	}
}

type diagnosticsManifest struct {
	Importer        string                    `json:"importer"`
	Level           DiagnosticsLevel          `json:"level"`
	DebugLocks      bool                      `json:"debugLocks"`
	StartedAt       string                    `json:"startedAt"`
	LaunchedAt      string                    `json:"launchedAt,omitempty"`
	CollectedAt     string                    `json:"collectedAt"`
	AppVersion      string                    `json:"appVersion"`
	RuntimeSource   string                    `json:"runtimeSource,omitempty"`
	RuntimeProvider string                    `json:"runtimeProvider,omitempty"`
	DeployedAt      string                    `json:"deployedAt,omitempty"`
	D3D11SHA256     string                    `json:"d3d11Sha256,omitempty"`
	D3D11Size       int64                     `json:"d3d11Size,omitempty"`
	Log             *diagnosticsManifestLog   `json:"log,omitempty"`
	Dumps           []diagnosticsManifestFile `json:"dumps,omitempty"`
	CrashEvents     int                       `json:"crashEvents"`
}

type diagnosticsManifestLog struct {
	Bytes        int64 `json:"bytes"`
	OmittedBytes int64 `json:"omittedBytes"`
}

type diagnosticsManifestFile struct {
	Name     string `json:"name"`
	Size     int64  `json:"size"`
	Modified string `json:"modified"`
}

func diagnosticsManifestFor(key string, record diagnosticsRecord, sources diagnosticsSources) diagnosticsManifest {
	manifest := diagnosticsManifest{
		Importer: key, Level: record.Level, DebugLocks: record.DebugLocks,
		StartedAt:     record.StartedAt.Format(time.RFC3339),
		CollectedAt:   sources.collectedAt.Format(time.RFC3339),
		AppVersion:    platform.AppVersion,
		RuntimeSource: sources.runtime.Source, RuntimeProvider: sources.runtime.Provider,
		DeployedAt:  sources.runtime.DeployedAt,
		D3D11SHA256: sources.dllSHA256, D3D11Size: sources.dllSize,
		CrashEvents: sources.eventCount,
	}
	if !record.LaunchedAt.IsZero() {
		manifest.LaunchedAt = record.LaunchedAt.Format(time.RFC3339)
	}
	return manifest
}
