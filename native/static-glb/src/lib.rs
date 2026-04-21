#![deny(clippy::all)]

use napi::bindgen_prelude::Buffer;
use napi_derive::napi;

#[napi(object)]
pub struct PngAnalysis {
  pub has_alpha: bool,
  pub low_ratio: f64,
  pub high_ratio: f64,
  pub partial_ratio: f64,
  pub low_alpha_rgb_mean: f64,
  pub channel_range_max: u32,
  pub luminance_std_dev: f64,
  pub mean_r: f64,
  pub mean_g: f64,
  pub mean_b: f64,
  pub blue_dominance: f64,
}

#[napi(object)]
pub struct DrawRange {
  pub index_count: i32,
  pub start_index: i32,
  pub base_vertex: i32,
}

#[napi(object)]
pub struct MergeDrawIndicesResult {
  pub indices: Buffer,
  pub invalid_ranges: Vec<String>,
}

#[napi(object)]
pub struct RemoveDegenerateTrianglesResult {
  pub indices: Buffer,
  pub removed: u32,
}

#[napi]
pub fn analyze_png(data: Buffer, width: u32, height: u32) -> napi::Result<PngAnalysis> {
  let pixel_count = width as usize * height as usize;
  let expected_len = pixel_count * 4;
  if data.len() < expected_len {
    return Err(napi::Error::from_reason(format!(
      "PNG buffer is shorter than expected RGBA data: got {}, expected at least {}",
      data.len(),
      expected_len
    )));
  }

  let mut low = 0usize;
  let mut high = 0usize;
  let mut partial = 0usize;
  let mut low_alpha_rgb_total = 0f64;

  let mut min_r = 255u8;
  let mut min_g = 255u8;
  let mut min_b = 255u8;
  let mut max_r = 0u8;
  let mut max_g = 0u8;
  let mut max_b = 0u8;
  let mut sample_count = 0usize;
  let mut sum_r = 0f64;
  let mut sum_g = 0f64;
  let mut sum_b = 0f64;
  let mut luminance_sum = 0f64;
  let mut luminance_square_sum = 0f64;

  for offset in (0..expected_len).step_by(4) {
    let alpha = data[offset + 3];
    if alpha <= 16 {
      low += 1;
      low_alpha_rgb_total +=
        (data[offset] as f64 + data[offset + 1] as f64 + data[offset + 2] as f64) / 3.0;
    } else if alpha >= 239 {
      high += 1;
    } else {
      partial += 1;
    }
  }

  let stride = (((width as usize * height as usize) as f64 / 4096.0).sqrt().floor() as usize)
    .max(1);

  for y in (0..height as usize).step_by(stride) {
    for x in (0..width as usize).step_by(stride) {
      let offset = (y * width as usize + x) * 4;
      let r = data[offset];
      let g = data[offset + 1];
      let b = data[offset + 2];
      min_r = min_r.min(r);
      min_g = min_g.min(g);
      min_b = min_b.min(b);
      max_r = max_r.max(r);
      max_g = max_g.max(g);
      max_b = max_b.max(b);
      sum_r += r as f64;
      sum_g += g as f64;
      sum_b += b as f64;
      let luminance = (0.2126 * r as f64 + 0.7152 * g as f64 + 0.0722 * b as f64) / 255.0;
      luminance_sum += luminance;
      luminance_square_sum += luminance * luminance;
      sample_count += 1;
    }
  }

  let mean = if sample_count > 0 {
    luminance_sum / sample_count as f64
  } else {
    0.0
  };
  let variance = if sample_count > 0 {
    (luminance_square_sum / sample_count as f64 - mean * mean).max(0.0)
  } else {
    0.0
  };
  let mean_r = if sample_count > 0 {
    sum_r / sample_count as f64 / 255.0
  } else {
    0.0
  };
  let mean_g = if sample_count > 0 {
    sum_g / sample_count as f64 / 255.0
  } else {
    0.0
  };
  let mean_b = if sample_count > 0 {
    sum_b / sample_count as f64 / 255.0
  } else {
    0.0
  };

  Ok(PngAnalysis {
    has_alpha: low > 0 || partial > 0,
    low_ratio: ratio(low, pixel_count),
    high_ratio: ratio(high, pixel_count),
    partial_ratio: ratio(partial, pixel_count),
    low_alpha_rgb_mean: if low > 0 {
      low_alpha_rgb_total / low as f64
    } else {
      0.0
    },
    channel_range_max: (max_r.saturating_sub(min_r) as u32)
      .max(max_g.saturating_sub(min_g) as u32)
      .max(max_b.saturating_sub(min_b) as u32),
    luminance_std_dev: variance.sqrt(),
    mean_r,
    mean_g,
    mean_b,
    blue_dominance: mean_b - mean_r.max(mean_g),
  })
}

#[napi]
pub fn invert_rgba_alpha(data: Buffer) -> Buffer {
  let mut out = data.to_vec();
  for offset in (3..out.len()).step_by(4) {
    out[offset] = 255u8.saturating_sub(out[offset]);
  }
  out.into()
}

#[napi]
pub fn parse_dds_srgb_state(bytes: Buffer) -> Option<bool> {
  if bytes.len() < 148 || &bytes[..4] != b"DDS " {
    return None;
  }

  let four_cc = std::str::from_utf8(&bytes[84..88]).ok()?;
  if four_cc != "DX10" {
    return None;
  }

  let dxgi_format = u32::from_le_bytes(bytes[128..132].try_into().ok()?);
  match dxgi_format {
    29 | 72 | 75 | 78 | 91 | 93 | 99 => Some(true),
    28 | 71 | 74 | 77 | 80 | 83 | 87 | 88 | 95 | 98 => Some(false),
    _ => None,
  }
}

#[napi]
pub fn interleave_vertex_buffers(
  position: Buffer,
  position_stride: u32,
  blend: Buffer,
  blend_stride: u32,
  texcoord: Buffer,
  texcoord_stride: u32,
) -> napi::Result<Buffer> {
  let position_stride = position_stride as usize;
  let blend_stride = blend_stride as usize;
  let texcoord_stride = texcoord_stride as usize;
  if position_stride == 0 || blend_stride == 0 || texcoord_stride == 0 {
    return Err(napi::Error::from_reason("Vertex buffer stride must be greater than zero"));
  }

  let stride = position_stride + blend_stride + texcoord_stride;
  let vertex_count = (position.len() / position_stride)
    .min(blend.len() / blend_stride)
    .min(texcoord.len() / texcoord_stride);
  let mut out = vec![0u8; vertex_count * stride];

  for i in 0..vertex_count {
    let mut offset = i * stride;
    let src = i * position_stride;
    out[offset..offset + position_stride].copy_from_slice(&position[src..src + position_stride]);
    offset += position_stride;
    let src = i * blend_stride;
    out[offset..offset + blend_stride].copy_from_slice(&blend[src..src + blend_stride]);
    offset += blend_stride;
    let src = i * texcoord_stride;
    out[offset..offset + texcoord_stride].copy_from_slice(&texcoord[src..src + texcoord_stride]);
  }

  Ok(out.into())
}

#[napi]
pub fn decode_indices(bytes: Buffer, format: String) -> napi::Result<Buffer> {
  let upper = format.to_uppercase();
  let out = if upper.contains("R16_UINT") {
    if bytes.len() % 2 != 0 {
      return Err(napi::Error::from_reason("R16_UINT index buffer has an odd byte length"));
    }
    let mut values = Vec::with_capacity(bytes.len() / 2);
    for chunk in bytes.chunks_exact(2) {
      values.push(u16::from_le_bytes([chunk[0], chunk[1]]) as u32);
    }
    values
  } else if upper.contains("R32_UINT") || upper.contains("UNKNOWN") {
    if bytes.len() % 4 != 0 {
      return Err(napi::Error::from_reason("R32_UINT index buffer byte length is not divisible by 4"));
    }
    let mut values = Vec::with_capacity(bytes.len() / 4);
    for chunk in bytes.chunks_exact(4) {
      values.push(u32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]));
    }
    values
  } else {
    return Err(napi::Error::from_reason(format!("Unsupported IB format: {format}")));
  };

  Ok(u32_vec_to_buffer(out))
}

#[napi]
pub fn merge_draw_indices(indices: Buffer, draws: Vec<DrawRange>) -> napi::Result<MergeDrawIndicesResult> {
  let indices = buffer_to_u32_vec(&indices)?;
  let mut merged = Vec::new();
  let mut invalid_ranges = Vec::new();

  for draw in draws {
    let start_index = draw.start_index;
    let index_count = draw.index_count;
    let end_index = start_index.saturating_add(index_count);
    if start_index < 0 || index_count < 0 || end_index < 0 || end_index as usize > indices.len() {
      invalid_ranges.push(format!(
        "Skipping invalid draw range start={} count={}",
        draw.start_index, draw.index_count
      ));
      continue;
    }

    for index in start_index as usize..end_index as usize {
      let value = indices[index] as i64 + draw.base_vertex as i64;
      if value < 0 {
        return Err(napi::Error::from_reason(format!(
          "Merged index became negative for draw start={} count={} baseVertex={}",
          draw.start_index, draw.index_count, draw.base_vertex
        )));
      }
      merged.push(value as u32);
    }
  }

  Ok(MergeDrawIndicesResult {
    indices: if merged.is_empty() {
      u32_vec_to_buffer(indices)
    } else {
      u32_vec_to_buffer(merged)
    },
    invalid_ranges,
  })
}

#[napi]
pub fn read_float_attribute(
  bytes: Buffer,
  stride: u32,
  vertex_count: u32,
  aligned_byte_offset: u32,
  format: String,
  width: u32,
) -> napi::Result<Buffer> {
  let stride = stride as usize;
  let vertex_count = vertex_count as usize;
  let aligned_byte_offset = aligned_byte_offset as usize;
  let width = width as usize;
  let mut out = Vec::with_capacity(vertex_count * width);

  for vertex in 0..vertex_count {
    let base = vertex * stride + aligned_byte_offset;
    let values = read_dxgi_values(&bytes, base, &format)?;
    for c in 0..width {
      out.push(*values.get(c).unwrap_or(&0.0));
    }
  }

  Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn ensure_vec4(data: Buffer, vertex_count: u32, width: u32, fill_w: f64) -> napi::Result<Buffer> {
  let data = buffer_to_f32_vec(&data)?;
  let vertex_count = vertex_count as usize;
  let width = width as usize;
  let fill_w = fill_w as f32;
  if width == 4 {
    return Ok(f32_vec_to_buffer(data));
  }

  let mut out = vec![0.0f32; vertex_count * 4];
  for i in 0..vertex_count {
    out[i * 4] = data.get(i * width).copied().unwrap_or(0.0);
    out[i * 4 + 1] = data.get(i * width + 1).copied().unwrap_or(0.0);
    out[i * 4 + 2] = data.get(i * width + 2).copied().unwrap_or(0.0);
    out[i * 4 + 3] = if width > 3 {
      data.get(i * width + 3).copied().unwrap_or(fill_w)
    } else {
      fill_w
    };
  }

  Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn normalize_vec3_array(data: Buffer) -> napi::Result<Buffer> {
  let data = buffer_to_f32_vec(&data)?;
  let mut out = vec![0.0f32; data.len()];
  for i in (0..data.len()).step_by(3) {
    let x = data.get(i).copied().unwrap_or(0.0);
    let y = data.get(i + 1).copied().unwrap_or(0.0);
    let z = data.get(i + 2).copied().unwrap_or(0.0);
    let length = (x * x + y * y + z * z).sqrt();
    if length > 1e-8 {
      out[i] = x / length;
      if i + 1 < out.len() {
        out[i + 1] = y / length;
      }
      if i + 2 < out.len() {
        out[i + 2] = z / length;
      }
    }
  }
  Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn normalize_tangent_array(data: Buffer) -> napi::Result<Buffer> {
  let data = buffer_to_f32_vec(&data)?;
  let mut out = vec![0.0f32; data.len()];
  for i in (0..data.len()).step_by(4) {
    let x = data.get(i).copied().unwrap_or(0.0);
    let y = data.get(i + 1).copied().unwrap_or(0.0);
    let z = data.get(i + 2).copied().unwrap_or(0.0);
    let length = (x * x + y * y + z * z).sqrt();
    if length > 1e-8 {
      out[i] = x / length;
      if i + 1 < out.len() {
        out[i + 1] = y / length;
      }
      if i + 2 < out.len() {
        out[i + 2] = z / length;
      }
    }
    if i + 3 < out.len() {
      out[i + 3] = if data.get(i + 3).copied().unwrap_or(0.0) >= 0.0 { 1.0 } else { -1.0 };
    }
  }
  Ok(f32_vec_to_buffer(out))
}

#[napi]
pub fn remove_degenerate_triangles(indices: Buffer) -> napi::Result<RemoveDegenerateTrianglesResult> {
  let indices = buffer_to_u32_vec(&indices)?;
  let mut removed = 0u32;
  for i in (0..indices.len()).step_by(3) {
    if i + 2 >= indices.len() {
      break;
    }
    let a = indices[i];
    let b = indices[i + 1];
    let c = indices[i + 2];
    if a == b || b == c || a == c {
      removed += 1;
    }
  }

  if removed == 0 {
    return Ok(RemoveDegenerateTrianglesResult {
      indices: u32_vec_to_buffer(indices),
      removed,
    });
  }

  let mut out = Vec::with_capacity(indices.len() - removed as usize * 3);
  for i in (0..indices.len()).step_by(3) {
    if i + 2 >= indices.len() {
      break;
    }
    let a = indices[i];
    let b = indices[i + 1];
    let c = indices[i + 2];
    if a == b || b == c || a == c {
      continue;
    }
    out.push(a);
    out.push(b);
    out.push(c);
  }

  Ok(RemoveDegenerateTrianglesResult {
    indices: u32_vec_to_buffer(out),
    removed,
  })
}

fn ratio(value: usize, total: usize) -> f64 {
  if total == 0 {
    0.0
  } else {
    value as f64 / total as f64
  }
}

fn buffer_to_u32_vec(buffer: &Buffer) -> napi::Result<Vec<u32>> {
  if buffer.len() % 4 != 0 {
    return Err(napi::Error::from_reason("Expected a Uint32-compatible buffer"));
  }

  Ok(
    buffer
      .chunks_exact(4)
      .map(|chunk| u32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
      .collect(),
  )
}

fn buffer_to_f32_vec(buffer: &Buffer) -> napi::Result<Vec<f32>> {
  if buffer.len() % 4 != 0 {
    return Err(napi::Error::from_reason("Expected a Float32-compatible buffer"));
  }

  Ok(
    buffer
      .chunks_exact(4)
      .map(|chunk| f32::from_le_bytes([chunk[0], chunk[1], chunk[2], chunk[3]]))
      .collect(),
  )
}

fn u32_vec_to_buffer(values: Vec<u32>) -> Buffer {
  let mut out = Vec::with_capacity(values.len() * 4);
  for value in values {
    out.extend_from_slice(&value.to_le_bytes());
  }
  out.into()
}

fn f32_vec_to_buffer(values: Vec<f32>) -> Buffer {
  let mut out = Vec::with_capacity(values.len() * 4);
  for value in values {
    out.extend_from_slice(&value.to_le_bytes());
  }
  out.into()
}

fn read_dxgi_values(bytes: &[u8], offset: usize, format: &str) -> napi::Result<Vec<f32>> {
  let upper = format.to_uppercase();
  let count = format_component_count(&upper);

  if upper == "DXGI_FORMAT_R10G10B10A2_UNORM" {
    let value = read_u32(bytes, offset)?;
    return Ok(vec![
      (value & 0x3ff) as f32 / 1023.0,
      ((value >> 10) & 0x3ff) as f32 / 1023.0,
      ((value >> 20) & 0x3ff) as f32 / 1023.0,
      ((value >> 30) & 0x3) as f32 / 3.0,
    ]);
  }

  if upper.contains("_FLOAT") {
    if upper.contains("32") {
      return (0..count)
        .map(|i| read_f32(bytes, offset + i * 4))
        .collect::<napi::Result<Vec<_>>>();
    }
    if upper.contains("16") {
      return (0..count)
        .map(|i| read_u16(bytes, offset + i * 2).map(half_to_float))
        .collect::<napi::Result<Vec<_>>>();
    }
  }

  if upper.contains("_UNORM") {
    if upper.contains("16") {
      return (0..count)
        .map(|i| read_u16(bytes, offset + i * 2).map(|v| v as f32 / 65535.0))
        .collect::<napi::Result<Vec<_>>>();
    }
    if upper.contains("8") {
      return (0..count)
        .map(|i| read_u8(bytes, offset + i).map(|v| v as f32 / 255.0))
        .collect::<napi::Result<Vec<_>>>();
    }
  }

  if upper.contains("_SNORM") {
    if upper.contains("16") {
      return (0..count)
        .map(|i| read_i16(bytes, offset + i * 2).map(|v| (v as f32 / 32767.0).max(-1.0)))
        .collect::<napi::Result<Vec<_>>>();
    }
    if upper.contains("8") {
      return (0..count)
        .map(|i| read_i8(bytes, offset + i).map(|v| (v as f32 / 127.0).max(-1.0)))
        .collect::<napi::Result<Vec<_>>>();
    }
  }

  if upper.contains("_UINT") {
    if upper.contains("32") {
      return (0..count)
        .map(|i| read_u32(bytes, offset + i * 4).map(|v| v as f32))
        .collect::<napi::Result<Vec<_>>>();
    }
    if upper.contains("16") {
      return (0..count)
        .map(|i| read_u16(bytes, offset + i * 2).map(|v| v as f32))
        .collect::<napi::Result<Vec<_>>>();
    }
    if upper.contains("8") {
      return (0..count)
        .map(|i| read_u8(bytes, offset + i).map(|v| v as f32))
        .collect::<napi::Result<Vec<_>>>();
    }
  }

  if upper.contains("_SINT") {
    if upper.contains("32") {
      return (0..count)
        .map(|i| read_i32(bytes, offset + i * 4).map(|v| v as f32))
        .collect::<napi::Result<Vec<_>>>();
    }
    if upper.contains("16") {
      return (0..count)
        .map(|i| read_i16(bytes, offset + i * 2).map(|v| v as f32))
        .collect::<napi::Result<Vec<_>>>();
    }
    if upper.contains("8") {
      return (0..count)
        .map(|i| read_i8(bytes, offset + i).map(|v| v as f32))
        .collect::<napi::Result<Vec<_>>>();
    }
  }

  Err(napi::Error::from_reason(format!("Unsupported DXGI format: {format}")))
}

fn format_component_count(format: &str) -> usize {
  let normalized = format.trim_start_matches("DXGI_FORMAT_");
  let mut count = 0usize;
  let mut chars = normalized.chars().peekable();
  while let Some(char) = chars.next() {
    if matches!(char, 'R' | 'G' | 'B' | 'A') {
      let mut has_digits = false;
      while matches!(chars.peek(), Some(next) if next.is_ascii_digit()) {
        has_digits = true;
        chars.next();
      }
      if has_digits {
        count += 1;
      }
    }
  }
  if count == 0 { 1 } else { count }
}

fn half_to_float(value: u16) -> f32 {
  let sign = if value & 0x8000 != 0 { -1.0 } else { 1.0 };
  let exponent = ((value >> 10) & 0x1f) as i32;
  let fraction = (value & 0x03ff) as f32;
  if exponent == 0 {
    return sign * 2f32.powi(-14) * (fraction / 1024.0);
  }
  if exponent == 31 {
    return if fraction > 0.0 {
      f32::NAN
    } else {
      sign * f32::INFINITY
    };
  }
  sign * 2f32.powi(exponent - 15) * (1.0 + fraction / 1024.0)
}

fn read_u8(bytes: &[u8], offset: usize) -> napi::Result<u8> {
  bytes
    .get(offset)
    .copied()
    .ok_or_else(|| napi::Error::from_reason(format!("Out-of-bounds read at offset {offset}")))
}

fn read_i8(bytes: &[u8], offset: usize) -> napi::Result<i8> {
  read_u8(bytes, offset).map(|value| value as i8)
}

fn read_u16(bytes: &[u8], offset: usize) -> napi::Result<u16> {
  bytes
    .get(offset..offset + 2)
    .and_then(|slice| slice.try_into().ok())
    .map(u16::from_le_bytes)
    .ok_or_else(|| napi::Error::from_reason(format!("Out-of-bounds read at offset {offset}")))
}

fn read_i16(bytes: &[u8], offset: usize) -> napi::Result<i16> {
  read_u16(bytes, offset).map(|value| value as i16)
}

fn read_u32(bytes: &[u8], offset: usize) -> napi::Result<u32> {
  bytes
    .get(offset..offset + 4)
    .and_then(|slice| slice.try_into().ok())
    .map(u32::from_le_bytes)
    .ok_or_else(|| napi::Error::from_reason(format!("Out-of-bounds read at offset {offset}")))
}

fn read_i32(bytes: &[u8], offset: usize) -> napi::Result<i32> {
  read_u32(bytes, offset).map(|value| value as i32)
}

fn read_f32(bytes: &[u8], offset: usize) -> napi::Result<f32> {
  read_u32(bytes, offset).map(f32::from_bits)
}
