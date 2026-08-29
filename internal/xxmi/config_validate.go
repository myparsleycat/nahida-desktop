package xxmi

import (
	"fmt"
	"slices"
)

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
	{"latest_version", configString}, {"skipped_version", configString}, {"deployed_version", configString},
	{"update_check_time", configNumber}, {"latest_release_notes", configString}, {"deployed_release_notes", configString},
}

var baseImporterConfigFields = []configField{
	{"game_exe_names", configStringArray}, {"game_folder_names", configStringArray},
	{"game_folder_children", configStringArray}, {"package_name", configString}, {"importer_folder", configString},
	{"game_folder", configString}, {"use_launch_options", configBoolean}, {"overwrite_ini", configBoolean},
	{"process_start_method", configString}, {"xxmi_dll_init_delay", configNumber}, {"process_priority", configString},
	{"window_mode", configString}, {"run_pre_launch_enabled", configBoolean}, {"run_pre_launch", configString},
	{"run_pre_launch_signature", configString}, {"run_pre_launch_wait", configBoolean},
	{"custom_launch_enabled", configBoolean}, {"custom_launch", configString}, {"custom_launch_signature", configString},
	{"custom_launch_inject_mode", configString}, {"run_post_load_enabled", configBoolean},
	{"run_post_load", configString}, {"run_post_load_signature", configString}, {"run_post_load_wait", configBoolean},
	{"extra_libraries_enabled", configBoolean}, {"extra_libraries", configString},
	{"extra_libraries_signature", configString}, {"deployed_migoto_signatures", configStringRecord},
	{"shortcut_deployed", configBoolean}, {"d3dx_ini", configObject}, {"configure_game", configBoolean},
	{"launch_count", configNumber}, {"launch_options", configString},
}

var migotoConfigFields = []configField{
	{"enforce_rendering", configBoolean}, {"enable_hunting", configBoolean}, {"dump_shaders", configBoolean},
	{"mute_warnings", configBoolean}, {"calls_logging", configBoolean}, {"debug_logging", configBoolean},
	{"unsafe_mode", configBoolean}, {"unsafe_mode_signature", configString},
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
	if err := validateD3DXConfig(importer["d3dx_ini"], path+".Importer.d3dx_ini"); err != nil {
		return err
	}
	migoto, err := requireConfigObject(wrapper, "Migoto", path+".Migoto")
	if err != nil {
		return err
	}
	if err := requireConfigFields(migoto, path+".Migoto", migotoConfigFields); err != nil {
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
		if err := requireConfigFields(importer, path+".Importer", []configField{
			{"apply_perf_tweaks", configBoolean}, {"perf_tweaks", configObject},
			{"mesh_lod_distance_scale", configNumber}, {"mesh_lod_distance_offset", configNumber},
			{"texture_streaming_boost", configNumber}, {"texture_streaming_min_boost", configNumber},
			{"texture_streaming_use_all_mips", configBoolean}, {"texture_streaming_pool_size", configNumber},
			{"texture_streaming_limit_to_vram", configBoolean}, {"texture_streaming_fixed_pool_size", configBoolean},
		}); err != nil {
			return err
		}
		perf, _ := importer["perf_tweaks"].(map[string]any)
		if err := requireConfigFields(perf, path+".Importer.perf_tweaks", []configField{{"SystemSettings", configScalarRecord}}); err != nil {
			return err
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

func validateD3DXConfig(value any, path string) error {
	d3dx, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("%s must be an object", path)
	}
	for _, field := range []string{"core", "enforce_rendering", "calls_logging", "debug_logging", "mute_warnings", "enable_hunting", "dump_shaders"} {
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
		{path + ".enforce_rendering.Rendering", nestedConfigValue(d3dx, "enforce_rendering", "Rendering"), []configField{{"texture_hash", configNumber}, {"track_texture_updates", configNumber}}},
		{path + ".calls_logging.Logging.calls", nestedConfigValue(d3dx, "calls_logging", "Logging", "calls"), []configField{{"on", configNumber}, {"off", configNumber}}},
		{path + ".debug_logging.Logging.debug", nestedConfigValue(d3dx, "debug_logging", "Logging", "debug"), []configField{{"on", configNumber}, {"off", configNumber}}},
		{path + ".mute_warnings.Logging.show_warnings", nestedConfigValue(d3dx, "mute_warnings", "Logging", "show_warnings"), []configField{{"on", configNumber}, {"off", configNumber}}},
		{path + ".enable_hunting.Hunting.hunting", nestedConfigValue(d3dx, "enable_hunting", "Hunting", "hunting"), []configField{{"on", configNumber}, {"off", configNumber}}},
		{path + ".dump_shaders.Hunting.marking_actions", nestedConfigValue(d3dx, "dump_shaders", "Hunting", "marking_actions"), []configField{{"on", configString}, {"off", configString}}},
	}
	for _, check := range checks {
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
	for _, field := range fields {
		value, ok := object[field.name]
		if !ok {
			return fmt.Errorf("%s.%s is required", path, field.name)
		}
		if !configValueMatches(value, field.kind) {
			return fmt.Errorf("%s.%s has an invalid type", path, field.name)
		}
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
