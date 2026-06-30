#![allow(non_snake_case)]

pub mod analysis;
pub mod checksum;
pub mod error;
pub mod options;
pub mod pe;
pub mod report;
pub mod transform;
pub mod validate;

use napi::bindgen_prelude::{Error, Result};
use napi_derive::napi;

use analysis::control_flow::collect_code_roots;
use analysis::decoder::analyze_code;
use analysis::padding::discover_padding;
use analysis::safety::evaluate_candidates;
use error::TransformError;
use options::TransformOptions;
use pe::PeImage;
use report::{TransformReport, TransformResult};
use transform::patcher::apply_patches;
use transform::planner::plan_patches;
use validate::validate_transformation;

pub use options::TransformOptions as PublicTransformOptions;
pub use report::{
  CandidateReport, CandidateStatus, PatchRecord, TransformReport as PublicTransformReport,
};

#[allow(non_snake_case)]
#[napi(object)]
pub struct DiversifyDllPaddingResult {
  pub candidates: u32,
  pub patchedCandidates: u32,
  pub alreadyPatched: u32,
  pub mutations: u32,
  pub hashBefore: String,
  pub hashAfter: String,
}

#[napi]
pub async fn diversify_dll_padding(
  input_path: String,
  output_path: String,
) -> Result<DiversifyDllPaddingResult> {
  napi::tokio::task::spawn_blocking(move || diversify_dll_padding_sync(&input_path, &output_path))
    .await
    .map_err(|error| Error::from_reason(format!("Failed to run PE padding diversifier: {error}")))?
}

fn diversify_dll_padding_sync(
  input_path: &str,
  output_path: &str,
) -> Result<DiversifyDllPaddingResult> {
  let input = std::fs::read(input_path)
    .map_err(|error| Error::from_reason(format!("Failed to read input DLL: {error}")))?;
  let result = transform_pe(
    &input,
    &TransformOptions {
      allow_invalid_signature: true,
      ..TransformOptions::default()
    },
  )
  .map_err(|error| Error::from_reason(format!("Failed to transform DLL: {error}")))?;

  if !result.report.patches.is_empty() {
    std::fs::write(output_path, &result.output)
      .map_err(|error| Error::from_reason(format!("Failed to write diversified DLL: {error}")))?;
  }

  Ok(DiversifyDllPaddingResult {
    candidates: result.report.discovered_regions as u32,
    patchedCandidates: result.report.patches.len() as u32,
    alreadyPatched: 0,
    mutations: result.report.modified_regions as u32,
    hashBefore: result.report.input_sha256,
    hashAfter: result
      .report
      .output_sha256
      .unwrap_or_else(|| checksum::sha256_hex(&result.output)),
  })
}

pub fn analyze_pe(
  input: &[u8],
  options: &TransformOptions,
) -> std::result::Result<TransformReport, TransformError> {
  let analysis = analyze_internal(input, options)?;
  Ok(analysis.report)
}

pub fn transform_pe(
  input: &[u8],
  options: &TransformOptions,
) -> std::result::Result<TransformResult, TransformError> {
  let mut analysis = analyze_internal(input, options)?;

  if analysis.pe.has_certificate() && !options.allow_invalid_signature && !options.dry_run {
    return Err(TransformError::AuthenticodeSignature);
  }

  let patches = plan_patches(&analysis.approved_candidates, input, options)?;
  let repeated_patches = plan_patches(&analysis.approved_candidates, input, options)?;
  let deterministic = patches == repeated_patches;
  if !deterministic {
    return Err(TransformError::Validation(
      "patch planning was not deterministic".to_string(),
    ));
  }

  analysis.report.mark_patches(&patches, options.dry_run);

  let output = if options.dry_run {
    input.to_vec()
  } else {
    apply_patches(input, &patches)?
  };

  if options.dry_run {
    analysis.report.output_sha256 = None;
    analysis.report.validation.deterministic = deterministic;
    return Ok(TransformResult {
      output,
      report: analysis.report,
    });
  }

  let validation = validate_transformation(input, &output, &analysis.pe, &patches)?;
  analysis.report.validation = validation.with_determinism(deterministic);
  analysis.report.output_sha256 = Some(checksum::sha256_hex(&output));

  Ok(TransformResult {
    output,
    report: analysis.report,
  })
}

pub fn verify_pair(
  original: &[u8],
  output: &[u8],
  options: &TransformOptions,
) -> std::result::Result<TransformReport, TransformError> {
  let mut analysis = analyze_internal(original, options)?;
  let synthetic_patches =
    validate::infer_patches_from_diff(original, output, &analysis.approved_candidates)?;
  let validation = validate_transformation(original, output, &analysis.pe, &synthetic_patches)?;
  analysis.report.mark_patches(&synthetic_patches, false);
  analysis.report.output_sha256 = Some(checksum::sha256_hex(output));
  analysis.report.validation = validation;
  Ok(analysis.report)
}

struct InternalAnalysis<'a> {
  pe: PeImage<'a>,
  report: TransformReport,
  approved_candidates: Vec<analysis::safety::ApprovedCandidate>,
}

fn analyze_internal<'a>(
  input: &'a [u8],
  options: &TransformOptions,
) -> std::result::Result<InternalAnalysis<'a>, TransformError> {
  let pe = PeImage::parse(input)?;
  if options.dll_only && !pe.headers.is_dll {
    return Err(TransformError::Unsupported(
      "input is a PE image but is not marked as a DLL".to_string(),
    ));
  }

  let mut report = TransformReport::new(input, &pe, options);
  if pe.has_certificate() {
    report.warnings.push(
      "Authenticode certificate table is present; byte changes invalidate the signature"
        .to_string(),
    );
  }
  report.warnings.extend(pe.warnings.iter().cloned());

  let roots = collect_code_roots(&pe);
  let code_analysis = analyze_code(&pe, input, &roots)?;
  report.decode_errors = code_analysis.decode_errors.clone();

  let candidates = discover_padding(&pe, input, options)?;
  let approved_candidates =
    evaluate_candidates(&pe, &code_analysis, &candidates, options, &mut report)?;
  report.recount();

  Ok(InternalAnalysis {
    pe,
    report,
    approved_candidates,
  })
}
