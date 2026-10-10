package xxmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"nahida.live/desktop/internal/infra"
)

type D3DXOptionKind string

const (
	D3DXOptionBool   D3DXOptionKind = "bool"
	D3DXOptionInt    D3DXOptionKind = "int"
	D3DXOptionFloat  D3DXOptionKind = "float"
	D3DXOptionEnum   D3DXOptionKind = "enum"
	D3DXOptionFlags  D3DXOptionKind = "flags"
	D3DXOptionHotkey D3DXOptionKind = "hotkey"
)

type D3DXOptionManaged string

const (
	D3DXOptionUnmanaged D3DXOptionManaged = ""
	// D3DXOptionManagedLaunch marks an option every launch writes from the importer settings.
	D3DXOptionManagedLaunch D3DXOptionManaged = "launch"
	// D3DXOptionManagedRendering marks an option a launch writes only while rendering is enforced.
	D3DXOptionManagedRendering D3DXOptionManaged = "rendering"
)

// D3DXOption is one option of an importer's d3dx.ini that the settings page can show.
type D3DXOption struct {
	Section string `json:"section"`
	Key     string `json:"key"`
	// Value is what the file holds now; a saved override replaces it on the next launch.
	Value   string            `json:"value"`
	Kind    D3DXOptionKind    `json:"kind"`
	Choices []string          `json:"choices"`
	Min     *float64          `json:"min"`
	Max     *float64          `json:"max"`
	Managed D3DXOptionManaged `json:"managed"`
}

type d3dxOptionSpec struct {
	section, key string
	kind         D3DXOptionKind
	choices      []string
	min, max     *float64
	launch       bool
}

// d3dxOptionSpecs lists the d3dx.ini options of the XXMI libraries that are safe to change one at a time, in
// the order of the packaged file. Left out are [Loader] and [Include], which a launch and mod loading own,
// the shader directories an importer package depends on, and the hunting keys that cycle shaders and buffers.
var d3dxOptionSpecs = func() []d3dxOptionSpec {
	limit := func(value float64) *float64 { return &value }
	option := func(section, key string, kind D3DXOptionKind, choices ...string) d3dxOptionSpec {
		return d3dxOptionSpec{section: section, key: key, kind: kind, choices: choices}
	}
	managed := func(spec d3dxOptionSpec) d3dxOptionSpec {
		spec.launch = true
		return spec
	}
	ranged := func(spec d3dxOptionSpec, lowest, highest float64) d3dxOptionSpec {
		spec.min, spec.max = limit(lowest), limit(highest)
		return spec
	}

	return []d3dxOptionSpec{
		managed(option("Hunting", "hunting", D3DXOptionEnum, "0", "1", "2")),
		ranged(option("Hunting", "overlay_buffer_hash_lifetime", D3DXOptionInt), -1, 100000),
		option("Hunting", "marking_mode", D3DXOptionEnum, "skip", "original", "pink", "mono"),
		option("Hunting", "next_marking_mode", D3DXOptionHotkey),
		managed(option("Hunting", "marking_actions", D3DXOptionFlags,
			"hlsl", "asm", "regex", "clipboard", "mono_snapshot", "snapshot_if_pink")),
		option("Hunting", "done_hunting", D3DXOptionHotkey),
		option("Hunting", "reload_fixes", D3DXOptionHotkey),
		option("Hunting", "toggle_hunting", D3DXOptionHotkey),
		option("Hunting", "reload_config", D3DXOptionHotkey),
		option("Hunting", "wipe_user_config", D3DXOptionHotkey),
		option("Hunting", "show_original", D3DXOptionHotkey),
		option("Hunting", "monitor_performance", D3DXOptionHotkey),
		option("Hunting", "freeze_performance_monitor", D3DXOptionHotkey),
		ranged(option("Hunting", "monitor_performance_interval", D3DXOptionFloat), 0.1, 60),
		ranged(option("Hunting", "repeat_rate", D3DXOptionInt), 1, 60),
		option("Hunting", "verbose_overlay", D3DXOptionBool),
		option("Hunting", "analyse_frame", D3DXOptionHotkey),
		option("Hunting", "analyse_options", D3DXOptionFlags,
			"dump_rt", "dump_depth", "dump_tex", "dump_cb", "dump_vb", "dump_ib", "jpg", "jps", "dds", "jps_dds",
			"desc", "buf", "txt", "hold", "clear_rt", "filename_reg", "mono", "stereo", "dump_on_unmap",
			"dump_on_update", "share_dupes", "symlink", "deferred_ctx_immediate", "deferred_ctx_accurate"),

		managed(option("System", "screen_width", D3DXOptionInt)),
		managed(option("System", "screen_height", D3DXOptionInt)),
		managed(option("System", "dll_initialization_delay", D3DXOptionInt)),
		option("System", "load_library_redirect", D3DXOptionEnum, "0", "1", "2"),
		option("System", "check_foreground_window", D3DXOptionBool),
		option("System", "allow_check_interface", D3DXOptionBool),
		option("System", "allow_create_device", D3DXOptionEnum, "0", "1", "2"),
		option("System", "allow_platform_update", D3DXOptionBool),
		option("System", "skip_early_includes_load", D3DXOptionBool),
		ranged(option("System", "config_initialization_delay", D3DXOptionInt), -1, 600),
		ranged(option("System", "settings_auto_save_interval", D3DXOptionInt), -1, 86400),
		managed(option("System", "clear_unknown_settings", D3DXOptionBool)),
		option("System", "persistent_variable_key", D3DXOptionEnum, "namespace", "path"),
		option("System", "force_detect_color_space", D3DXOptionBool),

		managed(option("Input", "input", D3DXOptionBool)),
		option("Input", "toggle_input", D3DXOptionHotkey),
		managed(option("Input", "input_disable_mode", D3DXOptionEnum, "mods", "all")),

		option("Device", "upscaling", D3DXOptionEnum, "0", "1", "2"),
		ranged(option("Device", "width", D3DXOptionInt), 320, 16384),
		ranged(option("Device", "height", D3DXOptionInt), 200, 16384),
		option("Device", "upscale_mode", D3DXOptionEnum, "0", "1"),
		ranged(option("Device", "refresh_rate", D3DXOptionInt), 1, 1000),
		option("Device", "full_screen", D3DXOptionEnum, "0", "1", "2"),
		option("Device", "allow_windowcommands", D3DXOptionBool),
		option("Device", "get_resolution_from", D3DXOptionEnum, "swap_chain", "depth_stencil"),
		option("Device", "hide_cursor", D3DXOptionBool),

		option("Rendering", "shader_hash", D3DXOptionEnum, "3dmigoto", "embedded", "bytecode"),
		option("Rendering", "texture_hash", D3DXOptionBool),
		option("Rendering", "cache_shaders", D3DXOptionBool),
		option("Rendering", "share_duplicate_resources", D3DXOptionBool),
		option("Rendering", "prefetch_resource_files", D3DXOptionBool),
		option("Rendering", "cache_resource_data", D3DXOptionFlags,
			"none", "vertex_buffer", "index_buffer", "constant_buffer", "all"),
		option("Rendering", "rasterizer_disable_scissor", D3DXOptionBool),
		option("Rendering", "track_texture_updates", D3DXOptionEnum, "0", "1", "2"),
		option("Rendering", "track_implicit_index_buffers", D3DXOptionBool),
		option("Rendering", "track_region_hashes", D3DXOptionBool),
		option("Rendering", "allow_buffer_resize", D3DXOptionBool),
		ranged(option("Rendering", "ini_params", D3DXOptionInt), -1, 127),
		option("Rendering", "assemble_signature_comments", D3DXOptionBool),
		option("Rendering", "disassemble_undecipherable_custom_data", D3DXOptionBool),
		option("Rendering", "patch_assembly_cb_offsets", D3DXOptionBool),
		// -1 is not "off": the DLL then compiles with the standard include handler instead of its own.
		option("Rendering", "recursive_include", D3DXOptionEnum, "-1", "0", "1"),
		option("Rendering", "export_fixed", D3DXOptionBool),
		option("Rendering", "export_shaders", D3DXOptionBool),
		option("Rendering", "export_hlsl", D3DXOptionEnum, "0", "1", "2", "3"),
		option("Rendering", "dump_usage", D3DXOptionBool),
		option("Rendering", "fix_sv_position", D3DXOptionBool),

		managed(option("Logging", "log_level", D3DXOptionEnum, "disabled", "warning", "info", "debug")),
		managed(option("Logging", "show_warnings", D3DXOptionBool)),
		option("Logging", "unbuffered", D3DXOptionBool),
		option("Logging", "force_cpu_affinity", D3DXOptionBool),
		option("Logging", "debug_locks", D3DXOptionBool),
		option("Logging", "crash", D3DXOptionBool),
	}
}()

func d3dxOverrideKey(section, key string) string { return section + "." + key }

func lookupD3DXOption(overrideKey string) (d3dxOptionSpec, bool) {
	index := slices.IndexFunc(d3dxOptionSpecs, func(spec d3dxOptionSpec) bool {
		return d3dxOverrideKey(spec.section, spec.key) == overrideKey
	})
	if index < 0 {
		return d3dxOptionSpec{}, false
	}
	return d3dxOptionSpecs[index], true
}

// enforcedRenderingOptions returns the [Rendering] values a launch writes for importer while its rendering
// options are enforced.
func enforcedRenderingOptions(importer string) map[string]string {
	values := map[string]string{
		"texture_hash": "0", "track_texture_updates": "0", "track_region_hashes": "0",
		"allow_buffer_resize": "1",
	}
	switch importer {
	case "WWMI":
		values["texture_hash"], values["track_texture_updates"] = "1", "1"
	case "SRMI":
		values["track_implicit_index_buffers"] = "1"
	case "EFMI":
		values["track_region_hashes"], values["track_implicit_index_buffers"] = "1", "1"
		values["allow_buffer_resize"] = "0"
	}
	return values
}

func validateD3DXOverrides(overrides map[string]string) error {
	for overrideKey, value := range overrides {
		spec, ok := lookupD3DXOption(overrideKey)
		if !ok || spec.launch {
			return fmt.Errorf("invalid d3dx.ini option %q", overrideKey)
		}
		if !spec.accepts(value) {
			return fmt.Errorf("invalid d3dx.ini value %q for %s", value, overrideKey)
		}
	}
	return nil
}

func (s d3dxOptionSpec) accepts(value string) bool {
	inRange := func(number float64) bool {
		return (s.min == nil || number >= *s.min) && (s.max == nil || number <= *s.max)
	}
	switch s.kind {
	case D3DXOptionBool:
		return value == "0" || value == "1"
	case D3DXOptionInt:
		number, err := strconv.Atoi(value)
		return err == nil && inRange(float64(number))
	case D3DXOptionFloat:
		number, err := strconv.ParseFloat(value, 64)
		// ParseFloat also reads "NaN" and "Inf", which the DLL does not.
		return err == nil && strings.Trim(value, "0123456789.-") == "" && inRange(number)
	case D3DXOptionEnum:
		return slices.Contains(s.choices, value)
	case D3DXOptionFlags:
		flags := strings.Fields(value)
		return len(flags) > 0 && strings.Join(flags, " ") == value &&
			!slices.ContainsFunc(flags, func(flag string) bool { return !slices.Contains(s.choices, flag) })
	case D3DXOptionHotkey:
		// A key binding is a list of key names; anything else could start a comment or another line.
		return value != "" && len(value) <= 128 && value == strings.TrimSpace(value) &&
			!strings.ContainsFunc(value, func(char rune) bool {
				return char != ' ' && char != '_' && (char < '0' || char > '9') &&
					(char < 'A' || char > 'Z') && (char < 'a' || char > 'z')
			})
	}
	return false
}

// applyD3DXOverrides writes the saved overrides of options the file already sets. An option the importer
// package leaves out or commented out stays that way.
func applyD3DXOverrides(doc *iniDocument, overrides map[string]string) {
	for _, spec := range d3dxOptionSpecs {
		value, ok := overrides[d3dxOverrideKey(spec.section, spec.key)]
		if !ok || spec.launch || !spec.accepts(value) {
			continue
		}
		if _, present := doc.Option(spec.section, spec.key); present {
			doc.SetOption(spec.section, spec.key, value, true)
		}
	}
}

// GetD3DXOptions lists the known options the importer's d3dx.ini sets, or none while it has no d3dx.ini.
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
	doc := parseINI(data)
	enforced := enforcedRenderingOptions(importer)

	options := []D3DXOption{}
	for _, spec := range d3dxOptionSpecs {
		value, ok := doc.Option(spec.section, spec.key)
		if !ok {
			continue
		}
		managed := D3DXOptionUnmanaged
		if spec.launch {
			managed = D3DXOptionManagedLaunch
		} else if _, ok := enforced[spec.key]; ok && spec.section == "Rendering" {
			managed = D3DXOptionManagedRendering
		}
		options = append(options, D3DXOption{
			Section: spec.section, Key: spec.key, Value: value, Kind: spec.kind,
			Choices: slices.Clone(spec.choices), Min: spec.min, Max: spec.max, Managed: managed,
		})
	}
	return options, nil
}

func readImporterINI(folder string) ([]byte, error) {
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
	data, _, err := iniRoot.readFile(iniName)
	return data, err
}
