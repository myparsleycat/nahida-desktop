use rand_core::Rng;

pub fn render_template<R: Rng>(length: usize, rng: &mut R) -> Option<(Vec<u8>, String)> {
  if length < 2 {
    return None;
  }

  let mut output = Vec::with_capacity(length);
  let template_name;

  if length <= 129 {
    output.push(0xeb);
    output.push((length - 2) as u8);
    template_name = "short_jmp_over_nop_payload".to_string();
    fill_nop_payload(&mut output, length - 2, rng);
  } else {
    output.push(0xe9);
    let displacement = (length - 5) as i32;
    output.extend_from_slice(&displacement.to_le_bytes());
    template_name = "near_jmp_over_nop_payload".to_string();
    fill_nop_payload(&mut output, length - 5, rng);
  }

  debug_assert_eq!(output.len(), length);
  Some((output, template_name))
}

fn fill_nop_payload<R: Rng>(output: &mut Vec<u8>, mut remaining: usize, rng: &mut R) {
  const NOPS: &[&[u8]] = &[
    &[0x90],
    &[0x66, 0x90],
    &[0x0f, 0x1f, 0x00],
    &[0x0f, 0x1f, 0x40, 0x00],
    &[0x0f, 0x1f, 0x44, 0x00, 0x00],
    &[0x66, 0x0f, 0x1f, 0x44, 0x00, 0x00],
    &[0x0f, 0x1f, 0x80, 0x00, 0x00, 0x00, 0x00],
    &[0x0f, 0x1f, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00],
    &[0x66, 0x0f, 0x1f, 0x84, 0x00, 0x00, 0x00, 0x00, 0x00],
  ];

  while remaining > 0 {
    let fitting: Vec<_> = NOPS
      .iter()
      .copied()
      .filter(|candidate| candidate.len() <= remaining)
      .collect();
    let index = (rng.next_u32() as usize) % fitting.len();
    let selected = fitting[index];
    output.extend_from_slice(selected);
    remaining -= selected.len();
  }
}
