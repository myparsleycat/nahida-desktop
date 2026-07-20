/** WWMI Blend.buf: 4×u8 bone indices @0, 4×u8 weights @8 (sum≈255), stride typically 16. */
export const BLEND_INDICES_OFFSET = 0;
export const BLEND_WEIGHTS_OFFSET = 8;
export const BLEND_INFLUENCE_COUNT = 4;
export const DEFAULT_BLEND_STRIDE = 16;

export type BlendBoneInfo = {
    id: number;
    vertexCount: number;
};

export type ParsedBlendBuffer = {
    bytes: Uint8Array;
    stride: number;
    vertexCount: number;
    bones: BlendBoneInfo[];
};

export function validateBlendBuffer(
    byteLength: number,
    vertexCount: number,
    stride = DEFAULT_BLEND_STRIDE,
): { ok: true; vertexCount: number } | { ok: false; reason: string } {
    if (stride < BLEND_WEIGHTS_OFFSET + BLEND_INFLUENCE_COUNT) {
        return { ok: false, reason: `Blend stride too small: ${stride}` };
    }
    if (vertexCount <= 0) {
        return { ok: false, reason: "Blend vertex count must be positive" };
    }
    if (byteLength < vertexCount * stride) {
        return {
            ok: false,
            reason: `Blend buffer too small: ${byteLength} < ${vertexCount * stride}`,
        };
    }
    return { ok: true, vertexCount };
}

export function listBlendBones(
    bytes: Uint8Array,
    vertexCount: number,
    stride = DEFAULT_BLEND_STRIDE,
): BlendBoneInfo[] {
    const counts = new Map<number, number>();
    const limit = Math.min(vertexCount, Math.floor(bytes.byteLength / stride));

    for (let i = 0; i < limit; i++) {
        const base = i * stride;
        let influenced = false;
        for (let k = 0; k < BLEND_INFLUENCE_COUNT; k++) {
            if (bytes[base + BLEND_WEIGHTS_OFFSET + k] === 0) continue;
            const boneId = bytes[base + BLEND_INDICES_OFFSET + k];
            counts.set(boneId, (counts.get(boneId) ?? 0) + 1);
            influenced = true;
        }
        // Skip verts with zero total weight from bone inventory
        void influenced;
    }

    return [...counts.entries()]
        .map(([id, vertexCount]) => ({ id, vertexCount }))
        .sort((a, b) => a.id - b.id);
}

export function extractBoneWeights(
    bytes: Uint8Array,
    boneId: number,
    vertexCount: number,
    stride = DEFAULT_BLEND_STRIDE,
): Float32Array {
    const weights = new Float32Array(vertexCount);
    const limit = Math.min(vertexCount, Math.floor(bytes.byteLength / stride));

    for (let i = 0; i < limit; i++) {
        const base = i * stride;
        let w = 0;
        for (let k = 0; k < BLEND_INFLUENCE_COUNT; k++) {
            if (bytes[base + BLEND_INDICES_OFFSET + k] !== boneId) continue;
            const nw = bytes[base + BLEND_WEIGHTS_OFFSET + k] / 255;
            if (nw > w) w = nw;
        }
        weights[i] = w;
    }

    return weights;
}

/** Per-vertex bone influences sorted by weight descending. */
export function influencesAtVertex(
    bytes: Uint8Array,
    vertexIndex: number,
    stride = DEFAULT_BLEND_STRIDE,
): Array<{ boneId: number; weight: number }> {
    const base = vertexIndex * stride;
    if (base + BLEND_WEIGHTS_OFFSET + BLEND_INFLUENCE_COUNT > bytes.byteLength) return [];

    const out: Array<{ boneId: number; weight: number }> = [];
    for (let k = 0; k < BLEND_INFLUENCE_COUNT; k++) {
        const weight = bytes[base + BLEND_WEIGHTS_OFFSET + k] / 255;
        if (weight <= 0) continue;
        out.push({
            boneId: bytes[base + BLEND_INDICES_OFFSET + k],
            weight,
        });
    }
    out.sort((a, b) => b.weight - a.weight);
    return out;
}

/**
 * Rank bones near a hit by summing max weights across the given vertices.
 * Used for mesh click-to-pick.
 */
export function rankBonesAtVertices(
    bytes: Uint8Array,
    vertexIndices: readonly number[],
    stride = DEFAULT_BLEND_STRIDE,
): Array<{ boneId: number; score: number }> {
    const scores = new Map<number, number>();
    for (const vertexIndex of vertexIndices) {
        for (const { boneId, weight } of influencesAtVertex(bytes, vertexIndex, stride)) {
            scores.set(boneId, (scores.get(boneId) ?? 0) + weight);
        }
    }
    return [...scores.entries()]
        .map(([boneId, score]) => ({ boneId, score }))
        .sort((a, b) => b.score - a.score);
}

export function parseBlendBuffer(
    bytes: Uint8Array,
    vertexCount: number,
    stride = DEFAULT_BLEND_STRIDE,
): ParsedBlendBuffer | null {
    const validation = validateBlendBuffer(bytes.byteLength, vertexCount, stride);
    if (!validation.ok) return null;
    return {
        bytes,
        stride,
        vertexCount,
        bones: listBlendBones(bytes, vertexCount, stride),
    };
}
