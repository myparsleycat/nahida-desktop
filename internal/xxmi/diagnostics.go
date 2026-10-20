package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	diagnosticsSessionsKey  = "xxmi_diagnostic_sessions"
	diagnosticsChangedEvent = "xxmi:diagnostics-changed"
	diagnosticsFolderName   = "diagnostics"

	errDiagnosticsUnsupported = "XXMI_DIAGNOSTICS_UNSUPPORTED"
	errDiagnosticsNotStarted  = "XXMI_DIAGNOSTICS_NOT_STARTED"
)

// The reasons DiagnosticsSupport gives for an importer that cannot run a logging session.
const (
	diagnosticsExternalLauncher   = "external-launcher"
	diagnosticsNotConfigured      = "not-configured"
	diagnosticsRuntime            = "runtime"
	diagnosticsDLLBypassed        = "dll-bypassed"
	diagnosticsCustomDLL          = "custom-dll"
	diagnosticsProvider           = "provider"
	diagnosticsProviderUnresolved = "provider-unresolved"
	diagnosticsNotInstalled       = "not-installed"
)

// diagnosticsRestoredOptions are the [Logging] options a session sets that no importer setting decides, so
// ending the session puts back the values the file had. The settings write the rest of [Logging] again.
var diagnosticsRestoredOptions = []string{"unbuffered", "crash", "debug_locks"}

type DiagnosticsLevel string

const (
	DiagnosticsInfo  DiagnosticsLevel = "info"
	DiagnosticsDebug DiagnosticsLevel = "debug"
)

type DiagnosticsState string

const (
	// DiagnosticsArmed is a session whose options are in d3dx.ini, so every launch logs.
	DiagnosticsArmed DiagnosticsState = "armed"
	// DiagnosticsCollected is a session whose bundle is written and whose options are taken out again.
	DiagnosticsCollected DiagnosticsState = "collected"
)

type DiagnosticsOptions struct {
	Level      DiagnosticsLevel `json:"level"`
	DebugLocks bool             `json:"debugLocks"`
}

type DiagnosticsSupport struct {
	Supported bool   `json:"supported"`
	Reason    string `json:"reason"`
}

// DiagnosticsSession is an importer's logging session. The times are RFC 3339; LaunchedAt is empty until a
// launch logged with the session's options.
type DiagnosticsSession struct {
	State      DiagnosticsState   `json:"state"`
	Level      DiagnosticsLevel   `json:"level"`
	DebugLocks bool               `json:"debugLocks"`
	StartedAt  string             `json:"startedAt"`
	LaunchedAt string             `json:"launchedAt"`
	Bundle     *DiagnosticsBundle `json:"bundle"`
}

type DiagnosticsMod struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Disabled bool   `json:"disabled"`
}

// diagnosticsRecord is the stored state of an importer's logging session.
type diagnosticsRecord struct {
	State      DiagnosticsState `json:"state"`
	Level      DiagnosticsLevel `json:"level"`
	DebugLocks bool             `json:"debugLocks"`
	StartedAt  time.Time        `json:"startedAt"`
	LaunchedAt time.Time        `json:"launchedAt,omitzero"`
	// ImporterFolder is the folder whose d3dx.ini the session changed and Restore was read from.
	ImporterFolder string `json:"importerFolder"`
	// Restore holds the diagnosticsRestoredOptions values the file had before the session; nil marks one it
	// did not set.
	Restore map[string]*string `json:"restore"`
	// DumpDirs are the folders a crash dump of a launch may land in: 3DMigoto writes it to the working
	// directory of the game process.
	DumpDirs []string           `json:"dumpDirs,omitempty"`
	Bundle   *DiagnosticsBundle `json:"bundle,omitempty"`
}

func (r diagnosticsRecord) session() *DiagnosticsSession {
	session := &DiagnosticsSession{
		State: r.State, Level: r.Level, DebugLocks: r.DebugLocks,
		StartedAt: r.StartedAt.Format(time.RFC3339), Bundle: r.Bundle,
	}
	if !r.LaunchedAt.IsZero() {
		session.LaunchedAt = r.LaunchedAt.Format(time.RFC3339)
	}
	return session
}

// DiagnosticsSupport reports whether the importer can run a logging session: the built-in launcher deploys
// a d3d11.dll of a provider whose logs this app collects.
func (x *XXMI) DiagnosticsSupport(ctx context.Context, importer string) (DiagnosticsSupport, error) {
	support, _, err := x.diagnosticsSupport(ctx, strings.ToUpper(strings.TrimSpace(importer)))
	return support, err
}

func (x *XXMI) diagnosticsSupport(ctx context.Context, key string) (DiagnosticsSupport, ImporterConfig, error) {
	external, err := x.usesExternalLauncher(ctx)
	if err != nil {
		return DiagnosticsSupport{}, ImporterConfig{}, err
	}
	if external {
		return DiagnosticsSupport{Reason: diagnosticsExternalLauncher}, ImporterConfig{}, nil
	}
	cfg, err := x.GetImporterConfig(ctx, key)
	if err != nil {
		return DiagnosticsSupport{}, cfg, err
	}
	switch {
	case !cfg.Enabled:
		return DiagnosticsSupport{Reason: diagnosticsNotConfigured}, cfg, nil
	case cfg.Mode != RuntimeXXMI:
		return DiagnosticsSupport{Reason: diagnosticsRuntime}, cfg, nil
	}
	used, err := x.migotoDLLUsed(ctx, cfg)
	if err != nil {
		return DiagnosticsSupport{}, cfg, err
	}
	if !used {
		return DiagnosticsSupport{Reason: diagnosticsDLLBypassed}, cfg, nil
	}
	support := x.dllFeatureSupport(ctx, cfg, func(spec libsProviderSpec) bool { return spec.diagnostics })
	if !support.supported {
		reason := diagnosticsProvider
		switch support.reason {
		case dllFeatureCustomDLL:
			reason = diagnosticsCustomDLL
		case dllFeatureProviderUnresolved:
			reason = diagnosticsProviderUnresolved
		}
		return DiagnosticsSupport{Reason: reason}, cfg, nil
	}
	if _, err := os.Stat(filepath.Join(cfg.ImporterFolder, "d3dx.ini")); err != nil {
		return DiagnosticsSupport{Reason: diagnosticsNotInstalled}, cfg, nil //nolint:nilerr // Reported as a reason.
	}
	return DiagnosticsSupport{Supported: true}, cfg, nil
}

// GetDiagnostics returns the importer's logging session, or nil when it has none.
func (x *XXMI) GetDiagnostics(ctx context.Context, importer string) (*DiagnosticsSession, error) {
	key := strings.ToUpper(strings.TrimSpace(importer))
	x.diagnosticsMu.Lock()
	defer x.diagnosticsMu.Unlock()
	records, err := x.loadDiagnostics(ctx)
	if err != nil || records[key] == nil {
		return nil, err
	}
	return records[key].session(), nil
}

// StartDiagnostics writes the logging options into the importer's d3dx.ini, where every launch reads them
// until the session is collected or ended. Starting again over a running session changes its options, and
// over a collected one drops that bundle.
func (x *XXMI) StartDiagnostics(ctx context.Context, importer string, options DiagnosticsOptions) (returnErr error) {
	key := strings.ToUpper(strings.TrimSpace(importer))
	stage := "validate"
	cfg := ImporterConfig{}
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.StartDiagnostics", infra.Diagnostic{
			Operation: "start-diagnostics", Stage: stage,
			Fields: map[string]any{
				"importer": key, "level": options.Level, "debugLocks": options.DebugLocks,
				"importerFolder": cfg.ImporterFolder,
			},
		})
	}()
	if options.Level != DiagnosticsInfo && options.Level != DiagnosticsDebug {
		return fmt.Errorf("unknown diagnostics level %q", options.Level)
	}

	stage = "support"
	support, cfg, err := x.diagnosticsSupport(ctx, key)
	if err != nil {
		return err
	}
	if !support.Supported {
		return infra.ContractError(errDiagnosticsUnsupported + ":" + support.Reason)
	}
	if !x.acquireImporter(key) {
		return errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(key)
	x.diagnosticsMu.Lock()
	defer x.diagnosticsMu.Unlock()

	stage = "read-session"
	records, err := x.loadDiagnostics(ctx)
	if err != nil {
		return err
	}
	// A collected session and its bundle are kept until the new session replaces it, so a start that fails
	// leaves the report still to be sent.
	record, fresh := records[key], false
	var collected *diagnosticsRecord
	if record != nil && record.State == DiagnosticsCollected {
		collected, record = record, nil
	}
	// A session left on a folder the importer moved away from gives that folder its values back and starts
	// over on the new one, whose own values it has not read.
	if record != nil && !record.targets(cfg.ImporterFolder) {
		stage = "restore-moved-ini"
		err := restoreDiagnosticsINI(ctx, key, record.config(cfg), *record)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		record = nil
	}
	if record == nil {
		record, fresh = &diagnosticsRecord{
			State: DiagnosticsArmed, StartedAt: time.Now(), ImporterFolder: cfg.ImporterFolder,
		}, true
	}
	// A write that fails puts back the stored session it would have replaced or changed.
	rollback := collected
	if !fresh {
		saved := *record
		rollback = &saved
	}
	record.Level, record.DebugLocks = options.Level, options.DebugLocks

	// The session is stored before the file changes, so a write that fails or a crash after it leaves a
	// session whose end restores the values the file had.
	stage = "write-ini"
	_, logLevel := migotoINISupport(cfg)
	_, err = editImporterINI(ctx, cfg.ImporterFolder, func(doc *iniDocument) error {
		if fresh {
			record.Restore = currentDiagnosticsOptions(doc)
		}
		records[key] = record
		if err := x.storeDiagnostics(ctx, records); err != nil {
			return err
		}
		applyDiagnosticsINI(doc, *record, logLevel)
		return nil
	})
	if err != nil {
		if rollback != nil {
			records[key] = rollback
		} else {
			delete(records, key)
		}
		x.reportCleanup(x.storeDiagnostics(context.WithoutCancel(ctx), records), "start-diagnostics-rollback")
	}
	if err != nil {
		return err
	}
	if collected != nil {
		x.removeDiagnosticsBundle(collected)
	}
	x.emitDiagnosticsChanged(key)
	return nil
}

// EndDiagnostics takes the session's options out of d3dx.ini while they are still in it and forgets the
// session along with its bundle.
func (x *XXMI) EndDiagnostics(ctx context.Context, importer string) (returnErr error) {
	key := strings.ToUpper(strings.TrimSpace(importer))
	stage := "acquire"
	defer func() {
		returnErr = infra.ReportError(x.log, returnErr, "XXMI.EndDiagnostics", infra.Diagnostic{
			Operation: "end-diagnostics", Stage: stage, Fields: map[string]any{"importer": key},
		})
	}()
	if !x.acquireImporter(key) {
		return errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(key)
	x.diagnosticsMu.Lock()
	defer x.diagnosticsMu.Unlock()

	stage = "read-session"
	records, err := x.loadDiagnostics(ctx)
	if err != nil {
		return err
	}
	record := records[key]
	if record == nil {
		return nil
	}
	if record.State == DiagnosticsArmed {
		stage = "restore-ini"
		cfg, err := x.GetImporterConfig(ctx, key)
		if err != nil {
			return err
		}
		// An importer folder that is gone has no options left to take out.
		if err := restoreDiagnosticsINI(ctx, key, record.config(cfg), *record); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	stage = "store"
	delete(records, key)
	if err := x.storeDiagnostics(ctx, records); err != nil {
		return err
	}
	x.removeDiagnosticsBundle(record)
	x.emitDiagnosticsChanged(key)
	return nil
}

// ListDiagnosticsMods lists the folders of the importer's Mods folder a report can attach, the enabled ones
// first.
func (x *XXMI) ListDiagnosticsMods(ctx context.Context, importer string) ([]DiagnosticsMod, error) {
	cfg, err := x.GetImporterConfig(ctx, strings.ToUpper(strings.TrimSpace(importer)))
	if err != nil {
		return nil, err
	}
	mods := []DiagnosticsMod{}
	if cfg.ImporterFolder == "" {
		return mods, nil
	}
	folder := filepath.Join(cfg.ImporterFolder, "Mods")
	entries, err := os.ReadDir(folder)
	if errors.Is(err, os.ErrNotExist) {
		return mods, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		path := filepath.Join(folder, entry.Name())
		// A junction reads as an irregular entry, so the folder is told by its target.
		if info, err := os.Stat(path); err != nil || !info.IsDir() {
			continue
		}
		// The report walks the folder it is given, which does not step into a junction.
		final, err := platform.FinalPath(path)
		if err != nil {
			continue
		}
		mods = append(mods, DiagnosticsMod{
			Name: entry.Name(), Path: final,
			Disabled: strings.HasPrefix(strings.ToUpper(entry.Name()), "DISABLED"),
		})
	}
	less := platform.NewLocaleLess()
	slices.SortFunc(mods, func(a, b DiagnosticsMod) int {
		switch {
		case a.Disabled != b.Disabled && a.Disabled:
			return 1
		case a.Disabled != b.Disabled:
			return -1
		case less(a.Name, b.Name):
			return -1
		case less(b.Name, a.Name):
			return 1
		}
		return 0
	})
	return mods, nil
}

// armedDiagnostics returns the importer's session while its options belong in the d3dx.ini of folder, or nil.
func (x *XXMI) armedDiagnostics(ctx context.Context, key, folder string) (*diagnosticsRecord, error) {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil, nil
	}
	x.diagnosticsMu.Lock()
	defer x.diagnosticsMu.Unlock()
	records, err := x.loadDiagnostics(ctx)
	if err != nil {
		return nil, err
	}
	if record := records[key]; record != nil && record.State == DiagnosticsArmed && record.targets(folder) {
		return record, nil
	}
	return nil, nil
}

// endMovedDiagnostics ends the importer's running session when the importer moved to another folder: the
// session's folder gets its values back, and the new folder never had the session's options.
func (x *XXMI) endMovedDiagnostics(ctx context.Context, key string, cfg ImporterConfig) error {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil
	}
	x.diagnosticsMu.Lock()
	defer x.diagnosticsMu.Unlock()
	records, err := x.loadDiagnostics(ctx)
	if err != nil {
		return err
	}
	record := records[key]
	if record == nil || record.State != DiagnosticsArmed || record.targets(cfg.ImporterFolder) {
		return nil
	}
	err = restoreDiagnosticsINI(ctx, key, record.config(cfg), *record)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	delete(records, key)
	if err := x.storeDiagnostics(ctx, records); err != nil {
		return err
	}
	x.removeDiagnosticsBundle(record)
	x.emitDiagnosticsChanged(key)
	return nil
}

func (r diagnosticsRecord) targets(folder string) bool {
	return platform.SamePathFold(r.ImporterFolder, folder)
}

// config returns cfg pointed at the folder the session changed, which the importer may have left since.
func (r diagnosticsRecord) config(cfg ImporterConfig) ImporterConfig {
	cfg.ImporterFolder = r.ImporterFolder
	return cfg
}

// recordDiagnosticsLaunch notes that a launch logged with the session's options and where the game may
// write a crash dump.
func (x *XXMI) recordDiagnosticsLaunch(ctx context.Context, key string, dumpDirs ...string) error {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return nil
	}
	x.diagnosticsMu.Lock()
	defer x.diagnosticsMu.Unlock()
	records, err := x.loadDiagnostics(ctx)
	if err != nil {
		return err
	}
	record := records[key]
	if record == nil || record.State != DiagnosticsArmed {
		return nil
	}
	record.LaunchedAt = time.Now()
	for _, dir := range dumpDirs {
		if dir != "" && !slices.ContainsFunc(record.DumpDirs, func(known string) bool {
			return platform.SamePathFold(known, dir)
		}) {
			record.DumpDirs = append(record.DumpDirs, dir)
		}
	}
	if err := x.storeDiagnostics(ctx, records); err != nil {
		return err
	}
	x.emitDiagnosticsChanged(key)
	return nil
}

// loadDiagnostics returns the stored sessions by importer. The caller must hold diagnosticsMu.
func (x *XXMI) loadDiagnostics(ctx context.Context) (map[string]*diagnosticsRecord, error) {
	client, err := x.settingsClient()
	if err != nil {
		return nil, err
	}
	stored, err := client.Settings.GetValue(ctx, diagnosticsSessionsKey)
	if err != nil {
		return nil, err
	}
	records := map[string]*diagnosticsRecord{}
	if stored == nil {
		return records, nil
	}
	if err := json.Unmarshal([]byte(*stored), &records); err != nil {
		return nil, fmt.Errorf("decode diagnostics sessions: %w", err)
	}
	if records == nil {
		records = map[string]*diagnosticsRecord{}
	}
	return records, nil
}

// storeDiagnostics replaces the stored sessions. The caller must hold diagnosticsMu.
func (x *XXMI) storeDiagnostics(ctx context.Context, records map[string]*diagnosticsRecord) error {
	client, err := x.settingsClient()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return client.Settings.Delete(ctx, diagnosticsSessionsKey)
	}
	encoded, err := json.Marshal(records)
	if err != nil {
		return err
	}
	value := string(encoded)
	return client.Settings.Upsert(ctx, diagnosticsSessionsKey, &value)
}

// storeDiagnosticsRecord replaces the importer's stored session. The other sessions are read again, so what
// another importer stored meanwhile stays.
func (x *XXMI) storeDiagnosticsRecord(ctx context.Context, key string, record *diagnosticsRecord) error {
	x.diagnosticsMu.Lock()
	defer x.diagnosticsMu.Unlock()
	records, err := x.loadDiagnostics(ctx)
	if err != nil {
		return err
	}
	records[key] = record
	return x.storeDiagnostics(ctx, records)
}

func (x *XXMI) emitDiagnosticsChanged(key string) {
	if x.eventEmit != nil {
		x.eventEmit(diagnosticsChangedEvent, map[string]any{"importer": key})
	}
}

// removeDiagnosticsBundle deletes a session's bundle. The stored path is only followed inside the folder
// bundles are written to.
func (x *XXMI) removeDiagnosticsBundle(record *diagnosticsRecord) {
	if record.Bundle == nil || record.Bundle.Path == "" {
		return
	}
	folder, err := diagnosticsFolder()
	if err != nil || !platform.SameOrChildPath(folder, record.Bundle.Path) ||
		platform.SamePathFold(folder, record.Bundle.Path) {
		return
	}
	x.reportCleanup(os.Remove(record.Bundle.Path), "remove-diagnostics-bundle")
}

func diagnosticsFolder() (string, error) {
	root, err := xxmiCacheRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, diagnosticsFolderName), nil
}

// currentDiagnosticsOptions reads the values of diagnosticsRestoredOptions the file sets.
func currentDiagnosticsOptions(doc *iniDocument) map[string]*string {
	values := make(map[string]*string, len(diagnosticsRestoredOptions))
	for _, option := range diagnosticsRestoredOptions {
		if value, ok := doc.Option("Logging", option); ok {
			values[option] = &value
		} else {
			values[option] = nil
		}
	}
	return values
}

// applyDiagnosticsINI writes the session's logging options. The log is unbuffered so a crash does not lose
// its end, and the crash handler writes a minidump. logLevel is whether the DLL reads log_level.
func applyDiagnosticsINI(doc *iniDocument, record diagnosticsRecord, logLevel bool) {
	if logLevel {
		doc.SetOption("Logging", "log_level", string(record.Level), true)
	} else {
		doc.SetOption("Logging", "calls", "1", true)
		debug := "0"
		if record.Level == DiagnosticsDebug {
			debug = "1"
		}
		doc.SetOption("Logging", "debug", debug, true)
	}
	doc.SetOption("Logging", "unbuffered", "1", true)
	doc.SetOption("Logging", "crash", "1", true)
	if record.DebugLocks {
		doc.SetOption("Logging", "debug_locks", "1", true)
	} else {
		restoreDiagnosticsOption(doc, "debug_locks", record.Restore)
	}
}

func restoreDiagnosticsOption(doc *iniDocument, option string, restore map[string]*string) {
	if value := restore[option]; value != nil {
		doc.SetOption("Logging", option, *value, true)
		return
	}
	doc.RemoveOption("Logging", option)
}

// restoreDiagnosticsINI puts back the values the file had before the session, then the options the
// importer settings decide, which also returns the log level to the setting.
func restoreDiagnosticsINI(ctx context.Context, key string, cfg ImporterConfig, record diagnosticsRecord) error {
	_, err := editImporterINI(ctx, cfg.ImporterFolder, func(doc *iniDocument) error {
		for _, option := range diagnosticsRestoredOptions {
			restoreDiagnosticsOption(doc, option, record.Restore)
		}
		applySettingsINI(doc, key, cfg, true)
		return nil
	})
	return err
}

// editImporterINI edits the importer's d3dx.ini in place and returns the bytes it read. The file is written
// only when the edit changed it.
func editImporterINI(ctx context.Context, folder string, edit func(*iniDocument) error) ([]byte, error) {
	root, err := openInstallRoot(folder)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	iniRoot, iniName, err := root.resolveUserFile("d3dx.ini")
	if err != nil {
		return nil, err
	}
	if iniRoot == nil {
		iniRoot = root
	} else {
		defer func() { _ = iniRoot.Close() }()
	}
	data, info, err := iniRoot.readFile(iniName)
	if err != nil {
		return nil, err
	}

	doc := parseINI(data)
	if err := edit(doc); err != nil {
		return data, err
	}
	if !doc.Changed() {
		return data, nil
	}
	return data, iniRoot.writeFileAtomic(ctx, iniName, bytes.NewReader(doc.Bytes()), 0o600, info)
}
