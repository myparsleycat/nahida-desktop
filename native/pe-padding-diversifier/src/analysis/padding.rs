use serde::{Deserialize, Serialize};

use crate::error::{TransformError, checked_add_usize};
use crate::options::TransformOptions;
use crate::pe::PeImage;

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum PaddingByteKind {
  Nop,
  Int3,
  Zero,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct PaddingCandidate {
  pub id: usize,
  pub section: String,
  pub rva: u32,
  pub file_offset: usize,
  pub length: usize,
  pub byte_kind: PaddingByteKind,
}

pub fn discover_padding(
  pe: &PeImage<'_>,
  data: &[u8],
  options: &TransformOptions,
) -> Result<Vec<PaddingCandidate>, TransformError> {
  let mut candidates = Vec::new();
  let minimum = options.minimum_sled_length.max(1);

  for section in pe.executable_sections() {
    let start = section.pointer_to_raw_data as usize;
    let end = checked_add_usize(start, section.size_of_raw_data as usize, "section raw")?;
    if end > data.len() {
      continue;
    }

    let mut cursor = start;
    while cursor < end {
      let Some(kind) = padding_kind(data[cursor], options.allow_zero_padding) else {
        cursor += 1;
        continue;
      };
      let mut run_end = cursor + 1;
      while run_end < end && data[run_end] == data[cursor] {
        run_end += 1;
      }
      let length = run_end - cursor;
      if length >= minimum {
        let delta = cursor - start;
        candidates.push(PaddingCandidate {
          id: candidates.len(),
          section: section.name.clone(),
          rva: section.virtual_address + delta as u32,
          file_offset: cursor,
          length,
          byte_kind: kind,
        });
      }
      cursor = run_end;
    }
  }

  Ok(candidates)
}

fn padding_kind(byte: u8, allow_zero: bool) -> Option<PaddingByteKind> {
  match byte {
    0x90 => Some(PaddingByteKind::Nop),
    0xcc => Some(PaddingByteKind::Int3),
    0x00 if allow_zero => Some(PaddingByteKind::Zero),
    _ => None,
  }
}
