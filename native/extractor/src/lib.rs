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

        let entries: Vec<PathBuf> = match fs::read_dir(&temp_folder) {
            Ok(dir) => dir
                .filter_map(|entry| entry.ok().map(|e| e.path()))
                .collect(),
            Err(e) => return Err(cleanup_temp(&e)),
        };

        let final_path = if entries.len() == 1 && entries[0].is_dir() {
            let extracted_folder = &entries[0];
            let folder_name = match extracted_folder.file_name().and_then(|n| n.to_str()) {
                Some(name) => name,
                None => return Err(cleanup_temp(&"Failed to get folder name")),
            };

            let target_path = get_unique_folder_name(dest_path, folder_name);

            if let Err(e) = fs::rename(extracted_folder, &target_path) {
                return Err(cleanup_temp(&format!(
                    "Failed to move extracted folder: {}",
                    e
                )));
            }

            let _ = fs::remove_dir_all(&temp_folder);

            target_path.to_string_lossy().to_string()
        } else {
            let archive_stem = match archive_path.file_stem().and_then(|s| s.to_str()) {
                Some(name) => name,
                None => return Err(cleanup_temp(&"Failed to get archive name")),
            };

            let target_path = get_unique_folder_name(dest_path, archive_stem);

            if let Err(e) = fs::rename(&temp_folder, &target_path) {
                return Err(cleanup_temp(&format!(
                    "Failed to rename temp folder: {}",
                    e
                )));
            }

            target_path.to_string_lossy().to_string()
        };

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
