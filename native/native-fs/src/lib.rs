#![deny(clippy::all)]

use jwalk::WalkDir;
use napi_derive::napi;
use notify::{Config, Event, RecommendedWatcher, RecursiveMode, Watcher};
use napi::threadsafe_function::{ThreadsafeFunction, ThreadsafeFunctionCallMode};

#[napi(object)]
pub struct RawFileComponent {
  pub path: String,
  pub name: String,
  pub size: f64,
  pub parent_path: String,
  pub full_path: String,
}

#[napi(object)]
pub struct RawDirectoryComponent {
  pub path: String,
  pub name: String,
  pub parent_path: String,
}

#[napi(object)]
pub struct RawCollectionResult {
  pub files: Vec<RawFileComponent>,
  pub directories: Vec<RawDirectoryComponent>,
}

#[napi]
pub fn collect_files(
  paths: Vec<String>,
  allowed_ext: Vec<String>,
) -> napi::Result<RawCollectionResult> {
  let mut ext_list = Vec::new();

  for ext in allowed_ext {
    let fixed_ext = if ext.starts_with('.') {
      ext.to_lowercase()
    } else {
      format!(".{}", ext).to_lowercase()
    };
    ext_list.push(fixed_ext);
  }

  let mut all_files = Vec::new();
  let mut all_directories = Vec::new();

  for p in paths {
    let absolute_path = match std::path::Path::new(&p).canonicalize() {
      Ok(p) => p,
      Err(_) => continue,
    };

    let absolute_path_str = absolute_path.to_string_lossy().to_string();
    let absolute_path_str = absolute_path_str.trim_start_matches(r#"\\?\"#).to_string();
    let absolute_path = std::path::PathBuf::from(absolute_path_str);

    let parent_dir = absolute_path
      .parent()
      .unwrap_or(&absolute_path)
      .to_path_buf();
    let root_name = absolute_path
      .file_name()
      .and_then(|n| n.to_str())
      .unwrap_or("")
      .to_string();

    all_directories.push(RawDirectoryComponent {
      path: root_name.replace('\\', "/"),
      name: root_name.clone(),
      parent_path: "".to_string(),
    });

    let allowed_ext = ext_list.clone();

    let parent_dir_str = parent_dir.to_string_lossy().replace('\\', "/");

    let entries: Vec<_> = WalkDir::new(&absolute_path)
      .skip_hidden(true)
      .process_read_dir(|_depth, _path, _state, children| {
        children.retain(|child| {
          child
            .as_ref()
            .map(|c| !c.file_name().to_string_lossy().starts_with('.'))
            .unwrap_or(false)
        });
      })
      .into_iter()
      .filter_map(|e| e.ok())
      .filter(|e| e.path() != absolute_path)
      .collect();

    for entry in entries {
      let full_path = entry.path().to_string_lossy().replace('\\', "/");
      let relative_path = full_path
        .strip_prefix(&format!("{}/", parent_dir_str))
        .unwrap_or(&full_path)
        .to_string();
      let name = entry.file_name().to_string_lossy().to_string();

      let mut parent_path = std::path::Path::new(&relative_path)
        .parent()
        .map(|p| p.to_string_lossy().replace('\\', "/"))
        .unwrap_or_else(|| "".to_string());

      if parent_path == "." {
        parent_path = "".to_string();
      }

      if entry.file_type().is_dir() {
        all_directories.push(RawDirectoryComponent {
          path: relative_path,
          name,
          parent_path,
        });
      } else {
        let name_lower = name.to_lowercase();
        let is_allowed = allowed_ext.is_empty()
          || allowed_ext
            .iter()
            .any(|ext| name_lower.ends_with(ext.as_str()));
        if is_allowed {
          all_files.push(RawFileComponent {
            path: relative_path,
            name,
            size: entry.metadata().map(|m| m.len() as f64).unwrap_or(0.0),
            parent_path,
            full_path,
          });
        }
      }
    }
  }

  Ok(RawCollectionResult {
    files: all_files,
    directories: all_directories,
  })
}

#[napi]
pub fn find_files(
  paths: Vec<String>,
  include_ext: Vec<String>,
  exclude_file_names: Vec<String>,
) -> napi::Result<Vec<String>> {
  let normalized_ext: Vec<String> = include_ext
    .into_iter()
    .map(|ext| {
      if ext.starts_with('.') {
        ext.to_lowercase()
      } else {
        format!(".{}", ext).to_lowercase()
      }
    })
    .collect();

  let excluded_name_set: std::collections::HashSet<String> = exclude_file_names
    .into_iter()
    .map(|name| name.to_lowercase())
    .collect();

  let mut all_files: Vec<String> = Vec::new();

  for p in paths {
    let absolute_path = match std::path::Path::new(&p).canonicalize() {
      Ok(p) => p,
      Err(_) => continue,
    };

    let absolute_path_str = absolute_path.to_string_lossy().to_string();
    let absolute_path_str = absolute_path_str.trim_start_matches(r#"\\?\"#).to_string();
    let absolute_path = std::path::PathBuf::from(absolute_path_str);

    let entries = WalkDir::new(&absolute_path)
      .skip_hidden(true)
      .process_read_dir(|_depth, _path, _state, children| {
        children.retain(|child| {
          child
            .as_ref()
            .map(|c| !c.file_name().to_string_lossy().starts_with('.'))
            .unwrap_or(false)
        });
      })
      .into_iter()
      .filter_map(|e| e.ok());

    for entry in entries {
      if !entry.file_type().is_file() {
        continue;
      }

      let name = entry.file_name().to_string_lossy().to_string();
      let name_lower = name.to_lowercase();

      if excluded_name_set.contains(&name_lower) {
        continue;
      }

      let is_allowed =
        normalized_ext.is_empty() || normalized_ext.iter().any(|ext| name_lower.ends_with(ext));
      if !is_allowed {
        continue;
      }

      all_files.push(entry.path().to_string_lossy().replace('\\', "/"));
    }
  }

  Ok(all_files)
}

#[napi(object)]
pub struct WatchEvent {
  #[napi(ts_type = "\"create\" | \"modify\" | \"remove\"")]
  pub event_name: String,
  pub path: String,
}

#[napi(object)]
pub struct NativeWatcherOptions {
  pub poll_interval_ms: Option<u32>,
  pub compare_contents: Option<bool>,
}

#[napi]
pub struct NativeWatcher {
  watcher: Option<RecommendedWatcher>,
}

#[napi]
impl NativeWatcher {
  #[napi(constructor)]
  pub fn new() -> napi::Result<Self> {
    Ok(NativeWatcher { watcher: None })
  }

  #[napi]
  pub fn watch(
    &mut self,
    paths: Vec<String>,
    depth: i32,
    options: Option<NativeWatcherOptions>,
    callback: ThreadsafeFunction<WatchEvent>,
  ) -> napi::Result<()> {
    
    let mut config = Config::default();
    if let Some(opts) = options {
      if let Some(interval) = opts.poll_interval_ms {
        config = config.with_poll_interval(std::time::Duration::from_millis(interval as u64));
      }
      if let Some(compare) = opts.compare_contents {
        config = config.with_compare_contents(compare);
      }
    }

    let mut watcher = notify::RecommendedWatcher::new(
      move |res: notify::Result<Event>| match res {
        Ok(event) => {
          let event_name = match event.kind {
            notify::EventKind::Create(_) => "create",
            notify::EventKind::Modify(_) => "modify",
            notify::EventKind::Remove(_) => "remove",
            _ => return,
          };

          for path_buf in event.paths {
            let path_str = path_buf.to_string_lossy().replace('\\', "/");
            let tsfn_event = WatchEvent {
              event_name: event_name.to_string(),
              path: path_str,
            };
            callback.call(Ok(tsfn_event), ThreadsafeFunctionCallMode::NonBlocking);
          }
        }
        Err(e) => {
          callback.call(Err(napi::Error::new(napi::Status::GenericFailure, e.to_string())), ThreadsafeFunctionCallMode::NonBlocking);
        }
      },
      config,
    )
    .map_err(|e| napi::Error::new(napi::Status::GenericFailure, e.to_string()))?;

    for p in paths {
      let path = std::path::Path::new(&p);
      if !path.exists() {
        continue;
      }

      if depth < 0 {
        watcher.watch(path, RecursiveMode::Recursive).map_err(|e| napi::Error::new(napi::Status::GenericFailure, e.to_string()))?;
      } else if depth == 0 {
        watcher.watch(path, RecursiveMode::NonRecursive).map_err(|e| napi::Error::new(napi::Status::GenericFailure, e.to_string()))?;
      } else {
        for entry in WalkDir::new(path).max_depth(depth as usize).into_iter().filter_map(|e| e.ok()) {
          if entry.file_type().is_dir() {
            watcher.watch(&entry.path(), RecursiveMode::NonRecursive).map_err(|e| napi::Error::new(napi::Status::GenericFailure, e.to_string()))?;
          }
        }
      }
    }

    self.watcher = Some(watcher);
    Ok(())
  }

  #[napi]
  pub fn unwatch(&mut self) {
    self.watcher = None;
  }
}
