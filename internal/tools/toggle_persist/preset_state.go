package togglepersist

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/gamelink"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modname"
	"nahida.live/desktop/internal/platform"
)

const (
	CaptureSourceGame  = "game"
	CaptureSourceINI   = "ini"
	CaptureSourceMixed = "mixed"
)

// ErrNoPersistVariables reports a mod whose INIs declare no `global persist` variable.
var ErrNoPersistVariables = errors.New("TOGGLE_PRESET_NO_VARIABLES")

// INIValues maps the path of an INI relative to the mod folder, with `/` separators, to the declared names of
// its persist variables (without `$`) and their values as INI text.
type INIValues = map[string]map[string]string

type CapturedState struct {
	Source string    `json:"source"`
	INIs   INIValues `json:"inis"`
}

// StateApplyResult reports where a toggle state landed. Variables are listed as "<ini>: <name>". The files are
// written before the games are set, so GameErr reports a failure that left the files changed.
type StateApplyResult struct {
	Written     []string `json:"written"`
	UserINI     int      `json:"userIni"`
	Skipped     []string `json:"skipped"`
	MissingINIs []string `json:"missingInis"`
	Games       int      `json:"games"`
	Applied     []string `json:"applied"`
	Missing     []string `json:"missing"`
	// UnlinkedGame reports a running game that loads the mod without the desktop channel. It keeps the old values
	// and may save them to d3dx_user.ini again when it exits.
	UnlinkedGame bool   `json:"unlinkedGame"`
	GameErr      string `json:"gameErr"`
	// Values maps the absolute path of each INI to the values applied to it.
	Values map[string]map[string]string `json:"values"`
}

type modINI struct {
	path string
	key  string
}

type declaredValue struct {
	name  string
	value string
}

type importerInfo struct {
	folder  string
	running bool
}

// CaptureModState reads the persist variables of every INI in the mod. A running game that loads the INI through
// the desktop channel gives its live values; the rest come from the INI, overridden by the values 3DMigoto saved
// in d3dx_user.ini.
func (t *Service) CaptureModState(ctx context.Context, modPath string) (CapturedState, error) {
	inis, err := modINIs(ctx, modPath)
	if err != nil {
		return CapturedState{}, err
	}
	importer := t.importerFor(ctx, modPath)
	userContent := readUserINI(importer.folder)

	captured := CapturedState{INIs: INIValues{}}
	total, live := 0, 0
	for _, ini := range inis {
		raw, _, err := readPersistINI(ini.path)
		if err != nil {
			return CapturedState{}, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "toggle-preset.capture", Stage: "read-ini",
				Fields: map[string]any{"modPath": modPath, "iniPath": ini.path},
			})
		}
		content := string(raw)
		declared := declaredPersistValues(content)
		if len(declared) == 0 {
			continue
		}

		values := make(map[string]string, len(declared))
		gameValues := t.liveValues(ctx, ini.path, content)
		keys := userKeys(importer.folder, ini.path, content)
		for _, variable := range declared {
			lower := strings.ToLower(variable.name)
			total++
			if value, ok := gameValues[lower]; ok {
				values[variable.name] = value
				live++
				continue
			}
			values[variable.name] = variable.value
			if value, ok := lastUserValue(userContent, keys[lower]); ok {
				values[variable.name] = value
			}
		}
		captured.INIs[ini.key] = values
	}

	switch {
	case total == 0:
		return CapturedState{}, ErrNoPersistVariables
	case live == total:
		captured.Source = CaptureSourceGame
	case live == 0:
		captured.Source = CaptureSourceINI
	default:
		captured.Source = CaptureSourceMixed
	}
	return captured, nil
}

// PersistValuesFromState keys the model viewer's state of each INI, given by absolute path, by the INI's path in
// the mod and keeps the persist variables it declares. Values are formatted as a save to the INI writes them.
func (t *Service) PersistValuesFromState(modPath string, inis map[string]map[string]any) (INIValues, error) {
	result := INIValues{}
	for path, state := range inis {
		relative, err := filepath.Rel(modPath, path)
		if err != nil || !platform.SameOrChildPath(modPath, path) {
			return nil, fmt.Errorf("toggle state INI %s is outside mod %s", path, modPath)
		}
		raw, _, err := readPersistINI(path)
		if err != nil {
			return nil, err
		}
		declared := map[string]string{}
		for _, variable := range declaredPersistValues(string(raw)) {
			declared[strings.ToLower(variable.name)] = variable.name
		}
		values := map[string]string{}
		for name, value := range persistUpdatesFromState(state) {
			if declaredName, ok := declared[name]; ok {
				values[declaredName] = value
			}
		}
		if len(values) > 0 {
			result[filepath.ToSlash(relative)] = values
		}
	}
	if len(result) == 0 {
		return nil, ErrNoPersistVariables
	}
	return result, nil
}

// ApplyModState writes values to the mod INIs and to the entries d3dx_user.ini holds for them, then sets them in
// every running game that loads the mod. Variables the INIs no longer declare as persist are skipped.
func (t *Service) ApplyModState(ctx context.Context, modPath string, values INIValues) (StateApplyResult, error) {
	result := StateApplyResult{
		Written: []string{}, Skipped: []string{}, MissingINIs: []string{}, Applied: []string{}, Missing: []string{},
		Values: map[string]map[string]string{},
	}
	inis, err := modINIs(ctx, modPath)
	if err != nil {
		return result, err
	}
	byKey := make(map[string]modINI, len(inis))
	for _, ini := range inis {
		byKey[strings.ToLower(ini.key)] = ini
	}

	type update struct {
		ini     modINI
		content string
		state   map[string]any
	}
	var updates []update
	for _, key := range slices.Sorted(maps.Keys(values)) {
		ini, ok := byKey[strings.ToLower(key)]
		if !ok {
			result.MissingINIs = append(result.MissingINIs, key)
			continue
		}
		raw, _, err := readPersistINI(ini.path)
		if err != nil {
			return result, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "toggle-preset.apply", Stage: "read-ini",
				Fields: map[string]any{"modPath": modPath, "iniPath": ini.path},
			})
		}
		declared := map[string]string{}
		for _, variable := range declaredPersistValues(string(raw)) {
			declared[strings.ToLower(variable.name)] = variable.name
		}
		state := map[string]any{}
		applied := map[string]string{}
		for _, name := range slices.Sorted(maps.Keys(values[key])) {
			declaredName, ok := declared[strings.ToLower(name)]
			if !ok {
				result.Skipped = append(result.Skipped, ini.key+": "+name)
				continue
			}
			state[declaredName] = values[key][name]
			applied[declaredName] = values[key][name]
		}
		if len(state) > 0 {
			updates = append(updates, update{ini: ini, content: string(raw), state: state})
			result.Values[ini.path] = applied
		}
	}
	if len(updates) == 0 {
		return result, nil
	}

	importer := t.importerFor(ctx, modPath)
	if err := func() error {
		t.persistMu.Lock()
		defer t.persistMu.Unlock()

		userValues := map[string]string{}
		for _, u := range updates {
			written, err := t.persist.PersistStateToINI(u.ini.path, u.state)
			if err != nil {
				return infra.AnnotateError(err, infra.Diagnostic{
					Operation: "toggle-preset.apply", Stage: "write-ini",
					Fields: map[string]any{"modPath": modPath, "iniPath": u.ini.path, "variables": len(u.state)},
				})
			}
			for _, name := range written.UpdatedVariables {
				result.Written = append(result.Written, u.ini.key+": "+name)
			}
			keys := userKeys(importer.folder, u.ini.path, u.content)
			for name, value := range u.state {
				for _, key := range keys[strings.ToLower(name)] {
					userValues[key] = fmt.Sprint(value)
				}
			}
		}
		if importer.folder == "" {
			return nil
		}
		count, err := updateUserINI(filepath.Join(importer.folder, "d3dx_user.ini"), userValues)
		result.UserINI = count
		return err
	}(); err != nil {
		return result, err
	}

	linked := false
	for _, u := range updates {
		applied, err := t.ApplyStateToGame(ctx, u.ini.path, u.state, true)
		// A linked game that rejected the values is still linked; GameErr reports the failure.
		linked = linked || applied.Games > 0
		if err != nil {
			if infra.IsCancellationError(err) {
				return result, err
			}
			result.GameErr = err.Error()
			_ = infra.ReportError(t.log, err, "TogglePersist.applyModState", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "toggle-preset.apply", Stage: "set-vars",
				Fields: map[string]any{"modPath": modPath, "iniPath": u.ini.path},
			})
			continue
		}
		result.Games = max(result.Games, applied.Games)
		result.Applied = append(result.Applied, applied.Applied...)
		result.Missing = append(result.Missing, applied.Missing...)
	}
	result.UnlinkedGame = importer.running && !linked && loadedBy(importer.folder, modPath)
	return result, nil
}

// loadedBy reports whether 3DMigoto loads path from the importer folder: no folder on the way is disabled.
func loadedBy(importer, path string) bool {
	relative, ok := gameRelativePath(importer, path)
	if !ok {
		return false
	}
	return !slices.ContainsFunc(strings.Split(relative, string(filepath.Separator)), modname.IsDisabled)
}

// modINIs lists the INIs 3DMigoto loads from the mod: no file or folder below it whose name starts with DISABLED
// or a dot. The mod folder itself may be disabled.
func modINIs(ctx context.Context, modPath string) ([]modINI, error) {
	release, err := diskio.AcquireDir(ctx, modPath)
	if err != nil {
		return nil, err
	}
	var inis []modINI
	err = filepath.WalkDir(modPath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		if path != modPath && (strings.HasPrefix(name, ".") || modname.IsDisabled(name)) {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if entry.IsDir() || !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(name), ".ini") ||
			strings.EqualFold(name, "d3dx_user.ini") {
			return nil
		}
		relative, err := filepath.Rel(modPath, path)
		if err != nil {
			return err
		}
		inis = append(inis, modINI{path: path, key: filepath.ToSlash(relative)})
		return nil
	})
	release()
	if err != nil {
		return nil, infra.AnnotateError(err, infra.Diagnostic{
			Operation: "toggle-preset", Stage: "list-inis", Fields: map[string]any{"modPath": modPath},
		})
	}
	return inis, nil
}

// declaredPersistValues lists the `global persist` variables in [Constants] with the values they start with.
func declaredPersistValues(content string) []declaredValue {
	var declared []declaredValue
	seen := map[string]bool{}
	inConstants := false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(line, "\uFEFF"))
		if strings.HasPrefix(trimmed, "[") {
			inConstants = strings.EqualFold(trimmed, "[Constants]")
			continue
		}
		if !inConstants {
			continue
		}
		match := persistDeclarationRE.FindStringSubmatch(trimmed)
		if match == nil || strings.EqualFold(match[1], "persis") {
			continue
		}
		name := strings.TrimSpace(match[2])
		if seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		declared = append(declared, declaredValue{name: name, value: strings.TrimSpace(match[3])})
	}
	return declared
}

// importerFor finds the importer folder that loads path: a linked game's, an enabled importer's, or the nearest
// folder above it with a d3dx.ini.
func (t *Service) importerFor(ctx context.Context, path string) importerInfo {
	if t.link != nil {
		for _, state := range t.link.States() {
			if state.Status != gamelink.StatusConnected {
				continue
			}
			if _, ok := gameRelativePath(state.Info.ImporterFolder, path); ok {
				return importerInfo{folder: state.Info.ImporterFolder, running: true}
			}
		}
	}
	if t.xxmi != nil {
		importers, err := t.xxmi.GetEnabledImporters(ctx)
		if err == nil {
			for _, importer := range importers {
				if _, ok := gameRelativePath(importer.ImporterFolder, path); ok {
					return importerInfo{folder: importer.ImporterFolder, running: importer.Running}
				}
			}
		}
	}
	current := filepath.Clean(path)
	for range 6 {
		parent := filepath.Dir(current)
		if platform.SamePathFold(parent, current) {
			break
		}
		current = parent
		if info, err := os.Stat(filepath.Join(current, "d3dx.ini")); err == nil && info.Mode().IsRegular() {
			return importerInfo{folder: current}
		}
	}
	return importerInfo{}
}

func readUserINI(importer string) string {
	if importer == "" {
		return ""
	}
	raw, _, err := readPersistINI(filepath.Join(importer, "d3dx_user.ini"))
	if err != nil {
		return ""
	}
	return string(raw)
}

// userKeys maps each persist variable of the INI, lowercased, to the names d3dx_user.ini may save it under: its
// declared namespace and its path below the importer. A disabled mod gets the names it has once enabled.
func userKeys(importer, iniPath, content string) map[string][]string {
	if importer == "" {
		return nil
	}
	relative, ok := gameRelativePath(importer, iniPath)
	if !ok {
		return nil
	}
	parts := strings.Split(relative, string(filepath.Separator))
	for i, part := range parts {
		if stripped := modname.StripDisabled(part); stripped != "" {
			parts[i] = stripped
		}
	}
	pathNamespace := strings.Join(parts, `\`)
	targets, namespace := parsePersistTargets(content, pathNamespace)
	keys := map[string][]string{}
	for _, target := range targets {
		if !target.persistent {
			continue
		}
		name := strings.ToLower(target.varName)
		keys[name] = append(keys[name], target.key)
		if !strings.EqualFold(namespace, pathNamespace) {
			keys[name] = append(keys[name], strings.ToLower(`$\`+pathNamespace+`\`+target.varName))
		}
	}
	return keys
}

// liveValues reads the persist variables of the INI from the first linked game that loads it, by lowercase name.
// A game that fails to answer leaves the values to the files.
func (t *Service) liveValues(ctx context.Context, iniPath, content string) map[string]string {
	links, _ := t.gameLinks(iniPath)
	if len(links) == 0 {
		return nil
	}
	link := links[0]
	targets, _ := parsePersistTargets(content, link.namespace)
	byKey := map[string]string{}
	var names []string
	for _, target := range targets {
		if target.persistent {
			byKey[target.key] = strings.ToLower(target.varName)
			names = append(names, target.key)
		}
	}
	if len(names) == 0 {
		return nil
	}

	reply, err := t.readGameVars(ctx, link.pid, names)
	if err != nil {
		t.liveReadFailures.Report(t.log, err, "TogglePersist.captureModState", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "toggle-preset.capture", Stage: "get-vars",
			Fields: map[string]any{"importer": link.importer, "pid": link.pid, "iniPath": iniPath},
		})
		return nil
	}
	values := map[string]string{}
	for _, value := range reply.Values {
		if name, ok := byKey[strings.ToLower(value.Name)]; ok && value.Exists {
			values[name] = strconv.FormatFloat(float64(value.Value), 'g', -1, 32)
		}
	}
	return values
}

func (t *Service) readGameVars(ctx context.Context, pid uint32, names []string) (gamelink.VarsReply, error) {
	delays := append([]time.Duration{0}, gamePreviewRetryDelays...)
	var err error
	for _, delay := range delays {
		if delay > 0 {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return gamelink.VarsReply{}, ctx.Err()
			case <-timer.C:
			}
		}
		requestCtx, cancel := context.WithTimeout(ctx, gameApplyTimeout)
		var reply gamelink.Message
		reply, err = t.link.Request(requestCtx, pid, gamelink.GetVars{Names: names})
		cancel()
		if err == nil {
			read, ok := reply.(gamelink.VarsReply)
			if !ok {
				return gamelink.VarsReply{}, fmt.Errorf("read game variables: unexpected reply %T", reply)
			}
			return read, nil
		}
		var remote *gamelink.RemoteError
		if !errors.Is(err, gamelink.ErrNotConnected) &&
			(!errors.As(err, &remote) || remote.Code != gamelink.ErrorBusy) {
			break
		}
	}
	return gamelink.VarsReply{}, err
}
