use crate::analysis::decoder::CodeAnalysis;
use crate::analysis::padding::{PaddingByteKind, PaddingCandidate};
use crate::analysis::references::{range_contains_any, range_overlaps_any};
use crate::error::{TransformError, checked_add_u32};
use crate::options::TransformOptions;
use crate::pe::PeImage;
use crate::pe::address::AddressRange;
use crate::report::{CandidateReport, CandidateStatus, TransformReport};

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ApprovedCandidate {
  pub id: usize,
  pub section: String,
  pub rva: u32,
  pub file_offset: usize,
  pub length: usize,
}

pub fn evaluate_candidates(
  pe: &PeImage<'_>,
  code: &CodeAnalysis,
  candidates: &[PaddingCandidate],
  options: &TransformOptions,
  report: &mut TransformReport,
) -> Result<Vec<ApprovedCandidate>, TransformError> {
  let mut approved = Vec::new();
  let relocation_ranges: Vec<_> = pe.relocations.iter().map(|reloc| reloc.rva_range).collect();
  let runtime_ranges: Vec<_> = pe
    .runtime_functions
    .iter()
    .map(|function| AddressRange::new(function.begin, function.end - function.begin))
    .collect();

  for candidate in candidates {
    let mut reasons = Vec::new();
    let rva_len = u32::try_from(candidate.length)
      .map_err(|_| TransformError::IntegerOverflow("candidate length"))?;
    let rva_range = AddressRange::new(candidate.rva, rva_len);
    let file_range =
      AddressRange::try_from_usize(candidate.file_offset, candidate.length, "candidate")?;

    let section = pe.section_by_rva(candidate.rva);
    if !section.is_some_and(|section| section.is_executable()) {
      reasons.push("section is not executable".to_string());
    }

    match pe.rva_range_to_file_range(candidate.rva, rva_len) {
      Ok(mapped) if mapped == file_range => {}
      Ok(_) => reasons.push("candidate RVA and file range do not map exactly".to_string()),
      Err(error) => reasons.push(format!("ambiguous candidate RVA conversion: {error}")),
    }

    if code
      .instruction_ranges
      .iter()
      .any(|instruction| instruction.overlaps(rva_range))
    {
      reasons.push("overlaps a decoded reachable instruction".to_string());
    }

    if range_contains_any(rva_range, &code.branch_targets) {
      reasons.push("a direct branch targets the candidate or its interior".to_string());
    }

    if pe.headers.entry_point != 0 && rva_range.contains(pe.headers.entry_point) {
      reasons.push("overlaps the PE entry point".to_string());
    }

    if range_contains_any(rva_range, &pe.export_rvas) {
      reasons.push("overlaps an exported symbol RVA".to_string());
    }

    if range_overlaps_any(rva_range, &relocation_ranges) {
      reasons.push("overlaps a relocation target".to_string());
    }

    if range_overlaps_any(rva_range, &runtime_ranges) {
      reasons.push("overlaps a runtime-function range".to_string());
    }

    for protected in &pe.protected_ranges {
      if protected
        .rva_range
        .is_some_and(|protected_range| protected_range.overlaps(rva_range))
        || protected
          .file_range
          .is_some_and(|protected_range| protected_range.overlaps(file_range))
      {
        reasons.push(format!("overlaps protected {}", protected.reason));
      }
    }

    if range_contains_any(rva_range, &code.referenced_rvas) {
      reasons.push("a RIP-relative or data reference points at the candidate".to_string());
    }

    if candidate.byte_kind == PaddingByteKind::Zero {
      if !options.allow_zero_padding {
        reasons.push("zero padding is disabled".to_string());
      } else if !zero_padding_is_alignment_tail(pe, candidate)? {
        reasons.push("zero padding is not proven to be alignment tail padding".to_string());
      }
    }

    let status = if reasons.is_empty() {
      approved.push(ApprovedCandidate {
        id: candidate.id,
        section: candidate.section.clone(),
        rva: candidate.rva,
        file_offset: candidate.file_offset,
        length: candidate.length,
      });
      CandidateStatus::Approved
    } else {
      CandidateStatus::Rejected
    };

    report.add_candidate(CandidateReport {
      id: candidate.id,
      section: candidate.section.clone(),
      rva: candidate.rva,
      file_offset: candidate.file_offset,
      length: candidate.length,
      byte_kind: candidate.byte_kind,
      status,
      rejection_reasons: reasons,
      template: None,
    });
  }

  Ok(approved)
}

fn zero_padding_is_alignment_tail(
  pe: &PeImage<'_>,
  candidate: &PaddingCandidate,
) -> Result<bool, TransformError> {
  let Some(section) = pe.section_by_rva(candidate.rva) else {
    return Ok(false);
  };
  let candidate_end =
    checked_add_u32(candidate.rva, candidate.length as u32, "zero candidate end")?;
  let virtual_end = checked_add_u32(
    section.virtual_address,
    section.virtual_size,
    "section virtual end",
  )?;
  Ok(candidate.rva >= virtual_end || candidate_end > virtual_end)
}
