//! BC1 to BC5 block encoders that reproduce ddsutil's Go encoders bit for bit.
//!
//! The endpoint search follows ddsutil's bcn_encode.go step by step, including its float32 arithmetic and
//! tie-breaking. Only the evaluation of a candidate over the 16 pixels uses AVX2.

use core::{arch::x86_64::*, mem, slice};

const QUALITY_FAST: u32 = 0;
const QUALITY_SLOW: u32 = 2;

const FORMAT_BC1: u32 = 1;
const FORMAT_BC2: u32 = 2;
const FORMAT_BC3: u32 = 3;
const FORMAT_BC4: u32 = 4;
const FORMAT_BC4_SIGNED: u32 = 5;
const FORMAT_BC5: u32 = 6;
const FORMAT_BC5_SIGNED: u32 = 7;

#[unsafe(no_mangle)]
pub extern "C" fn bcn_supported() -> u32 {
    u32::from(is_x86_feature_detected!("avx2"))
}

fn block_bytes(format: u32) -> Option<usize> {
    match format {
        FORMAT_BC1 | FORMAT_BC4 | FORMAT_BC4_SIGNED => Some(8),
        FORMAT_BC2 | FORMAT_BC3 | FORMAT_BC5 | FORMAT_BC5_SIGNED => Some(16),
        _ => None,
    }
}

/// # Safety
///
/// The caller provides disjoint input and output buffers of width * rows * 16 and
/// width / 4 * rows * (8 or 16, by format) bytes. Each row contains four scanlines of 8-bit RGBA pixels.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn bcn_encode_rows(
    format: u32,
    quality: u32,
    src: *const u8,
    dst: *mut u8,
    width: usize,
    rows: usize,
) -> u32 {
    if bcn_supported() == 0 {
        return 1;
    }
    let Some(size) = block_bytes(format) else {
        return 2;
    };
    if quality > QUALITY_SLOW || width % 4 != 0 {
        return 2;
    }
    // SAFETY: The caller validates the dimensions and keeps both buffers pinned.
    let (src, dst) = unsafe {
        (
            slice::from_raw_parts(src, width * rows * 16),
            slice::from_raw_parts_mut(dst, width / 4 * rows * size),
        )
    };
    // SAFETY: AVX2 was checked before constructing the borrowed slices.
    unsafe { encode(format, quality, src, dst, width, rows, size) };
    0
}

#[target_feature(enable = "avx2")]
fn encode(
    format: u32,
    quality: u32,
    src: &[u8],
    dst: &mut [u8],
    width: usize,
    rows: usize,
    size: usize,
) {
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
            let at = (row * (width / 4) + x) * size;
            let out = &mut dst[at..at + size];
            match format {
                FORMAT_BC1 => out.copy_from_slice(&compress_bc1(&block, quality).to_le_bytes()),
                FORMAT_BC2 => {
                    let mut alpha = 0u64;
                    for (i, pixel) in block.iter().enumerate() {
                        alpha |= u64::from((u32::from(pixel[3]) + 8) / 17) << (i * 4);
                    }
                    out[..8].copy_from_slice(&alpha.to_le_bytes());
                    out[8..].copy_from_slice(
                        &fit_bc1_color(&block, Layout::FourColor, quality).to_le_bytes(),
                    );
                }
                FORMAT_BC3 => {
                    let mut targets = [0i32; 16];
                    for (target, pixel) in targets.iter_mut().zip(&block) {
                        *target = i32::from(pixel[3]);
                    }
                    let fit = fit_alpha_endpoints(&targets, 0, 255, smooth_alpha_palette, quality);
                    out[..8].copy_from_slice(&pack_alpha_block(&fit));
                    out[8..].copy_from_slice(
                        &fit_bc1_color(&block, Layout::FourColor, quality).to_le_bytes(),
                    );
                }
                FORMAT_BC4 | FORMAT_BC4_SIGNED => {
                    out.copy_from_slice(&compress_bc4(
                        &block,
                        0,
                        format == FORMAT_BC4_SIGNED,
                        quality,
                    ));
                }
                _ => {
                    let signed = format == FORMAT_BC5_SIGNED;
                    out[..8].copy_from_slice(&compress_bc4(&block, 0, signed, quality));
                    out[8..].copy_from_slice(&compress_bc4(&block, 1, signed, quality));
                }
            }
        }
    }
}

// Go converts out-of-range floats to the minimum int64 on amd64; Rust saturates instead.
fn go_int(value: f64) -> i64 {
    if value.is_nan() || !(-9.223_372_036_854_776e18..9.223_372_036_854_776e18).contains(&value) {
        return i64::MIN;
    }
    value as i64
}

fn round_clamp(value: f32, max_value: i64) -> i64 {
    go_int((f64::from(value) + 0.5).floor()).clamp(0, max_value)
}

fn quantize(value: f32, max_value: i64) -> i64 {
    round_clamp(value * max_value as f32 / 255.0, max_value)
}

fn principal_axis(points: &[[f32; 4]], n: usize) -> ([f32; 4], [f32; 4]) {
    let mut mean = [0f32; 4];
    let mut axis = [0f32; 4];
    if points.is_empty() {
        return (mean, axis);
    }
    for p in points {
        for c in 0..n {
            mean[c] += p[c];
        }
    }
    for value in &mut mean[..n] {
        *value /= points.len() as f32;
    }

    let mut cov = [[0f64; 4]; 4];
    for p in points {
        for a in 0..n {
            let da = f64::from(p[a] - mean[a]);
            for b in 0..n {
                cov[a][b] += da * f64::from(p[b] - mean[b]);
            }
        }
    }

    let mut start = 0;
    for c in 1..n {
        if cov[c][c] > cov[start][start] {
            start = c;
        }
    }
    if cov[start][start] <= 0.0 {
        return (mean, axis);
    }
    let mut v = cov[start];
    if !normalize(&mut v, n) {
        return (mean, axis);
    }
    for _ in 0..8 {
        let mut next = [0f64; 4];
        for a in 0..n {
            for b in 0..n {
                next[a] += cov[a][b] * v[b];
            }
        }
        if !normalize(&mut next, n) {
            return (mean, axis);
        }
        v = next;
    }
    for c in 0..n {
        axis[c] = v[c] as f32;
    }
    (mean, axis)
}

fn normalize(v: &mut [f64; 4], n: usize) -> bool {
    let mut norm = 0f64;
    for value in &v[..n] {
        norm += value * value;
    }
    if !(norm > 0.0) || norm.is_infinite() {
        return false;
    }
    let norm = norm.sqrt();
    for value in &mut v[..n] {
        *value /= norm;
    }
    true
}

fn axis_extremes(points: &[[f32; 4]], n: usize) -> ([f32; 4], [f32; 4]) {
    let (mean, axis) = principal_axis(points, n);
    let (mut min_t, mut max_t) = (0f32, 0f32);
    for (i, p) in points.iter().enumerate() {
        let mut t = 0f32;
        for c in 0..n {
            t += (p[c] - mean[c]) * axis[c];
        }
        if i == 0 || t < min_t {
            min_t = t;
        }
        if i == 0 || t > max_t {
            max_t = t;
        }
    }
    let mut lo = [0f32; 4];
    let mut hi = [0f32; 4];
    for c in 0..n {
        lo[c] = mean[c] + min_t * axis[c];
        hi[c] = mean[c] + max_t * axis[c];
    }
    (lo, hi)
}

fn least_squares_endpoints(
    points: &[[f32; 4]],
    weights: &[f32],
    n: usize,
) -> Option<([f32; 4], [f32; 4])> {
    let (mut aa, mut ab, mut bb) = (0f32, 0f32, 0f32);
    let mut ax = [0f32; 4];
    let mut bx = [0f32; 4];
    for (p, &w) in points.iter().zip(weights) {
        let a = 1.0 - w;
        aa += a * a;
        ab += a * w;
        bb += w * w;
        for c in 0..n {
            ax[c] += a * p[c];
            bx[c] += w * p[c];
        }
    }
    let det = aa * bb - ab * ab;
    if det > -1e-6 && det < 1e-6 {
        return None;
    }
    let mut e0 = [0f32; 4];
    let mut e1 = [0f32; 4];
    for c in 0..n {
        e0[c] = (ax[c] * bb - bx[c] * ab) / det;
        e1[c] = (bx[c] * aa - ax[c] * ab) / det;
    }
    Some((e0, e1))
}

#[derive(Clone, Copy, Default, PartialEq)]
struct Rgb565 {
    r: u8,
    g: u8,
    b: u8,
}

impl Rgb565 {
    fn pack(self) -> u16 {
        u16::from(self.r) << 11 | u16::from(self.g) << 5 | u16::from(self.b)
    }

    fn expand(self) -> [u8; 3] {
        [
            ((u16::from(self.r) * 527 + 23) >> 6) as u8,
            ((u16::from(self.g) * 259 + 33) >> 6) as u8,
            ((u16::from(self.b) * 527 + 23) >> 6) as u8,
        ]
    }

    fn quantize(c: [f32; 4]) -> Self {
        Self {
            r: quantize(c[0], 31) as u8,
            g: quantize(c[1], 63) as u8,
            b: quantize(c[2], 31) as u8,
        }
    }
}

fn interpolate_color4(c0: Rgb565, c1: Rgb565) -> [[u8; 3]; 4] {
    let (r0, g0, b0) = (u32::from(c0.r), u32::from(c0.g), u32::from(c0.b));
    let (r1, g1, b1) = (u32::from(c1.r), u32::from(c1.g), u32::from(c1.b));
    [
        c0.expand(),
        c1.expand(),
        [
            (((2 * r0 + r1) * 351 + 61) >> 7) as u8,
            (((2 * g0 + g1) * 2763 + 1039) >> 11) as u8,
            (((2 * b0 + b1) * 351 + 61) >> 7) as u8,
        ],
        [
            (((r0 + 2 * r1) * 351 + 61) >> 7) as u8,
            (((g0 + 2 * g1) * 2763 + 1039) >> 11) as u8,
            (((b0 + 2 * b1) * 351 + 61) >> 7) as u8,
        ],
    ]
}

fn interpolate_color3(c0: Rgb565, c1: Rgb565) -> [[u8; 3]; 4] {
    let (r0, g0, b0) = (u32::from(c0.r), u32::from(c0.g), u32::from(c0.b));
    let (r1, g1, b1) = (u32::from(c1.r), u32::from(c1.g), u32::from(c1.b));
    [
        c0.expand(),
        c1.expand(),
        [
            (((r0 + r1) * 1053 + 125) >> 8) as u8,
            (((g0 + g1) * 4145 + 1019) >> 11) as u8,
            (((b0 + b1) * 1053 + 125) >> 8) as u8,
        ],
        [0, 0, 0],
    ]
}

#[derive(Clone, Copy, PartialEq)]
enum Layout {
    Opaque,
    Transparent,
    FourColor,
}

impl Layout {
    fn four_color(self, c0: Rgb565, c1: Rgb565) -> bool {
        self == Layout::FourColor || (self == Layout::Opaque && c0.pack() > c1.pack())
    }
}

#[derive(Clone, Copy)]
struct Bc1Fit {
    c0: Rgb565,
    c1: Rgb565,
    indices: [u8; 16],
    err: u64,
}

/// The pixels of a color block in the lanes that evaluate a candidate: 16-bit (r, g) and (b, 0) pairs, and the
/// 32-bit mask of the pixels that count.
struct Bc1Pixels<'a> {
    block: &'a [[u8; 4]; 16],
    layout: Layout,
    rg: [__m256i; 2],
    b: [__m256i; 2],
    counted: [__m256i; 2],
}

impl<'a> Bc1Pixels<'a> {
    #[target_feature(enable = "avx2")]
    fn new(block: &'a [[u8; 4]; 16], layout: Layout) -> Self {
        let mut rg = [[0i16; 16]; 2];
        let mut b = [[0i16; 16]; 2];
        let mut counted = [[0i32; 8]; 2];
        for (i, pixel) in block.iter().enumerate() {
            let (half, lane) = (i / 8, i % 8);
            rg[half][lane * 2] = i16::from(pixel[0]);
            rg[half][lane * 2 + 1] = i16::from(pixel[1]);
            b[half][lane * 2] = i16::from(pixel[2]);
            counted[half][lane] = if layout == Layout::Transparent && pixel[3] < 128 {
                0
            } else {
                -1
            };
        }
        // SAFETY: Both types are 32 bytes of plain integers.
        unsafe {
            Self {
                block,
                layout,
                rg: rg.map(|v| mem::transmute::<[i16; 16], __m256i>(v)),
                b: b.map(|v| mem::transmute::<[i16; 16], __m256i>(v)),
                counted: counted.map(|v| mem::transmute::<[i32; 8], __m256i>(v)),
            }
        }
    }

    /// Returns the error of (c0, c1) and the index of each pixel, or None if transparent pixels require the
    /// 3-color layout but c0 > c1.
    #[target_feature(enable = "avx2")]
    #[inline]
    fn evaluate(&self, c0: Rgb565, c1: Rgb565) -> Option<(u64, [__m256i; 2])> {
        let transparent = self.layout == Layout::Transparent;
        if transparent && c0.pack() > c1.pack() {
            return None;
        }
        let four_color = self.layout.four_color(c0, c1);
        let palette = if four_color {
            interpolate_color4(c0, c1)
        } else {
            interpolate_color3(c0, c1)
        };
        let entries = if four_color { 4 } else { 3 };

        let mut best = [_mm256_setzero_si256(); 2];
        let mut index = [_mm256_setzero_si256(); 2];
        for (k, color) in palette.iter().enumerate().take(entries) {
            let rg = _mm256_set1_epi32(i32::from(color[1]) << 16 | i32::from(color[0]));
            let b = _mm256_set1_epi32(i32::from(color[2]));
            let k_lanes = _mm256_set1_epi32(k as i32);
            for half in 0..2 {
                let drg = _mm256_sub_epi16(self.rg[half], rg);
                let db = _mm256_sub_epi16(self.b[half], b);
                let distance =
                    _mm256_add_epi32(_mm256_madd_epi16(drg, drg), _mm256_madd_epi16(db, db));
                if k == 0 {
                    best[half] = distance;
                } else {
                    let closer = _mm256_cmpgt_epi32(best[half], distance);
                    best[half] = _mm256_min_epi32(best[half], distance);
                    index[half] = _mm256_blendv_epi8(index[half], k_lanes, closer);
                }
            }
        }
        if transparent {
            let three = _mm256_set1_epi32(3);
            for half in 0..2 {
                best[half] = _mm256_and_si256(best[half], self.counted[half]);
                index[half] = _mm256_blendv_epi8(three, index[half], self.counted[half]);
            }
        }
        Some((horizontal_sum(_mm256_add_epi32(best[0], best[1])), index))
    }

    #[target_feature(enable = "avx2")]
    #[inline]
    fn consider(&self, best: &mut Bc1Fit, c0: Rgb565, c1: Rgb565) {
        if let Some((err, index)) = self.evaluate(c0, c1)
            && err < best.err
        {
            // SAFETY: Both types are 32 bytes of plain integers.
            let lanes = index.map(|v| unsafe { mem::transmute::<__m256i, [i32; 8]>(v) });
            let mut indices = [0u8; 16];
            for (i, value) in indices.iter_mut().enumerate() {
                *value = lanes[i / 8][i % 8] as u8;
            }
            *best = Bc1Fit {
                c0,
                c1,
                indices,
                err,
            };
        }
    }

    #[target_feature(enable = "avx2")]
    fn refine(&self, best: &mut Bc1Fit) {
        let mut points = [[0f32; 4]; 16];
        let mut weights = [0f32; 16];
        for _ in 0..4 {
            let four_color = self.layout.four_color(best.c0, best.c1);
            let mut n = 0;
            for (i, pixel) in self.block.iter().enumerate() {
                let w = match best.indices[i] {
                    0 => 0.0,
                    1 => 1.0,
                    2 if four_color => 1.0 / 3.0,
                    3 if four_color => 2.0 / 3.0,
                    2 => 0.5,
                    _ => continue,
                };
                points[n] = [
                    f32::from(pixel[0]),
                    f32::from(pixel[1]),
                    f32::from(pixel[2]),
                    0.0,
                ];
                weights[n] = w;
                n += 1;
            }
            let Some((e0, e1)) = least_squares_endpoints(&points[..n], &weights[..n], 3) else {
                return;
            };
            let (c0, c1) = (Rgb565::quantize(e0), Rgb565::quantize(e1));
            let previous = best.err;
            self.consider(best, c0, c1);
            self.consider(best, c1, c0);
            if best.err >= previous {
                return;
            }
        }
    }
}

#[target_feature(enable = "avx2")]
#[inline]
fn horizontal_sum(v: __m256i) -> u64 {
    let sum = _mm_add_epi32(_mm256_castsi256_si128(v), _mm256_extracti128_si256::<1>(v));
    let sum = _mm_add_epi32(sum, _mm_shuffle_epi32::<0b01_00_11_10>(sum));
    let sum = _mm_add_epi32(sum, _mm_shuffle_epi32::<0b10_11_00_01>(sum));
    u64::from(_mm_cvtsi128_si32(sum) as u32)
}

#[target_feature(enable = "avx2")]
fn fit_bc1_color(block: &[[u8; 4]; 16], layout: Layout, quality: u32) -> u64 {
    let pixels = Bc1Pixels::new(block, layout);
    let mut best = Bc1Fit {
        c0: Rgb565::default(),
        c1: Rgb565::default(),
        indices: [0; 16],
        err: u64::MAX,
    };

    let mut points = [[0f32; 4]; 16];
    let mut n = 0;
    for pixel in block {
        if layout != Layout::Transparent || pixel[3] >= 128 {
            points[n] = [
                f32::from(pixel[0]),
                f32::from(pixel[1]),
                f32::from(pixel[2]),
                0.0,
            ];
            n += 1;
        }
    }
    let (lo, hi) = axis_extremes(&points[..n], 3);
    let (c0, c1) = (Rgb565::quantize(hi), Rgb565::quantize(lo));
    pixels.consider(&mut best, c0, c1);
    pixels.consider(&mut best, c1, c0);
    if quality != QUALITY_FAST {
        pixels.refine(&mut best);
    }

    if quality == QUALITY_SLOW {
        let mut unique = [Rgb565::default(); 16];
        let mut n = 0;
        for pixel in block {
            let c = Rgb565 {
                r: pixel[0] >> 3,
                g: pixel[1] >> 2,
                b: pixel[2] >> 3,
            };
            if !unique[..n].contains(&c) {
                unique[n] = c;
                n += 1;
            }
        }
        for &c0 in &unique[..n] {
            for &c1 in &unique[..n] {
                pixels.consider(&mut best, c0, c1);
            }
        }
        pixels.refine(&mut best);
    }

    let mut packed = u64::from(best.c0.pack()) | u64::from(best.c1.pack()) << 16;
    for (i, &index) in best.indices.iter().enumerate() {
        packed |= u64::from(index & 3) << (32 + i * 2);
    }
    packed
}

#[target_feature(enable = "avx2")]
fn compress_bc1(block: &[[u8; 4]; 16], quality: u32) -> u64 {
    let layout = if block.iter().any(|p| p[3] < 128) {
        Layout::Transparent
    } else {
        Layout::Opaque
    };
    fit_bc1_color(block, layout, quality)
}

#[derive(Clone, Copy)]
struct AlphaFit {
    e0: i32,
    e1: i32,
    indices: u64,
    err: u64,
}

#[target_feature(enable = "avx2")]
#[inline]
fn consider_alpha(
    best: &mut AlphaFit,
    targets: __m256i,
    palette: fn(i32, i32) -> [i32; 8],
    a0: i32,
    a1: i32,
) {
    let values = palette(a0, a1);
    let mut nearest = _mm256_setzero_si256();
    let mut index = _mm256_setzero_si256();
    for (k, &value) in values.iter().enumerate() {
        let distance = _mm256_abs_epi16(_mm256_sub_epi16(_mm256_set1_epi16(value as i16), targets));
        if k == 0 {
            nearest = distance;
        } else {
            let closer = _mm256_cmpgt_epi16(nearest, distance);
            nearest = _mm256_min_epi16(nearest, distance);
            index = _mm256_blendv_epi8(index, _mm256_set1_epi16(k as i16), closer);
        }
    }
    let err = horizontal_sum(_mm256_madd_epi16(nearest, nearest));
    if err < best.err {
        // SAFETY: Both types are 32 bytes of plain integers.
        let lanes: [i16; 16] = unsafe { mem::transmute(index) };
        let mut indices = 0u64;
        for (i, &k) in lanes.iter().enumerate() {
            indices |= (k as u64) << (i * 3);
        }
        *best = AlphaFit {
            e0: a0,
            e1: a1,
            indices,
            err,
        };
    }
}

#[target_feature(enable = "avx2")]
fn fit_alpha_endpoints(
    targets: &[i32; 16],
    lo: i32,
    hi: i32,
    palette: fn(i32, i32) -> [i32; 8],
    quality: u32,
) -> AlphaFit {
    let mut best = AlphaFit {
        e0: 0,
        e1: 0,
        indices: 0,
        err: u64::MAX,
    };
    // SAFETY: Both types are 32 bytes of plain integers.
    let lanes: __m256i = unsafe { mem::transmute(targets.map(|v| v as i16)) };

    let (min_v, max_v) = targets[1..]
        .iter()
        .fold((targets[0], targets[0]), |(lo, hi), &v| {
            (lo.min(v), hi.max(v))
        });
    consider_alpha(&mut best, lanes, palette, max_v, min_v);
    if best.err == 0 {
        return best;
    }
    let (mut inner_min, mut inner_max) = (hi, lo);
    for &v in targets {
        if v != lo && v != hi {
            inner_min = inner_min.min(v);
            inner_max = inner_max.max(v);
        }
    }
    if inner_min <= inner_max {
        consider_alpha(&mut best, lanes, palette, inner_min, inner_max);
    }
    if best.err == 0 || quality == QUALITY_FAST {
        return best;
    }

    let window = if quality == QUALITY_SLOW { 4 } else { 2 };
    for d0 in -window..=window {
        for d1 in -window..=window {
            let a0 = (max_v + d0).max(lo).min(hi);
            let a1 = (min_v + d1).max(lo).min(hi);
            consider_alpha(&mut best, lanes, palette, a0, a1);
            consider_alpha(&mut best, lanes, palette, a1, a0);
        }
    }

    if quality == QUALITY_SLOW {
        let mut unique = [0i32; 16];
        let mut n = 0;
        for &v in targets {
            if !unique[..n].contains(&v) {
                unique[n] = v;
                n += 1;
            }
        }
        for &a0 in &unique[..n] {
            for &a1 in &unique[..n] {
                consider_alpha(&mut best, lanes, palette, a0, a1);
            }
        }
    }
    best
}

fn pack_alpha_block(fit: &AlphaFit) -> [u8; 8] {
    let mut out = [0u8; 8];
    out[0] = fit.e0 as u8;
    out[1] = fit.e1 as u8;
    out[2..].copy_from_slice(&fit.indices.to_le_bytes()[..6]);
    out
}

fn smooth_alpha_palette(a0: i32, a1: i32) -> [i32; 8] {
    let (a0, a1) = (a0 as u32, a1 as u32);
    let levels = if a0 > a1 {
        [
            a0,
            a1,
            (6 * a0 + a1 + 1) / 7,
            (5 * a0 + 2 * a1 + 1) / 7,
            (4 * a0 + 3 * a1 + 1) / 7,
            (3 * a0 + 4 * a1 + 1) / 7,
            (2 * a0 + 5 * a1 + 1) / 7,
            (a0 + 6 * a1 + 1) / 7,
        ]
    } else {
        [
            a0,
            a1,
            (4 * a0 + a1 + 1) / 5,
            (3 * a0 + 2 * a1 + 1) / 5,
            (2 * a0 + 3 * a1 + 1) / 5,
            (a0 + 4 * a1 + 1) / 5,
            0,
            255,
        ]
    };
    levels.map(|v| v as i32)
}

fn interpolate_bc4(a0: i32, a1: i32, signed: bool) -> [i32; 8] {
    const W4: [i32; 4] = [13107, 26215, 39321, 52429];
    const W6: [i32; 6] = [9363, 18724, 28086, 37450, 46812, 56173];
    if a0 > a1 {
        [
            a0,
            a1,
            (W6[5] * a0 + W6[0] * a1 + 32768) >> 16,
            (W6[4] * a0 + W6[1] * a1 + 32768) >> 16,
            (W6[3] * a0 + W6[2] * a1 + 32768) >> 16,
            (W6[2] * a0 + W6[3] * a1 + 32768) >> 16,
            (W6[1] * a0 + W6[4] * a1 + 32768) >> 16,
            (W6[0] * a0 + W6[5] * a1 + 32768) >> 16,
        ]
    } else {
        [
            a0,
            a1,
            (W4[3] * a0 + W4[0] * a1 + 32768) >> 16,
            (W4[2] * a0 + W4[1] * a1 + 32768) >> 16,
            (W4[1] * a0 + W4[2] * a1 + 32768) >> 16,
            (W4[0] * a0 + W4[3] * a1 + 32768) >> 16,
            if signed { -127 } else { 0 },
            if signed { 127 } else { 255 },
        ]
    }
}

fn bc4_unsigned_palette(a0: i32, a1: i32) -> [i32; 8] {
    interpolate_bc4(a0, a1, false)
}

fn bc4_signed_palette(a0: i32, a1: i32) -> [i32; 8] {
    interpolate_bc4(a0, a1, true)
}

/// Encodes one channel of the block. Signed blocks hold snorm8 bit patterns.
#[target_feature(enable = "avx2")]
fn compress_bc4(block: &[[u8; 4]; 16], channel: usize, signed: bool, quality: u32) -> [u8; 8] {
    let mut targets = [0i32; 16];
    for (target, pixel) in targets.iter_mut().zip(block) {
        *target = if signed {
            i32::from(pixel[channel] as i8).max(-127)
        } else {
            i32::from(pixel[channel])
        };
    }
    let fit = if signed {
        fit_alpha_endpoints(&targets, -127, 127, bc4_signed_palette, quality)
    } else {
        fit_alpha_endpoints(&targets, 0, 255, bc4_unsigned_palette, quality)
    };
    pack_alpha_block(&fit)
}
