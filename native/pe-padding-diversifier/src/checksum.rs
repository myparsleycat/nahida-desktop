use sha2::{Digest, Sha256};

pub fn sha256_hex(data: &[u8]) -> String {
  let digest = Sha256::digest(data);
  let mut out = String::with_capacity(digest.len() * 2);
  for byte in digest {
    use std::fmt::Write;
    let _ = write!(&mut out, "{byte:02x}");
  }
  out
}

pub fn compute_pe_checksum(data: &[u8], checksum_offset: usize) -> u32 {
  let mut sum = 0u64;
  let mut index = 0usize;

  while index < data.len() {
    let skip_checksum_word = index == checksum_offset || index == checksum_offset + 2;
    let word = if index + 1 < data.len() {
      u16::from_le_bytes([data[index], data[index + 1]]) as u64
    } else {
      data[index] as u64
    };

    if !skip_checksum_word {
      sum += word;
      sum = (sum & 0xffff) + (sum >> 16);
    }

    index += 2;
  }

  sum = (sum & 0xffff) + (sum >> 16);
  sum += data.len() as u64;
  sum as u32
}
