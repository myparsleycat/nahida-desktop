package xxmi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/mod/semver"

	"nahida.live/desktop/internal/infra"
)

// migotoLogLevelVersion is the first XXMI libraries release that reads log_level, the [Input] switches,
// and clear_unknown_settings. Earlier releases and the legacy 3DMigoto runtime read only calls and debug.
const migotoLogLevelVersion = "v1.1.7"

// migotoSupport names the options the deployed d3d11.dll reads: the [System] options a launch sets, which every
// XXMI libraries release has, the ones migotoLogLevelVersion names, and the [Desktop] section of a provider
// release with the desktop channel.
type migotoSupport struct {
	system, logLevel, desktopChannel bool
}

// iniWrite is one option the importer settings write into d3dx.ini.
type iniWrite struct {
	section, key, value string
	managed             D3DXOptionManaged
	// fallback marks a default written only where the file did not set the option before the write, so the
	// user and the importer package keep their own value.
	fallback bool
}

// loaderINI holds the [Loader] options a launch writes.
type loaderINI struct {
	target string
	// helper is the elevated helper that loads the XXMI DLL, or "" for a runtime the helper does not load.
	helper string
}

// importerINIInput is what one write puts into an importer's d3dx.ini. render applies it in a fixed order, each
// layer over the one before: the provider's template, the values a logging session puts back, the [Loader] of a
// launch, the saved overrides, the importer settings, and the options of a running logging session.
type importerINIInput struct {
	key     string
	cfg     ImporterConfig
	support migotoSupport
	// template is the provider's d3dx.ini the file is rebuilt from, or nil to edit the file in place. base is the
	// template of the previous rebuild, or nil when there was none.
	template, base []byte
	loader         *loaderINI
	restore        map[string]*string
	diagnostics    *diagnosticsRecord
}

func newImporterINIInput(key string, cfg ImporterConfig) importerINIInput {
	return importerINIInput{key: key, cfg: cfg, support: migotoINISupport(cfg, "")}
}

// merge returns the file rebuilt from the template, or the file itself without one. lost reports whether a line
// of data is gone.
func (in importerINIInput) merge(data []byte) (doc *iniDocument, lost bool) {
	if len(in.template) == 0 {
		return parseINI(data), false
	}
	return mergeINI(in.template, in.base, data)
}

func (in importerINIInput) render(data []byte) (doc *iniDocument, lost bool) {
	// A fallback yields to an option the file set before this write, which the template may have replaced.
	before := parseINI(data)
	doc, lost = in.merge(data)

	for _, restored := range diagnosticsRestoredOptions {
		restoreDiagnosticsOption(doc, restored.section, restored.option, in.restore)
	}
	if in.loader != nil {
		doc.SetOption("Loader", "target", in.loader.target, true)
		doc.SetOption("Loader", "module", "d3d11.dll", true)
		// 3DMigoto warns about an empty `launch =` line, and the helper starts the game itself.
		doc.RemoveOption("Loader", "launch")
		if in.loader.helper != "" {
			doc.SetOption("Loader", "loader", in.loader.helper, true)
		} else {
			doc.RemoveOption("Loader", "loader")
		}
	}
	applyD3DXOverrides(doc, in.cfg.D3DXOverrides)
	for _, write := range settingsINIWrites(in.key, in.cfg, in.support) {
		if write.fallback && len(before.optionIndexes(write.section, write.key)) > 0 {
			continue
		}
		doc.SetOption(write.section, write.key, write.value, true)
	}
	// A DLL without the channel warns about options it does not know, so they leave with a provider switch.
	if !in.support.desktopChannel {
		doc.RemoveOption("Desktop", "channel")
	}
	if !in.support.desktopChannel || strings.TrimSpace(in.cfg.Migoto.InspectKey) == "" {
		doc.RemoveOption("Desktop", "inspect_key")
	}
	if in.diagnostics != nil {
		applyDiagnosticsINI(doc, *in.diagnostics, in.support.logLevel)
	}
	return doc, lost
}

func (in importerINIInput) write(ctx context.Context) error {
	return writeImporterINI(ctx, in.key, in.cfg.ImporterFolder, func(data []byte) (*iniDocument, bool, error) {
		doc, lost := in.render(data)
		return doc, lost, nil
	})
}

// settingsINIWrites lists the options the importer settings decide, in the order they are written. Saved
// overrides are written before them, so a setting wins over an override of the option it manages.
func settingsINIWrites(key string, cfg ImporterConfig, support migotoSupport) []iniWrite {
	var writes []iniWrite
	set := func(section, option, value string) {
		writes = append(writes, iniWrite{section: section, key: option, value: value, managed: D3DXOptionManagedLaunch})
	}
	boolean := func(section, option string, enabled bool, on, off string) {
		if enabled {
			set(section, option, on)
		} else {
			set(section, option, off)
		}
	}

	if support.system {
		metric := syscall.NewLazyDLL("user32.dll").NewProc("GetSystemMetrics")
		width, _, _ := metric.Call(0)
		height, _, _ := metric.Call(1)
		set("System", "dll_initialization_delay", strconv.Itoa(cfg.XXMIDLLInitDelay))
		set("System", "screen_width", strconv.FormatUint(uint64(width), 10))
		set("System", "screen_height", strconv.FormatUint(uint64(height), 10))
	}

	migoto := cfg.Migoto
	if migoto.EnforceRendering {
		for _, option := range enforcedRenderingOptions(key) {
			writes = append(writes, iniWrite{
				section: "Rendering", key: option[0], value: option[1], managed: D3DXOptionManagedRendering,
			})
		}
	}
	if support.logLevel {
		set("Logging", "log_level", strings.ToLower(migoto.LogLevel))
		boolean("System", "clear_unknown_settings", migoto.ClearUnknownSettings, "1", "0")
		boolean("Input", "input", migoto.Input, "1", "0")
		set("Input", "input_disable_mode", strings.ToLower(migoto.InputDisableMode))

		// The hotkey setting is a default, and a saved override takes its place.
		hotkey := migoto.ToggleInput
		if value, ok := lookupD3DXOverride(cfg.D3DXOverrides, "Input", "toggle_input"); ok && validD3DXValue(value) {
			hotkey = value
		}
		if hotkey != "" {
			writes = append(writes, iniWrite{section: "Input", key: "toggle_input", value: hotkey, fallback: true})
		}
	} else {
		boolean("Logging", "calls", migoto.LogLevel == "Info" || migoto.LogLevel == "Debug", "1", "0")
		boolean("Logging", "debug", migoto.LogLevel == "Debug", "1", "0")
	}
	boolean("Logging", "show_warnings", migoto.MuteWarnings, "0", "1")
	boolean("Hunting", "hunting", migoto.EnableHunting, "2", "0")
	boolean("Hunting", "marking_actions", migoto.DumpShaders, "clipboard hlsl asm regex", "clipboard")
	if support.desktopChannel {
		set("Desktop", "channel", "1")
		if key := strings.TrimSpace(migoto.InspectKey); key != "" {
			set("Desktop", "inspect_key", key)
		}
	}
	return writes
}

// enforcedRenderingOptions returns the [Rendering] values a launch writes for importer while its rendering
// options are enforced.
func enforcedRenderingOptions(importer string) [][2]string {
	textures, regions, implicitIndices, resize := "0", "0", "", "1"
	switch importer {
	case "WWMI":
		textures = "1"
	case "SRMI":
		implicitIndices = "1"
	case "EFMI":
		regions, implicitIndices, resize = "1", "1", "0"
	}
	options := [][2]string{
		{"texture_hash", textures},
		{"track_texture_updates", textures},
		{"track_region_hashes", regions},
	}
	if implicitIndices != "" {
		options = append(options, [2]string{"track_implicit_index_buffers", implicitIndices})
	}
	return append(options, [2]string{"allow_buffer_resize", resize})
}

// migotoINISupport reports the options the d3d11.dll of the importer folder reads. libsVersion is the XXMI
// libraries release the next deployment puts there, or "" for the one deployed now.
func migotoINISupport(cfg ImporterConfig, libsVersion string) migotoSupport {
	var manifest runtimeManifest
	if data, err := os.ReadFile(filepath.Join(cfg.ImporterFolder, runtimeManifestName)); err == nil {
		_ = json.Unmarshal(data, &manifest)
	}
	if cfg.Mode == RuntimeXXMI && manifest.UserManaged[customDLLName] == "" {
		if libsVersion == "" {
			libsVersion, _ = deployedLibsVersion(cfg.ImporterFolder)
		}
		return migotoSupport{
			system: true,
			logLevel: semver.IsValid("v"+libsVersion) &&
				semver.Compare("v"+libsVersion, migotoLogLevelVersion) >= 0,
			desktopChannel: deployedDesktopChannel(manifest),
		}
	}

	// Neither the libraries version nor the version resource describes a custom DLL, one unsafe mode kept,
	// or a legacy runtime: XXMI builds before 1.2.0 carry the description and version of stock 3DMigoto.
	// The option names a DLL reads are in its image, as UTF-16 in all but a few builds.
	path := filepath.Join(cfg.ImporterFolder, customDLLName)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > customDLLSizeLimit {
		return migotoSupport{}
	}
	image, err := os.ReadFile(path)
	if err != nil {
		return migotoSupport{}
	}
	names := func(option string) bool {
		wide := make([]byte, 0, 2*len(option))
		for _, char := range []byte(option) {
			wide = append(wide, char, 0)
		}
		return bytes.Contains(image, wide) || bytes.Contains(image, []byte(option))
	}
	return migotoSupport{
		system:   names("dll_initialization_delay"),
		logLevel: cfg.Mode == RuntimeXXMI && names("log_level"),
		desktopChannel: cfg.Mode == RuntimeXXMI &&
			customDLLFeatures(os.Getenv(customDLLFeaturesEnv)).DesktopChannel,
	}
}

// deployedDesktopChannel reports whether the provider d3d11.dll a deployment wrote serves the desktop channel.
func deployedDesktopChannel(manifest runtimeManifest) bool {
	overlay, version, ok := strings.Cut(manifest.Provider, "@")
	if !ok {
		return false
	}
	for _, spec := range libsProviders {
		if spec.overlayPackage != "" && spec.overlayPackage == overlay {
			return providerFeatureAt(spec, version, spec.desktopChannelSince)
		}
	}
	return false
}

// updateLaunchINI writes the options this launch decides into the importer's d3dx.ini. providerINI is the
// d3dx.ini of the deployed libraries provider, or nil; the file is rebuilt from it with the values it had.
func (x *XXMI) updateLaunchINI(
	ctx context.Context,
	key string,
	cfg ImporterConfig,
	processName string,
	providerINI []byte,
) error {
	diagnostics, err := x.armedDiagnostics(ctx, key, cfg.ImporterFolder)
	if err != nil {
		return err
	}
	in := newImporterINIInput(key, cfg)
	in.template, in.loader, in.diagnostics = providerINI, &loaderINI{target: processName}, diagnostics
	if cfg.Mode == RuntimeXXMI {
		in.loader.helper = x.elevated.HelperImageName()
	}
	if len(providerINI) > 0 {
		in.base = x.readINIBase(key, cfg.ImporterFolder)
	}
	if err := in.write(ctx); err != nil {
		return err
	}

	// Without the base the next rebuild keeps every value the file holds, which is what it did before bases
	// were recorded, so a failure only warns.
	if len(providerINI) > 0 {
		if err := storeINIBase(ctx, cfg.ImporterFolder, providerINI); err != nil && !infra.IsCancellationError(err) {
			_ = infra.ReportError(x.log, err, "XXMI.StartGame", infra.Diagnostic{
				Severity: infra.DiagnosticWarn, Operation: "store-d3dx-base", Stage: "write-base",
				Fields: map[string]any{"importer": key, "importerFolder": cfg.ImporterFolder, "file": iniBaseName},
			})
		}
	}
	return nil
}

// iniBaseName is the provider's d3dx.ini the importer's d3dx.ini was last rebuilt from, kept in the importer
// folder so it moves with the file. It has no .ini extension, so tools that read every INI file of the folder
// pass over it.
const iniBaseName = ".nahida-d3dx-base"

// readINIBase returns the template of the importer's previous rebuild, or nil when there is none or it cannot be
// read; the rebuild then keeps every value the file holds.
func (x *XXMI) readINIBase(key, folder string) []byte {
	root, err := openInstallRoot(folder)
	if err == nil {
		defer func() { _ = root.Close() }()
		var data []byte
		data, _, err = root.readFile(iniBaseName)
		if err == nil && len(data) <= providerINISizeLimit {
			return data
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = infra.ReportError(x.log, err, "XXMI.ReadINIBase", infra.Diagnostic{
			Severity: infra.DiagnosticWarn, Operation: "read-d3dx-base", Stage: "read-base",
			Fields: map[string]any{"importer": key, "importerFolder": folder, "file": iniBaseName},
		})
	}
	return nil
}

func storeINIBase(ctx context.Context, folder string, template []byte) error {
	root, err := openInstallRoot(folder)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	current, snapshot, err := root.readFile(iniBaseName)
	if err == nil && bytes.Equal(current, template) {
		return nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return root.writeFileAtomic(ctx, iniBaseName, bytes.NewReader(template), 0o600, snapshot)
}

// writeSettingsINI writes the options the saved settings decide into the importer's d3dx.ini in place, so the
// file follows a save before the next launch. Rebuilding from the provider's d3dx.ini and the [Loader] options
// are left to the launch, which also deploys the libraries they depend on. The options of a logging session,
// when there is one, stay on top of the settings.
func writeSettingsINI(ctx context.Context, key string, cfg ImporterConfig, diagnostics *diagnosticsRecord) error {
	in := newImporterINIInput(key, cfg)
	in.diagnostics = diagnostics
	err := in.write(ctx)
	// An importer folder or d3dx.ini that is not there yet gets the settings from the next launch.
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// syncSettingsINI writes the latest saved settings into d3dx.ini unless another operation holds the importer: a
// running launch writes them itself. A failure leaves the saved config in place for the next launch to write.
func (x *XXMI) syncSettingsINI(ctx context.Context, key, action string) {
	// The config is read under the lock rather than passed in, so a save that reaches here late cannot write
	// over a newer save that already wrote d3dx.ini.
	x.settingsINIMu.Lock()
	defer x.settingsINIMu.Unlock()
	if !x.acquireImporter(key) {
		return
	}
	defer x.releaseImporter(key)

	stage := "read-config"
	cfg, err := x.GetImporterConfig(ctx, key)
	if err == nil {
		stage = "end-moved-diagnostics"
		err = x.endMovedDiagnostics(ctx, key, cfg)
	}
	if err == nil {
		if !cfg.Enabled {
			return
		}
		stage = "read-diagnostics"
		var diagnostics *diagnosticsRecord
		diagnostics, err = x.armedDiagnostics(ctx, key, cfg.ImporterFolder)
		if err == nil {
			stage = "write-ini"
			err = writeSettingsINI(ctx, key, cfg, diagnostics)
		}
	}
	if err == nil || infra.IsCancellationError(err) {
		return
	}
	_ = infra.ReportError(x.log, err, action, infra.Diagnostic{
		Operation: "write-d3dx-settings", Stage: stage,
		Fields: map[string]any{"importer": key, "importerFolder": cfg.ImporterFolder},
	})
}

// openImporterINI opens the folder of the importer's d3dx.ini and returns the file's name in it. The reference
// launcher may leave a link in its place, which is followed to its target. closeINI releases both roots.
func openImporterINI(folder string) (root *installRoot, name string, closeINI func(), err error) {
	importerRoot, err := openInstallRoot(folder)
	if err != nil {
		return nil, "", nil, err
	}
	linked, name, err := importerRoot.resolveUserFile("d3dx.ini")
	if err != nil {
		_ = importerRoot.Close()
		return nil, "", nil, err
	}
	if linked == nil {
		return importerRoot, name, func() { _ = importerRoot.Close() }, nil
	}
	return linked, name, func() {
		_ = linked.Close()
		_ = importerRoot.Close()
	}, nil
}

func readImporterINI(folder string) ([]byte, error) {
	root, name, closeINI, err := openImporterINI(folder)
	if err != nil {
		return nil, err
	}
	defer closeINI()
	data, _, err := root.readFile(name)
	return data, err
}

// writeImporterINI replaces the importer's d3dx.ini with what render makes of it, and writes nothing when that
// is the same file. A file that loses lines is backed up first, and the backup is removed when the write fails.
func writeImporterINI(
	ctx context.Context,
	key, folder string,
	render func(data []byte) (doc *iniDocument, lost bool, err error),
) error {
	root, name, closeINI, err := openImporterINI(folder)
	if err != nil {
		return err
	}
	defer closeINI()
	data, info, err := root.readFile(name)
	if err != nil {
		return err
	}

	doc, lost, err := render(data)
	if err != nil {
		return err
	}
	out := doc.Bytes()
	if bytes.Equal(out, data) {
		return nil
	}

	var backupFolder string
	if lost {
		backupFolder, err = backupImporterINI(key, data)
		if err != nil {
			return fmt.Errorf("back up d3dx.ini: %w", err)
		}
	}
	err = root.writeFileAtomic(ctx, name, bytes.NewReader(out), 0o600, info)
	if err != nil && backupFolder != "" {
		if cleanupErr := os.RemoveAll(backupFolder); cleanupErr != nil {
			err = errors.Join(err, fmt.Errorf("remove failed INI write backup %q: %w", backupFolder, cleanupErr))
		}
	}
	return err
}

// backupImporterINI keeps a d3dx.ini that is about to be replaced or lose lines.
func backupImporterINI(key string, data []byte) (string, error) {
	root, err := xxmiCacheRoot()
	if err != nil {
		return "", err
	}
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupRoot, 0o700); err != nil {
		return "", err
	}
	folder, err := os.MkdirTemp(backupRoot, key+" "+time.Now().Format("2006-01-02 15-04-05")+"-")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(folder, "d3dx.ini"), data, 0o600); err != nil {
		return "", errors.Join(err, os.RemoveAll(folder))
	}
	return folder, nil
}
