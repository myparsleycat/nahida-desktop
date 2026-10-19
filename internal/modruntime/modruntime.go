// Package modruntime installs the global mods that nahida features depend on.
// Every runtime is one mod in the GroupDir group of a game's Mods folder, so
// the library shows them together and they toggle like any other mod.
package modruntime

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"

	"nahida.live/desktop/internal/db"
	"nahida.live/desktop/internal/diskio"
	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/platform"
)

const (
	GroupDir     = "nhd-runtime"
	manifestFile = ".nhd-runtime.json"

	StateMissing  = "missing"
	StateCurrent  = "current"
	StateOutdated = "outdated"
	StateModified = "modified"
	StateDisabled = "disabled"
	StateForeign  = "foreign"

	// VariantBase is the file set every d3d11.dll reads: Files without the
	// directories of the Variants.
	VariantBase = "base"

	ErrorUnknown      = "MOD_RUNTIME_UNKNOWN"
	ErrorNoModsFolder = "MOD_RUNTIME_NO_MODS_FOLDER"
	ErrorForeign      = "MOD_RUNTIME_FOREIGN"
	ErrorDisabled     = "MOD_RUNTIME_DISABLED"
	ErrorNotInstalled = "MOD_RUNTIME_NOT_INSTALLED"
	ErrorInvalid      = "MOD_RUNTIME_INVALID_SETTING"

	diagnosticWhere = "ModRuntime"
	operationPrefix = "mod-runtime-"
)

// Spec describes one runtime bundled with the app.
type Spec struct {
	// Name is the install folder below GroupDir and the runtime's identity.
	Name string
	// Version moves with any change to Files, so existing installs read as
	// outdated and are updated.
	Version   string
	Importers []string
	// Namespace is the INI namespace the runtime declares. Another enabled INI
	// declaring it is a second copy the importer would load as well.
	Namespace string
	Files     fs.FS
	// Variants are alternative file sets for importers whose d3d11.dll has
	// more than the base files can use. The first one the capabilities allow
	// is installed; with none, the base files alone are.
	Variants []Variant
	// Seed builds, by slash path, files the runtime needs that its feature
	// owns. An install creates each one that is missing, and the runtime reads
	// as modified without it; updates never replace them.
	Seed map[string]func(root, modsRoot string) ([]byte, error)
	// Legacy lists earlier install locations to move below GroupDir.
	Legacy []Legacy
	// Settings are values in Files the user can change, such as hotkeys.
	Settings []Setting
}

// Variant is a set of files below Dir in Spec.Files that only a d3d11.dll
// with certain capabilities reads, such as INI keys a stock 3DMigoto build
// warns about and drops. Dir is a slash path. Its files are left out of every
// other variant and, when the variant is selected, installed over the base
// files at their paths relative to Dir, so one replaces the base file of the
// same name. A Dir that Files does not hold is a variant without files.
type Variant struct {
	Name     string
	Requires func(Capabilities) bool
	Dir      string
}

// Capabilities are the features of the d3d11.dll an importer deploys that a
// runtime's files can depend on.
type Capabilities struct {
	// ShaderSamplerSlots is whether INI shader sections can bind the extra
	// sampler slots (ps-s#, cs-s#).
	ShaderSamplerSlots bool
	DX12               bool
}

// Legacy is an install folder, as a slash path relative to the Mods folder,
// and the manifest that marks it as installed by this app.
type Legacy struct {
	Dir      string
	Manifest string
}

type Status struct {
	Name  string `json:"name"`
	State string `json:"state"`
	// Path is the install folder; the disabled one when State is disabled,
	// and the other copy when State is foreign.
	Path           string         `json:"path"`
	Version        string         `json:"version"`
	BundledVersion string         `json:"bundledVersion"`
	Settings       []SettingValue `json:"settings"`
}

type Listing struct {
	// ModsRoot is empty while no game uses the importer; Runtimes then carry no state.
	ModsRoot string   `json:"modsRoot"`
	Runtimes []Status `json:"runtimes"`
}

type GamePaths interface {
	List(context.Context) ([]db.GamePathRow, error)
}

type ModToggler interface {
	Enable(context.Context, string) (string, error)
	Disable(context.Context, string) (string, error)
}

type Options struct {
	Log       *infra.Log
	GamePaths GamePaths
	Mod       ModToggler
	// Trash moves a removed runtime to the recycle bin; nil deletes it.
	Trash func(string) error
	Specs []Spec
	// Capabilities reports what the importer's d3d11.dll can run, which
	// selects the variant of each runtime; nil installs the base variant.
	Capabilities func(ctx context.Context, importer string) (Capabilities, error)
}

type runtimeSpec struct {
	Spec
	namespace *regexp.Regexp
}

// variant selects the runtime's variant for the capabilities.
func (s runtimeSpec) variant(caps Capabilities) string {
	for _, variant := range s.Variants {
		if variant.Requires(caps) {
			return variant.Name
		}
	}
	return VariantBase
}

// selection is a runtime with the variant chosen for one importer.
type selection struct {
	runtimeSpec
	variant string
}

type Manager struct {
	log          *infra.Log
	games        GamePaths
	mod          ModToggler
	trash        func(string) error
	capabilities func(ctx context.Context, importer string) (Capabilities, error)
	specs        []runtimeSpec
	// mu keeps management actions out while a feature writes into a runtime.
	mu sync.Mutex
	// writeFile is replaced in tests to cut an install short.
	writeFile func(path string, data []byte) error
}

func New(opts Options) *Manager {
	manager := &Manager{
		log:          opts.Log,
		games:        opts.GamePaths,
		mod:          opts.Mod,
		trash:        opts.Trash,
		capabilities: opts.Capabilities,
		writeFile:    writeAtomic,
	}
	for _, spec := range opts.Specs {
		for _, variant := range spec.Variants {
			if variant.Name == "" || variant.Name == VariantBase || variant.Requires == nil ||
				variant.Dir == "." || !fs.ValidPath(variant.Dir) {
				panic(fmt.Sprintf("mod runtime %s: invalid variant %q", spec.Name, variant.Name))
			}
		}
		for _, setting := range spec.Settings {
			if setting.Kind == SettingColor && len(setting.Targets) != 3 {
				panic(fmt.Sprintf("mod runtime %s: color setting %s needs three targets", spec.Name, setting.ID))
			}
		}
		manager.specs = append(manager.specs, runtimeSpec{
			Spec: spec,
			namespace: regexp.MustCompile(
				`(?im)^[ \t]*namespace[ \t]*=[ \t]*` + regexp.QuoteMeta(spec.Namespace) + `[ \t]*\r?$`,
			),
		})
	}
	return manager
}

//wails:ignore
func (m *Manager) UseClient(client *db.Client) {
	if client != nil {
		m.games = client.GamePaths
	}
}

// List reports every runtime the importer can use, in its game's Mods folder.
func (m *Manager) List(ctx context.Context, importer string) (Listing, error) {
	listing := Listing{Runtimes: []Status{}}
	sc := &scope{stage: "resolve", fields: map[string]any{"importer": importer}}
	modsRoot, err := m.modsRoot(ctx, importer)
	if err != nil {
		return listing, m.report(sc, "list", err)
	}
	listing.ModsRoot = modsRoot
	for _, spec := range m.specs {
		if !slices.ContainsFunc(spec.Importers, func(name string) bool { return strings.EqualFold(name, importer) }) {
			continue
		}
		if modsRoot == "" {
			listing.Runtimes = append(listing.Runtimes, Status{Name: spec.Name, BundledVersion: spec.Version})
			continue
		}
		sc.fields["runtime"], sc.stage = spec.Name, "resolve-variant"
		sel, err := m.resolve(ctx, importer, spec)
		if err != nil {
			return listing, m.report(sc, "list", err)
		}
		sc.fields["variant"], sc.stage = sel.variant, "inspect"
		status, err := func() (Status, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			if err := m.migrate(modsRoot, spec); err != nil {
				return Status{}, err
			}
			return m.inspect(modsRoot, sel)
		}()
		if err != nil {
			return listing, m.report(sc, "list", err)
		}
		listing.Runtimes = append(listing.Runtimes, status)
	}
	return listing, nil
}

// Install installs, updates or repairs the runtime, enabling it first when it
// is disabled. Files the runtime's feature owns are kept.
func (m *Manager) Install(ctx context.Context, importer, name string) (Status, error) {
	return m.operate(ctx, "install", importer, name, func(sc *scope, modsRoot string, _ selection) (Status, error) {
		var installed Status
		err := m.Use(ctx, modsRoot, name, true, func(_ string, status Status) error {
			installed = status
			return nil
		})
		m.invalidateLibraries()
		return installed, err
	})
}

func (m *Manager) Disable(ctx context.Context, importer, name string) (Status, error) {
	return m.operate(
		ctx,
		"disable",
		importer,
		name,
		func(sc *scope, modsRoot string, sel selection) (Status, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			status, err := m.prepared(sc, modsRoot, sel)
			if err != nil {
				return status, err
			}
			switch status.State {
			case StateDisabled:
				return status, nil
			case StateCurrent, StateOutdated, StateModified:
			case StateForeign:
				return status, infra.ContractError(ErrorForeign + ":" + status.Path)
			default:
				return status, infra.ContractError(ErrorNotInstalled + ":" + sel.Name)
			}
			if m.mod == nil {
				return status, errors.New("mod service is unavailable")
			}
			sc.stage = "disable"
			if _, err := m.mod.Disable(ctx, status.Path); err != nil {
				return status, err
			}
			return m.inspect(modsRoot, sel)
		},
	)
}

// SetSettings stores the runtime's settings and writes them into its files,
// which also updates or repairs them. A disabled runtime stays disabled.
func (m *Manager) SetSettings(
	ctx context.Context,
	importer, name string,
	values map[string]string,
) (Status, error) {
	return m.operate(
		ctx,
		"set-settings",
		importer,
		name,
		func(sc *scope, modsRoot string, sel selection) (Status, error) {
			normalized, err := normalizeSettings(sel.runtimeSpec, values)
			if err != nil {
				return Status{}, infra.ContractError(ErrorInvalid + ":" + err.Error())
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			status, err := m.prepared(sc, modsRoot, sel)
			if err != nil {
				return status, err
			}
			switch status.State {
			case StateCurrent, StateOutdated, StateModified, StateDisabled:
			case StateForeign:
				return status, infra.ContractError(ErrorForeign + ":" + status.Path)
			default:
				return status, infra.ContractError(ErrorNotInstalled + ":" + sel.Name)
			}

			sc.stage, sc.fields["settings"] = "write", normalized
			err = func() error {
				release, err := diskio.AcquireDir(ctx, status.Path)
				if err != nil {
					return err
				}
				defer release()
				if err := writeSettings(m.writeFile, status.Path, normalized); err != nil {
					return err
				}
				return m.installAt(status.Path, modsRoot, sel)
			}()
			if err != nil {
				return status, err
			}
			return m.inspect(modsRoot, sel)
		},
	)
}

// Remove moves the runtime, together with the data its feature keeps there,
// to the recycle bin. Another copy is never removed: it is not this app's.
func (m *Manager) Remove(ctx context.Context, importer, name string) (Status, error) {
	return m.operate(ctx, "remove", importer, name, func(sc *scope, modsRoot string, sel selection) (Status, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		status, err := m.prepared(sc, modsRoot, sel)
		if err != nil {
			return status, err
		}
		if status.State == StateForeign {
			return status, infra.ContractError(ErrorForeign + ":" + status.Path)
		}
		if _, err := os.Stat(status.Path); errors.Is(err, fs.ErrNotExist) {
			return status, nil
		} else if err != nil {
			return status, err
		}

		sc.stage = "remove"
		err = func() error {
			release, err := diskio.AcquireDir(ctx, status.Path)
			if err != nil {
				return err
			}
			defer release()
			if m.trash != nil {
				return m.trash(status.Path)
			}
			return os.RemoveAll(status.Path)
		}()
		if err != nil {
			return status, err
		}
		// The group folder goes with its last runtime; Remove refuses a folder that is not empty.
		_ = os.Remove(filepath.Join(modsRoot, GroupDir))
		m.invalidateLibraries()
		return m.inspect(modsRoot, sel)
	})
}

// Refresh updates the importer's installed runtimes that read as outdated,
// such as after its d3d11.dll changed which variant they get. Disabled and
// foreign copies are left as they are.
//
//wails:ignore
func (m *Manager) Refresh(ctx context.Context, importer string) error {
	listing, err := m.List(ctx, importer)
	if err != nil {
		return err
	}
	var errs []error
	for _, status := range listing.Runtimes {
		if status.State != StateOutdated {
			continue
		}
		if _, err := m.Install(ctx, importer, status.Name); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Use runs fn with the folder the runtime keeps its data in, holding off
// management actions and the disk slot of modsRoot until fn returns. With
// install, the runtime is enabled and brought up to date first; a foreign
// copy then fails with ErrorForeign. fn must not take a disk slot.
//
//wails:ignore
func (m *Manager) Use(
	ctx context.Context,
	modsRoot, name string,
	install bool,
	fn func(root string, status Status) error,
) error {
	spec, ok := m.spec(name)
	if !ok {
		return infra.ContractError(ErrorUnknown + ":" + name)
	}
	sel, err := m.resolve(ctx, m.importerOf(ctx, modsRoot, spec), spec)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.migrate(modsRoot, spec); err != nil {
		return err
	}
	status, err := m.inspect(modsRoot, sel)
	if err != nil {
		return err
	}
	if install {
		if status, err = m.enable(ctx, modsRoot, sel, status); err != nil {
			return err
		}
	}

	release, err := diskio.AcquireDir(ctx, modsRoot)
	if err != nil {
		return err
	}
	defer release()
	if install && status.State != StateCurrent {
		if err := m.installAt(installRoot(modsRoot, spec.Name), modsRoot, sel); err != nil {
			return err
		}
		if status, err = m.inspect(modsRoot, sel); err != nil {
			return err
		}
	}
	root := installRoot(modsRoot, spec.Name)
	if status.State == StateDisabled {
		root = status.Path
	}
	return fn(root, status)
}

// enable turns a disabled install back on through the mod service, which
// takes its own disk slot, so none is held here.
func (m *Manager) enable(ctx context.Context, modsRoot string, sel selection, status Status) (Status, error) {
	switch status.State {
	case StateForeign:
		return status, infra.ContractError(ErrorForeign + ":" + status.Path)
	case StateDisabled:
	default:
		return status, nil
	}
	if m.mod == nil {
		return status, infra.ContractError(ErrorDisabled + ":" + status.Path)
	}
	if _, err := m.mod.Enable(ctx, status.Path); err != nil {
		return status, infra.AnnotateError(err, infra.Diagnostic{Fields: map[string]any{"runtimePath": status.Path}})
	}
	status, err := m.inspect(modsRoot, sel)
	if err != nil {
		return status, err
	}
	if status.State == StateForeign || status.State == StateDisabled {
		return status, infra.ContractError(fmt.Sprintf("%s:%s", ErrorDisabled, status.Path))
	}
	return status, nil
}

type scope struct {
	stage  string
	fields map[string]any
}

func (m *Manager) operate(
	ctx context.Context,
	operation, importer, name string,
	action func(sc *scope, modsRoot string, sel selection) (Status, error),
) (Status, error) {
	sc := &scope{stage: "resolve", fields: map[string]any{"importer": importer, "runtime": name}}
	spec, ok := m.spec(name)
	if !ok ||
		!slices.ContainsFunc(spec.Importers, func(item string) bool { return strings.EqualFold(item, importer) }) {
		return Status{}, infra.ContractError(ErrorUnknown + ":" + name)
	}
	modsRoot, err := m.modsRoot(ctx, importer)
	if err != nil {
		return Status{}, m.report(sc, operation, err)
	}
	if modsRoot == "" {
		return Status{}, infra.ContractError(ErrorNoModsFolder + ":" + importer)
	}
	sc.fields["modsRoot"], sc.stage = modsRoot, "resolve-variant"
	sel, err := m.resolve(ctx, importer, spec)
	if err != nil {
		return Status{}, m.report(sc, operation, err)
	}
	sc.fields["variant"] = sel.variant
	status, err := action(sc, modsRoot, sel)
	return status, m.report(sc, operation, err)
}

// prepared moves a legacy install into place and inspects the runtime.
func (m *Manager) prepared(sc *scope, modsRoot string, sel selection) (Status, error) {
	sc.stage = "inspect"
	if err := m.migrate(modsRoot, sel.runtimeSpec); err != nil {
		return Status{}, err
	}
	status, err := m.inspect(modsRoot, sel)
	sc.fields["state"], sc.fields["path"] = status.State, status.Path
	return status, err
}

// resolve picks the runtime's variant from what the importer's d3d11.dll can
// run. Without a capabilities source every importer gets the base variant.
func (m *Manager) resolve(ctx context.Context, importer string, spec runtimeSpec) (selection, error) {
	sel := selection{runtimeSpec: spec, variant: VariantBase}
	if m.capabilities == nil || len(spec.Variants) == 0 {
		return sel, nil
	}
	caps, err := m.capabilities(ctx, importer)
	if err != nil {
		return sel, fmt.Errorf("resolve capabilities of %s: %w", importer, err)
	}
	sel.variant = spec.variant(caps)
	return sel, nil
}

// importerOf names the importer whose Mods folder is modsRoot among the
// ones the runtime supports; the first one stands in when no game path says.
func (m *Manager) importerOf(ctx context.Context, modsRoot string, spec runtimeSpec) string {
	if len(spec.Importers) == 0 {
		return ""
	}
	if len(spec.Importers) == 1 || m.games == nil {
		return spec.Importers[0]
	}
	rows, err := m.games.List(ctx)
	if err != nil {
		return spec.Importers[0]
	}
	for _, row := range rows {
		if row.Importer == nil || !platform.SamePathFold(row.ModFolderPath, modsRoot) {
			continue
		}
		if slices.ContainsFunc(
			spec.Importers,
			func(name string) bool { return strings.EqualFold(name, *row.Importer) },
		) {
			return *row.Importer
		}
	}
	return spec.Importers[0]
}

func (m *Manager) report(sc *scope, operation string, err error) error {
	if err == nil || infra.IsCancellationError(err) {
		return err
	}
	var contract infra.ContractError
	if errors.As(err, &contract) {
		return err
	}
	return infra.ReportError(m.log, err, diagnosticWhere, infra.Diagnostic{
		Operation: operationPrefix + operation,
		Stage:     sc.stage,
		Fields:    sc.fields,
	})
}

func (m *Manager) spec(name string) (runtimeSpec, bool) {
	for _, spec := range m.specs {
		if spec.Name == name {
			return spec, true
		}
	}
	return runtimeSpec{}, false
}

func (m *Manager) modsRoot(ctx context.Context, importer string) (string, error) {
	if m.games == nil {
		return "", errors.New("mod runtimes are not bound to a database")
	}
	rows, err := m.games.List(ctx)
	if err != nil {
		return "", fmt.Errorf("list game paths: %w", err)
	}
	for _, row := range rows {
		if row.Importer != nil && strings.EqualFold(*row.Importer, importer) &&
			strings.TrimSpace(row.ModFolderPath) != "" {
			return row.ModFolderPath, nil
		}
	}
	return "", nil
}

func (m *Manager) invalidateLibraries() {
	if mods, ok := m.mod.(interface{ InvalidateLibraries() }); ok {
		mods.InvalidateLibraries()
	}
}
