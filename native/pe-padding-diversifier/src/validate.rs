use serde::{Deserialize, Serialize};

use crate::analysis::safety::ApprovedCandidate;
use crate::error::{TransformError, checked_add_usize};
use crate::pe::PeImage;
use crate::pe::address::AddressRange;
use crate::pe::directories::{
  BASERELOC_DIRECTORY, DELAY_IMPORT_DIRECTORY, EXCEPTION_DIRECTORY, EXPORT_DIRECTORY,
  IAT_DIRECTORY, IMPORT_DIRECTORY, LOAD_CONFIG_DIRECTORY, RESOURCE_DIRECTORY, TLS_DIRECTORY,
};
use crate::transform::planner::Patch;

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ValidationSummary {
  pub reparsed_output: bool,
  pub file_length_unchanged: bool,
  pub section_layout_unchanged: bool,
  pub entry_point_unchanged: bool,
  pub data_directories_unchanged: bool,
  pub imports_exports_metadata_unchanged: bool,
  pub exception_metadata_unchanged: bool,
  pub relocations_unchanged: bool,
  pub only_planned_ranges_changed: bool,
  pub changed_ranges_inside_candidates: bool,
  pub deterministic: bool,
}

impl ValidationSummary {
  pub fn with_determinism(mut self, deterministic: bool) -> Self {
    self.deterministic = deterministic;
    self
  }
}

pub fn validate_transformation(
  original: &[u8],
  output: &[u8],
  original_pe: &PeImage<'_>,
  patches: &[Patch],
) -> Result<ValidationSummary, TransformError> {
  let output_pe = PeImage::parse(output)?;
  let mut summary = ValidationSummary {
    reparsed_output: true,
    file_length_unchanged: original.len() == output.len(),
    section_layout_unchanged: original_pe.sections == output_pe.sections,
    entry_point_unchanged: original_pe.headers.entry_point == output_pe.headers.entry_point,
    data_directories_unchanged: original_pe.headers.data_directories
      == output_pe.headers.data_directories,
    imports_exports_metadata_unchanged: true,
    exception_metadata_unchanged: true,
    relocations_unchanged: original_pe.relocations == output_pe.relocations,
    only_planned_ranges_changed: false,
    changed_ranges_inside_candidates: false,
    deterministic: true,
  };

  if !summary.file_length_unchanged {
    return Err(TransformError::Validation(
      "file length changed during transformation".to_string(),
    ));
  }
  if !summary.section_layout_unchanged {
    return Err(TransformError::Validation(
      "section layout changed during transformation".to_string(),
    ));
  }
  if original_pe.headers.number_of_sections != output_pe.headers.number_of_sections {
    return Err(TransformError::Validation(
      "section count changed during transformation".to_string(),
    ));
  }
  if !summary.entry_point_unchanged {
    return Err(TransformError::Validation(
      "entry point changed during transformation".to_string(),
    ));
  }
  if original_pe.headers.size_of_image != output_pe.headers.size_of_image {
    return Err(TransformError::Validation(
      "SizeOfImage changed during transformation".to_string(),
    ));
  }
  if !summary.data_directories_unchanged {
    return Err(TransformError::Validation(
      "data-directory RVAs or sizes changed during transformation".to_string(),
    ));
  }

  summary.imports_exports_metadata_unchanged = directories_equal(
    original,
    output,
    original_pe,
    &[
      IMPORT_DIRECTORY,
      IAT_DIRECTORY,
      DELAY_IMPORT_DIRECTORY,
      EXPORT_DIRECTORY,
      RESOURCE_DIRECTORY,
      LOAD_CONFIG_DIRECTORY,
      TLS_DIRECTORY,
    ],
  )?;
  if !summary.imports_exports_metadata_unchanged {
    return Err(TransformError::Validation(
      "import/export/resource/TLS/load-config metadata bytes changed".to_string(),
    ));
  }

  summary.exception_metadata_unchanged =
    directories_equal(original, output, original_pe, &[EXCEPTION_DIRECTORY])?;
  if !summary.exception_metadata_unchanged
    || original_pe.runtime_functions != output_pe.runtime_functions
  {
    return Err(TransformError::Validation(
      "exception directory or runtime-function entries changed".to_string(),
    ));
  }

  summary.relocations_unchanged =
    directories_equal(original, output, original_pe, &[BASERELOC_DIRECTORY])?
      && original_pe.relocations == output_pe.relocations;
  if !summary.relocations_unchanged {
    return Err(TransformError::Validation(
      "relocation entries changed".to_string(),
    ));
  }

  let changed = changed_ranges(original, output);
  summary.only_planned_ranges_changed = changed
    .iter()
    .all(|range| patches.iter().any(|patch| patch_covers(patch, *range)));
  if !summary.only_planned_ranges_changed {
    return Err(TransformError::Validation(
      "bytes changed outside the patch plan".to_string(),
    ));
  }

  for patch in patches {
    let end = checked_add_usize(
      patch.file_offset,
      patch.replacement.len(),
      "patch validation range",
    )?;
    if output.get(patch.file_offset..end) != Some(patch.replacement.as_slice()) {
      return Err(TransformError::Validation(format!(
        "patch bytes for candidate {} do not match the plan",
        patch.candidate_id
      )));
    }
  }

  summary.changed_ranges_inside_candidates = true;
  Ok(summary)
}

pub fn infer_patches_from_diff(
  original: &[u8],
  output: &[u8],
  approved_candidates: &[ApprovedCandidate],
) -> Result<Vec<Patch>, TransformError> {
  if original.len() != output.len() {
    return Err(TransformError::Validation(
      "files have different lengths".to_string(),
    ));
  }

  let mut patches = Vec::new();
  for range in changed_ranges(original, output) {
    let Some(candidate) = approved_candidates.iter().find(|candidate| {
      let candidate_range =
        AddressRange::new(candidate.file_offset as u32, candidate.length as u32);
      candidate_range.overlaps(range)
        && candidate_range.start <= range.start
        && range.end_u64() <= candidate_range.end_u64()
    }) else {
      return Err(TransformError::Validation(format!(
        "changed range 0x{:x}+0x{:x} is not inside an approved padding candidate",
        range.start, range.len
      )));
    };

    let start = range.start as usize;
    let end = range.end() as usize;
    patches.push(Patch {
      candidate_id: candidate.id,
      rva: candidate.rva + (start - candidate.file_offset) as u32,
      file_offset: start,
      replacement: output[start..end].to_vec(),
      template: "external_diff".to_string(),
    });
  }
  Ok(patches)
}

pub fn changed_ranges(a: &[u8], b: &[u8]) -> Vec<AddressRange> {
  if a.len() != b.len() {
    return Vec::new();
  }
  let mut ranges = Vec::new();
  let mut cursor = 0usize;
  while cursor < a.len() {
    if a[cursor] == b[cursor] {
      cursor += 1;
      continue;
    }
    let start = cursor;
    while cursor < a.len() && a[cursor] != b[cursor] {
      cursor += 1;
    }
    ranges.push(AddressRange::new(start as u32, (cursor - start) as u32));
  }
  ranges
}

fn patch_covers(patch: &Patch, range: AddressRange) -> bool {
  let patch_range = AddressRange::new(patch.file_offset as u32, patch.replacement.len() as u32);
  patch_range.start <= range.start && range.end_u64() <= patch_range.end_u64()
}

fn directories_equal(
  original: &[u8],
  output: &[u8],
  pe: &PeImage<'_>,
  indexes: &[usize],
) -> Result<bool, TransformError> {
  for index in indexes {
    let Some(range) = pe.directory_file_range(*index) else {
      continue;
    };
    let start = range.start as usize;
    let end = range.end() as usize;
    if original.get(start..end) != output.get(start..end) {
      return Ok(false);
    }
  }
  Ok(true)
}
