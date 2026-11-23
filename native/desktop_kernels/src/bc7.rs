use core::{arch::x86_64::*, mem, slice};

const WEIGHTS: [i32; 16] = [0, 4, 9, 13, 17, 21, 26, 30, 34, 38, 43, 47, 51, 55, 60, 64];
const FAST_ERROR_LIMIT: u32 = 16 * 4 * 32 * 32;
const FAST_PIXEL_ERROR_LIMIT: u32 = 4 * 48 * 48;
const COVERAGE_PENALTY: u32 = 1 << 18;

#[unsafe(no_mangle)]
pub extern "C" fn bc7_fast_supported() -> u32 {
    u32::from(is_x86_feature_detected!("avx2"))
}

/// # Safety
///
/// The caller provides disjoint input and output buffers of width * rows * 16 and
/// width * rows * 4 bytes. Each row contains four scanlines of RGBA8 pixels.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn bc7_fast_encode_rows(
    src: *const u8,
    dst: *mut u8,
    width: usize,
    rows: usize,
) -> u32 {
    if bc7_fast_supported() == 0 {
        return 1;
    }
    // SAFETY: The caller validates the dimensions and keeps both buffers pinned.
    let (src, dst) = unsafe {
        (
            slice::from_raw_parts(src, width * rows * 16),
            slice::from_raw_parts_mut(dst, width * rows * 4),
        )
    };
    // SAFETY: AVX2 was checked before constructing the borrowed slices.
    unsafe { encode(src, dst, width, rows) };
    0
}
#[target_feature(enable = "avx2")]
fn encode(src: &[u8], dst: &mut [u8], width: usize, rows: usize) {
    let stride = width * 4;
    for row in 0..rows {
        for x in 0..width / 4 {
            let start = row * 4 * stride + x * 16;
            let mut block = [[0u8; 4]; 16];
            for y in 0..4 {
                for i in 0..4 {
                    let at = start + y * stride + i * 4;
                    block[y * 4 + i].copy_from_slice(&src[at..at + 4]);
                }
            }
            let at = (row * (width / 4) + x) * 16;
            dst[at..at + 16].copy_from_slice(&encode_block(&block));
        }
    }
}

#[target_feature(enable = "avx2")]
fn orientation(block: &[[u8; 4]; 16]) -> [bool; 4] {
    let points = block.map(|p| _mm256_setr_pd(p[0].into(), p[1].into(), p[2].into(), p[3].into()));
    let mut sum = _mm256_setzero_pd();
    for &p in &points {
        sum = _mm256_add_pd(sum, p);
    }
    let mean = _mm256_mul_pd(sum, _mm256_set1_pd(1.0 / 16.0));
    let mut cov = [_mm256_setzero_pd(); 4];
    for &p in &points {
        let d = _mm256_sub_pd(p, mean);
        let ds: [f64; 4] = unsafe { mem::transmute(d) };
        for a in 0..4 {
            cov[a] = _mm256_add_pd(cov[a], _mm256_mul_pd(_mm256_set1_pd(ds[a]), d));
        }
    }
    let flat = cov.map(|r| unsafe { mem::transmute::<_, [f64; 4]>(r) });
    let mut start = 0;
    for c in 1..4 {
        if flat[c][c] > flat[start][start] {
            start = c;
        }
    }
    if flat[start][start] <= 0.0 {
        return [false; 4];
    }
    // Keep the Go encoder's summation order when choosing the endpoint orientation.
    let mut v = cov[start];
    for step in 0..9 {
        if step != 0 {
            let lanes: [f64; 4] = unsafe { mem::transmute(v) };
            let p = cov
                .map(|r| _mm256_mul_pd(r, _mm256_setr_pd(lanes[0], lanes[1], lanes[2], lanes[3])));
            let p = p.map(|r| unsafe { mem::transmute::<_, [f64; 4]>(r) });
            let dot = p.map(|a| ((0.0 + a[0]) + a[1]) + a[2] + a[3]);
            v = _mm256_setr_pd(dot[0], dot[1], dot[2], dot[3]);
        }
        let sq: [f64; 4] = unsafe { mem::transmute(_mm256_mul_pd(v, v)) };
        let norm = (sq[0] + sq[1] + sq[2] + sq[3]).sqrt();
        if !(norm > 0.0) {
            return [false; 4];
        }
        v = _mm256_div_pd(v, _mm256_set1_pd(norm));
    }
    let v: [f64; 4] = unsafe { mem::transmute(v) };
    v.map(|x| (x as f32) < 0.0)
}

#[target_feature(enable = "avx2")]
fn encode_block(block: &[[u8; 4]; 16]) -> [u8; 16] {
    let orient = orientation(block)
        .into_iter()
        .enumerate()
        .fold(0, |mask, (c, flip)| mask | (usize::from(flip) << c));
    let mut min = [255u8; 4];
    let mut max = [0u8; 4];
    for p in block {
        for c in 0..4 {
            min[c] = min[c].min(p[c]);
            max[c] = max[c].max(p[c]);
        }
    }
    let mut best = fit_orientation(block, min, max, orient);
    if best.error > FAST_ERROR_LIMIT || best.max_pixel_error > FAST_PIXEL_ERROR_LIMIT {
        let varying = (0..4).fold(0, |mask, c| mask | (usize::from(min[c] != max[c]) << c));
        // Complementary directions share a palette in reverse order.
        let initial = orient & varying;
        let initial = initial.min(initial ^ varying);
        for candidate in 0..16 {
            if candidate & !varying == 0 && candidate < candidate ^ varying && candidate != initial
            {
                let fit = fit_orientation(block, min, max, candidate);
                if fit.error < best.error {
                    best = fit;
                }
            }
        }
    }
    let Bc7Fit {
        mut e0,
        mut e1,
        mut p0,
        mut p1,
        mut indices,
        ..
    } = best;
    // The mode 6 anchor stores only three bits. Swapping the endpoints reverses the indices.
    if indices & 15 >= 8 {
        mem::swap(&mut e0, &mut e1);
        mem::swap(&mut p0, &mut p1);
        indices = !indices;
    }
    let mut packed = 64u128;
    for c in 0..4 {
        packed |= (e0[c] as u128) << (7 + 14 * c);
        packed |= (e1[c] as u128) << (14 + 14 * c);
    }
    packed |= (p0 as u128) << 63;
    packed |= (p1 as u128) << 64;
    packed |= ((indices & 7) as u128) << 65;
    packed |= ((indices >> 4) as u128) << 68;
    packed.to_le_bytes()
}

struct Bc7Fit {
    e0: [u8; 4],
    e1: [u8; 4],
    p0: u8,
    p1: u8,
    indices: u64,
    error: u32,
    max_pixel_error: u32,
}

#[target_feature(enable = "avx2")]
fn fit_orientation(
    block: &[[u8; 4]; 16],
    mut min: [u8; 4],
    mut max: [u8; 4],
    orient: usize,
) -> Bc7Fit {
    for c in 0..4 {
        if orient & (1 << c) != 0 {
            mem::swap(&mut min[c], &mut max[c]);
        }
    }
    let e0 = min.map(|v| v >> 1);
    let e1 = max.map(|v| v >> 1);
    let p0 = u8::from(min.iter().filter(|&&v| v & 1 != 0).count() > 2);
    let p1 = u8::from(max.iter().filter(|&&v| v & 1 != 0).count() > 2);
    let d0 = e0.map(|v| i32::from(v << 1 | p0));
    let d1 = e1.map(|v| i32::from(v << 1 | p1));
    let weight =
        [0, 1].map(|half| unsafe { _mm256_loadu_si256(WEIGHTS.as_ptr().add(8 * half).cast()) });
    let palette = weight.map(|w| {
        [0, 1, 2, 3].map(|c| {
            let start = _mm256_set1_epi32(64 * d0[c] + 32);
            let step = _mm256_mullo_epi32(w, _mm256_set1_epi32(d1[c] - d0[c]));
            _mm256_srai_epi32::<6>(_mm256_add_epi32(start, step))
        })
    });
    let cutoff = _mm256_set1_epi32(127);
    let visible = palette.map(|colors| _mm256_cmpgt_epi32(colors[3], cutoff));
    // This bit exceeds the maximum squared RGBA error (4 * 255^2).
    let coverage_penalty = _mm256_set1_epi32(COVERAGE_PENALTY as i32);
    let mut indices = 0u64;
    let mut error = 0;
    let mut max_pixel_error = 0;
    for (i, pixel) in block.iter().enumerate() {
        let pixel = pixel.map(|v| _mm256_set1_epi32(v.into()));
        let pixel_visible = _mm256_cmpgt_epi32(pixel[3], cutoff);
        let scores = [0, 1].map(|half| {
            let colors = palette[half];
            let mut sum = _mm256_setzero_si256();
            for c in 0..4 {
                let d = _mm256_sub_epi32(colors[c], pixel[c]);
                sum = _mm256_add_epi32(sum, _mm256_mullo_epi32(d, d));
            }
            let mismatch = _mm256_xor_si256(visible[half], pixel_visible);
            let sum = _mm256_or_si256(sum, _mm256_and_si256(mismatch, coverage_penalty));
            unsafe { mem::transmute::<_, [u32; 8]>(sum) }
        });
        let mut best = u32::MAX;
        let mut index = 0;
        for (k, score) in scores.into_iter().flatten().enumerate() {
            if score < best {
                best = score;
                index = k;
            }
        }
        indices |= (index as u64) << (i * 4);
        let pixel_error = best & (COVERAGE_PENALTY - 1);
        error += pixel_error;
        max_pixel_error = max_pixel_error.max(pixel_error);
    }
    Bc7Fit {
        e0,
        e1,
        p0,
        p1,
        indices,
        error,
        max_pixel_error,
    }
}
