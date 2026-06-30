#![allow(dead_code)]

use pe_diversifier::pe::directories::{
  BASERELOC_DIRECTORY, EXCEPTION_DIRECTORY, SECURITY_DIRECTORY,
};

pub const TEXT_RVA: u32 = 0x1000;
pub const TEXT_RAW: usize = 0x200;
pub const RDATA_RVA: u32 = 0x2000;
pub const RDATA_RAW: usize = 0x400;
pub const RELOC_RVA: u32 = 0x3000;
pub const RELOC_RAW: usize = 0x600;

const SECTION_ALIGNMENT: u32 = 0x1000;
const FILE_ALIGNMENT: u32 = 0x200;

#[derive(Clone)]
struct SectionSpec {
  name: &'static str,
  rva: u32,
  raw: usize,
  virtual_size: u32,
  raw_size: u32,
  characteristics: u32,
  data: Vec<u8>,
}

pub fn minimal_dll(text: Vec<u8>) -> Vec<u8> {
  build(vec![text_section(text)], [None; 16], true, 0x8664, 0x20b)
}

pub fn exe_fixture(text: Vec<u8>) -> Vec<u8> {
  build(vec![text_section(text)], [None; 16], false, 0x8664, 0x20b)
}

pub fn pe32_fixture(text: Vec<u8>) -> Vec<u8> {
  build(vec![text_section(text)], [None; 16], true, 0x014c, 0x10b)
}

pub fn signed_fixture(text: Vec<u8>) -> Vec<u8> {
  let mut directories = [None; 16];
  directories[SECURITY_DIRECTORY] = Some((0x800, 0x80));
  let mut bytes = build(vec![text_section(text)], directories, true, 0x8664, 0x20b);
  bytes.resize(0x880, 0);
  bytes
}

pub fn exception_fixture(text: Vec<u8>, begin: u32, end: u32) -> Vec<u8> {
  let mut rdata = vec![0u8; 0x200];
  rdata[0] = 1;
  rdata[1] = 0;
  rdata[2] = 0;
  rdata[3] = 0;

  let mut pdata = vec![0u8; 0x200];
  put_u32(&mut pdata, 0, begin);
  put_u32(&mut pdata, 4, end);
  put_u32(&mut pdata, 8, RDATA_RVA);

  let mut directories = [None; 16];
  directories[EXCEPTION_DIRECTORY] = Some((RDATA_RVA + 0x200, 12));

  let pdata_section = SectionSpec {
    name: ".pdata",
    rva: RDATA_RVA + 0x200,
    raw: RDATA_RAW + 0x200,
    virtual_size: 0x200,
    raw_size: 0x200,
    characteristics: 0x4000_0040,
    data: pdata,
  };

  build(
    vec![text_section(text), rdata_section(rdata), pdata_section],
    directories,
    true,
    0x8664,
    0x20b,
  )
}

pub fn relocation_fixture(text: Vec<u8>, target_rva: u32) -> Vec<u8> {
  let mut reloc = vec![0u8; 0x200];
  let page = target_rva & !0x0fff;
  let offset = target_rva & 0x0fff;
  put_u32(&mut reloc, 0, page);
  put_u32(&mut reloc, 4, 12);
  put_u16(&mut reloc, 8, 0xa000 | offset as u16);
  put_u16(&mut reloc, 10, 0);

  let mut directories = [None; 16];
  directories[BASERELOC_DIRECTORY] = Some((RELOC_RVA, 12));
  build(
    vec![text_section(text), reloc_section(reloc)],
    directories,
    true,
    0x8664,
    0x20b,
  )
}

pub fn overlapping_sections_fixture(text: Vec<u8>) -> Vec<u8> {
  let mut sections = vec![text_section(text), rdata_section(vec![0u8; 0x200])];
  sections[1].raw = TEXT_RAW + 0x100;
  build(sections, [None; 16], true, 0x8664, 0x20b)
}

pub fn no_padding_text() -> Vec<u8> {
  let mut text = vec![0xc3];
  while text.len() < 0x200 {
    let byte = 0x40 + (text.len() % 0x20) as u8;
    text.push(byte);
  }
  text
}

pub fn ret_then_int3_padding(len: usize) -> Vec<u8> {
  let mut text = vec![0xc3];
  text.extend(std::iter::repeat_n(0xcc, len));
  text.resize(0x200, 0xcc);
  text
}

pub fn ret_then_nop_padding(len: usize) -> Vec<u8> {
  let mut text = vec![0xc3];
  text.extend(std::iter::repeat_n(0x90, len));
  text.resize(0x200, 0xcc);
  text
}

fn text_section(mut data: Vec<u8>) -> SectionSpec {
  data.resize(0x200, 0xcc);
  SectionSpec {
    name: ".text",
    rva: TEXT_RVA,
    raw: TEXT_RAW,
    virtual_size: 0x200,
    raw_size: 0x200,
    characteristics: 0x6000_0020,
    data,
  }
}

fn rdata_section(mut data: Vec<u8>) -> SectionSpec {
  data.resize(0x200, 0);
  SectionSpec {
    name: ".rdata",
    rva: RDATA_RVA,
    raw: RDATA_RAW,
    virtual_size: 0x200,
    raw_size: 0x200,
    characteristics: 0x4000_0040,
    data,
  }
}

fn reloc_section(mut data: Vec<u8>) -> SectionSpec {
  data.resize(0x200, 0);
  SectionSpec {
    name: ".reloc",
    rva: RELOC_RVA,
    raw: RELOC_RAW,
    virtual_size: 0x200,
    raw_size: 0x200,
    characteristics: 0x4200_0040,
    data,
  }
}

fn build(
  sections: Vec<SectionSpec>,
  directories: [Option<(u32, u32)>; 16],
  dll: bool,
  machine: u16,
  optional_magic: u16,
) -> Vec<u8> {
  let optional_size = 0xf0usize;
  let nt = 0x80usize;
  let section_table = nt + 4 + 20 + optional_size;
  let headers_size = 0x200usize;
  assert!(section_table + sections.len() * 40 <= headers_size);

  let file_len = sections
    .iter()
    .map(|section| section.raw + section.raw_size as usize)
    .max()
    .unwrap_or(headers_size)
    .max(headers_size);
  let mut bytes = vec![0u8; file_len];

  put_u16(&mut bytes, 0, 0x5a4d);
  put_u32(&mut bytes, 0x3c, nt as u32);
  put_u32(&mut bytes, nt, 0x0000_4550);

  let coff = nt + 4;
  put_u16(&mut bytes, coff, machine);
  put_u16(&mut bytes, coff + 2, sections.len() as u16);
  put_u16(&mut bytes, coff + 16, optional_size as u16);
  let characteristics = 0x0002 | 0x0020 | if dll { 0x2000 } else { 0 };
  put_u16(&mut bytes, coff + 18, characteristics);

  let opt = coff + 20;
  put_u16(&mut bytes, opt, optional_magic);
  bytes[opt + 2] = 14;
  put_u32(&mut bytes, opt + 4, 0x200);
  put_u32(&mut bytes, opt + 8, 0x200);
  put_u32(&mut bytes, opt + 16, TEXT_RVA);
  put_u32(&mut bytes, opt + 20, TEXT_RVA);
  put_u64(&mut bytes, opt + 24, 0x0000_0001_8000_0000);
  put_u32(&mut bytes, opt + 32, SECTION_ALIGNMENT);
  put_u32(&mut bytes, opt + 36, FILE_ALIGNMENT);
  put_u16(&mut bytes, opt + 40, 6);
  put_u16(&mut bytes, opt + 48, 6);
  let image_end = sections
    .iter()
    .map(|section| align(section.rva + section.virtual_size, SECTION_ALIGNMENT))
    .max()
    .unwrap_or(TEXT_RVA + SECTION_ALIGNMENT);
  put_u32(&mut bytes, opt + 56, image_end);
  put_u32(&mut bytes, opt + 60, headers_size as u32);
  put_u16(&mut bytes, opt + 68, 3);
  put_u16(&mut bytes, opt + 70, 0x8160);
  put_u64(&mut bytes, opt + 72, 0x100000);
  put_u64(&mut bytes, opt + 80, 0x1000);
  put_u64(&mut bytes, opt + 88, 0x100000);
  put_u64(&mut bytes, opt + 96, 0x1000);
  put_u32(&mut bytes, opt + 108, 16);

  for (index, directory) in directories.into_iter().enumerate() {
    if let Some((rva, size)) = directory {
      put_u32(&mut bytes, opt + 112 + index * 8, rva);
      put_u32(&mut bytes, opt + 116 + index * 8, size);
    }
  }

  for (index, section) in sections.iter().enumerate() {
    let offset = section_table + index * 40;
    let mut name = [0u8; 8];
    for (i, byte) in section.name.as_bytes().iter().copied().take(8).enumerate() {
      name[i] = byte;
    }
    bytes[offset..offset + 8].copy_from_slice(&name);
    put_u32(&mut bytes, offset + 8, section.virtual_size);
    put_u32(&mut bytes, offset + 12, section.rva);
    put_u32(&mut bytes, offset + 16, section.raw_size);
    put_u32(&mut bytes, offset + 20, section.raw as u32);
    put_u32(&mut bytes, offset + 36, section.characteristics);
    bytes[section.raw..section.raw + section.raw_size as usize]
      .copy_from_slice(&section.data[..section.raw_size as usize]);
  }

  bytes
}

fn align(value: u32, alignment: u32) -> u32 {
  value.div_ceil(alignment) * alignment
}

fn put_u16(bytes: &mut [u8], offset: usize, value: u16) {
  bytes[offset..offset + 2].copy_from_slice(&value.to_le_bytes());
}

fn put_u32(bytes: &mut [u8], offset: usize, value: u32) {
  bytes[offset..offset + 4].copy_from_slice(&value.to_le_bytes());
}

fn put_u64(bytes: &mut [u8], offset: usize, value: u64) {
  bytes[offset..offset + 8].copy_from_slice(&value.to_le_bytes());
}
