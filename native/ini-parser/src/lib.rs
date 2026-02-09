use napi_derive::napi;
use rayon::prelude::*;
use std::collections::HashMap;
use std::fs::File;
use std::io::{BufRead, BufReader};
use std::path::Path;

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
pub struct IniResult {
    pub name: String,
    pub path: String,
    pub toggle_keys: Vec<ToggleKey>,
    pub has_toggle_key: bool,
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
