mod fixtures;

use pe_diversifier::error::TransformError;
use pe_diversifier::options::TransformOptions;
use pe_diversifier::{analyze_pe, transform_pe};

#[test]
fn parses_minimal_pe64_dll() {
  let input = fixtures::minimal_dll(fixtures::ret_then_int3_padding(32));
  let report = analyze_pe(&input, &TransformOptions::default()).unwrap();
  assert!(report.is_dll);
  assert_eq!(report.entry_point_rva, fixtures::TEXT_RVA);
  assert!(report.discovered_regions >= 1);
}

#[test]
fn rejects_non_pe_input() {
  let error = analyze_pe(b"not a pe", &TransformOptions::default()).unwrap_err();
  assert!(matches!(error, TransformError::InvalidPe(_)));
}

#[test]
fn rejects_truncated_pe_input() {
  let error = analyze_pe(&[0x4d, 0x5a], &TransformOptions::default()).unwrap_err();
  assert!(matches!(error, TransformError::InvalidPe(_)));
}

#[test]
fn rejects_32_bit_pe() {
  let input = fixtures::pe32_fixture(fixtures::ret_then_int3_padding(32));
  let error = analyze_pe(&input, &TransformOptions::default()).unwrap_err();
  assert!(matches!(error, TransformError::Unsupported(_)));
}

#[test]
fn rejects_exe_when_dll_only() {
  let input = fixtures::exe_fixture(fixtures::ret_then_int3_padding(32));
  let error = analyze_pe(&input, &TransformOptions::default()).unwrap_err();
  assert!(matches!(error, TransformError::Unsupported(_)));
}

#[test]
fn rejects_overlapping_sections() {
  let input = fixtures::overlapping_sections_fixture(fixtures::ret_then_int3_padding(32));
  let error = analyze_pe(&input, &TransformOptions::default()).unwrap_err();
  assert!(matches!(error, TransformError::InvalidPe(_)));
}

#[test]
fn rejects_signed_transform_by_default() {
  let input = fixtures::signed_fixture(fixtures::ret_then_int3_padding(32));
  let error = transform_pe(&input, &TransformOptions::default()).unwrap_err();
  assert!(matches!(error, TransformError::AuthenticodeSignature));
}
