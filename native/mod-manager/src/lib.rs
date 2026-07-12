use alphanumeric_sort::compare_str;
use napi_derive::napi;
use rayon::prelude::*;
use sha2::{Digest, Sha256};
use std::collections::HashMap;
use std::fs::{self, File};
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::time::SystemTime;
use walkdir::WalkDir;

fn compare_paths(a: &Path, b: &Path) -> std::cmp::Ordering {
    compare_str(&a.to_string_lossy(), &b.to_string_lossy())
}

#[napi(object)]
#[derive(Clone, Default)]
pub struct ToggleKey {
    pub section_name: String,
    pub ini_file_name: String,
    pub key: Option<String>,
    pub back: Option<String>,
    #[napi(js_name = "type")]
    pub type_: Option<String>,
    pub variable: String,
    pub values: Vec<String>,
    pub current_value: Option<String>,
}

#[napi(object)]
#[derive(Clone, Default)]
pub struct IniResult {
    pub name: String,
    pub path: String,
    pub toggle_keys: Vec<ToggleKey>,
    pub has_toggle_key: bool,
}

#[napi(object)]
#[derive(Clone, Default)]
pub struct ModInfo {
    pub id: String,
    pub name: String,
    pub path: String,
    pub is_enabled: bool,
    pub preview: Option<String>,
    pub mtime: f64,
    pub size: f64,
    pub inis: Vec<IniResult>,
}

#[napi(object)]
#[derive(Clone, Default)]
pub struct FolderGroup {
    pub name: String,
    pub path: String,
    pub mods: Vec<ModInfo>,
    pub preview: Option<String>,
    pub mod_count: u32,
}

fn get_map_value(data: &mut HashMap<String, String>, key: &str) -> Option<String> {
    data.remove(key).filter(|s| !s.is_empty())
}

fn process_section_data(
    section_name: String,
    mut data: HashMap<String, String>,
    ini_file_name: &str,
) -> Option<ToggleKey> {
    if !section_name.to_ascii_lowercase().starts_with("key") {
        return None;
    }

    let type_val = get_map_value(&mut data, "type");
    let key_val = get_map_value(&mut data, "key");
    let back_val = get_map_value(&mut data, "back");

    let is_hold = type_val
        .as_deref()
        .map(|t| t.eq_ignore_ascii_case("hold"))
        .unwrap_or(false);

    let mut vars: Vec<_> = data
        .into_iter()
        .filter(|(k, _)| k.starts_with('$'))
        .collect();
    vars.sort_by(|(ka, _), (kb, _)| ka.cmp(kb));

    let (variable, values) = vars.into_iter().find_map(|(k, v)| {
        let mut iter = v.split(',').map(|s| s.trim());
        let first = iter.next()?;
        let second = iter.next();

        if second.is_some() || is_hold {
            let mut vals = vec![first.to_string()];
            if let Some(s) = second {
                vals.push(s.to_string());
            }
            vals.extend(iter.map(|s| s.to_string()));
            Some((k, vals))
        } else {
            None
        }
    })?;

    let current_value = values.first().cloned();

    Some(ToggleKey {
        section_name,
        ini_file_name: ini_file_name.to_string(),
        key: key_val,
        back: back_val,
        type_: type_val,
        variable,
        values,
        current_value,
    })
}

fn strip_value_comment(value: &str) -> String {
    value.trim().to_string()
}

fn strip_line_comment(line: &str) -> String {
    let trimmed = line.trim();
    if trimmed.starts_with(';') || trimmed.starts_with('#') {
        return String::new();
    }
    trimmed.to_string()
}

fn parse_ini(path_str: &str) -> Vec<ToggleKey> {
    let path = Path::new(path_str);
    let file = match File::open(path) {
        Ok(f) => f,
        Err(_) => return Vec::new(),
    };

    let ini_file_name = path
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("")
        .to_string();

    let reader = BufReader::new(file);
    let mut toggle_keys = Vec::new();
    let mut current_section = String::new();
    let mut section_data: HashMap<String, String> = HashMap::new();

    for line_result in reader.lines() {
        let Ok(line) = line_result else { continue };

        let raw = line.trim_start_matches('\u{FEFF}').trim();

        let clean_line = if let Some(eq_pos) = raw.find('=') {
            format!("{}={}", &raw[..eq_pos], strip_value_comment(&raw[eq_pos + 1..]))
        } else {
            strip_line_comment(raw)
        };

        if clean_line.is_empty() {
            continue;
        }

        if clean_line.starts_with('[') && clean_line.ends_with(']') {
            if !current_section.is_empty() {
                if let Some(tk) = process_section_data(
                    std::mem::take(&mut current_section),
                    std::mem::take(&mut section_data),
                    &ini_file_name,
                ) {
                    toggle_keys.push(tk);
                }
                section_data.clear();
            }

            current_section = clean_line[1..clean_line.len() - 1].to_string();
            continue;
        }

        if !current_section.is_empty() {
            if let Some((k, v)) = clean_line.split_once('=') {
                let key = k.trim().to_ascii_lowercase();
                let value = v.trim().to_string();
                section_data.insert(key, value);
            }
        }
    }

    if !current_section.is_empty() {
        if let Some(tk) = process_section_data(current_section, section_data, &ini_file_name) {
            toggle_keys.push(tk);
        }
    }

    toggle_keys
}

#[napi]
pub async fn process_ini_files(paths: Vec<String>) -> Vec<IniResult> {
    napi::tokio::task::spawn_blocking(move || process_ini_files_sync(paths))
        .await
        .unwrap_or_default()
}

pub fn process_ini_files_sync(paths: Vec<String>) -> Vec<IniResult> {
    paths
        .into_iter()
        .map(|path_str| {
            let mut toggle_keys = parse_ini(&path_str);

            toggle_keys.sort_by(|a, b| {
                let a_has_key = a.key.is_some();
                let b_has_key = b.key.is_some();
                b_has_key.cmp(&a_has_key)
            });

            let has_toggle_key = toggle_keys.iter().any(|tk| tk.key.is_some());
            let name = Path::new(&path_str)
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or("")
                .to_string();

            IniResult {
                name,
                path: path_str,
                toggle_keys,
                has_toggle_key,
            }
        })
        .collect()
}

fn is_media_ext(ext: &str) -> bool {
    matches!(
        ext,
        _ if ext.eq_ignore_ascii_case("png")
            || ext.eq_ignore_ascii_case("jpg")
            || ext.eq_ignore_ascii_case("jpeg")
            || ext.eq_ignore_ascii_case("gif")
            || ext.eq_ignore_ascii_case("webp")
            || ext.eq_ignore_ascii_case("bmp")
            || ext.eq_ignore_ascii_case("avif")
            || ext.eq_ignore_ascii_case("avifs")
            || ext.eq_ignore_ascii_case("mp4")
            || ext.eq_ignore_ascii_case("webm")
            || ext.eq_ignore_ascii_case("avi")
            || ext.eq_ignore_ascii_case("mkv")
            || ext.eq_ignore_ascii_case("mov")
    )
}

fn get_score(filename: &str, is_root: bool, is_video: bool) -> i32 {
    let mut score = 0;

    if filename.starts_with("preview") {
        score += 1000;
    } else if filename.contains("preview") {
        score += 500;
    }

    if is_root {
        score += 200;
    }

    if is_video {
        score += 10;
    }

    score
}

fn is_excluded_file(filename: &str) -> bool {
    const EXCLUDED: &[&str] = &["normal", "light", "material", "diffuse"];
    EXCLUDED.iter().any(|&k| filename.contains(k))
}

fn is_disabled_folder_name(folder_name: &str) -> bool {
    let lower = folder_name.trim().to_ascii_lowercase();
    lower.starts_with("disabled ") || lower.starts_with("disabled_")
}

fn strip_disabled_prefix(folder_name: &str) -> String {
    let trimmed = folder_name.trim();
    let lower = trimmed.to_ascii_lowercase();
    if lower.starts_with("disabled ") {
        trimmed[9..].trim().to_string()
    } else if lower.starts_with("disabled_") {
        trimmed[9..].trim_start_matches('_').trim().to_string()
    } else {
        trimmed.to_string()
    }
}

fn normalize_relative_path(relative: &Path) -> String {
    relative
        .components()
        .filter_map(|component| component.as_os_str().to_str())
        .filter(|segment| !segment.is_empty())
        .map(strip_disabled_prefix)
        .map(|segment| segment.to_ascii_lowercase())
        .collect::<Vec<_>>()
        .join("/")
}

fn build_stable_mod_id(group_path: &Path, mod_path: &Path) -> String {
    let relative = mod_path.strip_prefix(group_path).unwrap_or(mod_path);
    let normalized = normalize_relative_path(relative);
    let digest = Sha256::digest(normalized.as_bytes());
    format!("{digest:x}")
}

enum PreviewLocation {
    Root,
    EnabledFolder,
    DisabledFolder,
}

fn get_preview_location(relative: &Path) -> PreviewLocation {
    if relative.components().count() == 1 {
        return PreviewLocation::Root;
    }

    let is_in_disabled_folder = relative
        .parent()
        .into_iter()
        .flat_map(|parent| parent.components())
        .filter_map(|component| component.as_os_str().to_str())
        .any(is_disabled_folder_name);

    if is_in_disabled_folder {
        PreviewLocation::DisabledFolder
    } else {
        PreviewLocation::EnabledFolder
    }
}

fn update_preview_candidate(best: &mut Option<(i32, String)>, score: i32, path: &Path) {
    let path_str = path.to_string_lossy();

    match best {
        Some((best_score, best_path))
            if score < *best_score
                || (score == *best_score
                    && compare_str(path_str.as_ref(), best_path.as_str())
                        != std::cmp::Ordering::Less) => {}
        _ => {
            *best = Some((score, path_str.into_owned()));
        }
    }
}

fn find_preview_candidate(mod_path: &Path, max_depth: usize) -> Option<(i32, String)> {
    let mut root_best = None;
    let mut enabled_best = None;
    let mut disabled_best = None;

    let walker = WalkDir::new(mod_path)
        .max_depth(max_depth)
        .follow_links(true)
        .into_iter()
        .filter_map(|e| e.ok());

    for entry in walker {
        if !entry.file_type().is_file() {
            continue;
        }

        let path = entry.path();
        let filename_os = path.file_name().and_then(|n| n.to_str());
        let ext_os = path.extension().and_then(|e| e.to_str());

        if let (Some(filename), Some(ext)) = (filename_os, ext_os) {
            if is_media_ext(ext) {
                let lower_filename = filename.to_ascii_lowercase();
                if !is_excluded_file(&lower_filename) {
                    let Ok(relative) = path.strip_prefix(mod_path) else {
                        continue;
                    };
                    let is_root = relative.components().count() == 1;
                    let is_video =
                        ext.eq_ignore_ascii_case("mp4") || ext.eq_ignore_ascii_case("webm");
                    let score = get_score(&lower_filename, is_root, is_video);

                    match get_preview_location(relative) {
                        PreviewLocation::Root => {
                            update_preview_candidate(&mut root_best, score, path);
                        }
                        PreviewLocation::EnabledFolder => {
                            update_preview_candidate(&mut enabled_best, score, path);
                        }
                        PreviewLocation::DisabledFolder => {
                            update_preview_candidate(&mut disabled_best, score, path);
                        }
                    }
                }
            }
        }
    }

    root_best.or(enabled_best).or(disabled_best)
}

fn find_preview(mod_path: &Path, max_depth: usize) -> Option<String> {
    find_preview_candidate(mod_path, max_depth).map(|(_, path)| path)
}

fn is_disabled_folder_path(folder_path: &Path) -> bool {
    folder_path
        .file_name()
        .and_then(|name| name.to_str())
        .is_some_and(is_disabled_folder_name)
}

fn list_child_folders(folder_path: &Path) -> Vec<PathBuf> {
    let mut child_folders: Vec<PathBuf> = match fs::read_dir(folder_path) {
        Ok(entries) => entries
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect(),
        Err(_) => Vec::new(),
    };

    child_folders.sort_by(|a, b| compare_paths(a, b));
    child_folders
}

fn find_child_folder_preview(
    child_folders: &[PathBuf],
    search_depth: usize,
    disabled: bool,
) -> Option<String> {
    let mut best = None;

    child_folders
        .iter()
        .filter(|folder_path| is_disabled_folder_path(folder_path) == disabled)
        .filter_map(|folder_path| find_preview_candidate(folder_path, search_depth))
        .for_each(|(score, path)| update_preview_candidate(&mut best, score, Path::new(&path)));

    best.map(|(_, path)| path)
}

fn find_group_preview(group_path: &Path, search_depth: usize) -> Option<String> {
    if let Some(root_preview) = find_preview(group_path, 1) {
        return Some(root_preview);
    }

    if search_depth <= 1 {
        return None;
    }

    let child_folders = list_child_folders(group_path);

    find_child_folder_preview(&child_folders, search_depth, false)
        .or_else(|| find_child_folder_preview(&child_folders, search_depth, true))
}

fn has_any_file(dir: &Path) -> bool {
    WalkDir::new(dir)
        .min_depth(1)
        .into_iter()
        .filter_map(|e| e.ok())
        .any(|e| e.file_type().is_file())
}

#[napi]
pub async fn get_characters_folder(
    mod_folder_path: String,
    fallback_to_mod_preview: Option<bool>,
) -> Vec<FolderGroup> {
    napi::tokio::task::spawn_blocking(move || {
        get_characters_folder_sync(&mod_folder_path, fallback_to_mod_preview)
    })
    .await
    .unwrap_or_default()
}

pub fn get_characters_folder_sync(
    mod_folder_path: &str,
    fallback_to_mod_preview: Option<bool>,
) -> Vec<FolderGroup> {
    let root_path = Path::new(&mod_folder_path);

    if !root_path.exists() || !root_path.is_dir() {
        return Vec::new();
    }

    let mut groups: Vec<PathBuf> = match fs::read_dir(root_path) {
        Ok(entries) => entries
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect(),
        Err(_) => return Vec::new(),
    };

    groups.sort_by(|a, b| compare_paths(a, b));

    let search_depth = if fallback_to_mod_preview.unwrap_or(true) {
        3
    } else {
        1
    };

    let mut results: Vec<FolderGroup> = groups
        .par_iter()
        .map(|group_path| {
            let name = group_path
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or("")
                .to_string();

            let path_str = group_path.to_string_lossy().to_string();

            let mod_count = match fs::read_dir(group_path) {
                Ok(entries) => entries
                    .filter_map(|e| e.ok())
                    .filter(|e| e.path().is_dir() && has_any_file(&e.path()))
                    .count() as u32,
                Err(_) => 0,
            };

            let preview = find_group_preview(group_path, search_depth);

            FolderGroup {
                name,
                path: path_str,
                mods: Vec::new(),
                preview,
                mod_count,
            }
        })
        .collect();

    results.sort_by(|a, b| compare_str(&a.name, &b.name));
    results
}

fn scan_mod_folder(group_path: &Path, mod_path: &Path) -> Option<ModInfo> {
    let folder_name = mod_path.file_name()?.to_string_lossy().to_string();
    let is_enabled = !is_disabled_folder_name(&folder_name);

    let mut total_size = 0.0;
    let mut max_mtime_sys = SystemTime::UNIX_EPOCH;
    let mut ini_paths = Vec::new();
    let mut found_any_file = false;

    let mut root_preview = None;
    let mut enabled_preview = None;
    let mut disabled_preview = None;

    for entry in WalkDir::new(mod_path)
        .follow_links(true)
        .into_iter()
        .filter_map(|e| e.ok())
    {
        if entry.file_type().is_file() {
            found_any_file = true;
            let path = entry.path();

            if let Ok(metadata) = entry.metadata() {
                total_size += metadata.len() as f64;
                if let Ok(mtime) = metadata.modified() {
                    if mtime > max_mtime_sys {
                        max_mtime_sys = mtime;
                    }
                }
            }

            if let Some(ext) = path.extension().and_then(|e| e.to_str()) {
                if ext.eq_ignore_ascii_case("ini") {
                    let fname = path.file_name().unwrap_or_default().to_string_lossy();
                    if !fname.to_ascii_lowercase().starts_with("disabled") {
                        ini_paths.push(path.to_string_lossy().into_owned());
                    }
                } else if is_media_ext(ext) {
                    if let Some(filename) = path.file_name().and_then(|n| n.to_str()) {
                        let lower_filename = filename.to_ascii_lowercase();
                        if !is_excluded_file(&lower_filename) {
                            let Ok(relative) = path.strip_prefix(mod_path) else {
                                continue;
                            };
                            let is_root = relative.components().count() == 1;
                            let is_video =
                                ext.eq_ignore_ascii_case("mp4") || ext.eq_ignore_ascii_case("webm");
                            let score = get_score(&lower_filename, is_root, is_video);

                            match get_preview_location(relative) {
                                PreviewLocation::Root => {
                                    update_preview_candidate(&mut root_preview, score, path);
                                }
                                PreviewLocation::EnabledFolder => {
                                    update_preview_candidate(&mut enabled_preview, score, path);
                                }
                                PreviewLocation::DisabledFolder => {
                                    update_preview_candidate(&mut disabled_preview, score, path);
                                }
                            }
                        }
                    }
                }
            }
        }
    }

    if !found_any_file {
        return None;
    }

    if max_mtime_sys == SystemTime::UNIX_EPOCH {
        if let Ok(metadata) = fs::metadata(mod_path) {
            if let Ok(mtime) = metadata.modified() {
                max_mtime_sys = mtime;
            }
        }
    }

    let max_mtime = max_mtime_sys
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default()
        .as_secs_f64()
        * 1000.0;

    let mut inis = process_ini_files_sync(ini_paths);
    inis.sort_by(|a, b| match (a.has_toggle_key, b.has_toggle_key) {
        (true, false) => std::cmp::Ordering::Less,
        (false, true) => std::cmp::Ordering::Greater,
        _ => compare_str(&a.name, &b.name),
    });

    Some(ModInfo {
        id: build_stable_mod_id(group_path, mod_path),
        name: folder_name,
        path: mod_path.to_string_lossy().into_owned(),
        is_enabled,
        preview: root_preview
            .or(enabled_preview)
            .or(disabled_preview)
            .map(|(_, path)| path),
        mtime: max_mtime,
        size: total_size,
        inis,
    })
}

#[napi]
pub async fn get_mods(group_path: String) -> FolderGroup {
    napi::tokio::task::spawn_blocking(move || get_mods_sync(group_path))
        .await
        .unwrap_or_else(|_| FolderGroup::default())
}

pub fn get_mods_sync(group_path: String) -> FolderGroup {
    let group_path_buf = PathBuf::from(&group_path);
    let group_name = group_path_buf
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("")
        .to_string();

    let mut mod_folders: Vec<PathBuf> = match fs::read_dir(&group_path_buf) {
        Ok(entries) => entries
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect(),
        Err(_) => Vec::new(),
    };

    mod_folders.sort_by(|a, b| compare_paths(a, b));

    let (mut mods, preview) = rayon::join(
        || {
            mod_folders
                .par_iter()
                .filter_map(|p| scan_mod_folder(&group_path_buf, p))
                .collect::<Vec<ModInfo>>()
        },
        || find_group_preview(&group_path_buf, 3),
    );

    mods.sort_by(|a, b| compare_str(&a.name, &b.name));

    let mod_count = mods.len() as u32;

    FolderGroup {
        name: group_name,
        path: group_path,
        mods,
        preview,
        mod_count,
    }
}

#[cfg(test)]
mod tests {
    use super::{
        find_group_preview, is_disabled_folder_name, parse_ini, scan_mod_folder,
        strip_disabled_prefix, strip_line_comment, strip_value_comment,
    };
    use std::fs;
    use std::path::{Path, PathBuf};
    use std::time::{SystemTime, UNIX_EPOCH};

    struct TestDir {
        path: PathBuf,
    }

    impl TestDir {
        fn new(name: &str) -> Self {
            let path = std::env::temp_dir().join(format!(
                "nhd-mod-manager-{name}-{}-{}",
                std::process::id(),
                SystemTime::now()
                    .duration_since(UNIX_EPOCH)
                    .unwrap_or_default()
                    .as_nanos()
            ));
            fs::create_dir_all(&path).unwrap();
            Self { path }
        }

        fn path(&self) -> &Path {
            &self.path
        }

        fn write_file(&self, relative_path: &str) -> PathBuf {
            let file_path = relative_path
                .split('/')
                .fold(self.path.clone(), |path, segment| path.join(segment));
            fs::create_dir_all(file_path.parent().unwrap()).unwrap();
            fs::write(&file_path, b"preview").unwrap();
            file_path
        }

        fn write_ini(&self, name: &str, content: &str) -> PathBuf {
            let file_path = self.path.join(name);
            fs::write(&file_path, content).unwrap();
            file_path
        }
    }

    impl Drop for TestDir {
        fn drop(&mut self) {
            let _ = fs::remove_dir_all(&self.path);
        }
    }

    #[test]
    fn strip_disabled_prefix_handles_ascii_prefix() {
        assert_eq!(strip_disabled_prefix(" Disabled Example "), "Example");
    }

    #[test]
    fn strip_disabled_prefix_handles_underscore_prefix() {
        assert_eq!(strip_disabled_prefix("DISABLED_Example"), "Example");
        assert_eq!(strip_disabled_prefix("disabled_My_Mod"), "My_Mod");
    }

    #[test]
    fn is_disabled_folder_name_recognizes_both_separators() {
        assert!(is_disabled_folder_name("DISABLED Foo"));
        assert!(is_disabled_folder_name("DISABLED_Foo"));
        assert!(is_disabled_folder_name("disabled_bar"));
        assert!(!is_disabled_folder_name("DisableFoo"));
    }

    #[test]
    fn scan_mod_folder_marks_underscore_prefix_as_disabled() {
        let dir = TestDir::new("underscore-prefix-disabled");
        dir.write_file("DISABLED_Aino Nude toggle/mod.ini");

        let mod_info =
            scan_mod_folder(dir.path(), &dir.path().join("DISABLED_Aino Nude toggle")).unwrap();

        assert!(!mod_info.is_enabled);
    }

    #[test]
    fn strip_disabled_prefix_does_not_panic_on_multibyte_leading_text() {
        assert_eq!(
            strip_disabled_prefix("仪玄-黑珍珠（1、2、3、4切换）"),
            "仪玄-黑珍珠（1、2、3、4切换）"
        );
    }

    #[test]
    fn find_group_preview_prefers_group_root_preview() {
        let dir = TestDir::new("root-preview");
        let root_preview = dir.write_file("preview.png");
        dir.write_file("Enabled Mod/preview.png");

        assert_eq!(
            find_group_preview(dir.path(), 3),
            Some(root_preview.to_string_lossy().into_owned())
        );
    }

    #[test]
    fn find_group_preview_does_not_search_child_folders_at_depth_one() {
        let dir = TestDir::new("depth-one");
        dir.write_file("Enabled Mod/preview.png");

        assert_eq!(find_group_preview(dir.path(), 1), None);
    }

    #[test]
    fn find_group_preview_prefers_enabled_folder_before_disabled_folder() {
        let dir = TestDir::new("enabled-before-disabled");
        let enabled_preview = dir.write_file("Enabled Mod/nested/deeper/preview.png");
        dir.write_file("DISABLED Other Mod/preview.png");

        assert_eq!(
            find_group_preview(dir.path(), 3),
            Some(enabled_preview.to_string_lossy().into_owned())
        );
    }

    #[test]
    fn find_group_preview_falls_back_to_disabled_folder() {
        let dir = TestDir::new("disabled-fallback");
        let disabled_preview = dir.write_file("DISABLED Other Mod/preview.png");

        assert_eq!(
            find_group_preview(dir.path(), 3),
            Some(disabled_preview.to_string_lossy().into_owned())
        );
    }

    #[test]
    fn scan_mod_folder_prefers_root_preview_before_disabled_subfolder() {
        let dir = TestDir::new("mod-root-before-disabled-subfolder");
        let root_preview = dir.write_file("Enabled Mod/screenshot.png");
        dir.write_file("Enabled Mod/DISABLED Nested/preview.png");
        let mod_info = scan_mod_folder(dir.path(), &dir.path().join("Enabled Mod")).unwrap();

        assert_eq!(
            mod_info.preview,
            Some(root_preview.to_string_lossy().into_owned())
        );
    }

    #[test]
    fn strip_value_comment_preserves_bare_semicolon() {
        assert_eq!(strip_value_comment(";"), ";");
    }

    #[test]
    fn strip_value_comment_preserves_inline_semicolon() {
        assert_eq!(strip_value_comment("ctrl ;"), "ctrl ;");
    }

    #[test]
    fn strip_value_comment_preserves_inline_comment_text() {
        assert_eq!(strip_value_comment("0 ; some comment"), "0 ; some comment");
    }

    #[test]
    fn strip_value_comment_preserves_inline_hash() {
        assert_eq!(strip_value_comment("a#b"), "a#b");
    }

    #[test]
    fn strip_value_comment_trims_whitespace() {
        assert_eq!(strip_value_comment("  ;  "), ";");
    }

    #[test]
    fn strip_line_comment_strips_leading_semicolon_line() {
        assert_eq!(strip_line_comment("; full line comment"), "");
    }

    #[test]
    fn strip_line_comment_strips_leading_hash_line() {
        assert_eq!(strip_line_comment("# hash line comment"), "");
    }

    #[test]
    fn strip_line_comment_preserves_inline_semicolon() {
        assert_eq!(strip_line_comment("key = ;"), "key = ;");
    }

    #[test]
    fn strip_line_comment_preserves_inline_hash() {
        assert_eq!(strip_line_comment("key = a#b"), "key = a#b");
    }

    #[test]
    fn parse_ini_reads_bare_semicolon_key() {
        let dir = TestDir::new("ini-bare-semicolon");
        let ini = dir.write_ini(
            "mod.ini",
            "[KeyOne]\ntype = cycle\nkey = ;\nback = ctrl\n$var = 1,2\n",
        );

        let keys = parse_ini(&ini.to_string_lossy());
        assert_eq!(keys.len(), 1);
        assert_eq!(keys[0].key.as_deref(), Some(";"));
    }

    #[test]
    fn parse_ini_reads_modifier_semicolon_key() {
        let dir = TestDir::new("ini-modifier-semicolon");
        let ini = dir.write_ini(
            "mod.ini",
            "[KeyTwo]\ntype = cycle\nkey = ctrl ;\n$var = 1,2\n",
        );

        let keys = parse_ini(&ini.to_string_lossy());
        assert_eq!(keys.len(), 1);
        assert_eq!(keys[0].key.as_deref(), Some("ctrl ;"));
    }

    #[test]
    fn parse_ini_skips_leading_semicolon_comment_line() {
        let dir = TestDir::new("ini-leading-comment");
        let ini = dir.write_ini(
            "mod.ini",
            "; this is a comment\n[KeyThree]\ntype = cycle\nkey = x\n$var = 1,2\n",
        );

        let keys = parse_ini(&ini.to_string_lossy());
        assert_eq!(keys.len(), 1);
        assert_eq!(keys[0].key.as_deref(), Some("x"));
    }

    #[test]
    fn parse_ini_preserves_value_with_inline_comment() {
        let dir = TestDir::new("ini-inline-comment");
        let ini = dir.write_ini(
            "mod.ini",
            "[KeyFour]\ntype = cycle\nkey = 0 ; note\n$var = 1,2\n",
        );

        let keys = parse_ini(&ini.to_string_lossy());
        assert_eq!(keys.len(), 1);
        assert_eq!(keys[0].key.as_deref(), Some("0 ; note"));
    }
}
