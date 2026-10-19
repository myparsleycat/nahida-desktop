package liquidemitter

import (
	"embed"
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"nahida.live/desktop/internal/infra"
	"nahida.live/desktop/internal/modruntime"
	"nahida.live/desktop/internal/platform"
)

//go:embed runtime
var runtimeAssets embed.FS

var disabledPrefixRE = regexp.MustCompile(`(?i)^disabled[\s_-]*`)

const (
	// runtimeEnhancedDir below runtime/ holds the files only the forked
	// d3d11.dll reads; it is empty until the renderer uses its features.
	runtimeEnhancedDir     = "enhanced"
	runtimeVariantEnhanced = "enhanced"
)

// RuntimeSpec is the LiquidGravityProbe global mod the emitter hooks call.
// Hooks reach it by namespace, so its folder can move without touching them.
func RuntimeSpec() modruntime.Spec {
	files, err := fs.Sub(runtimeAssets, "runtime")
	if err != nil {
		panic(err)
	}
	return modruntime.Spec{
		Name:      runtimeName,
		Version:   runtimeVersion,
		Importers: []string{"EFMI"},
		Namespace: runtimeNamespace,
		Files:     files,
		// The forked d3d11.dll's sampler slots let the renderer bind more
		// resources; the INI files that use them go below enhanced/ and only
		// reach importers whose DLL reads them.
		Variants: []modruntime.Variant{{
			Name:     runtimeVariantEnhanced,
			Requires: func(caps modruntime.Capabilities) bool { return caps.ShaderSamplerSlots },
			Dir:      runtimeEnhancedDir,
		}},
		Seed:     map[string]func(string, string) ([]byte, error){profileLoaderFile: seedLoader},
		Legacy:   []modruntime.Legacy{{Dir: "LiquidGravityProbe/LiquidGravityProbe", Manifest: legacyRuntimeManifest}},
		Settings: runtimeSettings(),
	}
}

func runtimeSettings() []modruntime.Setting {
	key := func(id, section, vk string) modruntime.Setting {
		return modruntime.Setting{
			ID:      id,
			Kind:    modruntime.SettingKey,
			Default: "no_alt no_ctrl no_shift " + vk,
			Targets: []modruntime.Target{{File: "Probe.ini", Section: section, Name: "key"}},
		}
	}
	constant := func(name string) modruntime.Target {
		return modruntime.Target{File: "Probe.ini", Section: "Constants", Name: "global " + name}
	}
	number := func(id, name, value string, low, high, step float64) modruntime.Setting {
		return modruntime.Setting{
			ID:      id,
			Kind:    modruntime.SettingNumber,
			Default: value,
			Min:     low,
			Max:     high,
			Step:    step,
			Targets: []modruntime.Target{constant(name)},
		}
	}
	return []modruntime.Setting{
		key("toggleKey", "KeyToggle", "VK_NUMPAD1"),
		key("strengthUpKey", "KeyStrengthUp", "VK_NUMPAD2"),
		key("strengthDownKey", "KeyStrengthDown", "VK_NUMPAD3"),
		key("autoPulseKey", "KeyAutoPulse", "VK_NUMPAD4"),
		key("manualPulseKey", "KeyManualPulse", "VK_NUMPAD0"),
		// The toggle compares the request with the last one it saw, so starting
		// off must set all three or the first frame would read it as a toggle.
		{
			ID:      "startEnabled",
			Kind:    modruntime.SettingBool,
			Default: "1",
			Targets: []modruntime.Target{
				constant("$emitting"), constant("$effect_requested"), constant("$last_effect_request"),
			},
		},
		{
			ID:      "autoPulse",
			Kind:    modruntime.SettingBool,
			Default: "0",
			Targets: []modruntime.Target{constant("$auto_pulse")},
		},
		{
			ID:      "strength",
			Kind:    modruntime.SettingInt,
			Default: "3",
			Min:     0,
			Max:     7,
			Targets: []modruntime.Target{constant("$strength_level")},
		},
		// The look of the screen-space fluid, which Probe.ini hands to
		// FluidComposite.hlsl through IniParams.
		{
			ID:      "tint",
			Kind:    modruntime.SettingColor,
			Default: "0.9 0.75 0.03",
			Targets: []modruntime.Target{
				constant("$liquid_tint_r"), constant("$liquid_tint_g"), constant("$liquid_tint_b"),
			},
		},
		number("absorptionDepth", "$liquid_absorb_mm", "15", 2, 60, 1),
		number("wetGround", "$liquid_wet_darken", "0.85", 0.5, 1, 0.01),
		number("puddleReflection", "$liquid_puddle_reflect", "0.15", 0, 0.5, 0.01),
		number("streamReflection", "$liquid_stream_reflect", "1.6", 0, 3, 0.1),
		number("scattering", "$liquid_scatter", "0.35", 0, 1, 0.05),
		number("refraction", "$liquid_refraction", "0.02", 0, 0.06, 0.005),
	}
}

// seedLoader gives an install without one the profile loader its INI
// includes, binding the profiles left from an earlier install.
func seedLoader(root, modsRoot string) ([]byte, error) {
	layout := runtimeLayout{root: root}
	index, err := readIndex(layout, modsRoot)
	if err != nil {
		return nil, err
	}
	return loaderINI(loaderKeys(layout, index, "", false)), nil
}

// runtimeError keeps the emitter's own codes for runtime states the renderer
// explains in the tool.
func runtimeError(err error) error {
	var contract infra.ContractError
	if !errors.As(err, &contract) {
		return err
	}
	code, detail, _ := strings.Cut(string(contract), ":")
	switch code {
	case modruntime.ErrorForeign:
		return infra.ContractError(ErrorRuntimeForeign + ":" + detail)
	case modruntime.ErrorDisabled:
		return infra.ContractError(ErrorRuntimeDisabled + ":" + detail)
	}
	return err
}

// physicalPath resolves junctions and links. The Mods folder is often a
// junction, which filepath.WalkDir does not descend into, and the same mod can
// be reached through several spellings.
func physicalPath(path string) string {
	if resolved, err := platform.FinalPath(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
