#![deny(clippy::all)]

use compress_tools::{ArchiveContents, ArchiveIterator, ArchiveIteratorBuilder};
use napi::bindgen_prelude::*;
use napi::threadsafe_function::{ThreadsafeFunction, ThreadsafeFunctionCallMode};
use napi_derive::napi;
use std::collections::HashMap;
use std::fs::{self, File};
use std::io::Write;
use std::path::{Component, Path, PathBuf};
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

struct TempExtractionDir {
    path: PathBuf,
    keep: bool,
}

impl TempExtractionDir {
    fn new(path: PathBuf) -> Self {
        Self { path, keep: false }
    }

    fn path(&self) -> &Path {
        &self.path
    }

    fn disarm(&mut self) {
        self.keep = true;
    }
}

impl Drop for TempExtractionDir {
    fn drop(&mut self) {
        if !self.keep {
            let _ = fs::remove_dir_all(&self.path);
        }
    }
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
    name.eq_ignore_ascii_case("desktop.ini") || name.eq_ignore_ascii_case("thumbs.db")
}

fn is_directory_entry(name: &str) -> bool {
    name.ends_with('/') || name.ends_with('\\')
}

fn is_directory_stat(stat: &libc::stat) -> bool {
    let mode = u32::from(stat.st_mode);
    (mode & libc::S_IFMT as u32) == libc::S_IFDIR as u32
}

fn sanitize_entry_path(name: &str) -> Result<PathBuf> {
    let normalized = normalize_archive_entry_name(name);
    let path = Path::new(&normalized);

    if normalized.is_empty() {
        return Err(Error::from_reason("Archive entry has empty path"));
    }

    let sanitized = path
        .strip_prefix("/")
        .unwrap_or(path)
        .components()
        .try_fold(PathBuf::new(), |mut acc, component| match component {
            Component::Normal(segment) => {
                acc.push(segment);
                Ok(acc)
            }
            Component::CurDir => Ok(acc),
            Component::ParentDir => Err(Error::from_reason(
                "Archive entry uses an unsafe relative path",
            )),
            Component::Prefix(_) | Component::RootDir => Ok(acc),
        })?;

    if sanitized.as_os_str().is_empty() {
        return Err(Error::from_reason("Archive entry resolves to an empty path"));
    }

    Ok(sanitized)
}

fn should_skip_entry(name: &str) -> bool {
    let normalized = normalize_archive_entry_name(name);
    if normalized.is_empty() {
        return true;
    }

    let mut segments = normalized.split('/').filter(|segment| !segment.is_empty());
    let Some(top_level_name) = segments.next() else {
        return true;
    };

    segments.next().is_none() && is_ignored_top_level_entry(top_level_name)
}

fn extract_archive_entries(
    archive_path: &Path,
    temp_folder: &Path,
    callback: &Option<Arc<ThreadsafeFunction<ExtractProgress>>>,
    last_percent: &Arc<AtomicU8>,
) -> Result<()> {
    let source_file = File::open(archive_path)
        .map_err(|e| Error::from_reason(format!("Failed to open archive: {}", e)))?;
    let mut iterator = ArchiveIterator::from_read(source_file)
        .map_err(|e| Error::from_reason(format!("Failed to inspect archive: {}", e)))?;

    let mut file_entries = Vec::new();

    while let Some(content) = iterator.next_header() {
        match content {
            ArchiveContents::StartOfEntry(name, stat) => {
                if should_skip_entry(&name) {
                    continue;
                }

                let is_dir = is_directory_entry(&name) || is_directory_stat(&stat);
                if is_dir {
                    let relative_path = sanitize_entry_path(&name)?;
                    fs::create_dir_all(temp_folder.join(relative_path)).map_err(|e| {
                        Error::from_reason(format!(
                            "Failed to create extracted directory {}: {}",
                            name, e
                        ))
                    })?;
                } else {
                    file_entries.push(name);
                }
            }
            ArchiveContents::Err(e) => {
                return Err(Error::from_reason(format!(
                    "Failed to inspect archive entry: {}",
                    e
                )));
            }
            _ => {}
        }
    }

    if file_entries.is_empty() {
        return Ok(());
    }

    let total_files = file_entries.len() as u32;

    let source_file = File::open(archive_path)
        .map_err(|e| Error::from_reason(format!("Failed to reopen archive: {}", e)))?;
    let mut iterator = ArchiveIteratorBuilder::new(source_file)
        .filter(|name, stat| {
            !should_skip_entry(name) && !(is_directory_entry(name) || is_directory_stat(stat))
        })
        .build()
        .map_err(|e| Error::from_reason(format!("Failed to stream archive: {}", e)))?;
    let mut current_output: Option<File> = None;
    let mut current_entry_name: Option<String> = None;
    let mut extracted_files = 0u32;

    for content in &mut iterator {
        match content {
            ArchiveContents::StartOfEntry(name, _stat) => {
                let relative_path = sanitize_entry_path(&name)?;
                let target_path = temp_folder.join(&relative_path);

                if let Some(parent) = target_path.parent() {
                    fs::create_dir_all(parent).map_err(|e| {
                        Error::from_reason(format!(
                            "Failed to create extraction folder for {}: {}",
                            name, e
                        ))
                    })?;
                }

                current_output = Some(File::create(&target_path).map_err(|e| {
                    Error::from_reason(format!(
                        "Failed to create extracted file {}: {}",
                        target_path.display(),
                        e
                    ))
                })?);
                current_entry_name = Some(name);
            }
            ArchiveContents::DataChunk(data) => {
                if let Some(output_file) = current_output.as_mut() {
                    output_file.write_all(&data).map_err(|e| {
                        Error::from_reason(format!(
                            "Failed to write extracted file {}: {}",
                            current_entry_name.as_deref().unwrap_or("<unknown>"),
                            e
                        ))
                    })?;
                }
            }
            ArchiveContents::EndOfEntry => {
                if let Some(mut output_file) = current_output.take() {
                    output_file.flush().map_err(|e| {
                        Error::from_reason(format!(
                            "Failed to finalize extracted file {}: {}",
                            current_entry_name.as_deref().unwrap_or("<unknown>"),
                            e
                        ))
                    })?;

                    extracted_files += 1;
                    let raw_percent = ((extracted_files * 90) / total_files).clamp(1, 90) as u8;
                    let prev = last_percent.load(Ordering::Relaxed);
                    if raw_percent > prev
                        && last_percent
                            .compare_exchange(
                                prev,
                                raw_percent,
                                Ordering::Relaxed,
                                Ordering::Relaxed,
                            )
                            .is_ok()
                    {
                        emit_progress(
                            callback,
                            raw_percent as u32,
                            format!("Extracting... {}%", raw_percent),
                        );
                    }
                }
                current_entry_name = None;
            }
            ArchiveContents::Err(e) => {
                return Err(Error::from_reason(format!(
                    "Failed to extract archive entry {}: {}",
                    current_entry_name.as_deref().unwrap_or("<unknown>"),
                    e
                )));
            }
        }
    }

    Ok(())
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

        let mut temp_folder = TempExtractionDir::new(dest_path.join(format!(
            ".extract_temp_{}_{}",
            std::process::id(),
            timestamp
        )));

        fs::create_dir_all(temp_folder.path())
            .map_err(|e| Error::from_reason(format!("Failed to create temp folder: {}", e)))?;

        let last_percent = Arc::new(AtomicU8::new(0));

        emit_progress(&self.on_progress, 1, "Starting extraction");

        if let Err(e) = extract_archive_entries(
            archive_path,
            temp_folder.path(),
            &self.on_progress,
            &last_percent,
        ) {
            return Err(Error::from_reason(format!("Extraction failed: {}", e)));
        }

        if last_percent.load(Ordering::Relaxed) < 92 {
            last_percent.store(92, Ordering::Relaxed);
            emit_progress(&self.on_progress, 92, "Finalizing extracted files");
        }

        let mut current_path = temp_folder.path().to_path_buf();
        let mut target_folder_name = archive_path
            .file_stem()
            .and_then(|s| s.to_str())
            .unwrap_or("extracted")
            .to_string();

        loop {
            let entries: Vec<std::fs::DirEntry> = match fs::read_dir(&current_path) {
                Ok(dir) => dir.filter_map(|e| e.ok()).collect(),
                Err(e) => return Err(Error::from_reason(format!("Extraction failed: {}", e))),
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

        if current_path != temp_folder.path() {
            if let Err(e) = fs::rename(&current_path, &target_path) {
                return Err(Error::from_reason(format!(
                    "Failed to move extracted folder: {}",
                    e
                )));
            }
        } else {
            if let Err(e) = fs::rename(temp_folder.path(), &target_path) {
                return Err(Error::from_reason(format!(
                    "Failed to rename temp folder: {}",
                    e
                )));
            }

            temp_folder.disarm();
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
pub async fn has_single_top_level_directory(archive_path: String) -> Result<bool> {
    napi::tokio::task::spawn_blocking(move || -> Result<bool> {
        let source_file = File::open(&archive_path)
            .map_err(|e| Error::from_reason(format!("Failed to open archive: {}", e)))?;
        let mut iterator = ArchiveIterator::from_read(source_file)
            .map_err(|e| Error::from_reason(format!("Failed to inspect archive: {}", e)))?;

        let mut top_level_entries: HashMap<String, bool> = HashMap::new();

        while let Some(content) = iterator.next_header() {
            let ArchiveContents::StartOfEntry(name, stat) = content else {
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

            let is_dir = has_nested_segments || is_directory_entry(&name) || is_directory_stat(&stat);
            top_level_entries
                .entry(top_level_name.to_string())
                .and_modify(|existing| *existing = *existing || is_dir)
                .or_insert(is_dir);

            if top_level_entries.len() > 1 {
                return Ok(false);
            }
        }

        Ok(matches!(top_level_entries.into_values().next(), Some(true)))
    })
    .await
    .map_err(|e| Error::from_reason(format!("Failed to inspect archive: {}", e)))?
}
