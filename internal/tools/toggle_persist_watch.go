package tools

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"nahida.live/desktop/internal/watcher"
)

type persistImporter struct {
	Key    string
	Folder string
}

type persistTimer struct {
	due       int64
	fn        func()
	cancelled bool
	real      *time.Timer
}

type parsedD3dx struct {
	order  []string
	values map[string]string
}

type persistEngine struct {
	mu         sync.Mutex
	learner    *TogglePersistLearner
	logs       []string
	generation int
	cached     map[string]parsedD3dx
	revisions  map[string]int
	now        int64
	useFake    bool
	timers     []*persistTimer
	flushDue   map[string]*persistTimer
	unwatch    []func()
	infoFn     func(string)
	errorFn    func(string)
	emit       func([]string)
}

func newPersistEngine() *persistEngine {
	return &persistEngine{
		learner:   newTogglePersistLearner(),
		cached:    map[string]parsedD3dx{},
		revisions: map[string]int{},
		flushDue:  map[string]*persistTimer{},
	}
}

func (e *persistEngine) GetLogs() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string{}, e.logs...)
}

func (e *persistEngine) PersistStateToINI(targetINIPath string, state map[string]any) (PersistModelViewerResult, error) {
	updates := persistUpdatesFromState(state)
	if len(updates) == 0 {
		return PersistModelViewerResult{UpdatedVariables: []string{}}, nil
	}
	updated, err := applyPersistUpdates(targetINIPath, updates)
	if err != nil {
		return PersistModelViewerResult{}, err
	}
	return PersistModelViewerResult{UpdatedVariables: updated}, nil
}

func (e *persistEngine) Stop() {
	e.mu.Lock()
	e.generation++
	unwatch := e.unwatch
	e.unwatch = nil
	e.cached = map[string]parsedD3dx{}
	e.learner.Clear()
	for _, timer := range e.timers {
		timer.cancelled = true
		if timer.real != nil {
			timer.real.Stop()
		}
	}
	e.timers = nil
	for _, timer := range e.flushDue {
		timer.cancelled = true
		if timer.real != nil {
			timer.real.Stop()
		}
	}
	e.flushDue = map[string]*persistTimer{}
	count := len(unwatch)
	e.mu.Unlock()
	for _, stop := range unwatch {
		stop()
	}
	if count > 0 {
		e.logInfo("Stopped persist watcher (" + strconv.Itoa(count) + ")")
	}
}

func (e *persistEngine) Start(importers []persistImporter, watch func(path string, onModify func()) (func(), error)) error {
	e.Stop()
	e.mu.Lock()
	e.generation++
	generation := e.generation
	e.mu.Unlock()
	for _, importer := range importers {
		d3dxPath := filepath.Join(importer.Folder, "d3dx_user.ini")
		raw, err := os.ReadFile(d3dxPath)
		if err != nil {
			continue
		}
		parsed := parseD3dxUserINI(string(raw))
		e.mu.Lock()
		e.cached[importer.Key] = parsed
		e.mu.Unlock()
		onModify := func(importer persistImporter, path string, generation int) func() {
			return func() { e.handleD3dxUserINIChange(importer, path, generation) }
		}(importer, d3dxPath, generation)
		stop, err := watch(d3dxPath, onModify)
		if err != nil {
			e.logError("Error watching " + d3dxPath + ": " + err.Error())
			continue
		}
		e.mu.Lock()
		e.unwatch = append(e.unwatch, stop)
		e.mu.Unlock()
		e.logInfo("Started watching " + d3dxPath + " for persist updates")
	}
	return nil
}

func (e *persistEngine) handleD3dxUserINIChange(importer persistImporter, iniPath string, generation int) {
	// Electron retries isPathReadable + read 10 times with 200ms delay.
	// Go reads once; mid-write races are not covered by Electron tests.
	raw, err := os.ReadFile(iniPath)
	if err != nil {
		e.logError("Error handling d3dx_user.ini change: " + err.Error())
		return
	}
	if !e.active(generation) {
		return
	}
	newParsed := parseD3dxUserINI(string(raw))
	e.mu.Lock()
	oldParsed := e.cached[importer.Key]
	if oldParsed.values == nil {
		oldParsed.values = map[string]string{}
	}
	e.mu.Unlock()
	changed := false
	for _, key := range newParsed.order {
		if oldParsed.values[key] != newParsed.values[key] {
			changed = true
			break
		}
	}
	if !changed {
		e.mu.Lock()
		e.cached[importer.Key] = newParsed
		e.mu.Unlock()
		return
	}
	e.mu.Lock()
	e.revisions[importer.Key]++
	revision := e.revisions[importer.Key]
	at := e.nowMsLocked()
	e.mu.Unlock()
	suppressedByFile := map[string]map[string]struct{}{}
	learnedByFile := map[string]map[string]TogglePersistLearnedVariable{}
	// Electron Object.entries preserves INI insertion order. Go map ranging
	// would shuffle Observe() and can change quiet-window batching.
	for _, key := range newParsed.order {
		newValue := newParsed.values[key]
		if !e.active(generation) {
			return
		}
		if oldParsed.values[key] == newValue {
			continue
		}
		target := resolvePersistTarget(importer.Folder, key)
		if target == nil {
			continue
		}
		e.loadPersistProfile(target.iniPath, generation)
		if !e.active(generation) {
			return
		}
		e.mu.Lock()
		result := e.learner.Observe(target.iniPath, target.varName, newValue, revision, at)
		e.mu.Unlock()
		e.scheduleFlush(target.iniPath, result.NextDueAt, generation)
		if len(result.NewlySuppressed) > 0 {
			pending := suppressedByFile[target.iniPath]
			if pending == nil {
				pending = map[string]struct{}{}
				suppressedByFile[target.iniPath] = pending
			}
			for _, name := range result.NewlySuppressed {
				pending[name] = struct{}{}
			}
		}
		if len(result.NewlyLearned) > 0 {
			pending := learnedByFile[target.iniPath]
			if pending == nil {
				pending = map[string]TogglePersistLearnedVariable{}
				learnedByFile[target.iniPath] = pending
			}
			for _, variable := range result.NewlyLearned {
				pending[strings.ToLower(variable.Name)] = variable
			}
		}
	}
	if !e.active(generation) {
		return
	}
	e.mu.Lock()
	e.cached[importer.Key] = newParsed
	e.mu.Unlock()
	for pathKey, variables := range suppressedByFile {
		names := make([]string, 0, len(variables))
		for name := range variables {
			names = append(names, "$"+name)
		}
		e.logInfo("Suppressed continuously changing persist variables " + strings.Join(names, ", ") + " in " + pathKey)
	}
	for pathKey, variables := range learnedByFile {
		list := make([]TogglePersistLearnedVariable, 0, len(variables))
		for _, variable := range variables {
			list = append(list, variable)
		}
		e.savePersistProfile(pathKey, list, generation)
	}
}

func (e *persistEngine) loadPersistProfile(targetINIPath string, generation int) {
	profilePath := togglePersistProfilePath(targetINIPath)
	raw, err := os.ReadFile(profilePath)
	if err != nil {
		return
	}
	targetContent, err := os.ReadFile(targetINIPath)
	if err != nil {
		e.logPersistProfileError("load", targetINIPath, profilePath, err)
		return
	}
	if !e.active(generation) {
		return
	}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err != nil {
		e.logPersistProfileError("load", targetINIPath, profilePath, err)
		return
	}
	profile, err := parseTogglePersistProfile(parsed)
	if err != nil {
		e.logPersistProfileError("load", targetINIPath, profilePath, err)
		return
	}
	base := filepath.Base(targetINIPath)
	var file *TogglePersistProfileFile
	for name, entry := range profile.Files {
		if strings.EqualFold(name, base) {
			copied := entry
			file = &copied
			break
		}
	}
	if file == nil || file.Fingerprint != fingerprintTogglePersistINI(string(targetContent)) {
		return
	}
	e.mu.Lock()
	e.learner.RegisterLearnedVariables(targetINIPath, file.Variables)
	e.mu.Unlock()
}

func (e *persistEngine) savePersistProfile(targetINIPath string, variables []TogglePersistLearnedVariable, generation int) {
	if len(variables) == 0 || !e.active(generation) {
		return
	}
	profilePath := togglePersistProfilePath(targetINIPath)
	targetContent, err := os.ReadFile(targetINIPath)
	if err != nil {
		e.logPersistProfileError("save", targetINIPath, profilePath, err)
		return
	}
	profile := createEmptyTogglePersistProfile()
	if raw, err := os.ReadFile(profilePath); err == nil {
		var parsed any
		if json.Unmarshal(raw, &parsed) == nil {
			if loaded, err := parseTogglePersistProfile(parsed); err == nil {
				profile = loaded
			} else {
				e.logPersistProfileError("parse-before-save", targetINIPath, profilePath, err)
			}
		} else {
			e.logPersistProfileError("parse-before-save", targetINIPath, profilePath, err)
		}
	}
	actualFileName := filepath.Base(targetINIPath)
	fingerprint := fingerprintTogglePersistINI(string(targetContent))
	var existingName string
	for name := range profile.Files {
		if strings.EqualFold(name, actualFileName) {
			existingName = name
			break
		}
	}
	file := TogglePersistProfileFile{Fingerprint: fingerprint, Variables: map[string]TogglePersistLearnedVariable{}}
	if existingName != "" && profile.Files[existingName].Fingerprint == fingerprint {
		file = profile.Files[existingName]
		if file.Variables == nil {
			file.Variables = map[string]TogglePersistLearnedVariable{}
		}
	}
	for _, variable := range variables {
		file.Variables[strings.ToLower(variable.Name)] = variable
	}
	if existingName != "" && existingName != actualFileName {
		delete(profile.Files, existingName)
	}
	profile.Files[actualFileName] = file
	raw, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		e.logPersistProfileError("save", targetINIPath, profilePath, err)
		return
	}
	if !e.active(generation) {
		return
	}
	if _, err := applyPersistFileWrite(profilePath, append(raw, '\n')); err != nil {
		e.logPersistProfileError("save", targetINIPath, profilePath, err)
	}
}

func applyPersistFileWrite(path string, content []byte) (bool, error) {
	mode := os.FileMode(0o666)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return true, os.WriteFile(path, content, mode)
}

func (e *persistEngine) scheduleFlush(targetINIPath string, dueAt *int64, generation int) {
	fileKey := strings.ToLower(targetINIPath)
	e.mu.Lock()
	if existing := e.flushDue[fileKey]; existing != nil {
		existing.cancelled = true
		if existing.real != nil {
			existing.real.Stop()
		}
		delete(e.flushDue, fileKey)
	}
	e.mu.Unlock()
	if dueAt == nil || !e.active(generation) {
		return
	}
	delay := *dueAt - e.nowMs()
	if delay < 0 {
		delay = 0
	}
	e.afterFile(fileKey, delay, func() {
		e.mu.Lock()
		delete(e.flushDue, fileKey)
		e.mu.Unlock()
		e.flushReady(targetINIPath, generation)
	})
}

func (e *persistEngine) flushReady(targetINIPath string, generation int) {
	if !e.active(generation) {
		return
	}
	e.mu.Lock()
	ready := e.learner.TakeReady(targetINIPath, e.nowMsLocked())
	e.mu.Unlock()
	e.scheduleFlush(targetINIPath, ready.NextDueAt, generation)
	if len(ready.Updates) == 0 {
		return
	}
	updates := map[string]string{}
	for _, pair := range ready.Updates {
		updates[pair[0]] = pair[1]
	}
	updated, err := applyPersistUpdates(targetINIPath, updates)
	if err != nil {
		// A mod can be renamed or removed during the learner's quiet window.
		// The queued update belongs to the old path and must not interfere with
		// the mod-manager operation or be reported as an actionable failure.
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		e.logError("Error updating mod ini " + targetINIPath + ": " + err.Error())
		return
	}
	if len(updated) == 1 {
		e.logInfo("Updated persist variable $" + updated[0] + " in " + targetINIPath)
	} else if len(updated) > 1 {
		names := make([]string, len(updated))
		for i, name := range updated {
			names[i] = "$" + name
		}
		e.logInfo("Updated persist variables " + strings.Join(names, ", ") + " in " + targetINIPath)
	}
}

func (e *persistEngine) active(generation int) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.generation == generation
}

func (e *persistEngine) nowMs() int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.nowMsLocked()
}

func (e *persistEngine) nowMsLocked() int64 {
	if e.useFake {
		return e.now
	}
	return time.Now().UnixMilli()
}

func (e *persistEngine) afterFile(fileKey string, delayMs int64, fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.flushDue == nil {
		e.flushDue = map[string]*persistTimer{}
	}
	if e.useFake {
		timer := &persistTimer{due: e.now + delayMs, fn: fn}
		e.timers = append(e.timers, timer)
		e.flushDue[fileKey] = timer
		return
	}
	timer := &persistTimer{fn: fn}
	timer.real = time.AfterFunc(time.Duration(delayMs)*time.Millisecond, func() {
		e.mu.Lock()
		if timer.cancelled {
			e.mu.Unlock()
			return
		}
		e.mu.Unlock()
		fn()
	})
	e.flushDue[fileKey] = timer
}

func (e *persistEngine) useFakeClock() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.useFake = true
	e.now = 0
}

func (e *persistEngine) cachedValue(importer, key string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cached[importer].values[key]
}

func (e *persistEngine) nextDueAt(targetINIPath string) *int64 {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.learner.GetNextDueAt(targetINIPath)
}

func (e *persistEngine) Advance(ms int64) {
	e.mu.Lock()
	e.now += ms
	now := e.now
	var due []func()
	remaining := e.timers[:0]
	for _, timer := range e.timers {
		if timer.cancelled {
			continue
		}
		if timer.due <= now {
			due = append(due, timer.fn)
			continue
		}
		remaining = append(remaining, timer)
	}
	e.timers = remaining
	e.mu.Unlock()
	for _, fn := range due {
		fn()
	}
}

func (e *persistEngine) logInfo(message string) {
	if e.infoFn != nil {
		e.infoFn(message)
	}
	e.addLog("INFO", message)
}

func (e *persistEngine) logError(message string) {
	if e.errorFn != nil {
		e.errorFn(message)
	}
	e.addLog("ERROR", message)
}

func (e *persistEngine) logPersistProfileError(stage, targetINIPath, profilePath string, err error) {
	e.logError("Error processing toggle persist profile: stage=" + stage + ", targetIniPath=" + targetINIPath + ", profilePath=" + profilePath + ", error=" + err.Error())
}

func (e *persistEngine) addLog(level, message string) {
	// Electron formatDate uses date-fns PPpp (locale-dependent). Go uses a
	// fixed en-US-like layout; persist log text is not covered by Electron tests.
	entry := "[" + time.Now().Format("Jan 2, 2006, 3:04:05 PM") + "] [" + level + "] " + message
	e.mu.Lock()
	e.logs = append(e.logs, entry)
	if len(e.logs) > 10 {
		e.logs = e.logs[len(e.logs)-10:]
	}
	logs := append([]string{}, e.logs...)
	emit := e.emit
	e.mu.Unlock()
	if emit != nil {
		emit(logs)
	}
}

var persistTargetRE = regexp.MustCompile(`(?i)^\$\\(.+\.ini)\\([^\\]+)$`)

type persistTarget struct {
	iniPath string
	varName string
}

func parseD3dxUserINI(content string) parsedD3dx {
	result := parsedD3dx{values: map[string]string{}}
	inConstants := false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inConstants = strings.EqualFold(trimmed, "[Constants]")
			continue
		}
		if !inConstants || !strings.HasPrefix(trimmed, "$") {
			continue
		}
		parts := strings.SplitN(trimmed, "=", 2)
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		if _, exists := result.values[key]; !exists {
			result.order = append(result.order, key)
		}
		result.values[key] = strings.TrimSpace(parts[1])
	}
	return result
}

func resolvePersistTarget(importerFolder, key string) *persistTarget {
	match := persistTargetRE.FindStringSubmatch(key)
	if match == nil {
		return nil
	}
	importerRoot, err := filepath.Abs(importerFolder)
	if err != nil {
		importerRoot = filepath.Clean(importerFolder)
	}
	targetINIPath, err := filepath.Abs(filepath.Join(importerRoot, filepath.FromSlash(strings.ReplaceAll(match[1], `\`, "/"))))
	if err != nil {
		return nil
	}
	relative, err := filepath.Rel(importerRoot, targetINIPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) || filepath.IsAbs(relative) {
		return nil
	}
	info, err := os.Stat(targetINIPath)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	return &persistTarget{iniPath: targetINIPath, varName: match[2]}
}

func watchPersistFile(path string, onModify func()) (func(), error) {
	service, err := watcher.WatchFile(path, watcher.FileConfig{
		Ops:             watcher.Create | watcher.Write,
		SettleDelay:     200 * time.Millisecond,
		DistinctContent: true,
	}, func(watcher.Event) {
		onModify()
	})
	if err != nil {
		return nil, err
	}
	return func() {
		_ = service.Close()
	}, nil
}
