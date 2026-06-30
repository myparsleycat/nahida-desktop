use std::path::{Path, PathBuf};

use clap::{Parser, Subcommand};
use pe_diversifier::error::TransformError;
use pe_diversifier::options::TransformOptions;
use pe_diversifier::{analyze_pe, transform_pe, verify_pair};

#[derive(Debug, Parser)]
#[command(version, about = "Conservative PE64 DLL padding diversifier")]
struct Cli {
  #[command(subcommand)]
  command: Command,
}

#[derive(Debug, Subcommand)]
enum Command {
  Analyze {
    input: PathBuf,
    #[arg(long, default_value_t = 8)]
    minimum_sled_length: usize,
    #[arg(long)]
    allow_zero_padding: bool,
    #[arg(long)]
    json: bool,
    #[arg(long)]
    report: Option<PathBuf>,
  },
  Transform {
    input: PathBuf,
    #[arg(short, long)]
    output: PathBuf,
    #[arg(long)]
    seed: u64,
    #[arg(long, default_value_t = 8)]
    minimum_sled_length: usize,
    #[arg(long)]
    maximum_mutations: Option<usize>,
    #[arg(long)]
    dry_run: bool,
    #[arg(long)]
    overwrite: bool,
    #[arg(long)]
    allow_invalid_signature: bool,
    #[arg(long)]
    allow_zero_padding: bool,
    #[arg(long)]
    json: bool,
    #[arg(long)]
    report: Option<PathBuf>,
  },
  Verify {
    input: PathBuf,
    output: PathBuf,
    #[arg(long, default_value_t = 8)]
    minimum_sled_length: usize,
    #[arg(long)]
    allow_zero_padding: bool,
    #[arg(long)]
    json: bool,
    #[arg(long)]
    report: Option<PathBuf>,
  },
}

fn main() {
  if let Err(error) = run() {
    eprintln!("error: {error}");
    std::process::exit(1);
  }
}

fn run() -> Result<(), TransformError> {
  let cli = Cli::parse();
  match cli.command {
    Command::Analyze {
      input,
      minimum_sled_length,
      allow_zero_padding,
      json,
      report,
    } => {
      let bytes = std::fs::read(&input)?;
      let options = TransformOptions {
        minimum_sled_length,
        allow_zero_padding,
        dry_run: true,
        ..TransformOptions::default()
      };
      let report_data = analyze_pe(&bytes, &options)?;
      emit_report(&report_data, json, report.as_deref())?;
      if !json {
        print_summary(&report_data);
      }
    }
    Command::Transform {
      input,
      output,
      seed,
      minimum_sled_length,
      maximum_mutations,
      dry_run,
      overwrite,
      allow_invalid_signature,
      allow_zero_padding,
      json,
      report,
    } => {
      guard_output_path(&input, &output, overwrite, dry_run)?;
      let bytes = std::fs::read(&input)?;
      let options = TransformOptions {
        seed,
        minimum_sled_length,
        maximum_mutations,
        dry_run,
        allow_invalid_signature,
        allow_zero_padding,
        ..TransformOptions::default()
      };
      let result = transform_pe(&bytes, &options)?;
      if !dry_run {
        std::fs::write(&output, &result.output)?;
      }
      emit_report(&result.report, json, report.as_deref())?;
      if !json {
        print_summary(&result.report);
        if dry_run {
          println!("dry run: no output file written");
        } else {
          println!("wrote {}", output.display());
        }
      }
    }
    Command::Verify {
      input,
      output,
      minimum_sled_length,
      allow_zero_padding,
      json,
      report,
    } => {
      let original = std::fs::read(&input)?;
      let transformed = std::fs::read(&output)?;
      let options = TransformOptions {
        minimum_sled_length,
        allow_zero_padding,
        ..TransformOptions::default()
      };
      let report_data = verify_pair(&original, &transformed, &options)?;
      emit_report(&report_data, json, report.as_deref())?;
      if !json {
        print_summary(&report_data);
        println!("verify: output matches conservative layout constraints");
      }
    }
  }
  Ok(())
}

fn guard_output_path(
  input: &Path,
  output: &Path,
  overwrite: bool,
  dry_run: bool,
) -> Result<(), TransformError> {
  let input_canonical = input.canonicalize()?;
  if output.exists() {
    let output_canonical = output.canonicalize()?;
    if input_canonical == output_canonical {
      return Err(TransformError::Validation(
        "input and output resolve to the same file".to_string(),
      ));
    }
    if !overwrite && !dry_run {
      return Err(TransformError::Validation(format!(
        "output {} already exists; pass --overwrite to replace it",
        output.display()
      )));
    }
  } else if let Some(parent) = output.parent() {
    let parent = if parent.as_os_str().is_empty() {
      Path::new(".")
    } else {
      parent
    };
    let parent_canonical = parent.canonicalize()?;
    let candidate = parent_canonical.join(
      output
        .file_name()
        .ok_or_else(|| TransformError::Validation("output path has no file name".to_string()))?,
    );
    if input_canonical == candidate {
      return Err(TransformError::Validation(
        "input and output resolve to the same file".to_string(),
      ));
    }
  }
  Ok(())
}

fn emit_report(
  report: &pe_diversifier::report::TransformReport,
  json_stdout: bool,
  report_path: Option<&Path>,
) -> Result<(), TransformError> {
  if let Some(path) = report_path {
    let json = serde_json::to_string_pretty(report)?;
    std::fs::write(path, json)?;
  }
  if json_stdout {
    println!("{}", serde_json::to_string_pretty(report)?);
  }
  Ok(())
}

fn print_summary(report: &pe_diversifier::report::TransformReport) {
  println!("input sha256:  {}", report.input_sha256);
  if let Some(output) = &report.output_sha256 {
    println!("output sha256: {output}");
  } else {
    println!("output sha256: unavailable for analysis-only mode");
  }
  println!("discovered:     {}", report.discovered_regions);
  println!("rejected:       {}", report.rejected_regions);
  println!("approved:       {}", report.approved_regions);
  println!("modified:       {}", report.modified_regions);
  if report.has_authenticode_certificate {
    println!("signature:      certificate table present");
  }
  if report.pe_checksum_header != report.pe_checksum_computed {
    println!(
      "pe checksum:    header=0x{:08x}, computed=0x{:08x}",
      report.pe_checksum_header, report.pe_checksum_computed
    );
  }
  for warning in &report.warnings {
    println!("warning:        {warning}");
  }
}
