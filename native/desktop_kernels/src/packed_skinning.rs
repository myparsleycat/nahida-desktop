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

#[repr(C)]
pub struct ShapePass {
    target: *const f32,
    weight: f64,
}

const LEGACY: u32 = 0;
const OBJECT: u32 = 1;
const OBJECT_DIRECT: u32 = 2;
const EFMI_TYPED: u32 = 3;
const EFMI_COMPACT: u32 = 4;

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
    unsafe { skin_frame::<LEGACY>(&*args, &[]) }
}

/// # Safety
///
/// The caller must satisfy the buffer contract of `packed_skinning_frame`.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn packed_skinning_variant_frame(
    args: *const FrameArgs,
    variant: u32,
) -> isize {
    if packed_skinning_supported() == 0 {
        return -1;
    }
    // SAFETY: Both variants borrow the same prevalidated buffers as the legacy kernel.
    unsafe {
        match variant {
            OBJECT => skin_frame::<OBJECT>(&*args, &[]),
            OBJECT_DIRECT => skin_frame::<OBJECT_DIRECT>(&*args, &[]),
            _ => -1,
        }
    }
}

/// # Safety
///
/// In addition to `packed_skinning_frame`'s contract, a nonempty shape array and its targets
/// must stay pinned through the call. Each target has seven floats per vertex, like the base.
#[unsafe(no_mangle)]
pub unsafe extern "C" fn packed_skinning_efmi_frame(
    args: *const FrameArgs,
    variant: u32,
    shapes: *const ShapePass,
    shape_count: usize,
) -> isize {
    if packed_skinning_supported() == 0 {
        return -1;
    }
    // SAFETY: The Go binding keeps these arrays pinned and the target lengths match the base.
    unsafe {
        let shapes = if shape_count == 0 {
            &[]
        } else {
            slice::from_raw_parts(shapes, shape_count)
        };
        match variant {
            EFMI_TYPED => skin_frame::<EFMI_TYPED>(&*args, shapes),
            EFMI_COMPACT => skin_frame::<EFMI_COMPACT>(&*args, shapes),
            _ => -1,
        }
    }
}

#[target_feature(enable = "avx,f16c")]
fn skin_frame<const VARIANT: u32>(args: &FrameArgs, shapes: &[ShapePass]) -> isize {
    let efmi = VARIANT == EFMI_TYPED || VARIANT == EFMI_COMPACT;
    let direct = VARIANT == OBJECT_DIRECT;
    let unsigned_seed = direct || efmi;
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
                    let signed = _mm256_set1_pd(
                        if VARIANT != LEGACY && (!unsigned_seed || influence != 0 || sample != 0) {
                            let sign = if dot > 0.0 {
                                1.0
                            } else if dot < 0.0 {
                                -1.0
                            } else {
                                dot
                            };
                            factors[sample] * sign
                        } else if dot < 0.0 {
                            -factors[sample]
                        } else {
                            factors[sample]
                        },
                    );
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
        if (!unsigned_seed && length < 1e-8)
            || scale_bias0
                .iter()
                .chain(&scale_bias1[..2])
                .chain(&real_parts)
                .chain(&dual_parts)
                .any(|v| !v.is_finite())
        {
            return (vertex + 1) as isize;
        }
        let divisor = _mm256_set1_pd(if unsigned_seed {
            length.max(1e-6)
        } else {
            length
        });
        let (matrix, translation) = transform(
            lanes(_mm256_div_pd(real, divisor)),
            lanes(_mm256_div_pd(dual, divisor)),
        );

        let (base_x, base_y, base_z) = (
            f64::from(source[0]),
            f64::from(source[1]),
            f64::from(source[2]),
        );
        let (mut x, mut y, mut z) = (base_x, base_y, base_z);
        if efmi {
            for shape in shapes {
                // SAFETY: Each target has the same seven-float vertex layout and count as base.
                let target = unsafe { slice::from_raw_parts(shape.target.add(vertex * 7), 3) };
                x += (f64::from(target[0]) - base_x) * shape.weight;
                y += (f64::from(target[1]) - base_y) * shape.weight;
                z += (f64::from(target[2]) - base_z) * shape.weight;
            }
        }
        let (y, z) = if direct {
            (y, z)
        } else if efmi {
            (z, -y)
        } else {
            (-z, y)
        };
        let [x, y, z] = rotate(
            matrix,
            x * scale_bias0[0] + scale_bias0[3],
            y * scale_bias0[1] + scale_bias1[0],
            z * scale_bias0[2] + scale_bias1[1],
        );
        let (y, z) = (y + translation[1], z + translation[2]);
        let (y, z) = if direct {
            (y, z)
        } else if efmi {
            (-z, y)
        } else {
            (z, -y)
        };
        let position = _mm_setr_ps((x + translation[0]) as f32, y as f32, z as f32, 0.0);
        let position = if efmi {
            position
        } else {
            if _mm_movemask_ps(_mm_cmplt_ps(
                _mm_and_ps(position, position_abs_mask),
                position_limit,
            )) != 15
            {
                return -2;
            }
            _mm_cvtph_ps(_mm_cvtps_ph::<{ _MM_FROUND_TO_NEAREST_INT }>(position))
        };

        let (ny, nz) = if VARIANT == EFMI_COMPACT {
            (f64::from(source[6]), -f64::from(source[5]))
        } else {
            (-f64::from(source[6]), f64::from(source[5]))
        };
        let [nx, ny, nz] = rotate(matrix, f64::from(source[4]), ny, nz);
        let (ny, nz) = if direct {
            (ny, nz)
        } else if VARIANT == EFMI_COMPACT {
            (-nz, ny)
        } else {
            (nz, -ny)
        };
        let normal = _mm256_setr_pd(nx, ny, nz, 0.0);
        let normal = if efmi {
            normal
        } else {
            let rounded = _mm256_round_pd::<{ _MM_FROUND_TO_ZERO | _MM_FROUND_NO_EXC }>(normal);
            _mm256_max_pd(
                _mm256_set1_pd(-128.0),
                _mm256_min_pd(_mm256_set1_pd(127.0), rounded),
            )
        };
        let normal = _mm256_cvtpd_ps(normal);
        let mut p = [0.0f32; 4];
        let mut n = [0.0f32; 4];
        // SAFETY: Each local array contains four initialized, writable floats.
        unsafe {
            _mm_storeu_ps(p.as_mut_ptr(), position);
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
