use napi_derive::napi;
use rayon::prelude::*;
use std::collections::HashMap;
use std::fs::{self, File};
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
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

fn is_media_file(path: &Path) -> bool {
    // Check if it's a file first
    if !path.is_file() {
        return false;
    }

    let image_extensions = ["png", "jpg", "jpeg", "gif", "webp", "bmp"];
    let video_extensions = ["mp4", "webm", "avi", "mkv", "mov"];

    if let Some(ext) = path.extension().and_then(|e| e.to_str()) {
        let lower_ext = ext.to_lowercase();
        return image_extensions.contains(&lower_ext.as_str())
            || video_extensions.contains(&lower_ext.as_str());
    }
    false
}

fn get_score(path: &Path, root_path: &Path) -> i32 {
    let lower_path = path.to_string_lossy().to_lowercase();
    let filename = path
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("")
        .to_lowercase();
    let is_root = path.parent() == Some(root_path);
    let video_extensions = [".mp4", ".webm", ".avi", ".mkv", ".mov"];
    let is_video = video_extensions.iter().any(|ext| lower_path.ends_with(ext));

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

fn is_excluded_file(path: &Path) -> bool {
    let excluded_keywords = ["normal", "light", "material", "diffuse"];
    let filename = path
        .file_name()
        .and_then(|n| n.to_str())
        .unwrap_or("")
        .to_lowercase();

    if filename.contains("preview") {
        return false;
    }

    excluded_keywords.iter().any(|k| filename.contains(k))
}

fn find_preview(mod_path: &Path) -> Option<String> {
    let walker = WalkDir::new(mod_path).follow_links(true);
    let mut candidate_files: Vec<PathBuf> = Vec::new();

    for entry in walker.into_iter().filter_map(|e| e.ok()) {
        let path = entry.path();
        if is_media_file(path) && !is_excluded_file(path) {
            candidate_files.push(path.to_path_buf());
        }
    }

    if candidate_files.is_empty() {
        return None;
    }

    // Sort candidates
    candidate_files.sort_by(|a, b| {
        let score_a = get_score(a, mod_path);
        let score_b = get_score(b, mod_path);

        if score_a != score_b {
            return score_b.cmp(&score_a);
        }

        // Simplified string comparison
        a.cmp(b)
    });

    candidate_files
        .first()
        .map(|p| p.to_string_lossy().to_string())
}

#[napi]
pub fn get_characters_folder(mod_folder_path: String) -> Vec<FolderGroup> {
    let root_path = Path::new(&mod_folder_path);

    if !root_path.exists() || !root_path.is_dir() {
        return Vec::new();
    }

    // List group folders first
    let groups: Vec<PathBuf> = match fs::read_dir(root_path) {
        Ok(entries) => entries
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect(),
        Err(_) => return Vec::new(),
    };

    // Parallel process each group
    groups
        .par_iter()
        .map(|group_path| {
            let name = group_path
                .file_name()
                .and_then(|n| n.to_str())
                .unwrap_or("")
                .to_string();

            let path_str = group_path.to_string_lossy().to_string();

            // Count mod subdirectories
            let mod_count = match fs::read_dir(group_path) {
                Ok(entries) => entries
                    .filter_map(|e| e.ok())
                    .filter(|e| e.path().is_dir())
                    .count() as u32,
                Err(_) => 0,
            };

            let preview = find_preview(group_path);

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
