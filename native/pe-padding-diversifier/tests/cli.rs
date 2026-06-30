mod fixtures;

use std::process::{Command, Stdio};

#[test]
fn cli_analyze_transform_verify_end_to_end() {
  let temp = tempfile::tempdir().unwrap();
  let input = temp.path().join("input.dll");
  let output = temp.path().join("output.dll");
  let report = temp.path().join("report.json");
  std::fs::write(
    &input,
    fixtures::minimal_dll(fixtures::ret_then_int3_padding(64)),
  )
  .unwrap();

  let binary = env!("CARGO_BIN_EXE_pe-diversifier");

  let analyze = Command::new(binary)
    .arg("analyze")
    .arg(&input)
    .arg("--report")
    .arg(&report)
    .stdout(Stdio::null())
    .stderr(Stdio::null())
    .status()
    .unwrap();
  assert!(analyze.success());
  assert!(report.exists());

  let transform = Command::new(binary)
    .arg("transform")
    .arg(&input)
    .arg("--output")
    .arg(&output)
    .arg("--seed")
    .arg("1234")
    .stdout(Stdio::null())
    .stderr(Stdio::null())
    .status()
    .unwrap();
  assert!(transform.success());
  assert!(output.exists());

  let verify = Command::new(binary)
    .arg("verify")
    .arg(&input)
    .arg(&output)
    .stdout(Stdio::null())
    .stderr(Stdio::null())
    .status()
    .unwrap();
  assert!(verify.success());
}
