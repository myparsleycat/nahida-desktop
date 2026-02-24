use alphanumeric_sort::compare_str;
use napi_derive::napi;
use rayon::prelude::*;
use std::collections::HashMap;
use std::fs::{self, File};
use std::io::{BufRead, BufReader};
use std::path::{Path, PathBuf};
use std::time::SystemTime;
use walkdir::WalkDir;
use windows::Win32::Foundation::{BOOL, HWND, LPARAM};
use windows::Win32::UI::Input::KeyboardAndMouse::{
    SendInput, INPUT, INPUT_0, INPUT_KEYBOARD, KEYBDINPUT, KEYEVENTF_KEYUP, VK_F10,
};
use windows::Win32::UI::WindowsAndMessaging::{
    EnumWindows, GetWindowThreadProcessId, IsWindowVisible, SetForegroundWindow,
};

struct FindWindowData {
    target_pid: u32,
    found_hwnd: Option<HWND>,
}

unsafe extern "system" fn enum_windows_callback(hwnd: HWND, lparam: LPARAM) -> BOOL {
    let data = &mut *(lparam.0 as *mut FindWindowData);
    let mut process_id = 0;
    GetWindowThreadProcessId(hwnd, Some(&mut process_id));

    if process_id == data.target_pid && IsWindowVisible(hwnd).as_bool() {
        data.found_hwnd = Some(hwnd);
        return BOOL(0);
    }

    BOOL(1)
}

#[napi]
pub fn send_f10(pid: u32) -> bool {
    unsafe {
        let mut data = FindWindowData {
            target_pid: pid,
            found_hwnd: None,
        };

        let _ = EnumWindows(
            Some(enum_windows_callback),
            LPARAM(&mut data as *mut _ as isize),
        );

        if let Some(hwnd) = data.found_hwnd {
            if SetForegroundWindow(hwnd).as_bool() {
                std::thread::sleep(std::time::Duration::from_millis(100));

                let input_down = [INPUT {
                    r#type: INPUT_KEYBOARD,
                    Anonymous: INPUT_0 {
                        ki: KEYBDINPUT {
                            wVk: VK_F10,
                            ..Default::default()
                        },
                    },
                }];

                SendInput(&input_down, std::mem::size_of::<INPUT>() as i32);

                std::thread::sleep(std::time::Duration::from_millis(100));

                let input_up = [INPUT {
                    r#type: INPUT_KEYBOARD,
                    Anonymous: INPUT_0 {
                        ki: KEYBDINPUT {
                            wVk: VK_F10,
                            dwFlags: KEYEVENTF_KEYUP,
                            ..Default::default()
                        },
                    },
                }];

                SendInput(&input_up, std::mem::size_of::<INPUT>() as i32);
                return true;
            }
        }
    }
    false
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
    if !section_name.to_ascii_lowercase().starts_with("key") {
        return None;
    }

    let type_val = get_map_value(data, "type");
    let is_hold = type_val
        .as_deref()
        .map(|t| t.eq_ignore_ascii_case("hold"))
        .unwrap_or(false);

    let (variable, values) =
        data.iter()
            .filter(|(k, _)| k.starts_with('$'))
            .find_map(|(k, v)| {
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
                let key = k.trim().to_ascii_lowercase();
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
    if filename.contains("preview") {
        return false;
    }
    const EXCLUDED: &[&str] = &["normal", "light", "material", "diffuse"];
    EXCLUDED.iter().any(|&k| filename.contains(k))
}

fn find_preview(mod_path: &Path, max_depth: usize) -> Option<String> {
    let mut best_score = -1;
    let mut best_path: Option<String> = None;

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

                    if score > best_score {
                        best_score = score;
                        best_path = Some(path.to_string_lossy().into_owned());
                    } else if score == best_score {
                        if let Some(ref best) = best_path {
                            let path_str = path.to_string_lossy();
                            if compare_str(path_str.as_ref(), best) == std::cmp::Ordering::Less {
                                best_path = Some(path_str.into_owned());
                            }
                        }
                    }
                }
            }
        }
    }

    best_path
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

            let preview = find_preview(group_path, search_depth);

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
    let mut max_mtime_sys = SystemTime::UNIX_EPOCH;
    let mut ini_paths = Vec::new();

    let mut best_preview_score = -1;
    let mut best_preview_path: Option<String> = None;

    for entry in WalkDir::new(mod_path).into_iter().filter_map(|e| e.ok()) {
        if entry.file_type().is_file() {
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

                            if score > best_preview_score {
                                best_preview_score = score;
                                best_preview_path = Some(path.to_string_lossy().into_owned());
                            } else if score == best_preview_score {
                                if let Some(ref best_path) = best_preview_path {
                                    let path_str = path.to_string_lossy();
                                    if compare_str(path_str.as_ref(), best_path)
                                        == std::cmp::Ordering::Less
                                    {
                                        best_preview_path = Some(path_str.into_owned());
                                    }
                                }
                            }
                        }
                    }
                }
            }
        }
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

    let mut inis = process_ini_files(ini_paths);
    inis.sort_by(|a, b| match (a.has_toggle_key, b.has_toggle_key) {
        (true, false) => std::cmp::Ordering::Less,
        (false, true) => std::cmp::Ordering::Greater,
        _ => compare_str(&a.name, &b.name),
    });

    Some(ModInfo {
        name: folder_name,
        path: mod_path.to_string_lossy().into_owned(),
        is_enabled,
        preview: best_preview_path,
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
        .and_then(|n| n.to_str())
        .unwrap_or("")
        .to_string();

    let mod_folders: Vec<PathBuf> = match fs::read_dir(&group_path_buf) {
        Ok(entries) => entries
            .filter_map(|e| e.ok())
            .map(|e| e.path())
            .filter(|p| p.is_dir())
            .collect(),
        Err(_) => Vec::new(),
    };

    let (mods, preview) = rayon::join(
        || {
            mod_folders
                .par_iter()
                .filter_map(|p| scan_mod_folder(p))
                .collect::<Vec<ModInfo>>()
        },
        || find_preview(&group_path_buf, 3),
    );

    let mod_count = mods.len() as u32;

    FolderGroup {
        name: group_name,
        path: group_path,
        mods,
        preview,
        mod_count,
    }
}
