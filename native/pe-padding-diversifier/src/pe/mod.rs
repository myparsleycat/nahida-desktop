pub mod address;
pub mod directories;
pub mod headers;
pub mod sections;

use std::collections::BTreeSet;

use crate::error::{TransformError, checked_add_u32, checked_add_usize};

use address::AddressRange;
use directories::{
  BASERELOC_DIRECTORY, DataDirectory, EXCEPTION_DIRECTORY, EXPORT_DIRECTORY, LOAD_CONFIG_DIRECTORY,
  SECURITY_DIRECTORY, TLS_DIRECTORY, directory_name,
};
use headers::PeHeaders;
use sections::SectionHeader;

const DOS_MAGIC: u16 = 0x5a4d;
const PE_SIGNATURE: u32 = 0x0000_4550;
const MACHINE_AMD64: u16 = 0x8664;
const OPTIONAL_MAGIC_PE32: u16 = 0x10b;
const OPTIONAL_MAGIC_PE32_PLUS: u16 = 0x20b;
const IMAGE_FILE_DLL: u16 = 0x2000;
const IMAGE_SCN_MEM_EXECUTE: u32 = 0x2000_0000;
const IMAGE_REL_BASED_ABSOLUTE: u16 = 0;
const IMAGE_REL_BASED_HIGHLOW: u16 = 3;
const IMAGE_REL_BASED_DIR64: u16 = 10;

#[derive(Debug, Clone)]
pub struct PeImage<'a> {
  pub data: &'a [u8],
  pub headers: PeHeaders,
  pub sections: Vec<SectionHeader>,
  pub protected_ranges: Vec<ProtectedRange>,
  pub relocations: Vec<RelocationTarget>,
  pub runtime_functions: Vec<RuntimeFunction>,
  pub export_rvas: Vec<u32>,
  pub relocation_code_roots: Vec<u32>,
  pub tls_callback_rvas: Vec<u32>,
  pub guard_cf_function_rvas: Vec<u32>,
  pub warnings: Vec<String>,
}

impl<'a> PeImage<'a> {
  pub fn parse(data: &'a [u8]) -> Result<Self, TransformError> {
    parse_with_goblin(data)?;
    let headers = parse_headers(data)?;
    let sections = parse_sections(data, &headers)?;
    validate_sections(data, &sections)?;

    let mut image = Self {
      data,
      headers,
      sections,
      protected_ranges: Vec::new(),
      relocations: Vec::new(),
      runtime_functions: Vec::new(),
      export_rvas: Vec::new(),
      relocation_code_roots: Vec::new(),
      tls_callback_rvas: Vec::new(),
      guard_cf_function_rvas: Vec::new(),
      warnings: Vec::new(),
    };

    image.extract_metadata()?;
    Ok(image)
  }

  pub fn has_certificate(&self) -> bool {
    self
      .headers
      .data_directories
      .get(SECURITY_DIRECTORY)
      .is_some_and(|directory| directory.present())
  }

  pub fn executable_sections(&self) -> impl Iterator<Item = &SectionHeader> {
    self
      .sections
      .iter()
      .filter(|section| section.is_executable())
  }

  pub fn section_by_rva(&self, rva: u32) -> Option<&SectionHeader> {
    self
      .sections
      .iter()
      .find(|section| section.contains_rva(rva))
  }

  pub fn section_by_file_range(&self, start: usize, len: usize) -> Option<&SectionHeader> {
    self.sections.iter().find(|section| {
      len != 0
        && start >= section.pointer_to_raw_data as usize
        && start
          .checked_add(len)
          .is_some_and(|end| end <= section.raw_end() as usize)
    })
  }

  pub fn is_executable_rva(&self, rva: u32) -> bool {
    self
      .section_by_rva(rva)
      .is_some_and(SectionHeader::is_executable)
  }

  pub fn rva_to_file_offset(&self, rva: u32) -> Result<usize, TransformError> {
    if rva < self.headers.size_of_headers {
      let offset = rva as usize;
      if offset < self.data.len() {
        return Ok(offset);
      }
    }

    let mut matches = self
      .sections
      .iter()
      .filter_map(|section| section.rva_to_file_offset(rva));
    let first = matches.next();
    if matches.next().is_some() {
      return Err(TransformError::AddressConversion(format!(
        "RVA 0x{rva:x} maps to more than one section"
      )));
    }
    first.ok_or_else(|| {
      TransformError::AddressConversion(format!("RVA 0x{rva:x} does not map to file data"))
    })
  }

  pub fn rva_range_to_file_range(
    &self,
    rva: u32,
    len: u32,
  ) -> Result<AddressRange, TransformError> {
    if len == 0 {
      return Ok(AddressRange::new(self.rva_to_file_offset(rva)? as u32, 0));
    }

    let end_minus_one = checked_add_u32(rva, len - 1, "RVA range end")?;
    let first = self.rva_to_file_offset(rva)?;
    let last = self.rva_to_file_offset(end_minus_one)?;
    let expected_last = checked_add_usize(first, (len - 1) as usize, "file range end")?;

    if last != expected_last {
      return Err(TransformError::AddressConversion(format!(
        "RVA range 0x{rva:x}+0x{len:x} is not contiguous in the file"
      )));
    }

    AddressRange::try_from_usize(first, len as usize, "file range")
  }

  pub fn directory_file_range(&self, index: usize) -> Option<AddressRange> {
    let directory = self.headers.data_directories.get(index)?;
    if !directory.present() {
      return None;
    }
    if index == SECURITY_DIRECTORY {
      return AddressRange::try_from_usize(
        directory.virtual_address as usize,
        directory.size as usize,
        "certificate directory",
      )
      .ok();
    }
    self
      .rva_range_to_file_range(directory.virtual_address, directory.size)
      .ok()
  }

  pub fn protected_file_ranges(&self) -> impl Iterator<Item = &ProtectedRange> {
    self
      .protected_ranges
      .iter()
      .filter(|range| range.file_range.is_some())
  }

  fn extract_metadata(&mut self) -> Result<(), TransformError> {
    self.protect_data_directories();
    self.parse_exports()?;
    self.parse_relocations()?;
    self.parse_exceptions()?;
    self.parse_tls()?;
    self.parse_load_config()?;
    Ok(())
  }

  fn protect_data_directories(&mut self) {
    let directories = self.headers.data_directories.clone();
    for (index, directory) in directories.iter().enumerate() {
      if !directory.present() {
        continue;
      }

      let reason = format!("{} data directory", directory_name(index));
      if index == SECURITY_DIRECTORY {
        match AddressRange::try_from_usize(
          directory.virtual_address as usize,
          directory.size as usize,
          "certificate table",
        ) {
          Ok(file_range) => self.protected_ranges.push(ProtectedRange {
            rva_range: None,
            file_range: Some(file_range),
            reason,
          }),
          Err(error) => self.warnings.push(error.to_string()),
        }
        continue;
      }

      let rva_range = Some(AddressRange::new(directory.virtual_address, directory.size));
      let file_range = self
        .rva_range_to_file_range(directory.virtual_address, directory.size)
        .ok();
      if file_range.is_none() {
        self.warnings.push(format!(
          "{} at RVA 0x{:x}+0x{:x} does not map to one contiguous file range",
          directory_name(index),
          directory.virtual_address,
          directory.size
        ));
      }
      self.protected_ranges.push(ProtectedRange {
        rva_range,
        file_range,
        reason,
      });
    }
  }

  fn parse_exports(&mut self) -> Result<(), TransformError> {
    let Some(range) = self.directory_file_range(EXPORT_DIRECTORY) else {
      return Ok(());
    };
    if range.len < 40 {
      return Ok(());
    }

    let base = range.start as usize;
    let number_of_functions = read_u32(self.data, base + 20)? as usize;
    let address_of_functions = read_u32(self.data, base + 28)?;
    let functions_offset = match self.rva_to_file_offset(address_of_functions) {
      Ok(offset) => offset,
      Err(error) => {
        self
          .warnings
          .push(format!("export address table skipped: {error}"));
        return Ok(());
      }
    };

    for index in 0..number_of_functions {
      let entry_offset = checked_add_usize(functions_offset, index * 4, "export table")?;
      if entry_offset + 4 > self.data.len() {
        break;
      }
      let function_rva = read_u32(self.data, entry_offset)?;
      if function_rva == 0 {
        continue;
      }
      if AddressRange::new(
        self.headers.data_directories[EXPORT_DIRECTORY].virtual_address,
        self.headers.data_directories[EXPORT_DIRECTORY].size,
      )
      .contains(function_rva)
      {
        continue;
      }
      self.export_rvas.push(function_rva);
    }
    self.export_rvas.sort_unstable();
    self.export_rvas.dedup();
    Ok(())
  }

  fn parse_relocations(&mut self) -> Result<(), TransformError> {
    let Some(range) = self.directory_file_range(BASERELOC_DIRECTORY) else {
      return Ok(());
    };

    let mut offset = range.start as usize;
    let end = range.end() as usize;
    while offset + 8 <= end {
      let page_rva = read_u32(self.data, offset)?;
      let block_size = read_u32(self.data, offset + 4)? as usize;
      if page_rva == 0 || block_size == 0 {
        break;
      }
      if block_size < 8 || offset + block_size > end {
        self
          .warnings
          .push("base relocation block has invalid size; remaining blocks skipped".to_string());
        break;
      }

      let entry_count = (block_size - 8) / 2;
      let entries_offset = offset + 8;
      for index in 0..entry_count {
        let raw = read_u16(self.data, entries_offset + index * 2)?;
        let relocation_type = raw >> 12;
        let relocation_offset = raw & 0x0fff;
        if relocation_type == IMAGE_REL_BASED_ABSOLUTE {
          continue;
        }

        let target_rva = checked_add_u32(page_rva, relocation_offset as u32, "relocation target")?;
        let width = match relocation_type {
          IMAGE_REL_BASED_DIR64 => 8,
          IMAGE_REL_BASED_HIGHLOW => 4,
          _ => 2,
        };

        let target_range = AddressRange::new(target_rva, width);
        self.relocations.push(RelocationTarget {
          rva_range: target_range,
          relocation_type,
        });
        self.add_protected_rva_range(target_range, "relocation target");

        if relocation_type == IMAGE_REL_BASED_DIR64
          && let Ok(file_offset) = self.rva_to_file_offset(target_rva)
          && file_offset + 8 <= self.data.len()
        {
          let value = read_u64(self.data, file_offset)?;
          if value >= self.headers.image_base {
            let code_rva = value - self.headers.image_base;
            if code_rva <= u32::MAX as u64 && self.is_executable_rva(code_rva as u32) {
              self.relocation_code_roots.push(code_rva as u32);
            }
          }
        }
      }

      offset += block_size;
    }

    self.relocation_code_roots.sort_unstable();
    self.relocation_code_roots.dedup();
    Ok(())
  }

  fn parse_exceptions(&mut self) -> Result<(), TransformError> {
    let Some(range) = self.directory_file_range(EXCEPTION_DIRECTORY) else {
      return Ok(());
    };

    let mut offset = range.start as usize;
    let end = range.end() as usize;
    while offset + 12 <= end {
      let begin = read_u32(self.data, offset)?;
      let end_rva = read_u32(self.data, offset + 4)?;
      let unwind_rva = read_u32(self.data, offset + 8)?;
      if begin != 0 && begin < end_rva {
        self.runtime_functions.push(RuntimeFunction {
          begin,
          end: end_rva,
          unwind_info_rva: unwind_rva,
        });
      }
      self.protect_unwind_info(unwind_rva);
      offset += 12;
    }

    self
      .runtime_functions
      .sort_unstable_by_key(|function| function.begin);
    Ok(())
  }

  fn parse_tls(&mut self) -> Result<(), TransformError> {
    let Some(range) = self.directory_file_range(TLS_DIRECTORY) else {
      return Ok(());
    };
    if range.len < 40 {
      return Ok(());
    }

    let base = range.start as usize;
    let start_raw_va = read_u64(self.data, base)?;
    let end_raw_va = read_u64(self.data, base + 8)?;
    let callbacks_va = read_u64(self.data, base + 24)?;

    if let (Some(start), Some(end)) = (self.va_to_rva(start_raw_va), self.va_to_rva(end_raw_va))
      && start < end
    {
      self.add_protected_rva_range(AddressRange::new(start, end - start), "TLS raw data");
    }

    let Some(callbacks_rva) = self.va_to_rva(callbacks_va) else {
      return Ok(());
    };
    let Ok(callbacks_offset) = self.rva_to_file_offset(callbacks_rva) else {
      return Ok(());
    };

    for index in 0..1024usize {
      let entry_offset = callbacks_offset + index * 8;
      if entry_offset + 8 > self.data.len() {
        break;
      }
      let callback_va = read_u64(self.data, entry_offset)?;
      self.add_protected_rva_range(
        AddressRange::new(callbacks_rva + (index as u32) * 8, 8),
        "TLS callback table",
      );
      if callback_va == 0 {
        break;
      }
      if let Some(callback_rva) = self.va_to_rva(callback_va)
        && self.is_executable_rva(callback_rva)
      {
        self.tls_callback_rvas.push(callback_rva);
      }
    }

    self.tls_callback_rvas.sort_unstable();
    self.tls_callback_rvas.dedup();
    Ok(())
  }

  fn parse_load_config(&mut self) -> Result<(), TransformError> {
    let Some(range) = self.directory_file_range(LOAD_CONFIG_DIRECTORY) else {
      return Ok(());
    };
    if range.len < 0x94 {
      return Ok(());
    }

    let base = range.start as usize;
    let load_config_size = read_u32(self.data, base)?;
    if load_config_size < 0x94 {
      return Ok(());
    }

    let se_handler_table = read_u64(self.data, base + 0x60)?;
    let se_handler_count = read_u64(self.data, base + 0x68)?;
    self.protect_va_table(
      se_handler_table,
      se_handler_count,
      4,
      "load-config SE handler table",
      false,
    )?;

    let guard_table = read_u64(self.data, base + 0x80)?;
    let guard_count = read_u64(self.data, base + 0x88)?;
    let guard_flags = read_u32(self.data, base + 0x90)?;
    let guard_extra = ((guard_flags >> 28) & 0x0f) as u64;
    let guard_entry_size = 4 + guard_extra;
    self.protect_va_table(
      guard_table,
      guard_count,
      guard_entry_size,
      "Guard CF function table",
      true,
    )?;
    Ok(())
  }

  fn protect_va_table(
    &mut self,
    table_va: u64,
    count: u64,
    entry_size: u64,
    reason: &'static str,
    entries_are_code_rvas: bool,
  ) -> Result<(), TransformError> {
    if table_va == 0 || count == 0 || entry_size == 0 {
      return Ok(());
    }
    let Some(table_rva) = self.va_to_rva(table_va) else {
      return Ok(());
    };
    let byte_len = count
      .checked_mul(entry_size)
      .and_then(|value| u32::try_from(value).ok())
      .ok_or(TransformError::IntegerOverflow("load-config table size"))?;
    self.add_protected_rva_range(AddressRange::new(table_rva, byte_len), reason);

    if entries_are_code_rvas {
      let Ok(file_range) = self.rva_range_to_file_range(table_rva, byte_len) else {
        return Ok(());
      };
      for index in 0..count.min(100_000) as usize {
        let offset = file_range.start as usize + index * entry_size as usize;
        if offset + 4 > self.data.len() {
          break;
        }
        let function_rva = read_u32(self.data, offset)?;
        if self.is_executable_rva(function_rva) {
          self.guard_cf_function_rvas.push(function_rva);
        }
      }
      self.guard_cf_function_rvas.sort_unstable();
      self.guard_cf_function_rvas.dedup();
    }

    Ok(())
  }

  fn protect_unwind_info(&mut self, unwind_rva: u32) {
    if unwind_rva == 0 {
      return;
    }
    let Ok(offset) = self.rva_to_file_offset(unwind_rva) else {
      self.warnings.push(format!(
        "unwind info RVA 0x{unwind_rva:x} did not map to file data"
      ));
      return;
    };
    if offset + 4 > self.data.len() {
      return;
    }

    let flags_and_version = self.data[offset];
    let flags = flags_and_version >> 3;
    let code_count = self.data[offset + 2] as usize;
    let code_bytes = code_count.saturating_mul(2);
    let mut size = 4usize.saturating_add(code_bytes);
    if !size.is_multiple_of(4) {
      size += 2;
    }

    if flags & 0x4 != 0 {
      size = size.saturating_add(12);
    } else if flags & 0x3 != 0 {
      size = size.saturating_add(4);
    }

    if offset + size <= self.data.len() {
      self.add_protected_rva_range(AddressRange::new(unwind_rva, size as u32), "unwind info");
    }
  }

  fn add_protected_rva_range(&mut self, rva_range: AddressRange, reason: &'static str) {
    let file_range = self
      .rva_range_to_file_range(rva_range.start, rva_range.len)
      .ok();
    self.protected_ranges.push(ProtectedRange {
      rva_range: Some(rva_range),
      file_range,
      reason: reason.to_string(),
    });
  }

  fn va_to_rva(&self, va: u64) -> Option<u32> {
    va.checked_sub(self.headers.image_base)
      .and_then(|rva| u32::try_from(rva).ok())
  }
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ProtectedRange {
  pub rva_range: Option<AddressRange>,
  pub file_range: Option<AddressRange>,
  pub reason: String,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RelocationTarget {
  pub rva_range: AddressRange,
  pub relocation_type: u16,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct RuntimeFunction {
  pub begin: u32,
  pub end: u32,
  pub unwind_info_rva: u32,
}

fn parse_with_goblin(data: &[u8]) -> Result<(), TransformError> {
  if let Ok(pe) = goblin::pe::PE::parse(data)
    && !pe.is_64
  {
    return Err(TransformError::Unsupported(
      "only PE32+ AMD64 images are supported".to_string(),
    ));
  }
  Ok(())
}

fn parse_headers(data: &[u8]) -> Result<PeHeaders, TransformError> {
  if data.len() < 0x40 {
    return Err(TransformError::InvalidPe(
      "file is too small for DOS header".to_string(),
    ));
  }
  if read_u16(data, 0)? != DOS_MAGIC {
    return Err(TransformError::InvalidPe("missing MZ header".to_string()));
  }

  let e_lfanew = read_i32(data, 0x3c)?;
  if e_lfanew < 0 {
    return Err(TransformError::InvalidPe(
      "negative e_lfanew is invalid".to_string(),
    ));
  }
  let nt_offset = e_lfanew as usize;
  if read_u32(data, nt_offset)? != PE_SIGNATURE {
    return Err(TransformError::InvalidPe(
      "missing PE signature".to_string(),
    ));
  }

  let coff_offset = nt_offset + 4;
  let machine = read_u16(data, coff_offset)?;
  if machine != MACHINE_AMD64 {
    return Err(TransformError::Unsupported(format!(
      "machine 0x{machine:04x} is not AMD64"
    )));
  }
  let number_of_sections = read_u16(data, coff_offset + 2)?;
  let size_of_optional_header = read_u16(data, coff_offset + 16)?;
  let characteristics = read_u16(data, coff_offset + 18)?;
  let optional_offset = coff_offset + 20;
  let optional_end = checked_add_usize(
    optional_offset,
    size_of_optional_header as usize,
    "optional header",
  )?;
  if optional_end > data.len() {
    return Err(TransformError::InvalidPe(
      "optional header extends past end of file".to_string(),
    ));
  }

  let magic = read_u16(data, optional_offset)?;
  if magic == OPTIONAL_MAGIC_PE32 {
    return Err(TransformError::Unsupported(
      "32-bit PE images are not supported".to_string(),
    ));
  }
  if magic != OPTIONAL_MAGIC_PE32_PLUS {
    return Err(TransformError::InvalidPe(format!(
      "unknown optional header magic 0x{magic:04x}"
    )));
  }

  let entry_point = read_u32(data, optional_offset + 16)?;
  let image_base = read_u64(data, optional_offset + 24)?;
  let section_alignment = read_u32(data, optional_offset + 32)?;
  let file_alignment = read_u32(data, optional_offset + 36)?;
  let size_of_image = read_u32(data, optional_offset + 56)?;
  let size_of_headers = read_u32(data, optional_offset + 60)?;
  let checksum_file_offset = optional_offset + 64;
  let checksum = read_u32(data, checksum_file_offset)?;
  let subsystem = read_u16(data, optional_offset + 68)?;
  let dll_characteristics = read_u16(data, optional_offset + 70)?;
  let number_of_rva_and_sizes = read_u32(data, optional_offset + 108)?;
  let directories_offset = optional_offset + 112;

  let available_directories = ((optional_end.saturating_sub(directories_offset)) / 8).min(16);
  let declared_directories = number_of_rva_and_sizes.min(16) as usize;
  let directory_count = available_directories.min(declared_directories);
  let mut data_directories = vec![DataDirectory::default(); 16];
  for (index, directory) in data_directories
    .iter_mut()
    .enumerate()
    .take(directory_count)
  {
    let offset = directories_offset + index * 8;
    *directory = DataDirectory {
      virtual_address: read_u32(data, offset)?,
      size: read_u32(data, offset + 4)?,
    };
  }

  Ok(PeHeaders {
    nt_offset,
    coff_offset,
    optional_offset,
    section_table_offset: optional_end,
    machine,
    number_of_sections,
    size_of_optional_header,
    characteristics,
    entry_point,
    image_base,
    section_alignment,
    file_alignment,
    size_of_image,
    size_of_headers,
    checksum,
    checksum_file_offset,
    subsystem,
    dll_characteristics,
    number_of_rva_and_sizes,
    data_directories,
    is_dll: characteristics & IMAGE_FILE_DLL != 0,
  })
}

fn parse_sections(data: &[u8], headers: &PeHeaders) -> Result<Vec<SectionHeader>, TransformError> {
  let mut sections = Vec::with_capacity(headers.number_of_sections as usize);
  for index in 0..headers.number_of_sections as usize {
    let offset = checked_add_usize(headers.section_table_offset, index * 40, "section table")?;
    if offset + 40 > data.len() {
      return Err(TransformError::InvalidPe(
        "section table extends past end of file".to_string(),
      ));
    }
    sections.push(SectionHeader {
      name: section_name(&data[offset..offset + 8]),
      virtual_size: read_u32(data, offset + 8)?,
      virtual_address: read_u32(data, offset + 12)?,
      size_of_raw_data: read_u32(data, offset + 16)?,
      pointer_to_raw_data: read_u32(data, offset + 20)?,
      characteristics: read_u32(data, offset + 36)?,
    });
  }
  Ok(sections)
}

fn validate_sections(data: &[u8], sections: &[SectionHeader]) -> Result<(), TransformError> {
  let mut raw_ranges = Vec::new();
  let mut seen_rvas = BTreeSet::new();

  for section in sections {
    if !seen_rvas.insert(section.virtual_address) {
      return Err(TransformError::InvalidPe(format!(
        "duplicate section RVA 0x{:x}",
        section.virtual_address
      )));
    }

    if section.size_of_raw_data == 0 {
      continue;
    }
    let start = section.pointer_to_raw_data as usize;
    let end = checked_add_usize(start, section.size_of_raw_data as usize, "section raw end")?;
    if end > data.len() {
      return Err(TransformError::InvalidPe(format!(
        "section {} raw range extends past end of file",
        section.name
      )));
    }
    raw_ranges.push((start, end, section.name.clone()));
  }

  raw_ranges.sort_unstable_by_key(|(start, _, _)| *start);
  for pair in raw_ranges.windows(2) {
    let (_, first_end, first_name) = &pair[0];
    let (second_start, _, second_name) = &pair[1];
    if second_start < first_end {
      return Err(TransformError::InvalidPe(format!(
        "sections {first_name} and {second_name} have overlapping raw ranges"
      )));
    }
  }

  Ok(())
}

fn section_name(bytes: &[u8]) -> String {
  let end = bytes
    .iter()
    .position(|byte| *byte == 0)
    .unwrap_or(bytes.len());
  String::from_utf8_lossy(&bytes[..end]).to_string()
}

pub(crate) fn read_u16(data: &[u8], offset: usize) -> Result<u16, TransformError> {
  let bytes = data.get(offset..offset + 2).ok_or_else(|| {
    TransformError::InvalidPe(format!("read past end of file at offset 0x{offset:x}"))
  })?;
  Ok(u16::from_le_bytes([bytes[0], bytes[1]]))
}

pub(crate) fn read_i32(data: &[u8], offset: usize) -> Result<i32, TransformError> {
  let bytes = data.get(offset..offset + 4).ok_or_else(|| {
    TransformError::InvalidPe(format!("read past end of file at offset 0x{offset:x}"))
  })?;
  Ok(i32::from_le_bytes([bytes[0], bytes[1], bytes[2], bytes[3]]))
}

pub(crate) fn read_u32(data: &[u8], offset: usize) -> Result<u32, TransformError> {
  let bytes = data.get(offset..offset + 4).ok_or_else(|| {
    TransformError::InvalidPe(format!("read past end of file at offset 0x{offset:x}"))
  })?;
  Ok(u32::from_le_bytes([bytes[0], bytes[1], bytes[2], bytes[3]]))
}

pub(crate) fn read_u64(data: &[u8], offset: usize) -> Result<u64, TransformError> {
  let bytes = data.get(offset..offset + 8).ok_or_else(|| {
    TransformError::InvalidPe(format!("read past end of file at offset 0x{offset:x}"))
  })?;
  Ok(u64::from_le_bytes([
    bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7],
  ]))
}

impl SectionHeader {
  pub fn is_executable(&self) -> bool {
    self.characteristics & IMAGE_SCN_MEM_EXECUTE != 0
  }
}
