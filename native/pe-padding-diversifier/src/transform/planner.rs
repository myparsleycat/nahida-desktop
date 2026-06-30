use rand_chacha::ChaCha20Rng;
use rand_core::SeedableRng;
use sha2::{Digest, Sha256};

use crate::analysis::safety::ApprovedCandidate;
use crate::error::TransformError;
use crate::options::TransformOptions;

use super::templates::render_template;

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Patch {
  pub candidate_id: usize,
  pub rva: u32,
  pub file_offset: usize,
  pub replacement: Vec<u8>,
  pub template: String,
}

pub fn plan_patches(
  candidates: &[ApprovedCandidate],
  input: &[u8],
  options: &TransformOptions,
) -> Result<Vec<Patch>, TransformError> {
  let limit = options.maximum_mutations.unwrap_or(usize::MAX);
  let input_hash = Sha256::digest(input);
  let mut patches = Vec::new();

  for candidate in candidates.iter().take(limit) {
    let seed = candidate_seed(options.seed, &input_hash, candidate);
    let mut rng = ChaCha20Rng::from_seed(seed);
    let Some((replacement, template)) = render_template(candidate.length, &mut rng) else {
      continue;
    };

    if replacement.len() != candidate.length {
      return Err(TransformError::Validation(
        "template length did not match candidate length".to_string(),
      ));
    }

    patches.push(Patch {
      candidate_id: candidate.id,
      rva: candidate.rva,
      file_offset: candidate.file_offset,
      replacement,
      template,
    });
  }

  Ok(patches)
}

fn candidate_seed(seed: u64, input_hash: &[u8], candidate: &ApprovedCandidate) -> [u8; 32] {
  let mut hasher = Sha256::new();
  hasher.update(seed.to_le_bytes());
  hasher.update(input_hash);
  hasher.update(candidate.id.to_le_bytes());
  hasher.update(candidate.rva.to_le_bytes());
  hasher.update(candidate.file_offset.to_le_bytes());
  hasher.update(candidate.length.to_le_bytes());
  hasher.update(env!("CARGO_PKG_VERSION").as_bytes());
  hasher.finalize().into()
}
