use std::collections::{BTreeSet, VecDeque};

use iced_x86::{Decoder, DecoderOptions, FlowControl, Instruction, OpKind, Register};

use crate::error::{TransformError, checked_add_u32};
use crate::pe::PeImage;
use crate::pe::address::AddressRange;

#[derive(Debug, Clone, Default)]
pub struct CodeAnalysis {
  pub instruction_ranges: Vec<AddressRange>,
  pub branch_targets: Vec<u32>,
  pub referenced_rvas: Vec<u32>,
  pub roots: Vec<u32>,
  pub decode_errors: Vec<String>,
}

pub fn analyze_code(
  pe: &PeImage<'_>,
  data: &[u8],
  roots: &[u32],
) -> Result<CodeAnalysis, TransformError> {
  let mut result = CodeAnalysis {
    roots: roots.to_vec(),
    ..CodeAnalysis::default()
  };

  let mut queue: VecDeque<u32> = roots.iter().copied().collect();
  let mut queued_or_processed: BTreeSet<u32> = roots.iter().copied().collect();
  let mut decoded_starts = BTreeSet::new();

  while let Some(root) = queue.pop_front() {
    if !pe.is_executable_rva(root) {
      continue;
    }
    decode_from_root(
      pe,
      data,
      root,
      &mut decoded_starts,
      &mut queued_or_processed,
      &mut queue,
      &mut result,
    )?;
  }

  result.instruction_ranges.sort_unstable();
  result.branch_targets.sort_unstable();
  result.branch_targets.dedup();
  result.referenced_rvas.sort_unstable();
  result.referenced_rvas.dedup();
  Ok(result)
}

fn decode_from_root(
  pe: &PeImage<'_>,
  data: &[u8],
  root: u32,
  decoded_starts: &mut BTreeSet<u32>,
  queued_or_processed: &mut BTreeSet<u32>,
  queue: &mut VecDeque<u32>,
  result: &mut CodeAnalysis,
) -> Result<(), TransformError> {
  let mut current_rva = root;

  loop {
    if !pe.is_executable_rva(current_rva) {
      break;
    }
    if decoded_starts.contains(&current_rva) {
      break;
    }

    let file_offset = match pe.rva_to_file_offset(current_rva) {
      Ok(offset) => offset,
      Err(error) => {
        result.decode_errors.push(format!(
          "decode root 0x{root:x}: RVA 0x{current_rva:x} did not map to file data: {error}"
        ));
        break;
      }
    };
    let Some(section) = pe.section_by_rva(current_rva) else {
      break;
    };
    let raw_end = section.raw_end() as usize;
    if file_offset >= raw_end || file_offset >= data.len() {
      break;
    }

    if padding_stop(data, file_offset, raw_end) {
      break;
    }

    let bytes = &data[file_offset..raw_end.min(data.len())];
    let mut decoder = Decoder::with_ip(64, bytes, current_rva as u64, DecoderOptions::NONE);
    let instruction = decoder.decode();

    if instruction.is_invalid() || instruction.len() == 0 {
      result.decode_errors.push(format!(
        "decode failed at RVA 0x{current_rva:x} from root 0x{root:x}"
      ));
      break;
    }

    let length = instruction.len() as u32;
    decoded_starts.insert(current_rva);
    result
      .instruction_ranges
      .push(AddressRange::new(current_rva, length));

    collect_references(pe, &instruction, result, queued_or_processed, queue);

    let next_rva = checked_add_u32(current_rva, length, "next instruction RVA")?;
    match instruction.flow_control() {
      FlowControl::Next | FlowControl::Call | FlowControl::IndirectCall => {
        current_rva = next_rva;
      }
      FlowControl::ConditionalBranch => {
        if queued_or_processed.insert(next_rva) {
          queue.push_back(next_rva);
        }
        current_rva = next_rva;
      }
      FlowControl::UnconditionalBranch
      | FlowControl::IndirectBranch
      | FlowControl::Return
      | FlowControl::Interrupt
      | FlowControl::XbeginXabortXend
      | FlowControl::Exception => break,
    }
  }

  Ok(())
}

fn collect_references(
  pe: &PeImage<'_>,
  instruction: &Instruction,
  result: &mut CodeAnalysis,
  queued_or_processed: &mut BTreeSet<u32>,
  queue: &mut VecDeque<u32>,
) {
  if instruction.is_call_near()
    || instruction.is_jmp_short_or_near()
    || instruction.is_jcc_short_or_near()
    || instruction.is_loop()
  {
    let target = instruction.near_branch_target();
    if target <= u32::MAX as u64 {
      let target = target as u32;
      result.branch_targets.push(target);
      if pe.is_executable_rva(target) && queued_or_processed.insert(target) {
        queue.push_back(target);
      }
    }
  }

  for op_index in 0..instruction.op_count() {
    if instruction.op_kind(op_index) == OpKind::Memory && instruction.memory_base() == Register::RIP
    {
      let target = instruction.ip_rel_memory_address();
      if target <= u32::MAX as u64 {
        result.referenced_rvas.push(target as u32);
      }
    }
  }
}

fn padding_stop(data: &[u8], offset: usize, raw_end: usize) -> bool {
  let limit = raw_end.min(data.len());
  if offset >= limit {
    return true;
  }
  let byte = data[offset];
  if !matches!(byte, 0x90 | 0xcc | 0x00) {
    return false;
  }
  let mut count = 0usize;
  for current in &data[offset..limit] {
    if *current == byte {
      count += 1;
      if count >= 8 {
        return true;
      }
    } else {
      break;
    }
  }
  false
}
