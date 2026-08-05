/** Clamp value to [0, 1]. */
export function clamp01(value: number): number {
    if (value <= 0) return 0;
    if (value >= 1) return 1;
    return value;
}

/**
 * Smooth falloff for a surface brush.
 * hardness > 1 sharpens the core; hardness < 1 softens it.
 */
export function brushFalloff(distance: number, radius: number, hardness = 1): number {
    if (radius <= 0 || distance >= radius) return 0;
    if (distance <= 0) return 1;

    const normalized = clamp01(distance / radius);
    const smooth = 1 - normalized * normalized * (3 - 2 * normalized);
    if (hardness === 1) return smooth;
    return Math.pow(smooth, Math.max(0.01, hardness));
}

export function paintVertex(currentWeight: number, brushWeight: number, strength: number): number {
    return clamp01(currentWeight + brushWeight * strength);
}

export function eraseVertex(currentWeight: number, brushWeight: number, strength: number): number {
    return clamp01(currentWeight - brushWeight * strength);
}

export type BrushMode = "paint" | "erase";

/**
 * Apply a brush stroke to vertices near hitPoint.
 * Mutates `weights` in place and returns the count of vertices that changed.
 */
export function applyBrushStroke(options: {
    positions: Float32Array;
    weights: Float32Array;
    hitPoint: readonly [number, number, number];
    radius: number;
    strength: number;
    hardness?: number;
    mode: BrushMode;
    /** Optional front-face filter: skip vertices whose normal opposes the hit normal. */
    normals?: Float32Array;
    hitNormal?: readonly [number, number, number];
    normalThreshold?: number;
}): number {
    const {
        positions,
        weights,
        hitPoint,
        radius,
        strength,
        hardness = 1,
        mode,
        normals,
        hitNormal,
        normalThreshold = 0.15,
    } = options;

    const vertexCount = Math.min(weights.length, Math.floor(positions.length / 3));
    let changed = 0;
    const hx = hitPoint[0];
    const hy = hitPoint[1];
    const hz = hitPoint[2];
    const r2 = radius * radius;

    for (let i = 0; i < vertexCount; i++) {
        const ox = i * 3;
        const dx = positions[ox] - hx;
        const dy = positions[ox + 1] - hy;
        const dz = positions[ox + 2] - hz;
        const dist2 = dx * dx + dy * dy + dz * dz;
        if (dist2 > r2) continue;

        if (normals && hitNormal) {
            const nx = normals[ox];
            const ny = normals[ox + 1];
            const nz = normals[ox + 2];
            const dot = nx * hitNormal[0] + ny * hitNormal[1] + nz * hitNormal[2];
            if (dot < normalThreshold) continue;
        }

        const falloff = brushFalloff(Math.sqrt(dist2), radius, hardness);
        if (falloff <= 0) continue;

        const prev = weights[i];
        const next =
            mode === "paint"
                ? paintVertex(prev, falloff, strength)
                : eraseVertex(prev, falloff, strength);
        if (next !== prev) {
            weights[i] = next;
            changed += 1;
        }
    }

    return changed;
}

/**
 * Mirror weights across the X axis using nearest-neighbor search.
 * Applied after a stroke when symmetry is enabled.
 */
export function mirrorWeightsAcrossX(
    positions: Float32Array,
    weights: Float32Array,
    sourceIndices: Iterable<number>,
    maxDistance = 1e-3,
    mode: BrushMode = "paint",
    mirrorMap?: Int32Array,
): void {
    const vertexCount = Math.min(weights.length, Math.floor(positions.length / 3));

    if (mirrorMap && mirrorMap.length >= vertexCount) {
        for (const source of sourceIndices) {
            if (source < 0 || source >= vertexCount) continue;
            const mirror = mirrorMap[source];
            if (mirror < 0 || mirror >= vertexCount) continue;
            weights[mirror] =
                mode === "erase"
                    ? Math.min(weights[mirror], weights[source])
                    : Math.max(weights[mirror], weights[source]);
        }
        return;
    }

    const maxDistance2 = maxDistance * maxDistance;

    for (const source of sourceIndices) {
        if (source < 0 || source >= vertexCount) continue;
        const sx = -positions[source * 3];
        const sy = positions[source * 3 + 1];
        const sz = positions[source * 3 + 2];
        let best = -1;
        let bestDist2 = maxDistance2;

        for (let i = 0; i < vertexCount; i++) {
            if (i === source) continue;
            const ox = i * 3;
            const dx = positions[ox] - sx;
            const dy = positions[ox + 1] - sy;
            const dz = positions[ox + 2] - sz;
            const dist2 = dx * dx + dy * dy + dz * dz;
            if (dist2 < bestDist2) {
                bestDist2 = dist2;
                best = i;
            }
        }

        if (best >= 0) {
            weights[best] =
                mode === "erase"
                    ? Math.min(weights[best], weights[source])
                    : Math.max(weights[best], weights[source]);
        }
    }
}

/** Map weight 0–1 to RGB in [0,1] (black → blue → yellow → red). */
export function weightToRgb(weight: number): [number, number, number] {
    const w = clamp01(weight);
    if (w <= 0) return [0.12, 0.12, 0.14];
    if (w < 0.5) {
        const t = w / 0.5;
        return [0.1 * t, 0.25 + 0.35 * t, 0.85 - 0.2 * t];
    }
    if (w < 0.85) {
        const t = (w - 0.5) / 0.35;
        return [0.1 + 0.85 * t, 0.6 + 0.3 * t, 0.65 * (1 - t)];
    }
    const t = (w - 0.85) / 0.15;
    return [0.95, 0.9 * (1 - t), 0.05 * (1 - t)];
}

/**
 * Map weight 0–1 to RGB and write directly into `out` at `offset` (no allocation).
 * Equivalent to weightToRgb but avoids creating a temporary [r,g,b] array per vertex.
 */
export function weightToRgbInto(weight: number, out: Float32Array, offset: number): void {
    const w = clamp01(weight);
    if (w <= 0) {
        out[offset] = 0.12;
        out[offset + 1] = 0.12;
        out[offset + 2] = 0.14;
        return;
    }
    if (w < 0.5) {
        const t = w / 0.5;
        out[offset] = 0.1 * t;
        out[offset + 1] = 0.25 + 0.35 * t;
        out[offset + 2] = 0.85 - 0.2 * t;
        return;
    }
    if (w < 0.85) {
        const t = (w - 0.5) / 0.35;
        out[offset] = 0.1 + 0.85 * t;
        out[offset + 1] = 0.6 + 0.3 * t;
        out[offset + 2] = 0.65 * (1 - t);
        return;
    }
    const t = (w - 0.85) / 0.15;
    out[offset] = 0.95;
    out[offset + 1] = 0.9 * (1 - t);
    out[offset + 2] = 0.05 * (1 - t);
}

export function writeWeightColors(weights: Float32Array, colors: Float32Array): void {
    const count = Math.min(weights.length, Math.floor(colors.length / 3));
    for (let i = 0; i < count; i++) {
        weightToRgbInto(weights[i], colors, i * 3);
    }
}
