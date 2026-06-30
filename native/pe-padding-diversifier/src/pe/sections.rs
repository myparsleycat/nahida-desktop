use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct SectionHeader {
  pub name: String,
  pub virtual_size: u32,
  pub virtual_address: u32,
  pub size_of_raw_data: u32,
  pub pointer_to_raw_data: u32,
  pub characteristics: u32,
}

impl SectionHeader {
  pub fn raw_end(&self) -> u32 {
    self
      .pointer_to_raw_data
      .saturating_add(self.size_of_raw_data)
  }

  pub fn virtual_span(&self) -> u32 {
    self.virtual_size.max(self.size_of_raw_data)
  }

  pub fn contains_rva(&self, rva: u32) -> bool {
    let start = self.virtual_address as u64;
    let end = start + self.virtual_span() as u64;
    rva as u64 >= start && (rva as u64) < end
  }

  pub fn rva_to_file_offset(&self, rva: u32) -> Option<usize> {
    if !self.contains_rva(rva) {
      return None;
    }
    let delta = rva.checked_sub(self.virtual_address)?;
    if delta >= self.size_of_raw_data {
      return None;
    }
    Some(self.pointer_to_raw_data as usize + delta as usize)
  }
}
