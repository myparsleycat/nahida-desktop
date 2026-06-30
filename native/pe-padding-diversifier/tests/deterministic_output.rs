mod fixtures;

use pe_diversifier::options::TransformOptions;
use pe_diversifier::{transform_pe, verify_pair};

#[test]
fn fixed_seed_is_byte_for_byte_deterministic() {
  let input = fixtures::minimal_dll(fixtures::ret_then_int3_padding(64));
  let options = TransformOptions {
    seed: 1234,
    ..TransformOptions::default()
  };
  let first = transform_pe(&input, &options).unwrap();
  let second = transform_pe(&input, &options).unwrap();
  assert_eq!(first.output, second.output);
  assert!(first.report.validation.deterministic);
}

#[test]
fn different_seed_changes_output_when_candidate_exists() {
  let input = fixtures::minimal_dll(fixtures::ret_then_int3_padding(64));
  let first = transform_pe(
    &input,
    &TransformOptions {
      seed: 1,
      ..TransformOptions::default()
    },
  )
  .unwrap();
  let second = transform_pe(
    &input,
    &TransformOptions {
      seed: 2,
      ..TransformOptions::default()
    },
  )
  .unwrap();
  assert_ne!(first.output, second.output);
}

#[test]
fn verify_accepts_transformed_output() {
  let input = fixtures::minimal_dll(fixtures::ret_then_int3_padding(64));
  let options = TransformOptions {
    seed: 99,
    ..TransformOptions::default()
  };
  let transformed = transform_pe(&input, &options).unwrap();
  let report = verify_pair(&input, &transformed.output, &TransformOptions::default()).unwrap();
  assert!(report.validation.reparsed_output);
}

#[test]
fn input_buffer_is_not_modified() {
  let input = fixtures::minimal_dll(fixtures::ret_then_int3_padding(64));
  let original = input.clone();
  let _ = transform_pe(
    &input,
    &TransformOptions {
      seed: 42,
      ..TransformOptions::default()
    },
  )
  .unwrap();
  assert_eq!(input, original);
}
