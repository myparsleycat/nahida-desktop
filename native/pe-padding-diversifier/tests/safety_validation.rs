mod fixtures;

use pe_diversifier::options::TransformOptions;
use pe_diversifier::report::CandidateStatus;
use pe_diversifier::{analyze_pe, transform_pe};

#[test]
fn rejects_direct_branch_target_entering_candidate() {
  let mut text = vec![0xe9, 0x0b, 0x00, 0x00, 0x00];
  text.extend(std::iter::repeat_n(0x90, 11));
  text.extend(std::iter::repeat_n(0xcc, 32));
  text.resize(0x200, 0xcc);
  let input = fixtures::minimal_dll(text);
  let report = analyze_pe(&input, &TransformOptions::default()).unwrap();
  let target = report
    .candidates
    .iter()
    .find(|candidate| candidate.rva == fixtures::TEXT_RVA + 0x10)
    .unwrap();
  assert_eq!(target.status, CandidateStatus::Rejected);
  assert!(
    target
      .rejection_reasons
      .iter()
      .any(|reason| reason.contains("direct branch"))
  );
}

#[test]
fn rejects_exception_runtime_function_overlap() {
  let input = fixtures::exception_fixture(
    fixtures::ret_then_int3_padding(32),
    fixtures::TEXT_RVA,
    fixtures::TEXT_RVA + 0x40,
  );
  let report = analyze_pe(&input, &TransformOptions::default()).unwrap();
  assert!(report.candidates.iter().any(|candidate| {
    candidate.status == CandidateStatus::Rejected
      && candidate
        .rejection_reasons
        .iter()
        .any(|reason| reason.contains("runtime-function"))
  }));
}

#[test]
fn rejects_relocation_target_overlap() {
  let input =
    fixtures::relocation_fixture(fixtures::ret_then_int3_padding(32), fixtures::TEXT_RVA + 8);
  let report = analyze_pe(&input, &TransformOptions::default()).unwrap();
  assert!(report.candidates.iter().any(|candidate| {
    candidate.status == CandidateStatus::Rejected
      && candidate
        .rejection_reasons
        .iter()
        .any(|reason| reason.contains("relocation target"))
  }));
}

#[test]
fn transformation_preserves_layout_and_only_changes_planned_ranges() {
  let input = fixtures::minimal_dll(fixtures::ret_then_int3_padding(48));
  let options = TransformOptions {
    seed: 7,
    ..TransformOptions::default()
  };
  let result = transform_pe(&input, &options).unwrap();
  assert_eq!(result.output.len(), input.len());
  assert!(result.report.validation.reparsed_output);
  assert!(result.report.validation.section_layout_unchanged);
  assert!(result.report.validation.only_planned_ranges_changed);
  assert!(result.report.modified_regions > 0);
}
