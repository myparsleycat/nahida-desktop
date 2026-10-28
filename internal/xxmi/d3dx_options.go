package xxmi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"unicode"

	"nahida.live/desktop/internal/infra"
)

type D3DXOptionManaged string

const (
	D3DXOptionUnmanaged D3DXOptionManaged = ""
	// D3DXOptionManagedLaunch marks an option every save and launch writes from the importer settings.
	D3DXOptionManagedLaunch D3DXOptionManaged = "launch"
	// D3DXOptionManagedRendering marks an option a save or launch writes only while rendering is enforced.
	D3DXOptionManagedRendering D3DXOptionManaged = "rendering"
)

// D3DXOption is one option an importer's d3dx.ini sets in a settings section.
type D3DXOption struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	// Value is what the file holds now.
	Value   string            `json:"value"`
	Managed D3DXOptionManaged `json:"managed"`
}

func d3dxOptionKey(section, key string) string { return section + "." + key }

// splitD3DXOptionKey returns the section and option an option key names. Only options of a settings section can
// be edited: the others are commands, where a line means something only in its place, or [Loader] and [Include],
// which a launch and mod loading own.
func splitD3DXOptionKey(optionKey string) (section, key string, ok bool) {
	section, key, ok = strings.Cut(optionKey, ".")
	return section, key, ok && iniSettingKeyPattern.MatchString(section) && iniSettingKeyPattern.MatchString(key) &&
		iniSettingsSection(section)
}

// validD3DXValue reports whether value stays one option value on its line: a comment character would cut it
// short, and a line break would start another option or section.
func validD3DXValue(value string) bool {
	return value != "" && len(value) <= 1024 && value == strings.TrimSpace(value) &&
		!strings.ContainsFunc(value, func(char rune) bool {
			return char == ';' || char == '#' || unicode.IsControl(char)
		})
}

// validateD3DXValues checks that each value names one option of a settings section and stays one value on its
// line.
func validateD3DXValues(values map[string]string) error {
	seen := make(map[string]bool, len(values))
	for optionKey, value := range values {
		if _, _, ok := splitD3DXOptionKey(optionKey); !ok {
			return fmt.Errorf("invalid d3dx.ini option %q", optionKey)
		}
		// d3dx.ini names are case-insensitive, so of two spellings the file would get whichever sorts last.
		if seen[strings.ToLower(optionKey)] {
			return fmt.Errorf("duplicate d3dx.ini option %q", optionKey)
		}
		seen[strings.ToLower(optionKey)] = true
		if !validD3DXValue(value) {
			return fmt.Errorf("invalid d3dx.ini value %q for %s", value, optionKey)
		}
	}
	return nil
}

// SetD3DXOptions writes values, keyed "Section.key", into the importer's d3dx.ini in place. An option the file
// does not set yet, such as one the next launch adds from the provider's d3dx.ini, is added to its section. The
// next save or launch writes the options the importer settings manage over these values.
func (x *XXMI) SetD3DXOptions(ctx context.Context, importer string, values map[string]string) error {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	if err := validateD3DXValues(values); err != nil {
		return err
	}
	// Taken before the importer, as syncSettingsINI does, so a save meanwhile waits instead of skipping its write.
	x.settingsINIMu.Lock()
	defer x.settingsINIMu.Unlock()
	if !x.acquireImporter(importer) {
		return errors.New("XXMI_BUSY")
	}
	defer x.releaseImporter(importer)

	stage := "read-config"
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err == nil {
		stage = "write-ini"
		err = writeImporterINI(ctx, importer, cfg.ImporterFolder, func(data []byte) (*iniDocument, bool, error) {
			doc := parseINI(data)
			for _, optionKey := range slices.Sorted(maps.Keys(values)) {
				section, key, _ := splitD3DXOptionKey(optionKey)
				doc.SetOption(section, key, values[optionKey], true)
			}
			return doc, false, nil
		})
	}
	if err == nil {
		stage = "release-base"
		err = releaseINIBaseOptions(ctx, cfg.ImporterFolder, values)
	}
	if err == nil || infra.IsCancellationError(err) {
		return err
	}
	return infra.ReportError(x.log, err, "XXMI.SetD3DXOptions", infra.Diagnostic{
		Operation: "write-d3dx-options", Stage: stage,
		Fields: map[string]any{
			"importer": importer, "importerFolder": cfg.ImporterFolder, "options": slices.Sorted(maps.Keys(values)),
		},
	})
}

// MigrateD3DXOverrides writes the d3dx.ini overrides importer configs stored before the d3dx.ini tab wrote the file
// directly, then removes them from the configs. Saves and launches wrote most of them already; this reaches the
// ones saved for a disabled importer or with a release that wrote them only on launch. An importer whose write
// fails keeps its overrides for the next start.
//
//wails:ignore
func (x *XXMI) MigrateD3DXOverrides(ctx context.Context) error {
	x.mu.RLock()
	client := x.client
	x.mu.RUnlock()
	if client == nil {
		return errors.New("XXMI settings store is not configured")
	}
	rows, err := client.XXMIImporters.List(ctx)
	if err != nil {
		return err
	}

	var errs []error
	for _, row := range rows {
		var stored struct {
			D3DXOverrides map[string]string `json:"d3dxOverrides"`
		}
		// A config that cannot be read is reported by the reads that need it.
		if json.Unmarshal([]byte(row.Config), &stored) != nil || stored.D3DXOverrides == nil {
			continue
		}
		if err := x.applyStoredD3DXOverrides(ctx, row.Key, stored.D3DXOverrides); err != nil {
			errs = append(errs, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "migrate-d3dx-overrides", Stage: "write-ini", Fields: map[string]any{"importer": row.Key},
			}))
			continue
		}
		if err := client.XXMIImporters.RemoveConfigValue(ctx, row.Key, "$.d3dxOverrides"); err != nil {
			errs = append(errs, infra.AnnotateError(err, infra.Diagnostic{
				Operation: "migrate-d3dx-overrides",
				Stage:     "remove-overrides",
				Fields:    map[string]any{"importer": row.Key},
			}))
		}
	}
	return errors.Join(errs...)
}

// applyStoredD3DXOverrides writes the overrides of the options the d3dx.ini tab lists as editable, as a launch
// applied them: an option the importer settings manage keeps their value, and one the file and the next provider
// release leave out or comment out stays that way.
func (x *XXMI) applyStoredD3DXOverrides(ctx context.Context, importer string, overrides map[string]string) error {
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return err
	}
	options, err := x.GetD3DXOptions(ctx, importer)
	if err != nil {
		return err
	}

	// An override could spell the option differently than the file does, as d3dx.ini names are case-insensitive.
	stored := make(map[string]string, len(overrides))
	for optionKey, value := range overrides {
		stored[strings.ToLower(optionKey)] = value
	}
	values := map[string]string{}
	for _, option := range options {
		if option.Managed == D3DXOptionManagedLaunch ||
			option.Managed == D3DXOptionManagedRendering && cfg.Migoto.EnforceRendering {
			continue
		}
		// An override the file already holds is written too, so the base stops claiming it as a provider default.
		optionKey := d3dxOptionKey(option.Section, option.Key)
		if value, ok := stored[strings.ToLower(optionKey)]; ok && validD3DXValue(value) {
			values[optionKey] = value
		}
	}
	if len(values) == 0 {
		return nil
	}
	return x.SetD3DXOptions(ctx, importer, values)
}

// GetD3DXOptions lists the options the settings sections of the importer's d3dx.ini set, in file order, or none
// while it has no d3dx.ini.
func (x *XXMI) GetD3DXOptions(ctx context.Context, importer string) ([]D3DXOption, error) {
	importer = strings.ToUpper(strings.TrimSpace(importer))
	cfg, err := x.GetImporterConfig(ctx, importer)
	if err != nil {
		return nil, err
	}

	data, err := readImporterINI(cfg.ImporterFolder)
	if errors.Is(err, os.ErrNotExist) {
		return []D3DXOption{}, nil
	}
	if err != nil {
		return nil, infra.ReportError(x.log, err, "XXMI.GetD3DXOptions", infra.Diagnostic{
			Operation: "read-d3dx-options", Stage: "read-ini",
			Fields: map[string]any{"importer": importer, "importerFolder": cfg.ImporterFolder},
		})
	}

	// The next launch deploys the provider's release and rebuilds the file from its d3dx.ini, so its options are
	// listed, can be edited, and are locked as that release reads them before that launch has run.
	template, libsVersion := x.pendingProviderINI(ctx, cfg)
	in := importerINIInput{key: importer, cfg: cfg, support: migotoINISupport(cfg, libsVersion), template: template}
	if len(in.template) > 0 {
		in.base = x.readINIBase(importer, cfg.ImporterFolder)
	}
	doc, _ := in.merge(data)

	// Rendering options are marked while they are not enforced too: the settings page locks them as soon as
	// enforcing is switched on, before that is saved.
	enforced := cfg
	enforced.Migoto.EnforceRendering = true
	managed := map[string]D3DXOptionManaged{}
	for _, write := range settingsINIWrites(importer, enforced, in.support) {
		if !write.fallback {
			managed[strings.ToLower(d3dxOptionKey(write.section, write.key))] = write.managed
		}
	}

	options, listed, section := []D3DXOption{}, map[string]bool{}, ""
	for _, line := range doc.lines {
		if name, ok := iniSectionName(line); ok {
			section = name
			continue
		}
		match := iniOptionPattern.FindStringSubmatch(line)
		if len(match) == 0 {
			continue
		}
		key := match[2]
		optionKey := d3dxOptionKey(section, key)
		if _, _, ok := splitD3DXOptionKey(optionKey); !ok || listed[strings.ToLower(optionKey)] {
			continue
		}
		listed[strings.ToLower(optionKey)] = true

		value, _ := doc.Option(section, key)
		options = append(options, D3DXOption{
			Section: section, Key: key, Value: value, Managed: managed[strings.ToLower(optionKey)],
		})
	}
	return options, nil
}

// pendingProviderINI returns the cached d3dx.ini of the provider release the importer's next deployment
// writes, along with the XXMI libraries release deployed under it, or nil and "". Nothing is downloaded: a
// release that is not cached yet lists its options after the launch that fetches it.
func (x *XXMI) pendingProviderINI(ctx context.Context, cfg ImporterConfig) (providerINI []byte, libsVersion string) {
	spec, err := x.deployedLibsProvider(ctx, cfg)
	if err != nil || spec.overlayPackage == "" {
		return nil, ""
	}
	pin, _, err := x.libsPin(ctx, cfg)
	if err != nil {
		return nil, ""
	}
	version, err := x.resolveProviderDLLVersion(ctx, spec, pin, cfg.ImporterFolder)
	if err != nil {
		return nil, ""
	}
	root, err := xxmiCacheRoot()
	if err != nil {
		return nil, ""
	}
	if _, _, err := x.readVerifiedProviderDLL(ctx, root, spec, version); err != nil {
		return nil, ""
	}
	providerINI, err = x.readVerifiedProviderINI(ctx, root, spec, version)
	if err != nil {
		return nil, ""
	}
	return providerINI, signedLibsVersion(version)
}
