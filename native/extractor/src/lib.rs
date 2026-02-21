#![deny(clippy::all)]

use compress_tools::{uncompress_archive, Ownership};
use std::fs::{self, File};
use std::path::{Path, PathBuf};
use std::time::{SystemTime, UNIX_EPOCH};

use napi::bindgen_prelude::*;
use napi_derive::napi;

#[derive(Clone)]
pub struct ExtractorTask {
    pub archive: String,
    pub destination: String,
}

fn get_unique_folder_name(base_path: &Path, folder_name: &str) -> PathBuf {
    let mut target_path = base_path.join(folder_name);

    if !target_path.exists() {
        return target_path;
    }

    let mut counter = 2;
    loop {
        let new_name = format!("{} ({})", folder_name, counter);
        target_path = base_path.join(&new_name);

        if !target_path.exists() {
            return target_path;
        }

        counter += 1;
    }
}

#[napi]
impl Task for ExtractorTask {
    type Output = String;
    type JsValue = String;

    fn compute(&mut self) -> Result<Self::Output> {
        let dest_path = Path::new(&self.destination);
        let archive_path = Path::new(&self.archive);

        let timestamp = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos();

        let temp_folder = dest_path.join(format!(
            ".extract_temp_{}_{}",
            std::process::id(),
            timestamp
        ));

        fs::create_dir_all(&temp_folder)
            .map_err(|e| Error::from_reason(format!("Failed to create temp folder: {}", e)))?;

        let cleanup_temp = |e: &dyn std::fmt::Display| {
            let _ = fs::remove_dir_all(&temp_folder);
            Error::from_reason(format!("Extraction failed: {}", e))
        };

        let mut source = File::open(archive_path).map_err(|e| cleanup_temp(&e))?;

        if let Err(e) = uncompress_archive(&mut source, &temp_folder, Ownership::Ignore) {
            return Err(cleanup_temp(&e));
        }

        let mut current_path = temp_folder.clone();
        let mut target_folder_name = archive_path
            .file_stem()
            .and_then(|s| s.to_str())
            .unwrap_or("extracted")
            .to_string();

        loop {
            let entries: Vec<std::fs::DirEntry> = match fs::read_dir(&current_path) {
                Ok(dir) => dir.filter_map(|e| e.ok()).collect(),
                Err(e) => return Err(cleanup_temp(&e)),
            };

            let valid_entries: Vec<&std::fs::DirEntry> = entries
                .iter()
                .filter(|e| {
                    let name = e.file_name();
                    let lower = name.to_string_lossy().to_lowercase();
                    !matches!(lower.as_str(), "desktop.ini" | "thumbs.db")
                })
                .collect();

            if valid_entries.len() == 1 {
                let single_entry = valid_entries[0];
                if let Ok(file_type) = single_entry.file_type() {
                    if file_type.is_dir() {
                        current_path = single_entry.path();
                        target_folder_name = single_entry.file_name().to_string_lossy().to_string();
                        continue;
                    }
                }
            }
            break;
        }

        let target_path = get_unique_folder_name(dest_path, &target_folder_name);

        if current_path != temp_folder {
            if let Err(e) = fs::rename(&current_path, &target_path) {
                return Err(cleanup_temp(&format!(
                    "Failed to move extracted folder: {}",
                    e
                )));
            }
            let _ = fs::remove_dir_all(&temp_folder);
        } else {
            if let Err(e) = fs::rename(&temp_folder, &target_path) {
                return Err(cleanup_temp(&format!(
                    "Failed to rename temp folder: {}",
                    e
                )));
            }
        }

        let final_path = target_path.to_string_lossy().to_string();

        Ok(final_path)
    }

    fn resolve(&mut self, _env: Env, output: Self::Output) -> Result<Self::JsValue> {
        Ok(output)
    }
}

#[napi]
pub fn extract_archive(archive_path: String, destination_path: String) -> AsyncTask<ExtractorTask> {
    AsyncTask::new(ExtractorTask {
        archive: archive_path,
        destination: destination_path,
    })
}
