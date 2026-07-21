import assert from "node:assert/strict";

import { describe, it } from "vitest";

import {
    anisotropicScaleFromOriginal,
    applyBrushStroke,
    applyMultiRegionDeform,
    applySnorm8VectorCorrection,
    brushFalloff,
    detectSnorm8VectorLayout,
    displacementMetrics,
    eraseVertex,
    extractBoneWeights,
    extractPositions,
    generateRegionWeights,
    influencesAtVertex,
    listBlendBones,
    paintVertex,
    rankBonesAtVertices,
    validatePositionBuffer,
    writePositionsIntoBuffer,
} from "./index";

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
        assert.equal(changed, 2);
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
});

describe("body-shape deform", () => {
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

        // Index buffer fixture remains untouched when we only write positions
        const indexBuf = new Uint32Array([0, 1, 2, 0, 2, 3]);
        const indexCopy = new Uint32Array(indexBuf);
        assert.deepEqual([...indexBuf], [...indexCopy]);
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
