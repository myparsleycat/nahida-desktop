package blenderlink

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

type AddonState string

const (
	AddonMissing  AddonState = "missing"
	AddonCurrent  AddonState = "current"
	AddonOutdated AddonState = "outdated"
	// AddonNewer is an install of a later version than the app bundles, which installing would downgrade.
	AddonNewer    AddonState = "newer"
	AddonModified AddonState = "modified"
	// AddonForeign is a folder of the add-on's name that the app did not write, such as one installed from a zip.
	AddonForeign AddonState = "foreign"
	// AddonLinked is a junction or symbolic link, such as one to a development checkout, which the app leaves alone.
	AddonLinked AddonState = "linked"
)

var (
	// ErrBlenderRunning refuses a change that a running Blender would undo when it saves its preferences.
	ErrBlenderRunning = errors.New("blender is running")
	// ErrAddonConflict means a copy the app did not install sits where Blender would load it beside the new one.
	ErrAddonConflict = errors.New("another copy of the add-on is installed")
	// ErrAddonLinked refuses to write through a junction or symbolic link to a folder elsewhere.
	ErrAddonLinked = errors.New("the add-on folder is a link")
	// ErrAddonNewer refuses to replace a later version than the app bundles.
	ErrAddonNewer      = errors.New("a newer add-on is installed")
	errBlenderNotFound = errors.New("blender version not found")
)

// AddonTarget is one Blender release series found on this computer and the state of the add-on in it.
type AddonTarget struct {
	Version          string
	Path             string
	State            AddonState
	InstalledVersion string
	// Executable is empty when only the release's user configuration was found; the add-on can then be copied
	// but not enabled.
	Executable string
	Running    bool
	// Connected reports a live link from this release, which only an enabled add-on opens.
	Connected bool
	// Conflict is the path of a copy the app did not install that Blender would load as well, and ConflictState
	// is AddonForeign or AddonLinked.
	Conflict      string
	ConflictState AddonState
	// LegacyCopy reports a copy the app installed in scripts/addons of a release that loads extensions, as
	// Blender leaves behind when it copies an older release's settings, and LegacyState and LegacyVersion are its
	// state and version. Installing replaces it with the extension, which has to be enabled under another module
	// name, or updates it in place when no executable can switch the modules.
	LegacyCopy    bool
	LegacyState   AddonState
	LegacyVersion string
}

// AddonBlock is why installing into a release is refused, or empty when it is not.
type AddonBlock string

const (
	AddonBlockLinked   AddonBlock = "linked"
	AddonBlockConflict AddonBlock = "conflict"
	AddonBlockNewer    AddonBlock = "newer"
	// AddonBlockRunning means the release runs and installing needs an add-on enabled, which Blender would undo
	// when it saves its preferences on quitting. Only a connected copy in the target folder is already enabled.
	AddonBlockRunning AddonBlock = "running"
)

func (t AddonTarget) Blocked() AddonBlock {
	switch {
	case t.State == AddonLinked || t.ConflictState == AddonLinked:
		return AddonBlockLinked
	case t.Conflict != "":
		return AddonBlockConflict
	case t.State == AddonNewer || t.State == AddonMissing && t.LegacyState == AddonNewer:
		return AddonBlockNewer
	case t.Running && (!t.Connected || t.LegacyCopy || t.State == AddonMissing):
		return AddonBlockRunning
	}
	return ""
}

// AddonResult tells how an installed add-on takes effect.
type AddonResult struct {
	Enabled bool
	// Restart means Blender is running the previous version and loads the new files when it starts again.
	Restart bool
	// ManualEnable means no executable was found, so the user enables the add-on in Blender's preferences.
	ManualEnable bool
	// Legacy means the copy in scripts/addons was updated in place, as no executable could switch its module to
	// the extension's.
	Legacy bool
}

// addonEnv is what installing touches outside the add-on folder: where Blender keeps its user configuration
// and which Blender executables exist, run, and enable the add-on.
type addonEnv struct {
	configRoot func() (string, error)
	installed  func(ctx context.Context) (map[blenderVersion]string, error)
	running    func(ctx context.Context) (map[blenderVersion]string, error)
	run        func(ctx context.Context, executable, script string) (string, error)
	// portable reports an executable that keeps its configuration beside itself rather than under configRoot.
	portable func(executable string) bool
	image    func(pid uint32) (string, bool)
}

type addonRecordFile struct {
	Version string            `json:"version"`
	Files   map[string]string `json:"files"`
}

const addonDoneMarker = "NAHIDA_ADDON_DONE"

// AddonTargets lists the supported Blender releases found on this computer, newest first.
func (m *Manager) AddonTargets(ctx context.Context) ([]AddonTarget, error) {
	targets, _, err := m.addonTargets(ctx)
	if err = m.addonThrottle.ReportError(m.opts.Log, err, "BlenderLink.AddonTargets",
		infra.Diagnostic{Operation: "BlenderLink.AddonTargets"}); err != nil {
		return nil, err
	}
	return targets, nil
}

// RunningBlenders lists the release series with a running Blender, sorted, without the rest of AddonTargets.
func (m *Manager) RunningBlenders(ctx context.Context) ([]string, error) {
	running, err := m.runningBlenders(ctx)
	if err = m.runningThrottle.ReportError(m.opts.Log, err, "BlenderLink.RunningBlenders",
		infra.Diagnostic{Operation: "BlenderLink.RunningBlenders"}); err != nil {
		return nil, err
	}
	series := make([]string, 0, len(running))
	for _, v := range slices.SortedFunc(maps.Keys(running), blenderVersion.compare) {
		series = append(series, v.String())
	}
	return series, nil
}

// runningBlenders maps each supported release with a running Blender that uses configRoot to its executable.
func (m *Manager) runningBlenders(ctx context.Context) (map[blenderVersion]string, error) {
	running, err := m.addon.running(ctx)
	if err != nil {
		return nil, err
	}
	kept := make(map[blenderVersion]string, len(running))
	for v, executable := range running {
		if v.supported() && !m.addon.portable(executable) {
			kept[v] = executable
		}
	}
	return kept, nil
}

// ownsAddon reports whether a state is that of a copy the app installed.
func ownsAddon(state AddonState) bool {
	switch state {
	case AddonCurrent, AddonOutdated, AddonNewer, AddonModified:
		return true
	case AddonMissing, AddonForeign, AddonLinked:
	}
	return false
}

// InstallAddon writes the bundled add-on into a Blender release's user configuration and enables it when it
// can. The add-on's own files are replaced; files the user added to its folder stay.
func (m *Manager) InstallAddon(ctx context.Context, version string) (AddonResult, error) {
	m.addonMu.Lock()
	defer m.addonMu.Unlock()
	var result AddonResult
	target, root, err := m.addonTarget(ctx, version)
	stage := "find"
	if err == nil {
		stage, err = m.installAddon(ctx, target, root, &result)
	}
	if errors.Is(err, ErrBlenderRunning) || errors.Is(err, ErrAddonConflict) || errors.Is(err, ErrAddonLinked) ||
		errors.Is(err, ErrAddonNewer) {
		return result, err
	}
	if err != nil {
		return result, infra.ReportError(m.opts.Log, fmt.Errorf("install blender add-on: %w", err),
			"BlenderLink.InstallAddon", addonDiagnostic("BlenderLink.InstallAddon", stage, version, target))
	}
	return result, nil
}

func (m *Manager) installAddon(
	ctx context.Context,
	target AddonTarget,
	root string,
	result *AddonResult,
) (string, error) {
	v, _ := parseBlenderVersion(target.Version)
	switch target.Blocked() {
	case AddonBlockLinked:
		linked := target.Conflict
		if target.State == AddonLinked {
			linked = target.Path
		}
		return "check", fmt.Errorf("%w: %s", ErrAddonLinked, linked)
	case AddonBlockConflict:
		return "check", fmt.Errorf("%w: %s", ErrAddonConflict, target.Conflict)
	case AddonBlockNewer:
		return "check", fmt.Errorf("%w: %s", ErrAddonNewer, cmp.Or(target.InstalledVersion, target.LegacyVersion))
	case AddonBlockRunning:
		return "check", ErrBlenderRunning
	case "":
	}

	files, err := addonFiles()
	if err != nil {
		return "read bundle", err
	}
	if target.LegacyCopy && target.Executable == "" {
		if err := writeAddon(legacyAddonPath(root, v), files); err != nil {
			return "write legacy copy", err
		}
		result.Legacy = true
		return "", nil
	}
	if err := writeAddon(target.Path, files); err != nil {
		return "write", err
	}

	switch {
	case target.Running:
		result.Restart = true
	case target.Executable == "":
		result.ManualEnable = true
	default:
		var disable []string
		if target.LegacyCopy {
			disable = []string{addonID}
		}
		if err := m.runAddonScript(ctx, target.Executable, []string{v.addonModule()}, disable); err != nil {
			return "enable", err
		}
		result.Enabled = true
	}
	// The legacy copy goes only after the script turned its module off and the extension's on, so a failed
	// script leaves the previous setup working and a retry still sees the copy.
	if target.LegacyCopy {
		if err := removeAddonFiles(legacyAddonPath(root, v)); err != nil {
			return "remove legacy copy", err
		}
	}
	return "", nil
}

// RemoveAddon disables the add-on in a Blender release that is not running and deletes the files the app
// installed. A folder the app did not write is left alone.
func (m *Manager) RemoveAddon(ctx context.Context, version string) error {
	m.addonMu.Lock()
	defer m.addonMu.Unlock()
	target, root, err := m.addonTarget(ctx, version)
	stage := "find"
	if err == nil {
		stage, err = m.removeAddon(ctx, target, root)
	}
	if errors.Is(err, ErrBlenderRunning) {
		return err
	}
	if err != nil {
		return infra.ReportError(m.opts.Log, fmt.Errorf("remove blender add-on: %w", err),
			"BlenderLink.RemoveAddon", addonDiagnostic("BlenderLink.RemoveAddon", stage, version, target))
	}
	return nil
}

func (m *Manager) removeAddon(ctx context.Context, target AddonTarget, root string) (string, error) {
	owned := ownsAddon(target.State)
	if !owned && !target.LegacyCopy {
		return "", nil
	}
	if target.Running {
		return "check", ErrBlenderRunning
	}
	v, _ := parseBlenderVersion(target.Version)
	var modules []string
	if owned {
		modules = append(modules, v.addonModule())
	}
	if target.LegacyCopy {
		modules = append(modules, addonID)
	}
	if target.Executable != "" {
		if err := m.runAddonScript(ctx, target.Executable, nil, modules); err != nil {
			return "disable", err
		}
	}
	if owned {
		if err := removeAddonFiles(target.Path); err != nil {
			return "remove", err
		}
	}
	if target.LegacyCopy {
		if err := removeAddonFiles(legacyAddonPath(root, v)); err != nil {
			return "remove legacy copy", err
		}
	}
	return "", nil
}

func addonDiagnostic(operation, stage, version string, target AddonTarget) infra.Diagnostic {
	return infra.Diagnostic{Operation: operation, Stage: stage, Fields: map[string]any{
		"blender": version, "path": target.Path, "executable": target.Executable, "state": string(target.State),
		"bundled": AddonVersion(),
	}}
}

func (m *Manager) addonTarget(ctx context.Context, version string) (AddonTarget, string, error) {
	targets, root, err := m.addonTargets(ctx)
	if err != nil {
		return AddonTarget{}, "", err
	}
	for _, target := range targets {
		if target.Version == version {
			return target, root, nil
		}
	}
	return AddonTarget{}, "", fmt.Errorf("%w: %s", errBlenderNotFound, version)
}

func (m *Manager) addonTargets(ctx context.Context) ([]AddonTarget, string, error) {
	root, err := m.addon.configRoot()
	if err != nil {
		return nil, "", err
	}
	found := map[blenderVersion]*AddonTarget{}
	add := func(v blenderVersion) *AddonTarget {
		if found[v] == nil {
			found[v] = &AddonTarget{Version: v.String()}
		}
		return found[v]
	}

	entries, err := os.ReadDir(root)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, "", err
	}
	for _, entry := range entries {
		if v, ok := parseBlenderVersion(entry.Name()); ok && entry.IsDir() && strings.Count(entry.Name(), ".") == 1 {
			add(v)
		}
	}
	installed, err := m.addon.installed(ctx)
	if err != nil {
		m.throttle.Report(m.opts.Log, err, "BlenderLink.AddonTargets", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "BlenderLink.AddonTargets", Stage: "find executables",
		})
	}
	for v, executable := range installed {
		if !m.addon.portable(executable) {
			add(v).Executable = executable
		}
	}
	// A missed running Blender would undo an install, so a failed process scan fails the listing.
	running, err := m.runningBlenders(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list running blender: %w", err)
	}
	for v, executable := range running {
		target := add(v)
		target.Running = true
		if target.Executable == "" {
			target.Executable = executable
		}
	}
	for _, link := range m.State().Links {
		if executable, ok := m.addon.image(link.PID); ok && m.addon.portable(executable) {
			continue
		}
		if v, ok := parseBlenderVersion(link.BlenderVersion); ok {
			add(v).Connected = true
		}
	}

	versions := slices.SortedFunc(maps.Keys(found), func(a, b blenderVersion) int { return b.compare(a) })
	targets := make([]AddonTarget, 0, len(versions))
	for _, v := range versions {
		if !v.supported() {
			continue
		}
		target := found[v]
		target.Path = addonPath(root, v)
		if target.State, target.InstalledVersion, err = inspectAddon(target.Path); err != nil {
			return nil, "", err
		}
		if v.extensions() {
			legacy := legacyAddonPath(root, v)
			state, version, err := inspectAddon(legacy)
			if err != nil {
				return nil, "", err
			}
			switch {
			case state == AddonForeign || state == AddonLinked:
				target.Conflict, target.ConflictState = legacy, state
			case ownsAddon(state):
				target.LegacyCopy, target.LegacyState, target.LegacyVersion = true, state, version
			}
		}
		targets = append(targets, *target)
	}
	return targets, root, nil
}

func addonPath(root string, v blenderVersion) string {
	if v.extensions() {
		return filepath.Join(root, v.String(), "extensions", "user_default", addonID)
	}
	return legacyAddonPath(root, v)
}

func legacyAddonPath(root string, v blenderVersion) string {
	return filepath.Join(root, v.String(), "scripts", "addons", addonID)
}

func inspectAddon(root string) (AddonState, string, error) {
	info, err := os.Lstat(root)
	if errors.Is(err, fs.ErrNotExist) {
		return AddonMissing, "", nil
	}
	if err != nil {
		return "", "", err
	}
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
		return AddonLinked, "", nil
	}
	if !info.IsDir() {
		return AddonForeign, "", nil
	}
	record, err := readAddonRecord(root)
	if err != nil {
		return "", "", err
	}
	if record == nil {
		return AddonForeign, "", nil
	}
	switch semver.Compare("v"+record.Version, "v"+AddonVersion()) {
	case -1:
		return AddonOutdated, record.Version, nil
	case 1:
		return AddonNewer, record.Version, nil
	}
	for name, hash := range record.Files {
		target, ok := addonFilePath(root, name)
		if !ok {
			return AddonModified, record.Version, nil
		}
		if fileDigest(target) != hash {
			return AddonModified, record.Version, nil
		}
	}
	return AddonCurrent, record.Version, nil
}

// fileDigest is empty for a file that cannot be read.
func fileDigest(name string) string {
	data, err := os.ReadFile(name)
	if err != nil {
		return ""
	}
	return digest(data)
}

// writeAddon replaces the add-on's files one at a time and removes the ones only the previous install had. The
// record goes last, so an interrupted install still reads as outdated or modified.
func writeAddon(root string, files map[string][]byte) error {
	previous, err := readAddonRecord(root)
	if err != nil {
		return err
	}
	record := addonRecordFile{Version: AddonVersion(), Files: make(map[string]string, len(files))}
	for _, name := range slices.Sorted(maps.Keys(files)) {
		target, ok := addonFilePath(root, name)
		if !ok {
			return fmt.Errorf("bundled add-on file %q is outside the add-on folder", name)
		}
		if err := writeFileAtomic(target, files[name]); err != nil {
			return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": target}})
		}
		record.Files[name] = digest(files[name])
	}
	if previous != nil {
		for name := range previous.Files {
			if _, kept := files[name]; kept {
				continue
			}
			if err := removeAddonFile(root, name); err != nil {
				return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": name}})
			}
		}
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(root, addonRecord), append(data, '\n'))
}

// removeAddonFiles deletes what the record under root lists, the bytecode Blender compiled from it and the
// record, then the folder if nothing else is left. Without a record nothing there is the app's.
func removeAddonFiles(root string) error {
	record, err := readAddonRecord(root)
	if err != nil || record == nil {
		return err
	}
	for name := range record.Files {
		if err := removeAddonFile(root, name); err != nil {
			return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"file": name}})
		}
	}
	if err := os.RemoveAll(filepath.Join(root, "__pycache__")); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(root, addonRecord)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	_ = os.Remove(root)
	return nil
}

func removeAddonFile(root, name string) error {
	target, ok := addonFilePath(root, name)
	if !ok {
		return nil
	}
	if err := os.Remove(target); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	removeEmptyParents(root, filepath.Dir(target))
	return nil
}

// removeEmptyParents removes dir and its parents below root until one still holds something.
func removeEmptyParents(root, dir string) {
	for ; !platform.SamePathFold(dir, root) && platform.SameOrChildPath(root, dir); dir = filepath.Dir(dir) {
		if os.Remove(dir) != nil {
			return
		}
	}
}

// addonFilePath resolves a record entry below root, refusing one that names a parent, a drive or an absolute
// path, which an edited record could hold.
func addonFilePath(root, name string) (string, bool) {
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, `\:`) {
		return "", false
	}
	target := filepath.Join(root, filepath.FromSlash(name))
	if platform.SamePathFold(root, target) || !platform.SameOrChildPath(root, target) {
		return "", false
	}
	return target, true
}

func readAddonRecord(root string) (*addonRecordFile, error) {
	name := filepath.Join(root, addonRecord)
	data, err := os.ReadFile(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record addonRecordFile
	if err := json.Unmarshal(bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf}), &record); err != nil {
		return nil, fmt.Errorf("parse %q: %w", name, err)
	}
	return &record, nil
}

func writeFileAtomic(name string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	return platform.WriteFileAtomic(name, data, 0o644)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// runAddonScript starts Blender without a window to disable and enable add-on modules and save its preferences.
func (m *Manager) runAddonScript(ctx context.Context, executable string, enable, disable []string) error {
	output, err := m.addon.run(ctx, executable, addonScript(enable, disable))
	if err == nil && !strings.Contains(output, addonDoneMarker) {
		err = errors.New("blender did not confirm the change")
	}
	if err != nil {
		return infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{
			"enable": enable, "disable": disable, "output": tail(output, 2000),
		}})
	}
	return nil
}

// addonScript disables before it enables, so a legacy copy is unregistered before the extension registers the
// same classes. Module names are ASCII, so their JSON arrays are Python lists.
func addonScript(enable, disable []string) string {
	list := func(modules []string) string {
		data, _ := json.Marshal(append([]string{}, modules...))
		return string(data)
	}
	return fmt.Sprintf(`import addon_utils, bpy
enable = %s
disable = %s
for module in disable:
    addon_utils.disable(module, default_set=True)
for module in enable:
    addon_utils.enable(module, default_set=True)
addons = bpy.context.preferences.addons
if all(module in addons for module in enable) and not any(module in addons for module in disable):
    bpy.ops.wm.save_userpref()
    print(%s)
`, list(enable), list(disable), strconv.Quote(addonDoneMarker))
}

func tail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}
