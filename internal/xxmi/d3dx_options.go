package xxmi

import (
	"context"
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
	// Value is what the file holds now; saving the importer settings and every launch write their overrides.
	Value   string            `json:"value"`
	Managed D3DXOptionManaged `json:"managed"`
}

func d3dxOverrideKey(section, key string) string { return section + "." + key }

// splitD3DXOverrideKey returns the section and option an override key names. Only options of a settings section
// can be overridden: the others are commands, where a line means something only in its place, or [Loader] and
// [Include], which a launch and mod loading own.
func splitD3DXOverrideKey(overrideKey string) (section, key string, ok bool) {
	section, key, ok = strings.Cut(overrideKey, ".")
	return section, key, ok && iniSettingKeyPattern.MatchString(section) && iniSettingKeyPattern.MatchString(key) &&
		iniSettingsSection(section)
}

// lookupD3DXOverride returns the saved override of an option under any spelling of its name, as d3dx.ini does.
func lookupD3DXOverride(overrides map[string]string, section, key string) (string, bool) {
	for overrideKey, value := range overrides {
		if strings.EqualFold(overrideKey, d3dxOverrideKey(section, key)) {
			return value, true
		}
	}
	return "", false
}

// validD3DXValue reports whether value stays one option value on its line: a comment character would cut it
// short, and a line break would start another option or section.
func validD3DXValue(value string) bool {
	return value != "" && len(value) <= 1024 && value == strings.TrimSpace(value) &&
		!strings.ContainsFunc(value, func(char rune) bool {
			return char == ';' || char == '#' || unicode.IsControl(char)
		})
}

// validateD3DXOverrides checks that each override names one option of a settings section and stays one value on
// its line. An override of an option the importer settings manage is accepted and has no effect, since the
// settings are written after it.
func validateD3DXOverrides(overrides map[string]string) error {
	seen := make(map[string]bool, len(overrides))
	for overrideKey, value := range overrides {
		if _, _, ok := splitD3DXOverrideKey(overrideKey); !ok {
			return fmt.Errorf("invalid d3dx.ini option %q", overrideKey)
		}
		// d3dx.ini names are case-insensitive, so of two spellings a launch would write whichever sorts last.
		if seen[strings.ToLower(overrideKey)] {
			return fmt.Errorf("duplicate d3dx.ini option %q", overrideKey)
		}
		seen[strings.ToLower(overrideKey)] = true
		if !validD3DXValue(value) {
			return fmt.Errorf("invalid d3dx.ini value %q for %s", value, overrideKey)
		}
	}
	return nil
}

// applyD3DXOverrides writes the saved overrides of options the file already sets. An option the importer
// package leaves out or commented out stays that way.
func applyD3DXOverrides(doc *iniDocument, overrides map[string]string) {
	for _, overrideKey := range slices.Sorted(maps.Keys(overrides)) {
		section, key, ok := splitD3DXOverrideKey(overrideKey)
		value := overrides[overrideKey]
		if !ok || !validD3DXValue(value) {
			continue
		}
		if _, present := doc.Option(section, key); present {
			doc.SetOption(section, key, value, true)
		}
	}
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
	// listed, can be overridden, and are locked as that release reads them before that launch has run.
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
			managed[strings.ToLower(d3dxOverrideKey(write.section, write.key))] = write.managed
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
		overrideKey := d3dxOverrideKey(section, key)
		if _, _, ok := splitD3DXOverrideKey(overrideKey); !ok || listed[strings.ToLower(overrideKey)] {
			continue
		}
		listed[strings.ToLower(overrideKey)] = true

		value, _ := doc.Option(section, key)
		options = append(options, D3DXOption{
			Section: section, Key: key, Value: value, Managed: managed[strings.ToLower(overrideKey)],
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
