use thiserror::Error;

#[derive(Debug, Error)]
pub enum TransformError {
  #[error("invalid PE input: {0}")]
  InvalidPe(String),
  #[error("unsupported input: {0}")]
  Unsupported(String),
  #[error("integer overflow while processing {0}")]
  IntegerOverflow(&'static str),
  #[error("address conversion failed: {0}")]
  AddressConversion(String),
  #[error(
    "input contains an Authenticode certificate table; pass the explicit allow option to produce an invalidated signature"
  )]
  AuthenticodeSignature,
  #[error("transformation validation failed: {0}")]
  Validation(String),
  #[error("I/O error: {0}")]
  Io(#[from] std::io::Error),
  #[error("JSON error: {0}")]
  Json(#[from] serde_json::Error),
}

pub fn checked_add_u32(a: u32, b: u32, context: &'static str) -> Result<u32, TransformError> {
  a.checked_add(b)
    .ok_or(TransformError::IntegerOverflow(context))
}

pub fn checked_add_usize(
  a: usize,
  b: usize,
  context: &'static str,
) -> Result<usize, TransformError> {
  a.checked_add(b)
    .ok_or(TransformError::IntegerOverflow(context))
}
