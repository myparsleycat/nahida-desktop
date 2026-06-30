use serde::{Deserialize, Serialize};

use super::directories::DataDirectory;

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PeHeaders {
  pub nt_offset: usize,
  pub coff_offset: usize,
  pub optional_offset: usize,
  pub section_table_offset: usize,
  pub machine: u16,
  pub number_of_sections: u16,
  pub size_of_optional_header: u16,
  pub characteristics: u16,
  pub entry_point: u32,
  pub image_base: u64,
  pub section_alignment: u32,
  pub file_alignment: u32,
  pub size_of_image: u32,
  pub size_of_headers: u32,
  pub checksum: u32,
  pub checksum_file_offset: usize,
  pub subsystem: u16,
  pub dll_characteristics: u16,
  pub number_of_rva_and_sizes: u32,
  pub data_directories: Vec<DataDirectory>,
  pub is_dll: bool,
}
