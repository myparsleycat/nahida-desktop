use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TransformOptions {
  pub seed: u64,
  pub minimum_sled_length: usize,
  pub maximum_mutations: Option<usize>,
  pub dry_run: bool,
  pub allow_invalid_signature: bool,
  pub allow_zero_padding: bool,
  pub dll_only: bool,
}

impl Default for TransformOptions {
  fn default() -> Self {
    Self {
      seed: 0,
      minimum_sled_length: 8,
      maximum_mutations: None,
      dry_run: false,
      allow_invalid_signature: false,
      allow_zero_padding: false,
      dll_only: true,
    }
  }
}
