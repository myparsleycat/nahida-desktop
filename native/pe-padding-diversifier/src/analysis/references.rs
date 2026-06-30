use crate::pe::address::AddressRange;

pub fn range_contains_any(range: AddressRange, values: &[u32]) -> bool {
  values.iter().any(|value| range.contains(*value))
}

pub fn range_overlaps_any(range: AddressRange, ranges: &[AddressRange]) -> bool {
  ranges.iter().any(|other| range.overlaps(*other))
}
