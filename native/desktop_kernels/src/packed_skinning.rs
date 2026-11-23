use core::arch::x86_64::*;
use core::slice;

#[repr(C)]
pub struct FrameArgs {
    base: *const f32,
    weights: *const f32,
    bones: *const i32,
    palette0: *const f32,
    palette1: *const f32,
    positions: *mut f32,
    normals: *mut f32,
    vertex_count: usize,
    bone_count: usize,
    fraction: f64,
}

#[unsafe(no_mangle)]
pub extern "C" fn packed_skinning_supported() -> u32 {
    u32::from(is_x86_feature_detected!("avx") && is_x86_feature_detected!("f16c"))
}

/// # Safety
///
/// The synchronous caller supplies disjoint output buffers and pins all seven buffers across this call.
/// Each vertex has seven base floats and four prevalidated bone indices and weights.
/// Each selected palette has bone_count records of 14 floats.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn packed_skinning_frame(args: *const FrameArgs) -> isize {
    if packed_skinning_supported() == 0 {
        return -1;
    }
    // SAFETY: The Go binding validates the sizes and keeps the buffers pinned during the call.
    unsafe { skin_frame(&*args) }
}

#[target_feature(enable = "avx,f16c")]
fn skin_frame(args: &FrameArgs) -> isize {
    // SAFETY: Input lengths, bone indices and buffer lifetimes are part of FrameArgs' contract.
    let (base, weights, bones, palettes, positions, normals) = unsafe {
        (
            slice::from_raw_parts(args.base, args.vertex_count * 7),
            slice::from_raw_parts(args.weights, args.vertex_count * 4),
            slice::from_raw_parts(args.bones, args.vertex_count * 4),
            [args.palette0, args.palette1],
            slice::from_raw_parts_mut(args.positions, args.vertex_count * 3),
            slice::from_raw_parts_mut(args.normals, args.vertex_count * 3),
        )
    };
    let zero = _mm256_setzero_pd();
    let position_limit = _mm_set1_ps(65_520.0);
    let position_abs_mask = _mm_castsi128_ps(_mm_set1_epi32(0x7fff_ffff));

    for (vertex, ((source, weights), bones)) in base
        .chunks_exact(7)
        .zip(weights.chunks_exact(4))
        .zip(bones.chunks_exact(4))
        .enumerate()
    {
        let mut scale_bias0 = zero;
        let mut scale_bias1 = zero;
        let mut real = zero;
        let mut dual = zero;
        // SAFETY: The selected records exist and all four vector loads fit in a 14-float record.
        unsafe {
            let reference =
                _mm256_cvtps_pd(_mm_loadu_ps(palettes[0].add(bones[0] as usize * 14 + 6)));
            for influence in 0..4 {
                let offset = bones[influence] as usize * 14;
                debug_assert!(offset < args.bone_count * 14);
                let weight = f64::from(weights[influence]);
                let factors = [weight * (1.0 - args.fraction), weight * args.fraction];
                for sample in 0..2 {
                    let record = palettes[sample].add(offset);
                    let rotation = _mm256_cvtps_pd(_mm_loadu_ps(record.add(6)));
                    let products = lanes(_mm256_mul_pd(reference, rotation));
                    let dot = products[0] + products[1] + products[2] + products[3];
                    let weight = _mm256_set1_pd(factors[sample]);
                    let signed = _mm256_set1_pd(if dot < 0.0 {
                        -factors[sample]
                    } else {
                        factors[sample]
                    });
                    scale_bias0 = _mm256_add_pd(
                        scale_bias0,
                        _mm256_mul_pd(_mm256_cvtps_pd(_mm_loadu_ps(record)), weight),
                    );
                    scale_bias1 = _mm256_add_pd(
                        scale_bias1,
                        _mm256_mul_pd(_mm256_cvtps_pd(_mm_loadu_ps(record.add(4))), weight),
                    );
                    real = _mm256_add_pd(real, _mm256_mul_pd(rotation, signed));
                    dual = _mm256_add_pd(
                        dual,
                        _mm256_mul_pd(_mm256_cvtps_pd(_mm_loadu_ps(record.add(10))), signed),
                    );
                }
            }
        }

        let scale_bias0 = lanes(scale_bias0);
        let scale_bias1 = lanes(scale_bias1);
        let real_parts = lanes(real);
        let dual_parts = lanes(dual);
        let [qx, qy, qz, qw] = real_parts;
        let length = (qx * qx + qy * qy + qz * qz + qw * qw).sqrt();
        if length < 1e-8
            || scale_bias0
                .iter()
                .chain(&scale_bias1[..2])
                .chain(&real_parts)
                .chain(&dual_parts)
                .any(|v| !v.is_finite())
        {
            return (vertex + 1) as isize;
        }
        let divisor = _mm256_set1_pd(length);
        let (matrix, translation) = transform(
            lanes(_mm256_div_pd(real, divisor)),
            lanes(_mm256_div_pd(dual, divisor)),
        );

        let [x, y, z] = rotate(
            matrix,
            f64::from(source[0]) * scale_bias0[0] + scale_bias0[3],
            -f64::from(source[2]) * scale_bias0[1] + scale_bias1[0],
            f64::from(source[1]) * scale_bias0[2] + scale_bias1[1],
        );
        let position = _mm_setr_ps(
            (x + translation[0]) as f32,
            (z + translation[2]) as f32,
            -(y + translation[1]) as f32,
            0.0,
        );
        if _mm_movemask_ps(_mm_cmplt_ps(
            _mm_and_ps(position, position_abs_mask),
            position_limit,
        )) != 15
        {
            return -2;
        }

        let half = _mm_cvtps_ph::<{ _MM_FROUND_TO_NEAREST_INT }>(position);
        let rounded = _mm_cvtph_ps(half);
        let [nx, ny, nz] = rotate(
            matrix,
            f64::from(source[4]),
            -f64::from(source[6]),
            f64::from(source[5]),
        );
        let normal = _mm256_round_pd::<{ _MM_FROUND_TO_ZERO | _MM_FROUND_NO_EXC }>(_mm256_setr_pd(
            nx, nz, -ny, 0.0,
        ));
        let clamped = _mm256_max_pd(
            _mm256_set1_pd(-128.0),
            _mm256_min_pd(_mm256_set1_pd(127.0), normal),
        );
        let normal = _mm256_cvtpd_ps(clamped);
        let mut p = [0.0f32; 4];
        let mut n = [0.0f32; 4];
        // SAFETY: Each local array contains four initialized, writable floats.
        unsafe {
            _mm_storeu_ps(p.as_mut_ptr(), rounded);
            _mm_storeu_ps(n.as_mut_ptr(), normal);
        }
        let (nx, ny, nz) = (f64::from(n[0]), f64::from(n[1]), f64::from(n[2]));
        let length = (nx * nx + ny * ny + nz * nz).sqrt();
        if length > 1e-8 {
            n = [
                (nx / length) as f32,
                (ny / length) as f32,
                (nz / length) as f32,
                0.0,
            ];
        }
        let start = vertex * 3;
        positions[start..start + 3].copy_from_slice(&p[..3]);
        normals[start..start + 3].copy_from_slice(&n[..3]);
    }

    0
}

#[inline]
#[target_feature(enable = "avx")]
fn lanes(value: __m256d) -> [f64; 4] {
    let mut result = [0.0; 4];
    // SAFETY: The destination array has four writable doubles.
    unsafe { _mm256_storeu_pd(result.as_mut_ptr(), value) };
    result
}

#[inline]
fn transform([qx, qy, qz, qw]: [f64; 4], [dx, dy, dz, dw]: [f64; 4]) -> ([f64; 9], [f64; 3]) {
    (
        [
            1.0 - 2.0 * qy * qy - 2.0 * qz * qz,
            2.0 * (qx * qy - qw * qz),
            2.0 * (qx * qz + qw * qy),
            2.0 * (qx * qy + qw * qz),
            1.0 - 2.0 * qx * qx - 2.0 * qz * qz,
            2.0 * (qy * qz - qw * qx),
            2.0 * (qx * qz - qw * qy),
            2.0 * (qy * qz + qw * qx),
            1.0 - 2.0 * qx * qx - 2.0 * qy * qy,
        ],
        [
            2.0 * (-dw * qx + dx * qw - dy * qz + dz * qy),
            2.0 * (-dw * qy + dx * qz + dy * qw - dz * qx),
            2.0 * (-dw * qz - dx * qy + dy * qx + dz * qw),
        ],
    )
}

#[inline]
fn rotate(m: [f64; 9], x: f64, y: f64, z: f64) -> [f64; 3] {
    [
        m[0] * x + m[1] * y + m[2] * z,
        m[3] * x + m[4] * y + m[5] * z,
        m[6] * x + m[7] * y + m[8] * z,
    ]
}
