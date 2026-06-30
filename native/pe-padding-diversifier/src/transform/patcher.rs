use crate::error::{TransformError, checked_add_usize};

use super::planner::Patch;

pub fn apply_patches(input: &[u8], patches: &[Patch]) -> Result<Vec<u8>, TransformError> {
  let mut output = input.to_vec();

  for patch in patches {
    let end = checked_add_usize(
      patch.file_offset,
      patch.replacement.len(),
      "patch file range",
    )?;
    if end > output.len() {
      return Err(TransformError::Validation(format!(
        "patch for candidate {} extends past end of file",
        patch.candidate_id
      )));
    }
    output[patch.file_offset..end].copy_from_slice(&patch.replacement);
  }

  Ok(output)
}
