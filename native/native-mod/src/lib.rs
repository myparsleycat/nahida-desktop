use alphanumeric_sort::compare_str;
use napi_derive::napi;
use rayon::prelude::*;
use std::collections::HashMap;
use std::fs::{self, File};
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::time::SystemTime;
use walkdir::WalkDir;

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
    pub name: String,
    pub path: String,
    pub is_enabled: bool,
    pub preview: Option<String>,
    pub mtime: f64,
    pub size: f64,
    pub inis: Vec<IniResult>,
}

#[napi(object)]
pub struct FolderGroup {
    pub name: String,
    pub path: String,
    pub mods: Vec<ModInfo>,
    pub preview: Option<String>,
    pub mod_count: u32,
}

fn get_map_value(data: &HashMap<String, String>, key: &str) -> Option<String> {
    data.get(key).cloned().filter(|s| !s.is_empty())
}

fn process_section_data(
    section_name: &str,
    data: &HashMap<String, String>,
    ini_file_name: &str,
) -> Option<ToggleKey> {
    if !section_name.to_lowercase().starts_with("key") {
        return None;
    }

    let (variable, values_str) = data.iter().find(|(k, _)| k.starts_with('$'))?;

    let values: Vec<String> = values_str
        .split(',')
        .map(|s| s.trim().to_string())
        .collect();

    let type_val = get_map_value(data, "type");
    let is_hold = type_val
        .as_deref()
        .map(|t| t.eq_ignore_ascii_case("hold"))
        .unwrap_or(false);

    if values.len() < 2 && !is_hold {
        return None;
    }

    let current_value = values.first().cloned();

    Some(ToggleKey {
        section_name: section_name.to_string(),
        ini_file_name: ini_file_name.to_string(),
        key: get_map_value(data, "key"),
        back: get_map_value(data, "back"),
        type_: type_val,
        variable: variable.clone(),
        values,
        current_value,
    })
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
        let line = match line_result {
            Ok(l) => l,
            Err(_) => continue,
        };

        let clean_line = line.trim_start_matches('\u{FEFF}').trim();

        if clean_line.is_empty() || clean_line.starts_with(';') {
            continue;
        }

        if clean_line.starts_with('[') && clean_line.ends_with(']') {
            if !current_section.is_empty() {
                if let Some(tk) =
                    process_section_data(&current_section, &section_data, &ini_file_name)
                {
                    toggle_keys.push(tk);
                }
                section_data.clear();
            }

            current_section = clean_line[1..clean_line.len() - 1].to_string();
            continue;
        }

        if !current_section.is_empty() {
            if let Some((k, v)) = clean_line.split_once('=') {
                let key = k.trim().to_lowercase();
                let value = v.trim().to_string();
                section_data.insert(key, value);
            }
        }
    }

    if !current_section.is_empty() {
        if let Some(tk) = process_section_data(&current_section, &section_data, &ini_file_name) {
            toggle_keys.push(tk);
        }
    }

    toggle_keys
}

#[napi]
pub fn process_ini_files(paths: Vec<String>) -> Vec<IniResult> {
    paths
        .par_iter()
        .map(|path_str| {
            let mut toggle_keys = parse_ini(path_str);

            toggle_keys.sort_by(|a, b| {
                let a_has_key = a.key.is_some();
                let b_has_key = b.key.is_some();
                b_has_key.cmp(&a_has_key)
            });

            let has_toggle_key = toggle_keys.iter().any(|tk| tk.key.is_some());
            let name = Path::new(path_str)
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or("")
                .to_string();

            IniResult {
                name,
                path: path_str.clone(),
                toggle_keys,
                has_toggle_key,
            }
        })
        .collect()
}

fn is_media_ext(ext: &str) -> bool {
    match ext.to_ascii_lowercase().as_str() {
        "png" | "jpg" | "jpeg" | "gif" | "webp" | "bmp" | "avif" | "avifs" | "mp4" | "webm"
        | "avi" | "mkv" | "mov" => true,
        _ => false,
    }
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
    if filename.contains("preview") {
        return false;
    }
    const EXCLUDED: &[&str] = &["normal", "light", "material", "diffuse"];
    EXCLUDED.iter().any(|&k| filename.contains(k))
}

fn find_preview(
    mod_path: &Path,
    pre_scanned_files: Option<&[PathBuf]>,
    max_depth: usize,
) -> Option<String> {
    let mut candidate_files: Vec<(i32, PathBuf)> = Vec::new();
    let video_extensions = ["mp4", "webm"];

    let process_file = |path: PathBuf, candidate_files: &mut Vec<(i32, PathBuf)>| {
        let filename_os = path.file_name().and_then(|n| n.to_str());
        let ext_os = path.extension().and_then(|e| e.to_str());

        if let (Some(filename), Some(ext)) = (filename_os, ext_os) {
            let lower_filename = filename.to_lowercase();
            if is_media_ext(ext) && !is_excluded_file(&lower_filename) {
                let relative = path.strip_prefix(mod_path).unwrap_or(&path);
                let is_root = relative.components().count() == 1;
                let is_video = video_extensions.contains(&ext.to_lowercase().as_str());
                let score = get_score(&lower_filename, is_root, is_video);
                candidate_files.push((score, path));
            }
        }
    };

    if let Some(files) = pre_scanned_files {
        for path in files {
            process_file(path.clone(), &mut candidate_files);
        }
    } else {
        let walker = WalkDir::new(mod_path)
            .max_depth(max_depth)
            .follow_links(true)
            .into_iter()
            .filter_map(|e| e.ok());

        for entry in walker {
            if !entry.file_type().is_file() {
                continue;
            }
            process_file(entry.path().to_path_buf(), &mut candidate_files);
        }
    }

    if candidate_files.is_empty() {
        return None;
    }

    candidate_files.sort_by(|a, b| {
        b.0.cmp(&a.0).then_with(|| {
            compare_str(
                a.1.to_string_lossy().as_ref(),
                b.1.to_string_lossy().as_ref(),
            )
        })
    });

    candidate_files
        .first()
        .map(|p| p.1.to_string_lossy().to_string())
}

#[napi]
pub fn get_characters_folder(
    mod_folder_path: String,
    fallback_to_mod_preview: Option<bool>,
) -> Vec<FolderGroup> {
    let root_path = Path::new(&mod_folder_path);

    if !root_path.exists() || !root_path.is_dir() {
        return Vec::new();
    }

    let groups: Vec<PathBuf> = match fs::read_dir(root_path) {
        Ok(entries) => entries
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect(),
        Err(_) => return Vec::new(),
    };

    let search_depth = if fallback_to_mod_preview.unwrap_or(true) {
        3
    } else {
        1
    };

    groups
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
                    .filter(|e| e.path().is_dir())
                    .count() as u32,
                Err(_) => 0,
            };

            let preview = find_preview(group_path, None, search_depth);

            FolderGroup {
                name,
                path: path_str,
                mods: Vec::new(),
                preview,
                mod_count,
            }
        })
        .collect()
}

fn scan_mod_folder(mod_path: &Path) -> Option<ModInfo> {
    let folder_name = mod_path.file_name()?.to_string_lossy().to_string();
    let is_enabled = !folder_name.to_ascii_lowercase().starts_with("disabled ");

    let mut total_size = 0.0;
    let mut max_mtime = 0.0;
    let mut all_files = Vec::new();
    let mut ini_paths = Vec::new();

    for entry in WalkDir::new(mod_path).into_iter().filter_map(|e| e.ok()) {
        if entry.file_type().is_file() {
            let path = entry.path().to_path_buf();
            let metadata = entry.metadata().ok()?;
            let size = metadata.len() as f64;
            let mtime = metadata
                .modified()
                .unwrap_or(SystemTime::UNIX_EPOCH)
                .duration_since(SystemTime::UNIX_EPOCH)
                .unwrap_or_default()
                .as_secs_f64()
                * 1000.0;

            total_size += size;
            if mtime > max_mtime {
                max_mtime = mtime;
            }

            if let Some(ext) = path.extension().and_then(|e| e.to_str()) {
                let lower_ext = ext.to_lowercase();
                if is_media_ext(&lower_ext) || lower_ext == "ini" {
                    all_files.push(path.clone());
                    if lower_ext == "ini" {
                        let fname = path.file_name().unwrap_or_default().to_string_lossy();
                        if !fname.to_lowercase().starts_with("disabled") {
                            ini_paths.push(path.to_string_lossy().to_string());
                        }
                    }
                }
            }
        }
    }

    if max_mtime == 0.0 {
        if let Ok(metadata) = fs::metadata(mod_path) {
            max_mtime = metadata
                .modified()
                .unwrap_or(SystemTime::UNIX_EPOCH)
                .duration_since(SystemTime::UNIX_EPOCH)
                .unwrap_or_default()
                .as_secs_f64()
                * 1000.0;
        }
    }

    let mut inis = process_ini_files(ini_paths);
    inis.sort_by(|a, b| match (a.has_toggle_key, b.has_toggle_key) {
        (true, false) => std::cmp::Ordering::Less,
        (false, true) => std::cmp::Ordering::Greater,
        _ => compare_str(&a.name, &b.name),
    });

    let preview = find_preview(mod_path, Some(&all_files), 3);

    Some(ModInfo {
        name: folder_name,
        path: mod_path.to_string_lossy().to_string(),
        is_enabled,
        preview,
        mtime: max_mtime,
        size: total_size,
        inis,
    })
}

#[napi]
pub fn get_mods(group_path: String) -> FolderGroup {
    let group_path_buf = PathBuf::from(&group_path);
    let group_name = group_path_buf
        .file_name()
        .unwrap_or_default()
        .to_string_lossy()
        .to_string();

    let mod_folders: Vec<PathBuf> = match fs::read_dir(&group_path_buf) {
        Ok(entries) => entries
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect(),
        Err(_) => Vec::new(),
    };

    let mods: Vec<ModInfo> = mod_folders
        .par_iter()
        .filter_map(|p| scan_mod_folder(p))
        .collect();

    let preview = find_preview(&group_path_buf, None, 3);
    let mod_count = mods.len() as u32;

    FolderGroup {
        name: group_name,
        path: group_path,
        mods,
        preview,
        mod_count,
    }
}
