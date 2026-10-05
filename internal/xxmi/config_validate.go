package xxmi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func ValidateImporterSettings(key string, cfg ImporterConfig) error {
	if _, ok := lookupImporterPackage(key); !ok {
		return fmt.Errorf("unknown importer %q", key)
	}
	if cfg.SchemaVersion != importerConfigSchema {
		return fmt.Errorf("unsupported importer config schema version %d", cfg.SchemaVersion)
	}
	if cfg.Mode != RuntimeXXMI && cfg.Mode != RuntimeLegacy {
		return fmt.Errorf("invalid runtime mode %q", cfg.Mode)
	}
	for name, pin := range map[string]VersionPin{"packageVersion": cfg.PackageVersion, "xxmiVersion": cfg.XXMIVersion} {
		normalized := normalizeVersion(pin.Pinned)
		follows := pin.Follow == "latest" || name == "xxmiVersion" && pin.Follow == followShared
		if follows == (pin.Pinned != "") || pin.Notify && pin.Pinned == "" ||
			pin.Pinned != "" && (normalized == "" || normalized == "." || normalized == "..") ||
			strings.ContainsAny(pin.Pinned, `\/:*?"<>|`) {
			return fmt.Errorf("invalid %s", name)
		}
	}
	if cfg.CustomDLL != "" && !isCustomDLLID(cfg.CustomDLL) {
		return fmt.Errorf("invalid custom DLL %q", cfg.CustomDLL)
	}
	if err := validateLocalFolder("importerFolder", cfg.ImporterFolder, true); err != nil {
		return err
	}
	if err := validateLocalFolder("gameFolder", cfg.GameFolder, false); err != nil {
		return err
	}
	if cfg.GameFolder != "" {
		importer := strings.ToLower(filepath.Clean(cfg.ImporterFolder))
		game := strings.ToLower(filepath.Clean(cfg.GameFolder))
		if importer == game || strings.HasPrefix(importer, game+string(filepath.Separator)) ||
			strings.HasPrefix(game, importer+string(filepath.Separator)) {
			return fmt.Errorf("importer and game folders must not contain each other")
		}
	}
	for _, field := range []struct {
		name, value string
		values      []string
	}{
		{"game launch", cfg.GameLaunch, []string{"Direct", "Steam", "Epic", "Custom", "Manual"}},
		{"process start method", cfg.ProcessStartMethod, []string{"Native", "Shell"}},
		{"injection method", cfg.InjectionMethod, []string{"", "Default", "Native"}},
		{"XXMI DLL injection mode", cfg.XXMIDLLInjectMode, []string{"Hook", "Inject", "Bypass"}},
		{"Migoto log level", cfg.Migoto.LogLevel, []string{"Disabled", "Warning", "Info", "Debug"}},
		{"Migoto input disable mode", cfg.Migoto.InputDisableMode, []string{"Mods", "All"}},
	} {
		if !slices.Contains(field.values, field.value) {
			return fmt.Errorf("invalid %s %q", field.name, field.value)
		}
	}
	if cfg.GameLaunch == "Custom" && strings.TrimSpace(cfg.CustomLaunch.Command) == "" {
		return errors.New("custom launch requires a command")
	}
	if name := cfg.GameProcessExe; name != "" &&
		(filepath.Base(name) != name || strings.ContainsAny(name, `<>:"|?*`)) {
		return fmt.Errorf("invalid game process executable %q", name)
	}
	if strings.ContainsAny(cfg.Migoto.ToggleInput, "\r\n") {
		return errors.New("invalid Migoto input toggle hotkey")
	}
	if cfg.WWMI != nil && !slices.Contains([]string{"UHD", "HD", "SD"}, cfg.WWMI.ResourceTier) {
		return fmt.Errorf("invalid WWMI resource tier %q", cfg.WWMI.ResourceTier)
	}
	if !slices.Contains(
		[]string{"Low", "BelowNormal", "Normal", "AboveNormal", "High", "Realtime"},
		cfg.ProcessPriority,
	) {
		return fmt.Errorf("invalid process priority %q", cfg.ProcessPriority)
	}
	if cfg.ProcessTimeout < 5 || cfg.ProcessTimeout > 600 {
		return fmt.Errorf("process timeout must be between 5 and 600 seconds")
	}
	if cfg.XXMIDLLInitDelay < 0 || cfg.XXMIDLLInitDelay > 600000 {
		return fmt.Errorf("invalid XXMI DLL initialization delay")
	}
	if !slices.Contains([]string{"Windowed", "Borderless", "Fullscreen", "Exclusive Fullscreen"}, cfg.WindowMode) {
		return fmt.Errorf("invalid window mode %q", cfg.WindowMode)
	}
	return nil
}

func validateLocalFolder(name, path string, required bool) error {
	if path == "" && !required {
		return nil
	}
	if !filepath.IsAbs(path) || filepath.VolumeName(path) == "" || strings.HasPrefix(path, `\\`) {
		return fmt.Errorf("%s must be an absolute local drive path", name)
	}
	return nil
}

// validateImporterFolderTarget rejects the root of an XXMI Launcher installation. Installing a package swaps the
// whole importer folder, which there would stage and replace the launcher and every importer below it.
func validateImporterFolderTarget(folder string) error {
	if _, err := os.Stat(filepath.Join(folder, xxmiConfigName)); err == nil {
		return errors.New("XXMI_IMPORTER_FOLDER_IS_LAUNCHER")
	}
	return nil
}

type configValueKind uint8

const (
	configString configValueKind = iota
	configBoolean
	configNumber
	configObject
	configStringArray
	configStringRecord
	configScalarRecord
)

type configField struct {
	name string
	kind configValueKind
}

var launcherConfigFields = []configField{
	{"auto_update", configBoolean}, {"pre_release", configBoolean}, {"update_channel", configString},
	{"auto_close", configBoolean}, {"start_timeout", configNumber}, {"gui_theme", configString},
	{"theme_mode", configString}, {"active_importer", configString}, {"enabled_importers", configStringArray},
	{"log_level", configString}, {"config_version", configString}, {"theme_dev_mode", configBoolean},
	{"github_token", configString}, {"verify_ssl", configBoolean}, {"proxy", configObject},
	{"credits_shown", configBoolean}, {"locale", configString},
}

var proxyConfigFields = []configField{
	{"enable", configBoolean}, {"type", configString}, {"host", configString}, {"port", configString},
	{"use_credentials", configBoolean}, {"user", configString}, {"password", configString},
	{"proxy_dns_via_socks5", configBoolean},
}

var packageConfigFields = []configField{
	{"latest_version", configString},
	{"skipped_version", configString},
	{"deployed_version", configString},
	{
		"update_check_time",
		configNumber,
	},
	{"latest_release_notes", configString},
	{"deployed_release_notes", configString},
}

var baseImporterConfigFields = []configField{
	{"game_exe_names", configStringArray},
	{"game_folder_names", configStringArray},
	{"game_folder_children", configStringArray},
	{"package_name", configString},
	{"importer_folder", configString},
	{"game_folder", configString},
	{"use_launch_options", configBoolean},
	{"overwrite_ini", configBoolean},
	{"xxmi_dll_init_delay", configNumber},
	{"process_priority", configString},
	{"window_mode", configString},
	{"run_pre_launch_enabled", configBoolean},
	{"run_pre_launch", configString},
	{"run_pre_launch_signature", configString},
	{"run_pre_launch_wait", configBoolean},
	{"custom_launch", configString},
	{"custom_launch_signature", configString},
	{"run_post_load_enabled", configBoolean},
	{"run_post_load", configString},
	{"run_post_load_signature", configString},
	{"run_post_load_wait", configBoolean},
	{"extra_libraries_enabled", configBoolean},
	{"extra_libraries", configString},
	{"extra_libraries_signature", configString},
	{"deployed_migoto_signatures", configStringRecord},
	{"shortcut_deployed", configBoolean},
	{"d3dx_ini", configObject},
	{"configure_game", configBoolean},
	{"launch_count", configNumber},
	{"launch_options", configString},
}

var migotoConfigFields = []configField{
	{"enforce_rendering", configBoolean}, {"enable_hunting", configBoolean}, {"dump_shaders", configBoolean},
	{"mute_warnings", configBoolean},
	{"unsafe_mode", configBoolean}, {"unsafe_mode_signature", configString},
}

var legacyLaunchConfigFields = []configField{
	{"process_start_method", configString}, {"custom_launch_enabled", configBoolean},
	{"custom_launch_inject_mode", configString},
}

var legacyLoggingConfigFields = []configField{
	{"calls_logging", configBoolean}, {"debug_logging", configBoolean},
}

// The fields below are read when present, so only their types are checked.
var optionalImporterConfigFields = []configField{
	{"process_exe_names", configStringArray}, {"game_process_exe_enabled", configBoolean},
	{"game_process_exe", configString}, {"process_timeout", configNumber},
}

var optionalMigotoConfigFields = []configField{
	{"clear_unknown_settings", configBoolean}, {"input", configBoolean},
	{"input_disable_mode", configString}, {"toggle_input", configString},
}

var legacyWWMIConfigFields = []configField{
	{"apply_perf_tweaks", configBoolean}, {"perf_tweaks", configObject},
	{"mesh_lod_distance_scale", configNumber}, {"mesh_lod_distance_offset", configNumber},
	{"texture_streaming_boost", configNumber}, {"texture_streaming_min_boost", configNumber},
	{"texture_streaming_use_all_mips", configBoolean}, {"texture_streaming_pool_size", configNumber},
	{"texture_streaming_limit_to_vram", configBoolean}, {"texture_streaming_fixed_pool_size", configBoolean},
}

func validateXXMIConfig(config map[string]any) error {
	launcher, err := requireConfigObject(config, "Launcher", "Launcher")
	if err != nil {
		return err
	}
	if err := requireConfigFields(launcher, "Launcher", launcherConfigFields); err != nil {
		return err
	}
	proxy, err := requireConfigObject(launcher, "proxy", "Launcher.proxy")
	if err != nil {
		return err
	}
	if err := requireConfigFields(proxy, "Launcher.proxy", proxyConfigFields); err != nil {
		return err
	}

	packagesSection, err := requireConfigObject(config, "Packages", "Packages")
	if err != nil {
		return err
	}
	packages, err := requireConfigObject(packagesSection, "packages", "Packages.packages")
	if err != nil {
		return err
	}
	packageNames := sortedConfigKeys(packages)
	for _, name := range packageNames {
		item, ok := packages[name].(map[string]any)
		if !ok {
			return fmt.Errorf("packages.packages.%s must be an object", name)
		}
		if err := requireConfigFields(item, "Packages.packages."+name, packageConfigFields); err != nil {
			return err
		}
	}

	importers, err := requireConfigObject(config, "Importers", "Importers")
	if err != nil {
		return err
	}
	for _, name := range []string{"GIMI", "SRMI", "WWMI", "ZZMI", "EFMI", "HIMI"} {
		if _, ok := importers[name]; !ok {
			return fmt.Errorf("importers.%s is required", name)
		}
	}
	for _, name := range sortedConfigKeys(importers) {
		if err := validateImporterConfig(name, importers[name]); err != nil {
			return err
		}
	}

	security, err := requireConfigObject(config, "Security", "Security")
	if err != nil {
		return err
	}
	return requireConfigFields(security, "Security", []configField{{"user_signature", configString}})
}

func validateImporterConfig(name string, value any) error {
	path := "Importers." + name
	wrapper, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	importer, err := requireConfigObject(wrapper, "Importer", path+".Importer")
	if err != nil {
		return err
	}
	if err := requireConfigFields(importer, path+".Importer", baseImporterConfigFields); err != nil {
		return err
	}
	migoto, err := requireConfigObject(wrapper, "Migoto", path+".Migoto")
	if err != nil {
		return err
	}
	if err := requireConfigFields(migoto, path+".Migoto", migotoConfigFields); err != nil {
		return err
	}

	// Identify the shape by its fields: upgraded configs can retain obsolete values,
	// and config_version is metadata rather than a schema contract.
	modernLaunch := hasModernLaunchConfig(importer)
	if modernLaunch {
		for _, field := range []struct {
			name   string
			values []string
		}{
			{"game_launch", []string{"DIRECT", "CUSTOM", "MANUAL", "STEAM", "EPIC_GAMES"}},
			{"start_method", []string{"NATIVE", "SHELL"}},
			{"xxmi_dll_inject_mode", []string{"HOOK", "DIRECT", "SKIP"}},
		} {
			if err := requireConfigEnum(importer, path+".Importer", field.name, field.values); err != nil {
				return err
			}
		}
	}
	if err := validateConfigFields(importer, path+".Importer", legacyLaunchConfigFields, !modernLaunch); err != nil {
		return err
	}
	_, modernLogging := migoto["log_level"]
	if modernLogging {
		if err := requireConfigEnum(
			migoto,
			path+".Migoto",
			"log_level",
			[]string{"DISABLED", "WARNING", "INFO", "DEBUG"},
		); err != nil {
			return err
		}
	}
	if err := validateConfigFields(migoto, path+".Migoto", legacyLoggingConfigFields, !modernLogging); err != nil {
		return err
	}
	if err := validateD3DXConfig(importer["d3dx_ini"], path+".Importer.d3dx_ini", modernLogging); err != nil {
		return err
	}
	if err := validateConfigFields(importer, path+".Importer", optionalImporterConfigFields, false); err != nil {
		return err
	}
	if err := validateConfigFields(migoto, path+".Migoto", optionalMigotoConfigFields, false); err != nil {
		return err
	}

	switch name {
	case "GIMI":
		if err := requireConfigFields(importer, path+".Importer", []configField{
			{"unlock_fps", configBoolean}, {"disable_dcr", configBoolean}, {"enable_hdr", configBoolean},
		}); err != nil {
			return err
		}
		return optionalConfigField(importer, path+".Importer", "unlock_fps_value", configNumber)
	case "SRMI":
		return optionalConfigField(importer, path+".Importer", "unlock_fps", configBoolean)
	case "WWMI":
		if err := validateConfigFields(importer, path+".Importer", legacyWWMIConfigFields, !modernLaunch); err != nil {
			return err
		}
		if perf, ok := importer["perf_tweaks"].(map[string]any); ok {
			if err := requireConfigFields(
				perf,
				path+".Importer.perf_tweaks",
				[]configField{{"SystemSettings", configScalarRecord}},
			); err != nil {
				return err
			}
		}
		for _, field := range []string{"unlock_fps", "force_max_lod_bias", "disable_wounded_fx", "disable_wounded_fx_warned"} {
			if err := optionalConfigField(importer, path+".Importer", field, configBoolean); err != nil {
				return err
			}
		}
	case "HIMI":
		return requireConfigFields(importer, path+".Importer", []configField{
			{"unlock_fps", configBoolean}, {"unlock_fps_value", configNumber},
			{"disable_dcr", configBoolean}, {"enable_hdr", configBoolean},
		})
	}
	return nil
}

func hasModernLaunchConfig(importer map[string]any) bool {
	for _, field := range []string{"game_launch", "start_method", "xxmi_dll_inject_mode"} {
		if _, exists := importer[field]; exists {
			return true
		}
	}
	return false
}

func validateD3DXConfig(value any, path string, modernLogging bool) error {
	d3dx, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, field := range []string{"core", "enforce_rendering", "enable_hunting", "dump_shaders"} {
		if _, ok := d3dx[field].(map[string]any); !ok {
			return fmt.Errorf("%s.%s must be an object", path, field)
		}
	}
	checks := []struct {
		path   string
		value  any
		fields []configField
	}{
		{path + ".core.Loader", nestedConfigValue(d3dx, "core", "Loader"), []configField{{"loader", configString}}},
		{
			path + ".enforce_rendering.Rendering",
			nestedConfigValue(d3dx, "enforce_rendering", "Rendering"),
			[]configField{{"texture_hash", configNumber}, {"track_texture_updates", configNumber}},
		},
		{
			path + ".calls_logging.Logging.calls",
			nestedConfigValue(d3dx, "calls_logging", "Logging", "calls"),
			[]configField{{"on", configNumber}, {"off", configNumber}},
		},
		{
			path + ".debug_logging.Logging.debug",
			nestedConfigValue(d3dx, "debug_logging", "Logging", "debug"),
			[]configField{{"on", configNumber}, {"off", configNumber}},
		},
		{
			path + ".mute_warnings.Logging.show_warnings",
			nestedConfigValue(d3dx, "mute_warnings", "Logging", "show_warnings"),
			[]configField{{"on", configNumber}, {"off", configNumber}},
		},
		{
			path + ".enable_hunting.Hunting.hunting",
			nestedConfigValue(d3dx, "enable_hunting", "Hunting", "hunting"),
			[]configField{{"on", configNumber}, {"off", configNumber}},
		},
		{
			path + ".dump_shaders.Hunting.marking_actions",
			nestedConfigValue(d3dx, "dump_shaders", "Hunting", "marking_actions"),
			[]configField{{"on", configString}, {"off", configString}},
		},
	}
	for _, check := range checks {
		section := strings.TrimPrefix(check.path, path+".")
		section, _, _ = strings.Cut(section, ".")
		if modernLogging && slices.Contains([]string{"calls_logging", "debug_logging", "mute_warnings"}, section) {
			if _, exists := d3dx[section]; !exists {
				continue
			}
		}
		object, ok := check.value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", check.path)
		}
		if err := requireConfigFields(object, check.path, check.fields); err != nil {
			return err
		}
	}
	return nil
}

func nestedConfigValue(root map[string]any, keys ...string) any {
	var value any = root
	for _, key := range keys {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = object[key]
	}
	return value
}

func requireConfigObject(parent map[string]any, name, path string) (map[string]any, error) {
	object, ok := parent[name].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", path)
	}
	return object, nil
}

func requireConfigFields(object map[string]any, path string, fields []configField) error {
	return validateConfigFields(object, path, fields, true)
}

func validateConfigFields(object map[string]any, path string, fields []configField, required bool) error {
	for _, field := range fields {
		value, ok := object[field.name]
		if !ok {
			if !required {
				continue
			}
			return fmt.Errorf("%s.%s is required", path, field.name)
		}
		if !configValueMatches(value, field.kind) {
			return fmt.Errorf("%s.%s has an invalid type", path, field.name)
		}
	}
	return nil
}

func requireConfigEnum(object map[string]any, path, name string, values []string) error {
	if err := requireConfigFields(object, path, []configField{{name, configString}}); err != nil {
		return err
	}
	value, _ := object[name].(string)
	if !slices.Contains(values, value) {
		return fmt.Errorf("%s.%s has an unsupported value %q", path, name, value)
	}
	return nil
}

func optionalConfigField(object map[string]any, path, name string, kind configValueKind) error {
	value, ok := object[name]
	if !ok {
		return nil
	}
	if !configValueMatches(value, kind) {
		return fmt.Errorf("%s.%s has an invalid type", path, name)
	}
	return nil
}

func configValueMatches(value any, kind configValueKind) bool {
	switch kind {
	case configString:
		_, ok := value.(string)
		return ok
	case configBoolean:
		_, ok := value.(bool)
		return ok
	case configNumber:
		_, ok := value.(float64)
		return ok
	case configObject:
		_, ok := value.(map[string]any)
		return ok
	case configStringArray:
		values, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case configStringRecord:
		values, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, item := range values {
			if _, ok := item.(string); !ok {
				return false
			}
		}
		return true
	case configScalarRecord:
		values, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, item := range values {
			switch item.(type) {
			case string, float64, bool:
			default:
				return false
			}
		}
		return true
	default:
		return false
	}
}

func sortedConfigKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
