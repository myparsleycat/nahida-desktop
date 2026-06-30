use crate::pe::PeImage;

pub fn collect_code_roots(pe: &PeImage<'_>) -> Vec<u32> {
  let mut roots = Vec::new();

  if pe.headers.entry_point != 0 && pe.is_executable_rva(pe.headers.entry_point) {
    roots.push(pe.headers.entry_point);
  }

  for section in pe.executable_sections() {
    roots.push(section.virtual_address);
  }

  roots.extend(
    pe.export_rvas
      .iter()
      .copied()
      .filter(|rva| pe.is_executable_rva(*rva)),
  );
  roots.extend(
    pe.runtime_functions
      .iter()
      .map(|function| function.begin)
      .filter(|rva| pe.is_executable_rva(*rva)),
  );
  roots.extend(pe.relocation_code_roots.iter().copied());
  roots.extend(pe.tls_callback_rvas.iter().copied());
  roots.extend(pe.guard_cf_function_rvas.iter().copied());

  roots.sort_unstable();
  roots.dedup();
  roots
}
