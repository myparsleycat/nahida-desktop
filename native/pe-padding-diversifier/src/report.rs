use serde::{Deserialize, Serialize};

use crate::analysis::padding::PaddingByteKind;
use crate::checksum::{compute_pe_checksum, sha256_hex};
use crate::options::TransformOptions;
use crate::pe::PeImage;
use crate::pe::address::AddressRange;
use crate::transform::planner::Patch;
use crate::validate::ValidationSummary;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransformResult {
  pub output: Vec<u8>,
  pub report: TransformReport,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransformReport {
  pub program_version: String,
  pub input_sha256: String,
  pub output_sha256: Option<String>,
  pub file_length: usize,
  pub image_base: u64,
  pub entry_point_rva: u32,
  pub is_dll: bool,
  pub has_authenticode_certificate: bool,
  pub pe_checksum_header: u32,
  pub pe_checksum_computed: u32,
  pub options: TransformOptions,
  pub discovered_regions: usize,
  pub rejected_regions: usize,
  pub approved_regions: usize,
  pub modified_regions: usize,
  pub candidates: Vec<CandidateReport>,
  pub patches: Vec<PatchRecord>,
  pub warnings: Vec<String>,
  pub decode_errors: Vec<String>,
  pub validation: ValidationSummary,
  pub reference_concepts: Vec<String>,
}

impl TransformReport {
  pub fn new(input: &[u8], pe: &PeImage<'_>, options: &TransformOptions) -> Self {
    let pe_checksum_computed = compute_pe_checksum(input, pe.headers.checksum_file_offset);
    Self {
      program_version: env!("CARGO_PKG_VERSION").to_string(),
      input_sha256: sha256_hex(input),
      output_sha256: None,
      file_length: input.len(),
      image_base: pe.headers.image_base,
      entry_point_rva: pe.headers.entry_point,
      is_dll: pe.headers.is_dll,
      has_authenticode_certificate: pe.has_certificate(),
      pe_checksum_header: pe.headers.checksum,
      pe_checksum_computed,
      options: options.clone(),
      discovered_regions: 0,
      rejected_regions: 0,
      approved_regions: 0,
      modified_regions: 0,
      candidates: Vec::new(),
      patches: Vec::new(),
      warnings: Vec::new(),
      decode_errors: Vec::new(),
      validation: ValidationSummary::default(),
      reference_concepts: vec![
        "binprotect: first-class RVA tracking and conservative metadata awareness".to_string(),
        "binprotect: branch/RIP-relative reference discovery as safety input".to_string(),
        "ObfuGuard: PE64 validation and instruction-boundary awareness".to_string(),
        "ObfuGuard: deterministic replacement planning instead of time randomness".to_string(),
      ],
    }
  }

  pub fn add_candidate(&mut self, candidate: CandidateReport) {
    self.candidates.push(candidate);
    self.recount();
  }

  pub fn mark_patches(&mut self, patches: &[Patch], dry_run: bool) {
    self.patches = patches.iter().map(PatchRecord::from).collect();

    for patch in patches {
      if let Some(candidate) = self
        .candidates
        .iter_mut()
        .find(|candidate| candidate.id == patch.candidate_id)
      {
        candidate.status = if dry_run {
          CandidateStatus::Approved
        } else {
          CandidateStatus::Modified
        };
        candidate.template = Some(patch.template.clone());
      }
    }

    if dry_run {
      self.modified_regions = 0;
    } else {
      self.modified_regions = patches.len();
    }
    self.recount();
  }

  pub fn recount(&mut self) {
    self.discovered_regions = self.candidates.len();
    self.rejected_regions = self
      .candidates
      .iter()
      .filter(|candidate| candidate.status == CandidateStatus::Rejected)
      .count();
    self.approved_regions = self
      .candidates
      .iter()
      .filter(|candidate| {
        matches!(
          candidate.status,
          CandidateStatus::Approved | CandidateStatus::Modified | CandidateStatus::Unchanged
        )
      })
      .count();
    self.modified_regions = self
      .candidates
      .iter()
      .filter(|candidate| candidate.status == CandidateStatus::Modified)
      .count();
  }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct CandidateReport {
  pub id: usize,
  pub section: String,
  pub rva: u32,
  pub file_offset: usize,
  pub length: usize,
  pub byte_kind: PaddingByteKind,
  pub status: CandidateStatus,
  pub rejection_reasons: Vec<String>,
  pub template: Option<String>,
}

impl CandidateReport {
  pub fn range(&self) -> AddressRange {
    AddressRange::new(self.file_offset as u32, self.length as u32)
  }
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum CandidateStatus {
  Approved,
  Rejected,
  Modified,
  Unchanged,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct PatchRecord {
  pub candidate_id: usize,
  pub rva: u32,
  pub file_offset: usize,
  pub length: usize,
  pub template: String,
  pub replacement_sha256: String,
}

impl From<&Patch> for PatchRecord {
  fn from(value: &Patch) -> Self {
    Self {
      candidate_id: value.candidate_id,
      rva: value.rva,
      file_offset: value.file_offset,
      length: value.replacement.len(),
      template: value.template.clone(),
      replacement_sha256: sha256_hex(&value.replacement),
    }
  }
}
