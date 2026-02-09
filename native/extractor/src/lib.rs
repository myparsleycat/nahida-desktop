#![deny(clippy::all)]

use compress_tools::{uncompress_archive, Ownership};
use std::fs::File;
use std::path::Path;

use napi::bindgen_prelude::*;
use napi_derive::napi;

#[derive(Clone)]
pub struct ExtractorTask {
  pub archive: String,
  pub destination: String,
}

#[napi]
impl Task for ExtractorTask {
  type Output = ();
  type JsValue = ();

  fn compute(&mut self) -> Result<Self::Output> {
    let mut source = File::open(&self.archive)
      .map_err(|e| Error::from_reason(format!("Failed to open archive file: {}", e)))?;

    let dest_path = Path::new(&self.destination);

    uncompress_archive(&mut source, dest_path, Ownership::Ignore)
      .map_err(|e| Error::from_reason(format!("Failed to extract archive: {}", e)))?;

    Ok(())
  }

  fn resolve(&mut self, _env: Env, _output: Self::Output) -> Result<Self::JsValue> {
    Ok(())
  }
}

#[napi]
pub fn extract_archive(archive_path: String, destination_path: String) -> AsyncTask<ExtractorTask> {
  AsyncTask::new(ExtractorTask {
    archive: archive_path,
    destination: destination_path,
  })
}
