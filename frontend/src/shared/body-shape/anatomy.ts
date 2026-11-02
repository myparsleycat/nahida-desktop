import { computeBoundingCenter } from "./deform";
import {
    adjacencyVertexCount,
    neighborsAt,
    smoothSelectionWeights,
    type VertexAdjacency,
} from "./selection";
import { clamp01 } from "./weights";

type Vec3 = [number, number, number];

export type AnatomyModel = "volume" | "limb" | "torso";

export const ANATOMY_CONTROLS = {
    volume: ["size", "projection", "lift", "spacing"],
    limb: ["girth", "bias"],
    torso: ["girth", "width"],
} as const satisfies Record<AnatomyModel, readonly string[]>;

export type AnatomyBasis = {
    /** Smoothed selection that bounds the fields. */
    gate: Float32Array;
    /** One packed xyz displacement field per control in `ANATOMY_CONTROLS[model]`, at value 1. */
    fields: Float32Array[];
};

/** A lobe needs this share of the mask on each side before left and right are shaped apart. */
const LOBE_SIDE_SHARE = 0.1;
/** Width of the band, as a share of the mask's half width, over which split lobes fade out at x = 0. */
const MIDLINE_BAND_RATIO = 0.1;
const GATE_SMOOTH_ITERATIONS = 3;
const FIELD_SMOOTH_ITERATIONS = 6;
/** RMS footprint radius of a uniform disc is R/√2; the margin keeps the profile off the mask core. */
const FOOTPRINT_RMS_RATIO = 1.6;
const TORSO_BINS = 32;
const EPSILON = 1e-8;

/**
 * Builds displacement fields that follow body anatomy instead of the selection weights: the
 * selection only decides which surface may move, and the model decides how it moves.
 * Every field is linear in its control, so a slider change only recombines the fields.
 */
export function buildAnatomyBasis(options: {
    model: AnatomyModel;
    positions: Float32Array;
    mask: Float32Array;
    up: readonly [number, number, number];
    adjacency?: VertexAdjacency;
    normals?: Float32Array;
    weldMap?: Uint32Array;
}): AnatomyBasis | undefined {
    const { model, positions, mask, adjacency, normals, weldMap } = options;
    const vertexCount = Math.min(mask.length, Math.floor(positions.length / 3));
    const up = normalize([...options.up] as Vec3);
    if (!up || !mask.some((weight) => weight > 0)) return undefined;
    if (model === "volume" && (!normals || normals.length < vertexCount * 3)) return undefined;

    const gate = new Float32Array(mask);
    if (adjacency && adjacencyVertexCount(adjacency) >= vertexCount) {
        smoothSelectionWeights(gate, adjacency, 0.5, GATE_SMOOTH_ITERATIONS);
    }
    const fields = ANATOMY_CONTROLS[model].map(() => new Float32Array(vertexCount * 3));
    const context: ShapeContext = {
        positions,
        mask,
        gate,
        up,
        center: computeBoundingCenter(positions),
        vertexCount,
        fields,
    };

    if (model === "torso") {
        shapeTorso(context);
    } else {
        for (const lobe of splitLobes(positions, mask, gate, vertexCount)) {
            if (model === "volume") shapeVolume(context, lobe, normals!);
            else shapeLimb(context, lobe);
        }
    }

    if (adjacency && adjacencyVertexCount(adjacency) >= vertexCount) {
        const support: number[] = [];
        for (let vertex = 0; vertex < vertexCount; vertex += 1)
            if (gate[vertex] > 0) support.push(vertex);
        const scratch = new Float32Array(support.length * 3);
        for (const field of fields) smoothField(field, support, scratch, adjacency, vertexCount);
    }
    if (weldMap) {
        for (const field of fields) {
            for (let vertex = 0; vertex < Math.min(vertexCount, weldMap.length); vertex += 1) {
                field.copyWithin(vertex * 3, weldMap[vertex] * 3, weldMap[vertex] * 3 + 3);
            }
        }
    }
    return { gate, fields };
}

export function combineAnatomyBasis(
    basis: AnatomyBasis,
    values: readonly number[],
    out = new Float32Array(basis.fields[0]?.length ?? 0),
): Float32Array {
    out.fill(0);
    basis.fields.forEach((field, index) => {
        const value = values[index] ?? 0;
        if (value === 0) return;
        for (let i = 0; i < Math.min(out.length, field.length); i += 1) out[i] += field[i] * value;
    });
    return out;
}

type ShapeContext = {
    positions: Float32Array;
    mask: Float32Array;
    gate: Float32Array;
    up: Vec3;
    center: Vec3;
    vertexCount: number;
    fields: Float32Array[];
};

type Lobe = {
    vertices: number[];
    /** -1 or 1 for the side of the symmetry plane that holds most of the lobe. */
    side: number;
    /** Distance from x = 0 over which a split lobe fades in; 0 when the mask is not split. */
    midlineBand: number;
};

/** Split lobes reach zero at x = 0, so the left and right fields meet without a jump. */
function midlineFade(lobe: Lobe, x: number): number {
    if (lobe.midlineBand <= 0) return 1;
    const t = clamp01((lobe.side * x) / lobe.midlineBand);
    return t * t * (3 - 2 * t);
}

/** Splits a selection across the x = 0 symmetry plane, the same plane the brush mirror uses. */
function splitLobes(
    positions: Float32Array,
    mask: Float32Array,
    gate: Float32Array,
    vertexCount: number,
): Lobe[] {
    let negative = 0;
    let positive = 0;
    let halfWidth = 0;
    for (let vertex = 0; vertex < vertexCount; vertex += 1) {
        const x = positions[vertex * 3];
        if (x < 0) negative += mask[vertex];
        else positive += mask[vertex];
        if (mask[vertex] > 0) halfWidth = Math.max(halfWidth, Math.abs(x));
    }

    const total = negative + positive;
    const split = negative > total * LOBE_SIDE_SHARE && positive > total * LOBE_SIDE_SHARE;
    const midlineBand = halfWidth * MIDLINE_BAND_RATIO;
    const lobes: Lobe[] = split
        ? [
              { vertices: [], side: -1, midlineBand },
              { vertices: [], side: 1, midlineBand },
          ]
        : [{ vertices: [], side: positive >= negative ? 1 : -1, midlineBand: 0 }];
    for (let vertex = 0; vertex < vertexCount; vertex += 1) {
        if (gate[vertex] <= 0) continue;
        lobes[split && positions[vertex * 3] >= 0 ? 1 : 0].vertices.push(vertex);
    }
    return lobes.filter((lobe) => lobe.vertices.length > 0);
}

/** Breast and buttock model: tissue grows from a base on the body wall toward an apex. */
function shapeVolume(context: ShapeContext, lobe: Lobe, normals: Float32Array): void {
    const { positions, mask, gate, up, fields } = context;
    const [sizeField, projectionField, liftField, spacingField] = fields;

    const normalSum: Vec3 = [0, 0, 0];
    const centroid: Vec3 = [0, 0, 0];
    let weightSum = 0;
    let maxWeight = 0;
    for (const vertex of lobe.vertices) {
        const weight = mask[vertex];
        if (weight <= 0) continue;
        for (let axis = 0; axis < 3; axis += 1) {
            normalSum[axis] += normals[vertex * 3 + axis] * weight;
            centroid[axis] += positions[vertex * 3 + axis] * weight;
        }
        weightSum += weight;
        maxWeight = Math.max(maxWeight, weight);
    }
    const direction = normalize(normalSum);
    if (!direction || weightSum <= EPSILON) return;
    for (let axis = 0; axis < 3; axis += 1) centroid[axis] /= weightSum;

    const heightOf = (vertex: number) => dot(sub(positions, vertex, centroid), direction);
    const core = maxWeight * 0.5;
    let coreMin = Infinity;
    let coreMax = -Infinity;
    let edgeSum = 0;
    let edgeCount = 0;
    let maskMin = Infinity;
    for (const vertex of lobe.vertices) {
        const weight = mask[vertex];
        if (weight <= 0) continue;
        const height = heightOf(vertex);
        maskMin = Math.min(maskMin, height);
        if (weight >= core) {
            coreMin = Math.min(coreMin, height);
            coreMax = Math.max(coreMax, height);
        } else {
            edgeSum += height;
            edgeCount += 1;
        }
    }
    if (!Number.isFinite(coreMax)) return;

    // Averaging the top of the core keeps the apex stable on flat or noisy tips.
    const apex: Vec3 = [0, 0, 0];
    let apexCount = 0;
    const apexFloor = coreMax - (coreMax - coreMin) * 0.1;
    for (const vertex of lobe.vertices) {
        if (mask[vertex] < core || heightOf(vertex) < apexFloor) continue;
        for (let axis = 0; axis < 3; axis += 1) apex[axis] += positions[vertex * 3 + axis];
        apexCount += 1;
    }
    for (let axis = 0; axis < 3; axis += 1) apex[axis] /= apexCount;

    const base = edgeCount > 0 ? Math.min(edgeSum / edgeCount, coreMin) : maskMin;
    const height = dot(sub(apex, 0, centroid), direction) - base;
    if (height <= EPSILON) return;

    let planarSum = 0;
    for (const vertex of lobe.vertices) {
        const weight = mask[vertex];
        if (weight <= 0) continue;
        const planar = reject(sub(positions, vertex, apex), direction);
        planarSum += dot(planar, planar) * weight;
    }
    const radius = Math.sqrt(planarSum / weightSum) * FOOTPRINT_RMS_RATIO;
    if (radius <= EPSILON) return;

    const lift = normalize(reject(up, direction));
    const outward = normalize(reject([lobe.side, 0, 0], direction));
    for (const vertex of lobe.vertices) {
        const planar = reject(sub(positions, vertex, apex), direction);
        const r = Math.sqrt(dot(planar, planar)) / radius;
        if (r >= 1) continue;
        const profile = (1 - r * r) ** 2 * gate[vertex] * midlineFade(lobe, positions[vertex * 3]);
        const rise = Math.max(0, heightOf(vertex) - base);
        const t = Math.min(1, rise / height);
        const o = vertex * 3;
        for (let axis = 0; axis < 3; axis += 1) {
            const projection = direction[axis] * rise * profile;
            projectionField[o + axis] += projection;
            sizeField[o + axis] += projection + planar[axis] * 0.5 * t * profile;
            if (lift) liftField[o + axis] += lift[axis] * 0.5 * height * t * profile;
            if (outward) spacingField[o + axis] += outward[axis] * 0.5 * height * t * profile;
        }
    }
}

/** Thigh, arm and calf model: girth changes around the limb axis, never along it. */
function shapeLimb(context: ShapeContext, lobe: Lobe): void {
    const { positions, mask, gate, up, center, fields } = context;
    const [girthField, biasField] = fields;

    const centroid: Vec3 = [0, 0, 0];
    let weightSum = 0;
    for (const vertex of lobe.vertices) {
        const weight = mask[vertex];
        if (weight <= 0) continue;
        for (let axis = 0; axis < 3; axis += 1)
            centroid[axis] += positions[vertex * 3 + axis] * weight;
        weightSum += weight;
    }
    if (weightSum <= EPSILON) return;
    for (let axis = 0; axis < 3; axis += 1) centroid[axis] /= weightSum;

    const covariance = [0, 0, 0, 0, 0, 0, 0, 0, 0];
    for (const vertex of lobe.vertices) {
        const weight = mask[vertex];
        if (weight <= 0) continue;
        const offset = sub(positions, vertex, centroid);
        for (let row = 0; row < 3; row += 1) {
            for (let column = 0; column < 3; column += 1) {
                covariance[row * 3 + column] += offset[row] * offset[column] * weight;
            }
        }
    }
    const axis = principalAxis(covariance, up);
    if (!axis) return;
    // The proximal end faces the body center for legs and for arms in any pose, unlike `up`,
    // which is nearly perpendicular to T-pose arms.
    if (dot(axis, sub(center, 0, centroid)) < 0) for (let i = 0; i < 3; i += 1) axis[i] = -axis[i];

    const along = (vertex: number) => dot(sub(positions, vertex, centroid), axis);
    const [low, high] = maskedRange(lobe.vertices, mask, along);
    if (high - low <= EPSILON) return;

    for (const vertex of lobe.vertices) {
        const offset = sub(positions, vertex, centroid);
        const radial = reject(offset, axis);
        const u = Math.min(1, Math.max(0, (dot(offset, axis) - low) / (high - low)));
        const belly =
            (1 - (2 * u - 1) ** 2) * gate[vertex] * midlineFade(lobe, positions[vertex * 3]);
        const o = vertex * 3;
        for (let i = 0; i < 3; i += 1) {
            girthField[o + i] += radial[i] * 0.5 * belly;
            biasField[o + i] += radial[i] * 0.5 * (2 * u - 1) * belly;
        }
    }
}

/** Waist and hip model: circumference changes around a centerline that follows the torso. */
function shapeTorso(context: ShapeContext): void {
    const { positions, mask, gate, up, vertexCount, fields } = context;
    const [girthField, widthField] = fields;

    const vertices: number[] = [];
    for (let vertex = 0; vertex < vertexCount; vertex += 1)
        if (gate[vertex] > 0) vertices.push(vertex);
    const heightOf = (vertex: number) => dot(sub(positions, vertex, [0, 0, 0]), up);
    const [low, high] = maskedRange(vertices, mask, heightOf);
    if (high - low <= EPSILON) return;

    // Slice centroids trace the torso centerline, which leans and curves in real models.
    const sums = Array.from({ length: TORSO_BINS }, () => [0, 0, 0, 0]);
    const binOf = (height: number) =>
        Math.min(
            TORSO_BINS - 1,
            Math.max(0, Math.floor(((height - low) / (high - low)) * TORSO_BINS)),
        );
    for (const vertex of vertices) {
        const weight = mask[vertex];
        if (weight <= 0) continue;
        const planar = reject(sub(positions, vertex, [0, 0, 0]), up);
        const sum = sums[binOf(heightOf(vertex))];
        for (let axis = 0; axis < 3; axis += 1) sum[axis] += planar[axis] * weight;
        sum[3] += weight;
    }
    const centers = fillBins(sums);
    if (!centers) return;

    const lateral = normalize(reject([1, 0, 0], up));
    for (const vertex of vertices) {
        const height = heightOf(vertex);
        const position = (height - low) / (high - low);
        const scaled = Math.min(TORSO_BINS - 1, Math.max(0, position * TORSO_BINS - 0.5));
        const lower = Math.floor(scaled);
        const upper = Math.min(TORSO_BINS - 1, lower + 1);
        const blend = scaled - lower;
        const planar = reject(sub(positions, vertex, [0, 0, 0]), up);
        const radial: Vec3 = [0, 0, 0];
        for (let axis = 0; axis < 3; axis += 1) {
            radial[axis] =
                planar[axis] - (centers[lower][axis] * (1 - blend) + centers[upper][axis] * blend);
        }
        const u = Math.min(1, Math.max(0, position));
        const belly = (1 - (2 * u - 1) ** 2) * gate[vertex];
        const side = lateral ? dot(radial, lateral) : 0;
        const o = vertex * 3;
        for (let axis = 0; axis < 3; axis += 1) {
            girthField[o + axis] += radial[axis] * 0.5 * belly;
            if (lateral) widthField[o + axis] += lateral[axis] * side * 0.5 * belly;
        }
    }
}

/** Fills empty slices from their neighbors and smooths the centerline. */
function fillBins(sums: number[][]): Vec3[] | undefined {
    const known = sums.flatMap((sum, index) => (sum[3] > EPSILON ? [index] : []));
    if (known.length === 0) return undefined;

    let centers = sums.map((_, index): Vec3 => {
        const below = known.findLast((candidate) => candidate <= index) ?? known[0];
        const above = known.find((candidate) => candidate >= index) ?? known[known.length - 1];
        const t = above === below ? 0 : (index - below) / (above - below);
        return [0, 1, 2].map(
            (axis) =>
                (sums[below][axis] / sums[below][3]) * (1 - t) +
                (sums[above][axis] / sums[above][3]) * t,
        ) as Vec3;
    });
    for (let pass = 0; pass < 2; pass += 1) {
        centers = centers.map((center, index) => {
            const previous = centers[Math.max(0, index - 1)];
            const next = centers[Math.min(centers.length - 1, index + 1)];
            return [0, 1, 2].map(
                (axis) => (previous[axis] + center[axis] * 2 + next[axis]) / 4,
            ) as Vec3;
        });
    }
    return centers;
}

/**
 * Smooths a displacement field over the gated surface with the outside held at zero. Smoothing
 * the displacement rather than the positions keeps surface detail while removing the steps that
 * selection edges would otherwise leave.
 */
function smoothField(
    field: Float32Array,
    support: readonly number[],
    next: Float32Array,
    adjacency: VertexAdjacency,
    vertexCount: number,
): void {
    for (let iteration = 0; iteration < FIELD_SMOOTH_ITERATIONS; iteration += 1) {
        support.forEach((vertex, index) => {
            const neighbors = neighborsAt(adjacency, vertex);
            const o = vertex * 3;
            if (neighbors.length === 0) {
                next.set(field.subarray(o, o + 3), index * 3);
                return;
            }
            let x = 0;
            let y = 0;
            let z = 0;
            for (const neighbor of neighbors) {
                if (neighbor >= vertexCount) continue;
                x += field[neighbor * 3];
                y += field[neighbor * 3 + 1];
                z += field[neighbor * 3 + 2];
            }
            next[index * 3] = (field[o] + x / neighbors.length) / 2;
            next[index * 3 + 1] = (field[o + 1] + y / neighbors.length) / 2;
            next[index * 3 + 2] = (field[o + 2] + z / neighbors.length) / 2;
        });
        support.forEach((vertex, index) =>
            field.set(next.subarray(index * 3, index * 3 + 3), vertex * 3),
        );
    }
}

function maskedRange(
    vertices: readonly number[],
    mask: Float32Array,
    value: (vertex: number) => number,
): [number, number] {
    let low = Infinity;
    let high = -Infinity;
    for (const vertex of vertices) {
        if (mask[vertex] <= 0) continue;
        const current = value(vertex);
        low = Math.min(low, current);
        high = Math.max(high, current);
    }
    return [low, high];
}

function principalAxis(covariance: readonly number[], seed: Vec3): Vec3 | undefined {
    let axis: Vec3 | undefined = [seed[0] + 0.1, seed[1] + 0.2, seed[2] + 0.3];
    for (let iteration = 0; iteration < 32 && axis; iteration += 1) {
        const current: Vec3 = axis;
        axis = normalize(
            [0, 1, 2].map(
                (row) =>
                    covariance[row * 3] * current[0] +
                    covariance[row * 3 + 1] * current[1] +
                    covariance[row * 3 + 2] * current[2],
            ) as Vec3,
        );
    }
    return axis;
}

function sub(values: ArrayLike<number>, index: number, origin: readonly number[]): Vec3 {
    const o = index * 3;
    return [values[o] - origin[0], values[o + 1] - origin[1], values[o + 2] - origin[2]];
}

function dot(a: readonly number[], b: readonly number[]): number {
    return a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
}

function reject(vector: Vec3, axis: Vec3): Vec3 {
    const along = dot(vector, axis);
    return [vector[0] - axis[0] * along, vector[1] - axis[1] * along, vector[2] - axis[2] * along];
}

function normalize(vector: Vec3): Vec3 | undefined {
    const length = Math.hypot(vector[0], vector[1], vector[2]);
    if (!Number.isFinite(length) || length <= EPSILON) return undefined;
    return [vector[0] / length, vector[1] / length, vector[2] / length];
}
