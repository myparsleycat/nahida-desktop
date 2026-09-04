import { computeBoundingCenter } from "./deform";
import { clamp01 } from "./weights";

/** Built-in body-shape region ids selectable in the UI. */
export const BODY_REGION_IDS = [
    "shoulders",
    "chest",
    "waist",
    "hips",
    "buttocks",
    "thighs",
    "calves",
    "arms",
    "head",
] as const;

export type BodyRegionId = (typeof BODY_REGION_IDS)[number];

export type MeshBounds = {
    min: [number, number, number];
    max: [number, number, number];
    center: [number, number, number];
    size: [number, number, number];
    /** Axis index 0/1/2 for height (longest extent). */
    heightAxis: 0 | 1 | 2;
    lateralAxis: 0 | 1 | 2;
    depthAxis: 0 | 1 | 2;
};

export type RegionPreset = {
    id: BodyRegionId;
    /** Normalized height band along the height axis [0=feet/bottom, 1=head/top]. */
    heightStart: number;
    heightEnd: number;
    /** Soft fade outside the band as a fraction of total height. */
    heightFade: number;
    /** Lateral falloff: 0 = full width, 1 = center only. */
    lateralInner: number;
    lateralOuter: number;
    /** Optional depth bias in [-1,1]: negative prefers "back", positive "front". */
    depthBias?: number;
    depthInner?: number;
    depthOuter?: number;
    /** Default anisotropic axis scale when the region is first selected. */
    defaultAxisScale: [number, number, number];
};

/**
 * Spatial region presets in normalized body space.
 * Coordinates are relative to the mesh bounding box so they work across mods
 * without fixed vertex-index maps.
 */
export const REGION_PRESETS: Record<BodyRegionId, RegionPreset> = {
    head: {
        id: "head",
        heightStart: 0.86,
        heightEnd: 1.0,
        heightFade: 0.04,
        // lateralOuter > 1 keeps bbox edge vertices (normalized |lat| can be 1)
        lateralInner: 0.0,
        lateralOuter: 1.15,
        defaultAxisScale: [1, 0.2, 1],
    },
    shoulders: {
        id: "shoulders",
        heightStart: 0.72,
        heightEnd: 0.88,
        heightFade: 0.05,
        lateralInner: 0.15,
        lateralOuter: 1.15,
        defaultAxisScale: [1, 0.15, 0.35],
    },
    chest: {
        id: "chest",
        heightStart: 0.55,
        heightEnd: 0.76,
        heightFade: 0.05,
        lateralInner: 0.0,
        lateralOuter: 1.05,
        depthBias: 1,
        depthInner: 0.0,
        depthOuter: 1.15,
        defaultAxisScale: [0.7, 0.15, 1],
    },
    waist: {
        id: "waist",
        heightStart: 0.42,
        heightEnd: 0.58,
        heightFade: 0.06,
        lateralInner: 0.0,
        lateralOuter: 1.05,
        defaultAxisScale: [1, 0.1, 0.65],
    },
    hips: {
        id: "hips",
        heightStart: 0.3,
        heightEnd: 0.48,
        heightFade: 0.05,
        lateralInner: 0.0,
        lateralOuter: 1.15,
        defaultAxisScale: [1, 0.1, 0.35],
    },
    buttocks: {
        id: "buttocks",
        heightStart: 0.28,
        heightEnd: 0.48,
        heightFade: 0.05,
        lateralInner: 0.05,
        lateralOuter: 1.1,
        depthBias: -1,
        depthInner: 0.1,
        depthOuter: 1.15,
        defaultAxisScale: [0.4, 0.1, 1],
    },
    thighs: {
        id: "thighs",
        heightStart: 0.12,
        heightEnd: 0.36,
        heightFade: 0.05,
        lateralInner: 0.08,
        lateralOuter: 1.15,
        defaultAxisScale: [1, 0.1, 0.85],
    },
    calves: {
        id: "calves",
        heightStart: 0.0,
        heightEnd: 0.18,
        heightFade: 0.04,
        lateralInner: 0.05,
        lateralOuter: 1.15,
        defaultAxisScale: [1, 0.1, 0.85],
    },
    arms: {
        id: "arms",
        heightStart: 0.48,
        heightEnd: 0.82,
        heightFade: 0.05,
        lateralInner: 0.55,
        lateralOuter: 1.2,
        defaultAxisScale: [1, 0.15, 0.85],
    },
};

export function computeMeshBounds(positions: Float32Array): MeshBounds {
    const count = Math.floor(positions.length / 3);
    if (count === 0) {
        return {
            min: [0, 0, 0],
            max: [0, 0, 0],
            center: [0, 0, 0],
            size: [1, 1, 1],
            heightAxis: 1,
            lateralAxis: 0,
            depthAxis: 2,
        };
    }

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

    const size: [number, number, number] = [
        Math.max(maxX - minX, 1e-6),
        Math.max(maxY - minY, 1e-6),
        Math.max(maxZ - minZ, 1e-6),
    ];

    // Longest extent = height; among remaining, wider = lateral.
    const order: Array<0 | 1 | 2> = [0, 1, 2];
    order.sort((a, b) => size[b] - size[a]);
    const heightAxis = order[0];
    const lateralAxis = order[1];
    const depthAxis = order[2];

    return {
        min: [minX, minY, minZ],
        max: [maxX, maxY, maxZ],
        center: [(minX + maxX) / 2, (minY + maxY) / 2, (minZ + maxZ) / 2],
        size,
        heightAxis,
        lateralAxis,
        depthAxis,
    };
}

function smoothBand(value: number, start: number, end: number, fade: number): number {
    if (end <= start) return 0;
    // Keep full strength inside [start, end]; fade only outside the band.
    const rise = smoothstep(start - fade, start, value);
    const fall = 1 - smoothstep(end, end + fade, value);
    return clamp01(rise * fall);
}

function smoothstep(edge0: number, edge1: number, value: number): number {
    if (edge1 <= edge0) return value < edge0 ? 0 : 1;
    const t = clamp01((value - edge0) / (edge1 - edge0));
    return t * t * (3 - 2 * t);
}

/**
 * Generate continuous 0–1 vertex weights for a body region from mesh positions.
 * Uses bounding-box normalized coordinates (not fixed vertex indices).
 */
export function generateRegionWeights(
    positions: Float32Array,
    regionId: BodyRegionId,
    bounds = computeMeshBounds(positions),
): Float32Array {
    const preset = REGION_PRESETS[regionId];
    const count = Math.floor(positions.length / 3);
    const weights = new Float32Array(count);
    const hAxis = bounds.heightAxis;
    const lAxis = bounds.lateralAxis;
    const dAxis = bounds.depthAxis;
    const hMin = bounds.min[hAxis];
    const hSize = bounds.size[hAxis];
    const lCenter = bounds.center[lAxis];
    const lHalf = bounds.size[lAxis] * 0.5;
    const dCenter = bounds.center[dAxis];
    const dHalf = bounds.size[dAxis] * 0.5;

    for (let i = 0; i < count; i++) {
        const o = i * 3;
        const h = (positions[o + hAxis] - hMin) / hSize;
        const lateral = Math.abs(positions[o + lAxis] - lCenter) / lHalf;
        const depthSigned = (positions[o + dAxis] - dCenter) / dHalf;

        let w = smoothBand(h, preset.heightStart, preset.heightEnd, preset.heightFade);
        if (w <= 0) continue;

        // Lateral mask: 1 near center/inner band, fall off toward outer
        const lat = 1 - smoothstep(preset.lateralInner, preset.lateralOuter, lateral);
        w *= clamp01(lat);

        if (preset.depthBias !== undefined) {
            // Prefer front (+1) or back (-1) of the depth axis
            const preferred = depthSigned * preset.depthBias;
            const dInner = preset.depthInner ?? 0;
            const dOuter = preset.depthOuter ?? 1;
            // Map preferred direction into 0..1 influence
            const depthMask = smoothstep(dInner, dOuter, (preferred + 1) * 0.5);
            w *= clamp01(depthMask);
        }

        weights[i] = clamp01(w);
    }

    return weights;
}

export type ActiveRegionDeform = {
    /** Region preset id, bone id string, or any stable control key. */
    id: string;
    weights: Float32Array;
    amount: number;
    axisScale: readonly [number, number, number];
    pivot: readonly [number, number, number];
    operation?: "scale" | "inflate" | "translate" | "taper";
    normals?: Float32Array;
    translation?: readonly [number, number, number];
    taperFactor?: number;
};

export type DeformMetrics = {
    vertexCount: number;
    movedVertices: number;
    maxDisplacement: number;
    meanDisplacement: number;
};

/**
 * Compose multiple region deformations from original positions (never cumulative).
 * p' = p + Σ_r  w_r · (A_r(p - c_r) + c_r - p)
 */
export function applyMultiRegionDeform(options: {
    originalPositions: Float32Array;
    previewPositions: Float32Array;
    regions: readonly ActiveRegionDeform[];
    epsilon?: number;
}): DeformMetrics {
    const { originalPositions, previewPositions, regions } = options;
    const vertexCount = Math.min(
        Math.floor(originalPositions.length / 3),
        Math.floor(previewPositions.length / 3),
    );

    let movedVertices = 0;
    let maxDisplacement = 0;
    let displacementSum = 0;
    const epsilon = options.epsilon ?? 1e-6;

    for (let i = 0; i < vertexCount; i++) {
        const o = i * 3;
        const px = originalPositions[o];
        const py = originalPositions[o + 1];
        const pz = originalPositions[o + 2];
        let dx = 0;
        let dy = 0;
        let dz = 0;

        for (const region of regions) {
            if (region.amount === 0) continue;
            const w = region.weights[i] ?? 0;
            if (w <= 0) continue;

            if (region.operation === "inflate") {
                const normals = region.normals;
                if (!normals || o + 2 >= normals.length) continue;
                dx += normals[o] * region.amount * w;
                dy += normals[o + 1] * region.amount * w;
                dz += normals[o + 2] * region.amount * w;
                continue;
            }

            if (region.operation === "translate") {
                const tr = region.translation ?? [0, 1, 0];
                dx += tr[0] * region.amount * w;
                dy += tr[1] * region.amount * w;
                dz += tr[2] * region.amount * w;
                continue;
            }

            if (region.operation === "taper") {
                const tf = region.taperFactor ?? 0.5;
                const heightDiff = py - region.pivot[1];
                const scale = region.amount * tf * w * heightDiff;
                dx += (px - region.pivot[0]) * scale;
                dz += (pz - region.pivot[2]) * scale;
                continue;
            }

            const sx = 1 + region.amount * region.axisScale[0] * w;
            const sy = 1 + region.amount * region.axisScale[1] * w;
            const sz = 1 + region.amount * region.axisScale[2] * w;
            const qx = region.pivot[0] + (px - region.pivot[0]) * sx;
            const qy = region.pivot[1] + (py - region.pivot[1]) * sy;
            const qz = region.pivot[2] + (pz - region.pivot[2]) * sz;
            dx += qx - px;
            dy += qy - py;
            dz += qz - pz;
        }

        previewPositions[o] = px + dx;
        previewPositions[o + 1] = py + dy;
        previewPositions[o + 2] = pz + dz;
        const displacement = Math.hypot(dx, dy, dz);
        if (displacement > epsilon) {
            movedVertices += 1;
            displacementSum += displacement;
            if (displacement > maxDisplacement) maxDisplacement = displacement;
        }
    }

    return {
        vertexCount,
        movedVertices,
        maxDisplacement,
        meanDisplacement: movedVertices > 0 ? displacementSum / movedVertices : 0,
    };
}

/** Max absolute influence across active regions for weight-color visualization. */
export function composeDisplayWeights(
    vertexCount: number,
    regions: readonly ActiveRegionDeform[],
    options?: {
        /** When true, show raw masks even if amount is 0 (bone picking). */ ignoreAmount?: boolean;
    },
): Float32Array {
    const out = new Float32Array(vertexCount);
    composeDisplayWeightsInto(out, vertexCount, regions, options);
    return out;
}

export function composeDisplayWeightsInto(
    out: Float32Array,
    vertexCount: number,
    regions: readonly ActiveRegionDeform[],
    options?: {
        /** When true, show raw masks even if amount is 0 (bone picking). */ ignoreAmount?: boolean;
    },
): void {
    out.fill(0, 0, Math.min(out.length, vertexCount));
    const ignoreAmount = options?.ignoreAmount === true;
    for (const region of regions) {
        if (!ignoreAmount && region.amount === 0) continue;
        const amountStrength = ignoreAmount ? 1 : Math.min(1, Math.abs(region.amount) * 2);
        for (let i = 0; i < vertexCount; i++) {
            const w = Math.min(1, Math.max(0, (region.weights[i] ?? 0) * amountStrength));
            if (w > out[i]) out[i] = w;
        }
    }
}

/** Pivot for a region = weighted centroid of its mask on original positions. */
export function computeRegionPivot(
    positions: Float32Array,
    weights: Float32Array,
    fallback: readonly [number, number, number],
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
    if (total <= 1e-8) return [fallback[0], fallback[1], fallback[2]];
    return [sx / total, sy / total, sz / total];
}

/** Effective per-vertex diagonal scale for vector correction (product of region factors). */
export function composeEffectiveScales(
    vertexCount: number,
    regions: readonly ActiveRegionDeform[],
): Float32Array {
    // packed xyz scales per vertex
    const scales = new Float32Array(vertexCount * 3);
    for (let i = 0; i < vertexCount; i++) {
        scales[i * 3] = 1;
        scales[i * 3 + 1] = 1;
        scales[i * 3 + 2] = 1;
    }

    for (const region of regions) {
        if (region.amount === 0 || region.operation === "inflate") continue;
        for (let i = 0; i < vertexCount; i++) {
            const w = region.weights[i] ?? 0;
            if (w <= 0) continue;
            scales[i * 3] *= 1 + region.amount * region.axisScale[0] * w;
            scales[i * 3 + 1] *= 1 + region.amount * region.axisScale[1] * w;
            scales[i * 3 + 2] *= 1 + region.amount * region.axisScale[2] * w;
        }
    }

    return scales;
}

export type SelectionMask = {
    id: string;
    name: string;
    weights: Float32Array;
    visible: boolean;
    locked: boolean;
};

export type ShapeLayer = {
    id: string;
    name: string;
    maskId: string;
    operation: "scale" | "inflate" | "translate" | "taper";
    amount: number;
    axisScale: [number, number, number];
    translation: [number, number, number];
    taperFactor: number;
    enabled: boolean;
};

export function evaluateShapeLayers(options: {
    originalPositions: Float32Array;
    previewPositions: Float32Array;
    masks: readonly SelectionMask[];
    layers: readonly ShapeLayer[];
    normals?: Float32Array;
}): void {
    const { originalPositions, previewPositions, masks, layers, normals } = options;
    const maskMap = new Map(masks.map((m) => [m.id, m]));
    const activeDeforms: ActiveRegionDeform[] = [];

    for (const layer of layers) {
        if (!layer.enabled || layer.amount === 0) continue;
        const mask = maskMap.get(layer.maskId);
        if (!mask || !mask.visible) continue;

        activeDeforms.push({
            id: layer.id,
            weights: mask.weights,
            amount: layer.amount,
            axisScale: layer.axisScale,
            pivot: computeRegionPivot(
                originalPositions,
                mask.weights,
                computeBoundingCenter(originalPositions),
            ),
            operation: layer.operation,
            normals,
            translation: layer.translation,
            taperFactor: layer.taperFactor,
        });
    }

    applyMultiRegionDeform({
        originalPositions,
        previewPositions,
        regions: activeDeforms,
    });
}
