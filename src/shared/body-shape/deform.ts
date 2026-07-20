/**
 * Weighted anisotropic scale around a pivot, computed from original positions.
 * Always recompute preview from originals — never accumulate on deformed mesh.
 *
 * p' = pivot + (original - pivot) * (1 + amount * axisScale * weight)
 */
export function anisotropicScaleFromOriginal(options: {
    originalPositions: Float32Array;
    previewPositions: Float32Array;
    weights: Float32Array;
    pivot: readonly [number, number, number];
    /** Overall strength multiplier (e.g. 0.2 = up to +20% when weight and axis are 1). */
    amount: number;
    /** Per-axis relative scale factors. */
    axisScale: readonly [number, number, number];
}): void {
    const { originalPositions, previewPositions, weights, pivot, amount, axisScale } = options;
    const vertexCount = Math.min(
        weights.length,
        Math.floor(originalPositions.length / 3),
        Math.floor(previewPositions.length / 3),
    );

    const px = pivot[0];
    const py = pivot[1];
    const pz = pivot[2];
    const sx = axisScale[0];
    const sy = axisScale[1];
    const sz = axisScale[2];

    for (let i = 0; i < vertexCount; i++) {
        const o = i * 3;
        const w = weights[i];
        if (w <= 0 || amount === 0) {
            previewPositions[o] = originalPositions[o];
            previewPositions[o + 1] = originalPositions[o + 1];
            previewPositions[o + 2] = originalPositions[o + 2];
            continue;
        }

        const factorX = 1 + amount * sx * w;
        const factorY = 1 + amount * sy * w;
        const factorZ = 1 + amount * sz * w;

        previewPositions[o] = px + (originalPositions[o] - px) * factorX;
        previewPositions[o + 1] = py + (originalPositions[o + 1] - py) * factorY;
        previewPositions[o + 2] = pz + (originalPositions[o + 2] - pz) * factorZ;
    }
}

/** Effective per-vertex scale factors for normal/tangent correction: A_eff = I + w(A - I). */
export function effectiveScaleFactors(
    weight: number,
    amount: number,
    axisScale: readonly [number, number, number],
): [number, number, number] {
    return [
        1 + amount * axisScale[0] * weight,
        1 + amount * axisScale[1] * weight,
        1 + amount * axisScale[2] * weight,
    ];
}

/**
 * Correct SNORM8 tangent (bytes 0–2) and normal (bytes 4–6) for non-uniform scale.
 * bytes layout: [tx, ty, tz, tw, nx, ny, nz, nw] as int8.
 * Unknown layouts must not call this — leave vectors untouched.
 */
export function correctSnorm8TangentNormal(
    vectors: Int8Array,
    vertexIndex: number,
    scales: readonly [number, number, number],
): void {
    const base = vertexIndex * 8;
    if (base + 7 >= vectors.length) return;

    const [sx, sy, sz] = scales;
    if (Math.abs(sx - 1) < 1e-8 && Math.abs(sy - 1) < 1e-8 && Math.abs(sz - 1) < 1e-8) return;

    let tx = vectors[base] / 127;
    let ty = vectors[base + 1] / 127;
    let tz = vectors[base + 2] / 127;
    let nx = vectors[base + 4] / 127;
    let ny = vectors[base + 5] / 127;
    let nz = vectors[base + 6] / 127;

    // T' = normalize(A T)
    tx *= sx;
    ty *= sy;
    tz *= sz;
    const tLen = Math.hypot(tx, ty, tz);
    if (tLen > 1e-8) {
        tx /= tLen;
        ty /= tLen;
        tz /= tLen;
    }

    // N' = normalize((A^-1)^T N) for diagonal A
    nx /= sx || 1;
    ny /= sy || 1;
    nz /= sz || 1;
    const nLen = Math.hypot(nx, ny, nz);
    if (nLen > 1e-8) {
        nx /= nLen;
        ny /= nLen;
        nz /= nLen;
    }

    // Gram-Schmidt: orthogonalize tangent to normal
    const dot = tx * nx + ty * ny + tz * nz;
    tx -= nx * dot;
    ty -= ny * dot;
    tz -= nz * dot;
    const tLen2 = Math.hypot(tx, ty, tz);
    if (tLen2 > 1e-8) {
        tx /= tLen2;
        ty /= tLen2;
        tz /= tLen2;
    }

    vectors[base] = clampSnorm8(tx);
    vectors[base + 1] = clampSnorm8(ty);
    vectors[base + 2] = clampSnorm8(tz);
    vectors[base + 4] = clampSnorm8(nx);
    vectors[base + 5] = clampSnorm8(ny);
    vectors[base + 6] = clampSnorm8(nz);
}

function clampSnorm8(value: number): number {
    const rounded = Math.round(value * 127);
    if (rounded < -127) return -127;
    if (rounded > 127) return 127;
    return rounded;
}

export function computeWeightedPivot(
    positions: Float32Array,
    weights: Float32Array,
): [number, number, number] {
    let sx = 0;
    let sy = 0;
    let sz = 0;
    let total = 0;
    const count = Math.min(weights.length, Math.floor(positions.length / 3));

    for (let i = 0; i < count; i++) {
        const w = weights[i];
        if (w <= 0) continue;
        const o = i * 3;
        sx += positions[o] * w;
        sy += positions[o + 1] * w;
        sz += positions[o + 2] * w;
        total += w;
    }

    if (total <= 0) return computeBoundingCenter(positions);
    return [sx / total, sy / total, sz / total];
}

export function computeBoundingCenter(positions: Float32Array): [number, number, number] {
    const count = Math.floor(positions.length / 3);
    if (count === 0) return [0, 0, 0];

    let minX = Infinity;
    let minY = Infinity;
    let minZ = Infinity;
    let maxX = -Infinity;
    let maxY = -Infinity;
    let maxZ = -Infinity;

    for (let i = 0; i < count; i++) {
        const o = i * 3;
        const x = positions[o];
        const y = positions[o + 1];
        const z = positions[o + 2];
        if (x < minX) minX = x;
        if (y < minY) minY = y;
        if (z < minZ) minZ = z;
        if (x > maxX) maxX = x;
        if (y > maxY) maxY = y;
        if (z > maxZ) maxZ = z;
    }

    return [(minX + maxX) / 2, (minY + maxY) / 2, (minZ + maxZ) / 2];
}

export function displacementMetrics(
    originalPositions: Float32Array,
    previewPositions: Float32Array,
    epsilon = 1e-6,
): {
    vertexCount: number;
    movedVertices: number;
    maxDisplacement: number;
    meanDisplacement: number;
} {
    const vertexCount = Math.min(
        Math.floor(originalPositions.length / 3),
        Math.floor(previewPositions.length / 3),
    );
    let movedVertices = 0;
    let maxDisplacement = 0;
    let sum = 0;

    for (let i = 0; i < vertexCount; i++) {
        const o = i * 3;
        const dx = previewPositions[o] - originalPositions[o];
        const dy = previewPositions[o + 1] - originalPositions[o + 1];
        const dz = previewPositions[o + 2] - originalPositions[o + 2];
        const dist = Math.hypot(dx, dy, dz);
        if (dist > epsilon) {
            movedVertices += 1;
            sum += dist;
            if (dist > maxDisplacement) maxDisplacement = dist;
        }
    }

    return {
        vertexCount,
        movedVertices,
        maxDisplacement,
        meanDisplacement: movedVertices > 0 ? sum / movedVertices : 0,
    };
}
