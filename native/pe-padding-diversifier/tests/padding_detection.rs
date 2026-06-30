mod fixtures;

use pe_diversifier::analysis::padding::PaddingByteKind;
use pe_diversifier::options::TransformOptions;
use pe_diversifier::report::CandidateStatus;
use pe_diversifier::{analyze_pe, transform_pe};

#[test]
fn finds_int3_padding_after_ret() {
  let input = fixtures::minimal_dll(fixtures::ret_then_int3_padding(24));
  let report = analyze_pe(&input, &TransformOptions::default()).unwrap();
  assert!(
    report
      .candidates
      .iter()
      .any(|candidate| candidate.status == CandidateStatus::Approved)
  );
}

#[test]
fn rejects_nop_bytes_embedded_inside_instruction() {
  let mut text = vec![0xb8, 0x90, 0x90, 0x90, 0x90, 0xc3];
  text.resize(0x200, 0xcc);
  let options = TransformOptions {
    minimum_sled_length: 4,
    ..TransformOptions::default()
  };
  let input = fixtures::minimal_dll(text);
  let report = analyze_pe(&input, &options).unwrap();
  let embedded = report
    .candidates
    .iter()
    .find(|candidate| candidate.rva == fixtures::TEXT_RVA + 1)
    .unwrap();
  assert_eq!(embedded.status, CandidateStatus::Rejected);
  assert!(
    embedded
      .rejection_reasons
      .iter()
      .any(|reason| reason.contains("decoded reachable instruction"))
  );
}

#[test]
fn zero_padding_requires_explicit_alignment_tail_proof() {
  let mut text = vec![0xc3];
  text.extend(std::iter::repeat_n(0, 16));
  text.resize(0x200, 0xcc);
  let input = fixtures::minimal_dll(text);
  let report = analyze_pe(&input, &TransformOptions::default()).unwrap();
  assert!(
    !report
      .candidates
      .iter()
      .any(|candidate| candidate.byte_kind == PaddingByteKind::Zero)
  );
}

#[test]
fn no_safe_candidate_produces_identical_output() {
  let input = fixtures::minimal_dll(fixtures::no_padding_text());
  let result = transform_pe(&input, &TransformOptions::default()).unwrap();
  assert_eq!(result.output, input);
  assert_eq!(result.report.modified_regions, 0);
}
