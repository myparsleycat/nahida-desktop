use serde::{Deserialize, Serialize};

use crate::error::{TransformError, checked_add_usize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord, Serialize, Deserialize)]
pub struct AddressRange {
  pub start: u32,
  pub len: u32,
}

impl AddressRange {
  pub const fn new(start: u32, len: u32) -> Self {
    Self { start, len }
  }

  pub fn try_from_usize(
    start: usize,
    len: usize,
    context: &'static str,
  ) -> Result<Self, TransformError> {
    checked_add_usize(start, len, context)?;
    let start = u32::try_from(start).map_err(|_| TransformError::IntegerOverflow(context))?;
    let len = u32::try_from(len).map_err(|_| TransformError::IntegerOverflow(context))?;
    Ok(Self { start, len })
  }

  pub fn end(self) -> u32 {
    self.start.saturating_add(self.len)
  }

  pub fn end_u64(self) -> u64 {
    self.start as u64 + self.len as u64
  }

  pub fn contains(self, value: u32) -> bool {
    value as u64 >= self.start as u64 && (value as u64) < self.end_u64()
  }

  pub fn overlaps(self, other: Self) -> bool {
    (self.start as u64) < other.end_u64() && (other.start as u64) < self.end_u64()
  }
}
