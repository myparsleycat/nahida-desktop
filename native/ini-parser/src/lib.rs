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

fn trim(s: &str) -> &str {
    s.trim_matches(|c| c == ' ' || c == '\t' || c == '\r' || c == '\n')
}

fn iequals(a: &str, b: &str) -> bool {
    a.eq_ignore_ascii_case(b)
}

fn istarts_with(s: &str, prefix: &str) -> bool {
    if s.len() < prefix.len() {
        return false;
    }
    s[..prefix.len()].eq_ignore_ascii_case(prefix)
}

fn get_map_value(data: &HashMap<String, String>, key: &str) -> Option<String> {
    data.get(key).cloned().filter(|s| !s.is_empty())
}

fn extract_toggle_key(
    section_name: &str,
    data: &HashMap<String, String>,
    ini_file_name: &str,
) -> Option<ToggleKey> {
    let mut variable = String::new();
    let mut values_str = String::new();
    let mut found_variable = false;

    for (k, v) in data {
        if k.starts_with('$') {
            variable = k.clone();
            values_str = v.clone();
            found_variable = true;
            break;
        }
    }

    if !found_variable {
        return None;
    }

    let values: Vec<String> = values_str.split(',').map(|s| trim(s).to_string()).collect();

    let type_val = get_map_value(data, "type");
    let is_hold = type_val
        .as_deref()
        .map(|t| iequals(t, "hold"))
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
        variable,
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
    let mut first_line = true;

    let flush_section =
        |section: &str, data: &mut HashMap<String, String>, keys: &mut Vec<ToggleKey>| {
            if !section.is_empty() && istarts_with(section, "key") {
                if let Some(tk) = extract_toggle_key(section, data, &ini_file_name) {
                    keys.push(tk);
                }
            }
            data.clear();
        };

    for line_result in reader.lines() {
        let mut line = match line_result {
            Ok(l) => l,
            Err(_) => continue,
        };

        if first_line {
            if line.starts_with("\u{FEFF}") {
                line.remove(0);
            }
            first_line = false;
        }

        let line_view = trim(&line);
        if line_view.is_empty() || line_view.starts_with(';') {
            continue;
        }

        if line_view.starts_with('[') && line_view.ends_with(']') {
            flush_section(&current_section, &mut section_data, &mut toggle_keys);
            current_section = line_view[1..line_view.len() - 1].to_string();
            continue;
        }

        if !current_section.is_empty() {
            if let Some(eq_pos) = line_view.find('=') {
                let key = trim(&line_view[..eq_pos]).to_lowercase();
                let value = trim(&line_view[eq_pos + 1..]).to_string();
                section_data.insert(key, value);
            }
        }
    }

    flush_section(&current_section, &mut section_data, &mut toggle_keys);

    toggle_keys
}

#[napi(js_name = "processIniFiles")]
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
