import assert from "node:assert/strict";

import { describe, it } from "vitest";

import {
    ANATOMY_CONTROLS,
    anisotropicScaleFromOriginal,
    applyBrushStroke,
    applyGeodesicBrush,
    applyMultiRegionDeform,
    applySnorm8VectorCorrection,
    brushFalloff,
    buildAnatomyBasis,
    buildConnectedComponents,
    buildPositionWeldMap,
    buildSymmetryMap,
    buildVertexAdjacency,
    collectWeldGroups,
    computeMaskPivots,
    detectSnorm8VectorLayout,
    displacementMetrics,
    computeVertexNormals,
    createGeodesicBrushWorkspace,
    eraseVertex,
    extractBoneWeights,
    extractPositions,
    generateRegionWeights,
    growSelectionWeights,
    influencesAtVertex,
    listBlendBones,
    mirrorWeightsWithCache,
    paintVertex,
    rankBonesAtVertices,
    recalculateNormalsAndTangents,
    selectConnectedComponent,
    shrinkSelectionWeights,
    smoothSelectionWeights,
    syncWeldedWeights,
    validatePositionBuffer,
    writePositionsIntoBuffer,
} from "./index";

describe("body-shape seam welding", () => {
    // Two triangles sharing an edge through split vertices 1/3 and 2/5, plus a flipped copy of the first.
    const positions = new Float32Array([
        0, 0, 0, 1, 0, 0, 0, 1, 0, 1, 0, 0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 1, 0,
    ]);
    const indices = new Uint32Array([0, 1, 2, 3, 4, 5, 6, 8, 7]);
    const weldMap = buildPositionWeldMap(positions);
    const weldGroups = collectWeldGroups(weldMap);

    it("welds coincident vertices, tolerating float rounding but not nearby vertices", () => {
        assert.deepEqual([...weldMap], [0, 1, 2, 1, 4, 2, 0, 1, 2]);

        const rounded = buildPositionWeldMap(
            new Float32Array([0, 0, 0, 1e-6, 0, 0, 1e-2, 0, 0, 1, 0, 0]),
        );
        assert.deepEqual([...rounded], [0, 0, 2, 3]);

        const acrossCells = buildPositionWeldMap(
            new Float32Array([0, 0, 0, 2.9e-5, 0, 0, 3.1e-5, 0, 0, 1, 0, 0]),
        );
        assert.deepEqual([...acrossCells], [0, 1, 1, 3]);
    });

    it("carries a surface brush across a seam without tearing the mask", () => {
        const weights = new Float32Array(9);
        applyGeodesicBrush({
            positions,
            adjacency: buildVertexAdjacency(9, indices, weldGroups),
            weights,
            seedVertexIndices: [0],
            radius: 1.2,
            strength: 1,
            mode: "paint",
        });
        syncWeldedWeights(weights, weldMap, "paint");

        assert.ok(weights[1] > 0);
        assert.equal(weights[1], weights[3]);
        assert.equal(weights[2], weights[5]);
    });

    it("keeps erased weights when syncing duplicates", () => {
        const weights = new Float32Array([0, 1, 0, 0.25, 0, 0, 0, 1, 0]);
        syncWeldedWeights(weights, weldMap, "erase");
        assert.deepEqual([weights[1], weights[3], weights[7]], [0.25, 0.25, 0.25]);
    });

    it("merges seam normals by facing so a double-sided shell does not cancel", () => {
        const folded = new Float32Array(positions);
        folded[14] = 1;
        const normals = computeVertexNormals(folded, indices, weldGroups);
        const normalAt = (vertex: number) =>
            [...normals.subarray(vertex * 3, vertex * 3 + 3)].map((value) => value + 0);
        assert.deepEqual(normalAt(1), normalAt(3));
        assert.ok(normalAt(1)[0] < 0);
        assert.deepEqual(normalAt(7), [0, 0, -1]);
    });
});

describe("body-shape mask pivots", () => {
    function triangles(points: number[][]) {
        return {
            positions: new Float32Array(points.flat()),
            adjacency: buildVertexAdjacency(
                points.length,
                new Uint32Array(
                    points.flatMap((_, index) =>
                        index % 3 === 0 ? [index, index + 1, index + 2] : [],
                    ),
                ),
            ),
            weights: new Float32Array(points.length).fill(1),
        };
    }
    const left = [
        [-2, 0, 0],
        [-1, 0, 0],
        [-1.5, 1, 0],
    ];
    const right = left.map(([x, y, z]) => [-x, y, z]);

    it("scales separate parts around their own centers", () => {
        const mesh = triangles([...left, ...right]);
        const vertexPivots = computeMaskPivots(mesh.positions, mesh.weights, mesh.adjacency);
        assert.ok(vertexPivots);

        const previewPositions = new Float32Array(mesh.positions.length);
        applyMultiRegionDeform({
            originalPositions: mesh.positions,
            previewPositions,
            regions: [
                {
                    id: "legs",
                    weights: mesh.weights,
                    amount: 0.5,
                    axisScale: [1, 0, 0],
                    pivot: [0, 0, 0],
                    vertexPivots,
                },
            ],
        });
        assert.deepEqual([previewPositions[0], previewPositions[3]], [-2.25, -0.75]);
        assert.deepEqual([previewPositions[9], previewPositions[12]], [2.25, 0.75]);
    });

    it("keeps one pivot for parts inside each other's bounds and for a single part", () => {
        const pocket = [
            [-1.6, 0.2, 0],
            [-1.5, 0.2, 0],
            [-1.55, 0.3, 0],
        ];
        const nested = triangles([...left, ...pocket]);
        assert.equal(
            computeMaskPivots(nested.positions, nested.weights, nested.adjacency),
            undefined,
        );

        const single = triangles(left);
        assert.equal(
            computeMaskPivots(single.positions, single.weights, single.adjacency),
            undefined,
        );
    });

    it("ignores unmasked vertices when splitting parts", () => {
        const mesh = triangles([...left, ...right]);
        mesh.weights.fill(0, 3);
        assert.equal(computeMaskPivots(mesh.positions, mesh.weights, mesh.adjacency), undefined);
    });
});

describe("body-shape weights", () => {
    it("computes smooth falloff clamped outside radius", () => {
        assert.equal(brushFalloff(0, 1), 1);
        assert.equal(brushFalloff(1, 1), 0);
        assert.equal(brushFalloff(2, 1), 0);
        const mid = brushFalloff(0.5, 1);
        assert.ok(mid > 0 && mid < 1);
        assert.ok(brushFalloff(0.25, 1) > brushFalloff(0.75, 1));
    });

    it("paints and erases with clamp to [0, 1]", () => {
        assert.equal(paintVertex(0.9, 1, 0.5), 1);
        assert.equal(paintVertex(0.2, 0.5, 0.5), 0.45);
        assert.equal(eraseVertex(0.1, 1, 0.5), 0);
        assert.ok(Math.abs(eraseVertex(0.8, 0.5, 0.4) - 0.6) < 1e-9);
    });

    it("applies brush stroke only within radius and clamps weights", () => {
        // Three vertices on X axis: at 0, 0.5, 2
        const positions = new Float32Array([0, 0, 0, 0.5, 0, 0, 2, 0, 0]);
        const weights = new Float32Array([0, 0, 0]);
        const changed = applyBrushStroke({
            positions,
            weights,
            hitPoint: [0, 0, 0],
            radius: 1,
            strength: 1,
            mode: "paint",
        });
        assert.deepEqual([...changed], [0, 1]);
        assert.equal(weights[0], 1);
        assert.ok(weights[1] > 0 && weights[1] < 1);
        assert.equal(weights[2], 0);

        applyBrushStroke({
            positions,
            weights,
            hitPoint: [0, 0, 0],
            radius: 1,
            strength: 1,
            mode: "erase",
        });
        assert.equal(weights[0], 0);
        assert.equal(weights[1], 0);
    });

    it("through brush selects opposite-facing vertices while front filter skips them", () => {
        // Two vertices at same position with opposite normals
        const positions = new Float32Array([0, 0, 0, 0, 0, 0]);
        const normals = new Float32Array([0, 0, 1, 0, 0, -1]);
        const frontWeights = new Float32Array(2);
        const throughWeights = new Float32Array(2);

        applyBrushStroke({
            positions,
            weights: frontWeights,
            hitPoint: [0, 0, 0],
            hitNormal: [0, 0, 1],
            normals,
            radius: 1,
            strength: 1,
            mode: "paint",
        });
        assert.ok(frontWeights[0] > 0);
        assert.equal(frontWeights[1], 0);

        applyBrushStroke({
            positions,
            weights: throughWeights,
            hitPoint: [0, 0, 0],
            radius: 1,
            strength: 1,
            mode: "paint",
        });
        assert.ok(throughWeights[0] > 0);
        assert.ok(throughWeights[1] > 0);
    });
});

describe("body-shape mesh selection", () => {
    it("builds the same neighbor relation in CSR order", () => {
        const adjacency = buildVertexAdjacency(4, new Uint32Array([0, 1, 2, 2, 1, 3]));

        assert.deepEqual([...adjacency.offsets], [0, 2, 5, 8, 10]);
        assert.deepEqual([...adjacency.neighbors], [1, 2, 0, 2, 3, 0, 1, 3, 1, 2]);
    });

    it("does not cross to a nearby disconnected surface", () => {
        const positions = new Float32Array([
            0, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 0.01, 1, 0, 0.01, 0, 1, 0.01,
        ]);
        const adjacency = buildVertexAdjacency(6, new Uint32Array([0, 1, 2, 3, 4, 5]));
        const weights = new Float32Array(6);

        const changed = applyGeodesicBrush({
            positions,
            adjacency,
            weights,
            seedVertexIndices: [0],
            radius: 2,
            strength: 1,
            mode: "paint",
        });

        assert.deepEqual([...changed], [0, 1, 2]);
        assert.ok(weights[0] > 0 && weights[1] > 0 && weights[2] > 0);
        assert.deepEqual(Array.from(weights.slice(3)), [0, 0, 0]);
    });

    it("limits geodesic distance to the radius and applies falloff", () => {
        const positions = new Float32Array([0, 0, 0, 0.5, 0, 0, 1, 0, 0]);
        const adjacency = {
            offsets: new Uint32Array([0, 1, 3, 4]),
            neighbors: new Uint32Array([1, 0, 2, 1]),
        };
        const weights = new Float32Array(3);

        const changed = applyGeodesicBrush({
            positions,
            adjacency,
            weights,
            seedVertexIndices: [0],
            radius: 1,
            strength: 1,
            mode: "paint",
        });

        assert.deepEqual([...changed], [0, 1]);
        assert.deepEqual([...weights], [1, 0.5, 0]);
    });

    it("reuses geodesic workspace without leaking distances between strokes", () => {
        const positions = new Float32Array([0, 0, 0, 0.5, 0, 0, 1, 0, 0]);
        const adjacency = {
            offsets: new Uint32Array([0, 1, 3, 4]),
            neighbors: new Uint32Array([1, 0, 2, 1]),
        };
        const workspace = createGeodesicBrushWorkspace(3);
        const reusedWeights = new Float32Array(3);
        applyGeodesicBrush({
            positions,
            adjacency,
            weights: reusedWeights,
            seedVertexIndices: [0],
            radius: 0.75,
            strength: 1,
            mode: "paint",
            workspace,
        });
        reusedWeights.fill(0);
        const reused = applyGeodesicBrush({
            positions,
            adjacency,
            weights: reusedWeights,
            seedVertexIndices: [2],
            radius: 0.75,
            strength: 1,
            mode: "paint",
            workspace,
        });
        const freshWeights = new Float32Array(3);
        const fresh = applyGeodesicBrush({
            positions,
            adjacency,
            weights: freshWeights,
            seedVertexIndices: [2],
            radius: 0.75,
            strength: 1,
            mode: "paint",
        });

        assert.deepEqual([...reused], [...fresh]);
        assert.deepEqual([...reusedWeights], [...freshWeights]);
    });

    it("smooths weights from an iteration snapshot", () => {
        const weights = new Float32Array([1, 0, 0]);
        const adjacency = {
            offsets: new Uint32Array([0, 1, 3, 4]),
            neighbors: new Uint32Array([1, 0, 2, 1]),
        };

        smoothSelectionWeights(weights, adjacency, 0.5, 1);

        assert.deepEqual([...weights], [0.5, 0.25, 0]);
    });
});

describe("body-shape deform", () => {
    it("computes normalized vertex normals for a planar triangle", () => {
        const normals = computeVertexNormals(
            new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]),
            new Uint32Array([0, 1, 2]),
        );

        assert.ok(normals instanceof Float32Array);
        assert.deepEqual([...normals], [0, 0, 1, 0, 0, 1, 0, 0, 1]);
    });

    it("scales around pivot from original positions without stacking", () => {
        const original = new Float32Array([0, 0, 0, 2, 0, 0, 0, 4, 0]);
        const preview = new Float32Array(original.length);
        const weights = new Float32Array([0, 1, 0.5]);
        const pivot = [0, 0, 0] as const;
        const axisScale = [1, 0, 0] as const;

        anisotropicScaleFromOriginal({
            originalPositions: original,
            previewPositions: preview,
            weights,
            pivot,
            amount: 0.5,
            axisScale,
        });

        // weight 0 → unchanged
        assert.deepEqual([...preview.slice(0, 3)], [0, 0, 0]);
        // weight 1, amount 0.5, axis X → scale 1.5 → x=3
        assert.deepEqual([...preview.slice(3, 6)], [3, 0, 0]);
        // weight 0.5 on (0,4,0) with axis X only → x scaled but stays 0; y untouched
        assert.deepEqual([...preview.slice(6, 9)], [0, 4, 0]);

        // Separate anisotropic Y scale for the third vertex
        const yPreview = new Float32Array(original.length);
        anisotropicScaleFromOriginal({
            originalPositions: original,
            previewPositions: yPreview,
            weights,
            pivot,
            amount: 0.5,
            axisScale: [0, 1, 0],
        });
        // weight 0.5 → scale 1.25 → y=5
        assert.deepEqual([...yPreview.slice(6, 9)], [0, 5, 0]);

        // Re-apply same params must match (no cumulative drift)
        const second = new Float32Array(original.length);
        anisotropicScaleFromOriginal({
            originalPositions: original,
            previewPositions: second,
            weights,
            pivot,
            amount: 0.5,
            axisScale,
        });
        assert.deepEqual([...second], [...preview]);

        // amount 0 restores original
        anisotropicScaleFromOriginal({
            originalPositions: original,
            previewPositions: preview,
            weights,
            pivot,
            amount: 0,
            axisScale,
        });
        assert.deepEqual([...preview], [...original]);
    });

    it("reports displacement only for painted vertices", () => {
        const original = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]);
        const preview = new Float32Array([0, 0, 0, 2, 0, 0, 0, 1, 0]);
        const metrics = displacementMetrics(original, preview);
        assert.equal(metrics.vertexCount, 3);
        assert.equal(metrics.movedVertices, 1);
        assert.equal(metrics.maxDisplacement, 1);
        assert.equal(metrics.meanDisplacement, 1);
    });
});

describe("body-shape regions", () => {
    it("generates spatial weights without fixed vertex indices", () => {
        // Tall figure along Y with clear height bands
        const positions = new Float32Array([
            0,
            0,
            0, // feet / calves
            0,
            0.5,
            0, // mid / waist
            0,
            1,
            0, // head
            0.2,
            0.38,
            -0.4, // rear hip
        ]);
        const waist = generateRegionWeights(positions, "waist");
        const head = generateRegionWeights(positions, "head");
        const calves = generateRegionWeights(positions, "calves");
        // Head vertex is strongest in head region; feet strongest in calves
        assert.ok(head[2] > head[0]);
        assert.ok(calves[0] > calves[2]);
        // Mid-height participates in waist at least as much as the head vertex
        assert.ok(waist[1] >= waist[2]);
        for (const w of [...waist, ...head, ...calves]) {
            assert.ok(w >= 0 && w <= 1);
        }
        // Not a fixed vertex-index map: same spatial rule on a second mesh shape still works
        const shifted = new Float32Array(positions);
        for (let i = 0; i < shifted.length; i += 3) shifted[i + 1] += 10;
        const waistShifted = generateRegionWeights(shifted, "waist");
        assert.equal(waistShifted.length, waist.length);
        assert.ok(waistShifted.some((w) => w > 0));
    });

    it("composes multi-region deform from originals without stacking", () => {
        const original = new Float32Array([0, 0, 0, 2, 0, 0, 0, 2, 0]);
        const preview = new Float32Array(original.length);
        const w0 = new Float32Array([1, 0, 0]);
        const w1 = new Float32Array([0, 1, 0]);

        applyMultiRegionDeform({
            originalPositions: original,
            previewPositions: preview,
            regions: [
                {
                    id: "waist",
                    weights: w0,
                    amount: 0.5,
                    axisScale: [1, 0, 0],
                    pivot: [0, 0, 0],
                },
                {
                    id: "hips",
                    weights: w1,
                    amount: 1,
                    axisScale: [1, 0, 0],
                    pivot: [0, 0, 0],
                },
            ],
        });

        // v0 weight1 amount0.5 → scale 1.5 on x but at origin stays 0
        assert.deepEqual([...preview.slice(0, 3)], [0, 0, 0]);
        // v1 weight1 amount1 → x=4
        assert.deepEqual([...preview.slice(3, 6)], [4, 0, 0]);
        // v2 untouched
        assert.deepEqual([...preview.slice(6, 9)], [0, 2, 0]);

        const second = new Float32Array(original.length);
        applyMultiRegionDeform({
            originalPositions: original,
            previewPositions: second,
            regions: [
                {
                    id: "waist",
                    weights: w0,
                    amount: 0.5,
                    axisScale: [1, 0, 0],
                    pivot: [0, 0, 0],
                },
                {
                    id: "hips",
                    weights: w1,
                    amount: 1,
                    axisScale: [1, 0, 0],
                    pivot: [0, 0, 0],
                },
            ],
        });
        assert.deepEqual([...second], [...preview]);
    });

    it("inflates vertices along normals by their selected weights", () => {
        const original = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]);
        const preview = new Float32Array(original.length);

        applyMultiRegionDeform({
            originalPositions: original,
            previewPositions: preview,
            regions: [
                {
                    id: "chest",
                    weights: new Float32Array([0, 0.5, 1]),
                    amount: 2,
                    axisScale: [1, 1, 1],
                    pivot: [0, 0, 0],
                    operation: "inflate",
                    normals: new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]),
                },
            ],
        });

        assert.deepEqual([...preview], [0, 0, 0, 1, 0, 1, 0, 1, 2]);
    });
});

describe("body-shape blend bones", () => {
    it("lists bones and extracts per-bone normalized weights", () => {
        // 3 verts, stride 16: indices@0 weights@8
        const bytes = new Uint8Array(48);
        // v0: bone 5 weight 255
        bytes[0] = 5;
        bytes[8] = 255;
        // v1: bone 5 w=128, bone 7 w=127
        bytes[16] = 5;
        bytes[17] = 7;
        bytes[24] = 128;
        bytes[25] = 127;
        // v2: bone 7 weight 255
        bytes[32] = 7;
        bytes[40] = 255;

        const bones = listBlendBones(bytes, 3, 16);
        assert.deepEqual(
            bones.map((b) => b.id),
            [5, 7],
        );
        assert.equal(bones.find((b) => b.id === 5)?.vertexCount, 2);
        assert.equal(bones.find((b) => b.id === 7)?.vertexCount, 2);

        const w5 = extractBoneWeights(bytes, 5, 3, 16);
        assert.ok(Math.abs(w5[0] - 1) < 1e-6);
        assert.ok(Math.abs(w5[1] - 128 / 255) < 1e-6);
        assert.equal(w5[2], 0);

        const w7 = extractBoneWeights(bytes, 7, 3, 16);
        assert.equal(w7[0], 0);
        assert.ok(Math.abs(w7[1] - 127 / 255) < 1e-6);
        assert.ok(Math.abs(w7[2] - 1) < 1e-6);

        const at1 = influencesAtVertex(bytes, 1, 16);
        assert.equal(at1[0]?.boneId, 5);
        assert.equal(at1[1]?.boneId, 7);

        const ranked = rankBonesAtVertices(bytes, [1, 2], 16);
        assert.equal(ranked[0]?.boneId, 7);
    });

    it("parses compact 8-byte stride layout (indices@0 weights@4)", () => {
        // 3 verts, stride 8: indices@0 weights@4 (WWMI Tools v1.5+ compact layout)
        const bytes = new Uint8Array(24);
        // v0: bone 3 weight 255
        bytes[0] = 3;
        bytes[4] = 255;
        // v1: bone 3 w=128, bone 7 w=127
        bytes[8] = 3;
        bytes[9] = 7;
        bytes[12] = 128;
        bytes[13] = 127;
        // v2: bone 7 weight 255
        bytes[16] = 7;
        bytes[20] = 255;

        const bones = listBlendBones(bytes, 3, 8);
        assert.deepEqual(
            bones.map((b) => b.id),
            [3, 7],
        );
        assert.equal(bones.find((b) => b.id === 3)?.vertexCount, 2);
        assert.equal(bones.find((b) => b.id === 7)?.vertexCount, 2);

        const w3 = extractBoneWeights(bytes, 3, 3, 8);
        assert.ok(Math.abs(w3[0] - 1) < 1e-6);
        assert.ok(Math.abs(w3[1] - 128 / 255) < 1e-6);
        assert.equal(w3[2], 0);

        const w7 = extractBoneWeights(bytes, 7, 3, 8);
        assert.equal(w7[0], 0);
        assert.ok(Math.abs(w7[1] - 127 / 255) < 1e-6);
        assert.ok(Math.abs(w7[2] - 1) < 1e-6);

        const at1 = influencesAtVertex(bytes, 1, 8);
        assert.equal(at1[0]?.boneId, 3);
        assert.equal(at1[1]?.boneId, 7);

        const ranked = rankBonesAtVertices(bytes, [1, 2], 8);
        assert.equal(ranked[0]?.boneId, 7);
    });

    it("parses MiHoYo 32-byte stride layout (float32 weights@0 u32 indices@16)", () => {
        const bytes = new Uint8Array(96);
        const view = new DataView(bytes.buffer);
        // v0: bone 7 weight 1
        view.setFloat32(0, 1, true);
        view.setUint32(16, 7, true);
        // v1: bone 7 weight .75, bone 12 weight .25
        view.setFloat32(32, 0.75, true);
        view.setFloat32(36, 0.25, true);
        view.setUint32(48, 7, true);
        view.setUint32(52, 12, true);
        // v2: bone 12 weight 1
        view.setFloat32(64, 1, true);
        view.setUint32(80, 12, true);

        const bones = listBlendBones(bytes, 3, 32);
        assert.deepEqual(
            bones.map((bone) => bone.id),
            [7, 12],
        );
        assert.equal(bones.find((bone) => bone.id === 7)?.vertexCount, 2);
        assert.equal(bones.find((bone) => bone.id === 12)?.vertexCount, 2);

        const w7 = extractBoneWeights(bytes, 7, 3, 32);
        assert.deepEqual([...w7], [1, 0.75, 0]);
        const w12 = extractBoneWeights(bytes, 12, 3, 32);
        assert.deepEqual([...w12], [0, 0.25, 1]);

        const at1 = influencesAtVertex(bytes, 1, 32);
        assert.deepEqual(at1, [
            { boneId: 7, weight: 0.75 },
            { boneId: 12, weight: 0.25 },
        ]);
        assert.equal(rankBonesAtVertices(bytes, [1, 2], 32)[0]?.boneId, 12);
    });

    it("parses EFMI 12-byte stride layout (u16 weights@0 u8 indices@8)", () => {
        // 3 verts, stride 12: 4×u16 UNORM weights @0, 4×u8 indices @8
        const bytes = new Uint8Array(36);
        const view = new DataView(bytes.buffer);
        // v0: bone 3 weight 65535
        view.setUint16(0, 65535, true);
        bytes[8] = 3;
        // v1: bone 3 w=32768, bone 7 w=32767
        view.setUint16(12, 32768, true);
        view.setUint16(14, 32767, true);
        bytes[20] = 3;
        bytes[21] = 7;
        // v2: bone 7 weight 65535
        view.setUint16(24, 65535, true);
        bytes[32] = 7;

        const bones = listBlendBones(bytes, 3, 12);
        assert.deepEqual(
            bones.map((b) => b.id),
            [3, 7],
        );
        assert.equal(bones.find((b) => b.id === 3)?.vertexCount, 2);
        assert.equal(bones.find((b) => b.id === 7)?.vertexCount, 2);

        const w3 = extractBoneWeights(bytes, 3, 3, 12);
        assert.ok(Math.abs(w3[0] - 1) < 1e-6);
        assert.ok(Math.abs(w3[1] - 32768 / 65535) < 1e-6);
        assert.equal(w3[2], 0);

        const w7 = extractBoneWeights(bytes, 7, 3, 12);
        assert.equal(w7[0], 0);
        assert.ok(Math.abs(w7[1] - 32767 / 65535) < 1e-6);
        assert.ok(Math.abs(w7[2] - 1) < 1e-6);

        const at1 = influencesAtVertex(bytes, 1, 12);
        assert.equal(at1[0]?.boneId, 3);
        assert.equal(at1[1]?.boneId, 7);

        const ranked = rankBonesAtVertices(bytes, [1, 2], 12);
        assert.equal(ranked[0]?.boneId, 7);
    });

    it("parses EFMI rigid 4-byte stride layout (u32 bone index @0)", () => {
        const bytes = new Uint8Array(12);
        const view = new DataView(bytes.buffer);
        view.setUint32(0, 3, true);
        view.setUint32(4, 7, true);
        view.setUint32(8, 3, true);

        const bones = listBlendBones(bytes, 3, 4);
        assert.deepEqual(
            bones.map((b) => b.id),
            [3, 7],
        );
        assert.equal(bones.find((b) => b.id === 3)?.vertexCount, 2);
        assert.equal(bones.find((b) => b.id === 7)?.vertexCount, 1);

        const w3 = extractBoneWeights(bytes, 3, 3, 4);
        assert.equal(w3[0], 1);
        assert.equal(w3[1], 0);
        assert.equal(w3[2], 1);

        const at1 = influencesAtVertex(bytes, 1, 4);
        assert.equal(at1[0]?.boneId, 7);
        assert.equal(at1[0]?.weight, 1);
    });
});

describe("body-shape buffer", () => {
    it("validates stride/size and rejects bad layouts", () => {
        const ok = validatePositionBuffer(36, 12);
        assert.equal(ok.ok, true);
        if (ok.ok) assert.equal(ok.vertexCount, 3);

        const bad = validatePositionBuffer(35, 12);
        assert.equal(bad.ok, false);

        const strideBad = validatePositionBuffer(24, 8);
        assert.equal(strideBad.ok, false);
    });

    it("round-trips positions with same length and leaves index/other buffers out of scope", () => {
        // Synthetic stride-16 buffer: float3 + padding int32
        const stride = 16;
        const vertexCount = 4;
        const original = new Uint8Array(vertexCount * stride);
        const view = new DataView(original.buffer);
        for (let i = 0; i < vertexCount; i++) {
            view.setFloat32(i * stride, i, true);
            view.setFloat32(i * stride + 4, i * 2, true);
            view.setFloat32(i * stride + 8, i * 3, true);
            view.setUint32(i * stride + 12, 0xdeadbeef, true);
        }

        const positions = extractPositions(original, stride);
        assert.equal(positions.length, vertexCount * 3);

        // Deform X of painted verts
        const weights = new Float32Array([1, 0, 1, 0]);
        const preview = new Float32Array(positions.length);
        anisotropicScaleFromOriginal({
            originalPositions: positions,
            previewPositions: preview,
            weights,
            pivot: [0, 0, 0],
            amount: 1,
            axisScale: [1, 0, 0],
        });

        const written = writePositionsIntoBuffer(original, stride, preview);
        assert.equal(written.byteLength, original.byteLength);

        // Padding preserved
        for (let i = 0; i < vertexCount; i++) {
            const pad = new DataView(
                written.buffer,
                written.byteOffset,
                written.byteLength,
            ).getUint32(i * stride + 12, true);
            assert.equal(pad, 0xdeadbeef);
        }

        // Positions updated only where weighted
        const roundTrip = extractPositions(written, stride);
        assert.equal(roundTrip[0], 0); // 0 * 2 scale still 0
        assert.equal(roundTrip[3], 1); // unpainted
        assert.equal(roundTrip[6], 4); // 2 * 2
        assert.equal(roundTrip[9], 3); // unpainted
    });

    it("detects SNORM8 vector layout and corrects only active verts", () => {
        const vertexCount = 2;
        assert.equal(detectSnorm8VectorLayout(16, vertexCount), "snorm8-tangent-normal");
        assert.equal(detectSnorm8VectorLayout(16, 3), null);
        assert.equal(detectSnorm8VectorLayout(12, 2, 12), null);

        const vectors = new Int8Array(16);
        // vertex 0: diagonal tangent + normal so non-uniform scale is visible after normalize
        vectors[0] = 90;
        vectors[1] = 90;
        vectors[2] = 0;
        vectors[4] = 0;
        vectors[5] = 90;
        vectors[6] = 90;
        // vertex 1: same baseline
        vectors.set(vectors.subarray(0, 8), 8);
        vectors[3] = 1; // handedness preserved
        vectors[7] = -1;
        vectors[11] = 1;
        vectors[15] = -1;

        const corrected = applySnorm8VectorCorrection({
            originalVectors: vectors,
            weights: new Float32Array([1, 0]),
            amount: 1,
            axisScale: [2, 1, 0.5],
        });

        assert.equal(corrected.length, vectors.length);
        assert.equal(corrected[3], 1);
        assert.equal(corrected[7], -1);
        // unpainted vertex identical
        assert.deepEqual([...corrected.slice(8)], [...vectors.slice(8)]);
        // painted vertex normals/tangents changed for non-uniform scale
        assert.notDeepEqual([...corrected.slice(0, 3)], [...vectors.slice(0, 3)]);
        assert.notDeepEqual([...corrected.slice(4, 7)], [...vectors.slice(4, 7)]);
    });
});

describe("body-shape selection refinement", () => {
    it("grows weights to adjacent vertices without propagating across disconnected components", () => {
        // Triangles (0, 1, 4) and (1, 2, 5): 0 is adjacent to 1, 1 is adjacent to 2, but 0 is not adjacent to 2.
        // Vertex 3 is isolated (part of triangle 3, 6, 7).
        const indices = new Uint32Array([0, 1, 4, 1, 2, 5, 3, 6, 7]);
        const adj = buildVertexAdjacency(8, indices);
        const weights = new Float32Array(8);
        weights[0] = 1;

        growSelectionWeights(weights, adj, 1.0, 1);
        assert.equal(weights[0], 1);
        assert.equal(weights[1], 1);
        assert.equal(weights[2], 0);
        assert.equal(weights[3], 0);

        growSelectionWeights(weights, adj, 0.5, 1);
        assert.equal(weights[2], 0.5);
        assert.equal(weights[3], 0);
    });

    it("shrinks weights from boundaries", () => {
        const indices = new Uint32Array([0, 1, 4, 1, 2, 3]);
        const adj = buildVertexAdjacency(5, indices);
        const weights = new Float32Array([0, 1, 1, 1, 1]);

        shrinkSelectionWeights(weights, adj, 1.0, 1);
        assert.equal(weights[0], 0);
        assert.equal(weights[1], 0);
        assert.equal(weights[2], 1);
    });

    it("builds connected component IDs for separated submeshes", () => {
        const indices = new Uint32Array([0, 1, 2, 3, 4, 5]);
        const adj = buildVertexAdjacency(6, indices);
        const comp = buildConnectedComponents(6, adj);

        assert.equal(comp[0], comp[1]);
        assert.equal(comp[1], comp[2]);
        assert.equal(comp[3], comp[4]);
        assert.equal(comp[4], comp[5]);
        assert.notEqual(comp[0], comp[3]);
    });

    it("selects connected components with add, subtract, and replace modes", () => {
        const comp = new Uint32Array([0, 0, 0, 1, 1, 1]);
        const weights = new Float32Array(6);

        selectConnectedComponent({
            weights,
            componentIds: comp,
            targetComponentId: 0,
            mode: "add",
            weightValue: 0.8,
        });
        assert.ok(Math.abs(weights[0] - 0.8) < 1e-6);
        assert.ok(Math.abs(weights[1] - 0.8) < 1e-6);
        assert.ok(Math.abs(weights[2] - 0.8) < 1e-6);
        assert.equal(weights[3], 0);

        selectConnectedComponent({
            weights,
            componentIds: comp,
            targetComponentId: 1,
            mode: "replace",
            weightValue: 1.0,
        });
        assert.deepEqual([...weights], [0, 0, 0, 1, 1, 1]);

        selectConnectedComponent({
            weights,
            componentIds: comp,
            targetComponentId: 1,
            mode: "subtract",
        });
        assert.deepEqual([...weights], [0, 0, 0, 0, 0, 0]);
    });

    it("builds symmetry map and mirrors weights with cache in O(1) per vertex", () => {
        // Vertices: v0 at (-1, 2, 3), v1 at (1, 2, 3), v2 at (0, 5, 0)
        const positions = new Float32Array([-1, 2, 3, 1, 2, 3, 0, 5, 0]);
        const symmetryMap = buildSymmetryMap(positions, "x", 1e-3);

        assert.equal(symmetryMap[0], 1);
        assert.equal(symmetryMap[1], 0);

        const weights = new Float32Array([0.8, 0, 0]);
        mirrorWeightsWithCache(weights, symmetryMap, [0], "paint");
        assert.ok(Math.abs(weights[1] - 0.8) < 1e-6);
    });

    it("filters geodesic brush strokes by front-facing normal direction", () => {
        const positions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]);
        const indices = new Uint32Array([0, 1, 2]);
        const adj = buildVertexAdjacency(3, indices);
        const normals = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]);
        const weights = new Float32Array(3);

        // Facing opposite direction (dot < 0) -> skipped when frontFacingOnly is true
        applyGeodesicBrush({
            positions,
            adjacency: adj,
            weights,
            seedVertexIndices: [0],
            radius: 2,
            strength: 1,
            mode: "paint",
            frontFacingOnly: true,
            normals,
            hitNormal: [0, 0, -1],
        });

        assert.deepEqual([...weights], [0, 0, 0]);

        // Facing same direction (dot > 0) -> applied
        applyGeodesicBrush({
            positions,
            adjacency: adj,
            weights,
            seedVertexIndices: [0],
            radius: 2,
            strength: 1,
            mode: "paint",
            frontFacingOnly: true,
            normals,
            hitNormal: [0, 0, 1],
        });

        assert.ok(weights[0] > 0);
    });

    it("applies translate region deformation according to vertex weights", () => {
        const original = new Float32Array([0, 0, 0, 1, 0, 0]);
        const preview = new Float32Array(original.length);

        const metrics = applyMultiRegionDeform({
            originalPositions: original,
            previewPositions: preview,
            regions: [
                {
                    id: "test",
                    weights: new Float32Array([1, 0.5]),
                    amount: 1,
                    axisScale: [1, 1, 1],
                    pivot: [0, 0, 0],
                    operation: "translate",
                    translation: [0, 2, 0],
                },
            ],
        });

        assert.deepEqual([...preview], [0, 2, 0, 1, 1, 0]);
        assert.deepEqual(metrics, displacementMetrics(original, preview));
    });

    it("applies taper region deformation relative to pivot height", () => {
        // v0 at pivot height (no taper), v1 above pivot with full weight
        const original = new Float32Array([1, 0, 0, 1, 2, 0]);
        const preview = new Float32Array(original.length);

        applyMultiRegionDeform({
            originalPositions: original,
            previewPositions: preview,
            regions: [
                {
                    id: "test",
                    weights: new Float32Array([1, 1]),
                    amount: 1,
                    axisScale: [1, 1, 1],
                    pivot: [0, 0, 0],
                    operation: "taper",
                    taperFactor: 0.5,
                },
            ],
        });

        // heightDiff=0 -> unchanged; heightDiff=2, scale=1*0.5*1*2=1 -> x' = 1 + (1-0)*1 = 2
        assert.deepEqual([...preview], [1, 0, 0, 2, 2, 0]);
    });

    it("recalculates vertex normals and encodes into snorm8 vector buffer", () => {
        const positions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]);
        const indices = new Uint32Array([0, 1, 2]);
        const vectorBuffer = new Int8Array(24);

        const updated = recalculateNormalsAndTangents({
            positions,
            indices,
            vectorBuffer,
            layout: "snorm8-tangent-normal",
        });

        // Face normal of xy plane triangle is +z (0, 0, 1) -> 127 in snorm8 at byte offset 6 (bytes 4-6 are nx,ny,nz)
        assert.equal(updated[6], 127);
        assert.equal(updated[14], 127);
        assert.equal(updated[22], 127);
    });
});

describe("body-shape anatomy", () => {
    const grid = (
        columns: number,
        rows: number,
        point: (column: number, row: number) => number[],
    ) => {
        const positions = new Float32Array(columns * rows * 3);
        for (let row = 0; row < rows; row += 1) {
            for (let column = 0; column < columns; column += 1) {
                positions.set(point(column, row), (row * columns + column) * 3);
            }
        }
        const indices: number[] = [];
        for (let row = 0; row + 1 < rows; row += 1) {
            for (let column = 0; column + 1 < columns; column += 1) {
                const a = row * columns + column;
                indices.push(a, a + 1, a + columns, a + 1, a + columns + 1, a + columns);
            }
        }
        return { positions, indices: new Uint32Array(indices) };
    };

    it("grows each breast from its own apex and leaves the gap between them", () => {
        const columns = 51;
        const rows = 31;
        const { positions, indices } = grid(columns, rows, (column, row) => {
            const x = column * 0.1 - 2.5;
            const y = row * 0.1 - 1.5;
            const bump = (cx: number) => 0.5 * Math.exp(-((x - cx) ** 2 + y ** 2) / 0.18);
            return [x, y, bump(-1) + bump(1)];
        });
        const vertexAt = (x: number, y: number) =>
            Math.round((y + 1.5) / 0.1) * columns + Math.round((x + 2.5) / 0.1);
        const mask = new Float32Array(columns * rows).map((_, vertex) => {
            const x = positions[vertex * 3];
            const y = positions[vertex * 3 + 1];
            const distance = Math.hypot(Math.abs(x) - 1, y);
            return Math.min(1, Math.max(0, (0.8 - distance) / 0.2));
        });

        const basis = buildAnatomyBasis({
            model: "volume",
            positions,
            mask,
            up: [0, 1, 0],
            adjacency: buildVertexAdjacency(columns * rows, indices),
            normals: computeVertexNormals(positions, indices),
        });
        assert.ok(basis);
        const size = basis.fields[ANATOMY_CONTROLS.volume.indexOf("size")];
        const rise = (vertex: number) => size[vertex * 3 + 2];

        const rightApex = vertexAt(1, 0);
        assert.ok(rise(rightApex) > rise(vertexAt(1, 0.5)));
        assert.ok(Math.abs(rise(rightApex) - rise(vertexAt(-1, 0))) < 1e-4);
        // Centered on its own apex, a breast grows the same toward the sternum and the armpit.
        const inner = rise(vertexAt(0.5, 0));
        const outer = rise(vertexAt(1.5, 0));
        assert.ok(Math.abs(inner - outer) < Math.max(inner, outer) * 0.05, `${inner} vs ${outer}`);
        assert.ok(Math.abs(rise(vertexAt(0, 0))) < rise(rightApex) * 0.01);
        const outside = vertexAt(0, 1.4);
        assert.deepEqual([...size.subarray(outside * 3, outside * 3 + 3)], [0, 0, 0]);
    });

    it("changes limb girth without moving vertices along a tilted limb", () => {
        const segments = 16;
        const rings = 21;
        const axis = [1 / Math.sqrt(5), 2 / Math.sqrt(5), 0];
        const side = [-axis[1], axis[0], 0];
        const { positions, indices } = grid(segments, rings, (segment, ring) => {
            const angle = (segment / segments) * Math.PI * 2;
            const along = ring * 0.2;
            return [0, 1, 2].map(
                (i) =>
                    axis[i] * along +
                    side[i] * Math.cos(angle) * 0.3 +
                    (i === 2 ? Math.sin(angle) * 0.3 : 0),
            );
        });

        const basis = buildAnatomyBasis({
            model: "limb",
            positions,
            mask: new Float32Array(segments * rings).fill(1),
            up: [0, 1, 0],
            adjacency: buildVertexAdjacency(segments * rings, indices),
        });
        assert.ok(basis);
        const girth = basis.fields[ANATOMY_CONTROLS.limb.indexOf("girth")];
        for (let vertex = 0; vertex < segments * rings; vertex += 1) {
            const o = vertex * 3;
            const axial = girth[o] * axis[0] + girth[o + 1] * axis[1] + girth[o + 2] * axis[2];
            assert.ok(Math.abs(axial) < 1e-5, `vertex ${vertex} moved ${axial} along the limb`);
        }
        const radial = (ring: number) =>
            Math.hypot(...girth.subarray(ring * segments * 3, ring * segments * 3 + 3));
        assert.ok(radial(10) > radial(2));
    });
});
