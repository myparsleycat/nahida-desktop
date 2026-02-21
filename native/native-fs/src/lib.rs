#![deny(clippy::all)]

use jwalk::WalkDir;
use napi_derive::napi;

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
