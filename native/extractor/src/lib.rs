#![deny(clippy::all)]

use compress_tools::{uncompress_archive, ArchiveContents, ArchiveIterator, Ownership};
use napi::bindgen_prelude::*;
use napi::threadsafe_function::{ThreadsafeFunction, ThreadsafeFunctionCallMode};
use napi_derive::napi;
use std::collections::HashMap;
use std::fs::{self, File};
use std::io::{Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::sync::{
    atomic::{AtomicU8, Ordering},
    Arc,
};
use std::time::{SystemTime, UNIX_EPOCH};

#[napi(object)]
pub struct ExtractProgress {
    pub percent: u32,
    pub message: String,
}

#[allow(non_snake_case)]
#[napi(object)]
pub struct ExtractOptions {
    #[allow(non_snake_case)]
    pub flattenSingleRoot: Option<bool>,
}

#[derive(Clone)]
pub struct ExtractorTask {
    pub archive: String,
    pub destination: String,
    pub flatten_single_root: bool,
    pub on_progress: Option<Arc<ThreadsafeFunction<ExtractProgress>>>,
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

fn emit_progress(
    callback: &Option<Arc<ThreadsafeFunction<ExtractProgress>>>,
    percent: u32,
    message: impl Into<String>,
) {
    if let Some(callback) = callback {
        callback.call(
            Ok(ExtractProgress {
                percent,
                message: message.into(),
            }),
            ThreadsafeFunctionCallMode::NonBlocking,
        );
    }
}

fn normalize_archive_entry_name(name: &str) -> String {
    name.replace('\\', "/").trim_start_matches("./").trim_matches('/').to_string()
}

fn is_ignored_top_level_entry(name: &str) -> bool {
    matches!(name.to_ascii_lowercase().as_str(), "desktop.ini" | "thumbs.db")
}

fn is_directory_entry(name: &str) -> bool {
    name.ends_with('/') || name.ends_with('\\')
}

struct ProgressReader<R> {
    inner: R,
    total_bytes: u64,
    processed_bytes: u64,
    last_percent: Arc<AtomicU8>,
    callback: Option<Arc<ThreadsafeFunction<ExtractProgress>>>,
}

impl<R> ProgressReader<R> {
    fn new(
        inner: R,
        total_bytes: u64,
        callback: Option<Arc<ThreadsafeFunction<ExtractProgress>>>,
        last_percent: Arc<AtomicU8>,
    ) -> Self {
        Self {
            inner,
            total_bytes,
            processed_bytes: 0,
            last_percent,
            callback,
        }
    }

    fn report_if_needed(&self) {
        if self.total_bytes == 0 {
            return;
        }

        let raw_percent = ((self.processed_bytes.saturating_mul(90)) / self.total_bytes) as u32;
        let capped_percent = raw_percent.clamp(1, 90) as u8;
        let prev = self.last_percent.load(Ordering::Relaxed);

        if capped_percent > prev
            && self
                .last_percent
                .compare_exchange(prev, capped_percent, Ordering::Relaxed, Ordering::Relaxed)
                .is_ok()
        {
            emit_progress(
                &self.callback,
                capped_percent as u32,
                format!("Extracting... {}%", capped_percent),
            );
        }
    }
}

impl<R: Read> Read for ProgressReader<R> {
    fn read(&mut self, buf: &mut [u8]) -> std::io::Result<usize> {
        let read = self.inner.read(buf)?;
        if read > 0 {
            self.processed_bytes = self.processed_bytes.saturating_add(read as u64);
            self.report_if_needed();
        }
        Ok(read)
    }
}

impl<R: Seek> Seek for ProgressReader<R> {
    fn seek(&mut self, pos: SeekFrom) -> std::io::Result<u64> {
        let next = self.inner.seek(pos)?;
        self.processed_bytes = next;
        self.report_if_needed();
        Ok(next)
    }
}

#[napi]
impl Task for ExtractorTask {
    type Output = String;
    type JsValue = String;

    fn compute(&mut self) -> Result<Self::Output> {
        let dest_path = Path::new(&self.destination);
        let archive_path = Path::new(&self.archive);

        emit_progress(&self.on_progress, 0, "Preparing extraction");

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

        let source_file = File::open(archive_path).map_err(|e| cleanup_temp(&e))?;
        let archive_size = source_file.metadata().map(|m| m.len()).unwrap_or(0);

        let last_percent = Arc::new(AtomicU8::new(0));
        let mut source = ProgressReader::new(
            source_file,
            archive_size,
            self.on_progress.clone(),
            last_percent.clone(),
        );

        emit_progress(&self.on_progress, 1, "Starting extraction");

        if let Err(e) = uncompress_archive(&mut source, &temp_folder, Ownership::Ignore) {
            return Err(cleanup_temp(&e));
        }

        if last_percent.load(Ordering::Relaxed) < 92 {
            last_percent.store(92, Ordering::Relaxed);
            emit_progress(&self.on_progress, 92, "Finalizing extracted files");
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

            if self.flatten_single_root && valid_entries.len() == 1 {
                let single_entry = valid_entries[0];
                if let Ok(file_type) = single_entry.file_type() {
                    if file_type.is_dir() {
                        current_path = single_entry.path();
                        target_folder_name = single_entry.file_name().to_string_lossy().to_string();

                        if last_percent.load(Ordering::Relaxed) < 95 {
                            last_percent.store(95, Ordering::Relaxed);
                            emit_progress(
                                &self.on_progress,
                                95,
                                format!("Resolving extracted folder: {}", target_folder_name),
                            );
                        }

                        continue;
                    }
                }
            }
            break;
        }

        let target_path = get_unique_folder_name(dest_path, &target_folder_name);

        if last_percent.load(Ordering::Relaxed) < 97 {
            last_percent.store(97, Ordering::Relaxed);
            emit_progress(&self.on_progress, 97, "Moving extracted contents");
        }

        if current_path != temp_folder {
            if let Err(e) = fs::rename(&current_path, &target_path) {
                return Err(cleanup_temp(&format!(
                    "Failed to move extracted folder: {}",
                    e
                )));
            }
            let _ = fs::remove_dir_all(&temp_folder);
        } else if let Err(e) = fs::rename(&temp_folder, &target_path) {
            return Err(cleanup_temp(&format!(
                "Failed to rename temp folder: {}",
                e
            )));
        }

        let final_path = target_path.to_string_lossy().to_string();

        emit_progress(&self.on_progress, 100, "Extraction complete");

        Ok(final_path)
    }

    fn resolve(&mut self, _env: Env, output: Self::Output) -> Result<Self::JsValue> {
        Ok(output)
    }
}

#[napi]
pub fn extract_archive(
    archive_path: String,
    destination_path: String,
    options: Option<ExtractOptions>,
    on_progress: Option<ThreadsafeFunction<ExtractProgress>>,
) -> AsyncTask<ExtractorTask> {
    AsyncTask::new(ExtractorTask {
        archive: archive_path,
        destination: destination_path,
        flatten_single_root: options
            .and_then(|opts| opts.flattenSingleRoot)
            .unwrap_or(true),
        on_progress: on_progress.map(Arc::new),
    })
}

#[napi]
pub fn has_single_top_level_directory(archive_path: String) -> Result<bool> {
    let source_file = File::open(&archive_path)
        .map_err(|e| Error::from_reason(format!("Failed to open archive: {}", e)))?;
    let iterator = ArchiveIterator::from_read(source_file)
        .map_err(|e| Error::from_reason(format!("Failed to inspect archive: {}", e)))?;

    let mut top_level_entries: HashMap<String, bool> = HashMap::new();

    for content in iterator {
        let ArchiveContents::StartOfEntry(name, _stat) = content else {
            continue;
        };

        let normalized = normalize_archive_entry_name(&name);
        if normalized.is_empty() {
            continue;
        }

        let mut segments = normalized.split('/').filter(|segment| !segment.is_empty());
        let Some(top_level_name) = segments.next() else {
            continue;
        };

        let has_nested_segments = segments.next().is_some();
        if !has_nested_segments && is_ignored_top_level_entry(top_level_name) {
            continue;
        }

        let is_dir = has_nested_segments || is_directory_entry(&name);
        top_level_entries
            .entry(top_level_name.to_string())
            .and_modify(|existing| *existing = *existing || is_dir)
            .or_insert(is_dir);

        if top_level_entries.len() > 1 {
            return Ok(false);
        }
    }

    Ok(matches!(top_level_entries.into_values().next(), Some(true)))
}
