/**
 * Blend.buf layouts:
 *  - WWMI padded (stride 16): 4×u8 bone indices @0, 4×u8 weights @8 (sum≈255).
 *  - WWMI compact (stride 8): 4×u8 bone indices @0, 4×u8 weights @4 (sum≈255).
 *  - EFMI (stride 12): 4×u16 UNORM weights @0 (sum≈65535), 4×u8 bone indices @8.
 *  - EFMI rigid (stride 4): 1×u32 bone index @0, implied weight 1.0.
 * WWMI Tools v1.5+ emits the compact 8-byte layout; older builds use padded 16-byte.
 */
export const BLEND_INDICES_OFFSET = 0;
export const BLEND_WEIGHTS_OFFSET = 8;
export const BLEND_INFLUENCE_COUNT = 4;
export const DEFAULT_BLEND_STRIDE = 16;

/** EFMI stride-12: u16 weights start at 0, u8 indices at 8. */
export const EFMI_BLEND_STRIDE = 12;
export const EFMI_WEIGHTS_OFFSET = 0;
export const EFMI_INDICES_OFFSET = 8;
const EFMI_WEIGHT_SCALE = 65535;

/** EFMI rigid single-bone: one u32 bone index per vertex. */
export const EFMI_RIGID_STRIDE = 4;

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

type BlendLayout =
    | { kind: "wwmi"; indicesOffset: number; weightsOffset: number }
    | { kind: "efmi" }
    | { kind: "rigid" };

function layoutFor(stride: number): BlendLayout | null {
    if (stride === EFMI_RIGID_STRIDE) return { kind: "rigid" };
    if (stride === EFMI_BLEND_STRIDE) return { kind: "efmi" };
    if (stride === 8) return { kind: "wwmi", indicesOffset: 0, weightsOffset: 4 };
    if (stride >= 16)
        return { kind: "wwmi", indicesOffset: 0, weightsOffset: BLEND_WEIGHTS_OFFSET };
    return null;
}

export function validateBlendBuffer(
    byteLength: number,
    vertexCount: number,
    stride = DEFAULT_BLEND_STRIDE,
): { ok: true; vertexCount: number } | { ok: false; reason: string } {
    if (!layoutFor(stride)) {
        return { ok: false, reason: `Unsupported blend stride: ${stride}` };
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

function forEachInfluence(
    bytes: Uint8Array,
    base: number,
    stride: number,
    visit: (boneId: number, weight: number) => void,
): void {
    const layout = layoutFor(stride);
    if (!layout) return;

    if (layout.kind === "rigid") {
        if (base + 4 > bytes.byteLength) return;
        const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
        visit(view.getUint32(base, true), 1);
        return;
    }

    if (layout.kind === "efmi") {
        if (base + stride > bytes.byteLength) return;
        const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
        for (let k = 0; k < BLEND_INFLUENCE_COUNT; k++) {
            const weight =
                view.getUint16(base + EFMI_WEIGHTS_OFFSET + k * 2, true) / EFMI_WEIGHT_SCALE;
            if (weight <= 0) continue;
            visit(bytes[base + EFMI_INDICES_OFFSET + k], weight);
        }
        return;
    }

    if (base + layout.weightsOffset + BLEND_INFLUENCE_COUNT > bytes.byteLength) return;
    for (let k = 0; k < BLEND_INFLUENCE_COUNT; k++) {
        const weight = bytes[base + layout.weightsOffset + k] / 255;
        if (weight <= 0) continue;
        visit(bytes[base + layout.indicesOffset + k], weight);
    }
}

export function listBlendBones(
    bytes: Uint8Array,
    vertexCount: number,
    stride = DEFAULT_BLEND_STRIDE,
): BlendBoneInfo[] {
    const counts = new Map<number, number>();
    const limit = Math.min(vertexCount, Math.floor(bytes.byteLength / stride));

    for (let i = 0; i < limit; i++) {
        const seen = new Set<number>();
        forEachInfluence(bytes, i * stride, stride, (boneId, weight) => {
            if (weight <= 0 || seen.has(boneId)) return;
            seen.add(boneId);
            counts.set(boneId, (counts.get(boneId) ?? 0) + 1);
        });
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
        let w = 0;
        forEachInfluence(bytes, i * stride, stride, (id, weight) => {
            if (id !== boneId) return;
            if (weight > w) w = weight;
        });
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
    if (base + stride > bytes.byteLength) return [];

    const out: Array<{ boneId: number; weight: number }> = [];
    forEachInfluence(bytes, base, stride, (boneId, weight) => {
        out.push({ boneId, weight });
    });
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
