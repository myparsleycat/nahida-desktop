package modelviewer

import (
	"slices"
	"strconv"
	"strings"
)

// The original GLB contains the Bang mesh but does not carry 3DMigoto buffer hashes.
var genshinBangCharacters = map[string]string{
	"35e439e2": "columbina", //nolint:misspell // Columbina is the GIMI character ID.
	"34111581": "nicole",
	"425d891c": "odette",
}

type modelViewerBangOverride struct {
	character, form string
	conditions      ModelViewerDNF
}

func (o modelViewerBangOverride) active(resolved map[string]any, form string) bool {
	return viewerFormVisible(o.form, form) && viewerEvalDNFSatisfied(o.conditions, resolved)
}

func collectModelViewerBangOverrides(
	sections []modINISection,
	defaults map[string]any,
	form string,
) ([]modelViewerBangOverride, error) {
	var overrides []modelViewerBangOverride
	for _, section := range sections {
		if !strings.EqualFold(section.Header, "TextureOverride") {
			continue
		}
		hash := strings.ToLower(modelViewerSectionValue(section, "hash"))
		character, known := genshinBangCharacters[hash]
		if !known {
			continue
		}
		if first := modelViewerSectionValue(section, "match_first_index"); first != "" {
			if index, err := strconv.Atoi(first); err != nil || index != 0 {
				continue
			}
		}
		var conditions ModelViewerDNF
		_, variables, err := scanModelViewerSymbolicRootWithOverride(sections, section, defaults, &conditions)
		if err != nil {
			return nil, err
		}
		if conditions = normalizeModelViewerDNFWithDomains(conditions, variables); len(conditions) != 0 {
			overrides = append(
				overrides,
				modelViewerBangOverride{character: character, form: form, conditions: conditions},
			)
		}
	}
	return overrides, nil
}

// HasGenshinBangOverrides reports whether any INI condition overrides this character's original Bang draw.
func (t ModelViewerTransport) HasGenshinBangOverrides(character string) bool {
	return slices.ContainsFunc(t.bangOverrides, func(override modelViewerBangOverride) bool {
		return override.character == character
	})
}

// OverridesGenshinBangs reports whether the mod suppresses the original Bang draw in the resolved state.
func (t ModelViewerTransport) OverridesGenshinBangs(character string, resolved map[string]any, form string) bool {
	for _, override := range t.bangOverrides {
		if override.character == character && override.active(resolved, form) {
			return true
		}
	}
	return false
}
