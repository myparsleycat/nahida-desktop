import assert from "node:assert/strict";

import { describe, it } from "vitest";

import type { TouchComponentAnalysis } from "./touch-profile-types";

import { analyzeComponentWithBones } from "./touch-profile-bone";

function makeComponent(vertexCount: number): TouchComponentAnalysis {
    const indices = new Uint32Array(
        Array.from({ length: Math.floor(vertexCount / 3) * 3 }, (_, index) => index),
    );
    return {
        id: "bodyPosition",
        name: "bodyPosition",
        kind: "body",
        interactiveCandidate: true,
        supportGrade: "A",
        supportReasons: [],
        positionResourceName: "bodyPosition",
        positionRelativePath: "bodyPosition.buf",
        positionPath: "bodyPosition.buf",
        positionStride: 40,
        vertexCount,
        indexCount: indices.length,
        drawRanges: [{ firstIndex: 0, indexCount: indices.length, baseVertex: 0 }],
        objectMaps: [],
        bones: [],
    };
}

function makeBlendBytesWWMI(vertexCount: number, boneAssignments: number[][]): Uint8Array {
    const stride = 16;
    const bytes = new Uint8Array(vertexCount * stride);
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        const influences = boneAssignments[vertex] ?? [];
        const base = vertex * stride;
        for (let k = 0; k < 4; k++) {
            const influence = influences[k];
            if (influence !== undefined) {
                bytes[base + k] = influence;
                bytes[base + 8 + k] = 64;
            }
        }
    }
    return bytes;
}

function makeBlendBytesWithWeights(
    vertexCount: number,
    boneWeights: [number, number][][],
): Uint8Array {
    const stride = 16;
    const bytes = new Uint8Array(vertexCount * stride);
    for (let vertex = 0; vertex < vertexCount; vertex++) {
        const base = vertex * stride;
        const influences = boneWeights[vertex] ?? [];
        for (let k = 0; k < 4; k++) {
            const influence = influences[k];
            if (influence !== undefined) {
                bytes[base + k] = influence[0];
                bytes[base + 8 + k] = influence[1];
            }
        }
    }
    return bytes;
}

describe("analyzeComponentWithBones", () => {
    it("produces interactive draft with seed vertices from bone weights", () => {
        const vertexCount = 300;
        const component = makeComponent(vertexCount);
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < vertexCount; i++) {
            positions[i * 3] = (i % 10) * 0.1;
            positions[i * 3 + 1] = Math.floor(i / 10) * 0.1;
            positions[i * 3 + 2] = 0;
        }
        const boneAssignments = Array.from({ length: vertexCount }, (_, i) =>
            i < 200 ? [5] : [3],
        );
        const blendBytes = makeBlendBytesWWMI(vertexCount, boneAssignments);

        const result = analyzeComponentWithBones({
            component,
            positions,
            indices: new Uint32Array(),
            blendBytes,
            blendStride: 16,
            bones: [
                { id: 5, vertexCount: 200 },
                { id: 3, vertexCount: 100 },
            ],
            selections: [{ boneId: 5, channel: 0, label: "Test" }],
            weightThreshold: [0.01, 1],
            objectId: 1,
        });

        assert.equal(result.interactive, true);
        assert.equal(result.zones.length, 1);
        assert.equal(result.zones[0].channel, 0);
        assert.equal(result.zones[0].source, "bone");
        assert.equal(result.zones[0].label, "Test");
        assert.equal(result.zones[0].seedVertices?.length, 200);
        assert.equal(result.confidence, 1);
    });

    it("returns non-interactive when no blend buffer", () => {
        const component = makeComponent(300);
        const result = analyzeComponentWithBones({
            component,
            positions: new Float32Array(900),
            indices: new Uint32Array(),
            blendBytes: new Uint8Array(),
            blendStride: 0,
            bones: [],
            selections: [{ boneId: 0, channel: 0 }],
            weightThreshold: [0.01, 1],
            objectId: 1,
        });

        assert.equal(result.interactive, false);
        assert.equal(result.zones.length, 0);
        assert.ok(result.warnings.some((w) => w.includes("blend")));
    });

    it("returns non-interactive when no selections", () => {
        const component = makeComponent(300);
        const blendBytes = makeBlendBytesWWMI(
            300,
            Array.from({ length: 300 }, () => [5]),
        );
        const result = analyzeComponentWithBones({
            component,
            positions: new Float32Array(900),
            indices: new Uint32Array(),
            blendBytes,
            blendStride: 16,
            bones: [{ id: 5, vertexCount: 300 }],
            selections: [],
            weightThreshold: [0.01, 1],
            objectId: 1,
        });

        assert.equal(result.interactive, false);
        assert.equal(result.zones.length, 0);
    });

    it("skips bones with too few vertices above threshold", () => {
        const vertexCount = 600;
        const component = makeComponent(vertexCount);
        const positions = new Float32Array(vertexCount * 3);
        const boneAssignments = Array.from({ length: vertexCount }, (_, i) => (i < 5 ? [5] : [3]));
        const blendBytes = makeBlendBytesWWMI(vertexCount, boneAssignments);

        const result = analyzeComponentWithBones({
            component,
            positions,
            indices: new Uint32Array(),
            blendBytes,
            blendStride: 16,
            bones: [
                { id: 5, vertexCount: 5 },
                { id: 3, vertexCount: 295 },
            ],
            selections: [{ boneId: 5, channel: 0 }],
            weightThreshold: [0.01, 1],
            objectId: 1,
        });

        assert.equal(result.zones.length, 0);
        assert.equal(result.interactive, false);
        assert.ok(result.warnings.some((w) => w.includes("only 5 vertices")));
    });

    it("computes center and radius from seed vertices", () => {
        const vertexCount = 30;
        const component = makeComponent(vertexCount);
        const positions = new Float32Array(vertexCount * 3);
        for (let i = 0; i < vertexCount; i++) {
            positions[i * 3] = i * 0.1;
            positions[i * 3 + 1] = i * 0.2;
            positions[i * 3 + 2] = i * 0.3;
        }
        const blendBytes = makeBlendBytesWWMI(
            vertexCount,
            Array.from({ length: vertexCount }, () => [1]),
        );

        const result = analyzeComponentWithBones({
            component,
            positions,
            indices: new Uint32Array(),
            blendBytes,
            blendStride: 16,
            bones: [{ id: 1, vertexCount: vertexCount }],
            selections: [{ boneId: 1, channel: 2 }],
            weightThreshold: [0.01, 1],
            objectId: 1,
        });

        assert.equal(result.zones.length, 1);
        const zone = result.zones[0];
        assert.equal(zone.channel, 2);
        const expectedCenterX =
            Array.from({ length: vertexCount }, (_, i) => i * 0.1).reduce((a, b) => a + b, 0) /
            vertexCount;
        assert.ok(Math.abs(zone.center[0] - expectedCenterX) < 0.001);
        assert.ok(zone.radius[0] > 0);
        assert.ok(zone.radius[1] > 0);
        assert.ok(zone.radius[2] > 0);
    });

    it("selects only vertices whose weight is within the threshold range", () => {
        const vertexCount = 300;
        const component = makeComponent(vertexCount);
        const positions = new Float32Array(vertexCount * 3);
        const boneWeights: [number, number][][] = Array.from({ length: vertexCount }, (_, i) =>
            i < 100 ? [[5, 64]] : i < 200 ? [[5, 128]] : [[5, 255]],
        );
        const blendBytes = makeBlendBytesWithWeights(vertexCount, boneWeights);

        const result = analyzeComponentWithBones({
            component,
            positions,
            indices: new Uint32Array(),
            blendBytes,
            blendStride: 16,
            bones: [{ id: 5, vertexCount }],
            selections: [{ boneId: 5, channel: 0 }],
            weightThreshold: [0.3, 0.9],
            objectId: 1,
        });

        assert.equal(result.interactive, true);
        assert.equal(result.zones.length, 1);
        assert.equal(result.zones[0].seedVertices?.length, 100);
        assert.equal(result.zones[0].seedVertices?.[0], 100);
    });

    it("returns non-interactive when no vertices are within the threshold range", () => {
        const vertexCount = 300;
        const component = makeComponent(vertexCount);
        const positions = new Float32Array(vertexCount * 3);
        const boneWeights: [number, number][][] = Array.from({ length: vertexCount }, () => [
            [5, 64],
        ]);
        const blendBytes = makeBlendBytesWithWeights(vertexCount, boneWeights);

        const result = analyzeComponentWithBones({
            component,
            positions,
            indices: new Uint32Array(),
            blendBytes,
            blendStride: 16,
            bones: [{ id: 5, vertexCount }],
            selections: [{ boneId: 5, channel: 0 }],
            weightThreshold: [0.9, 1],
            objectId: 1,
        });

        assert.equal(result.interactive, false);
        assert.equal(result.zones.length, 0);
        assert.ok(result.warnings.some((w) => w.includes("within threshold")));
    });

    it("excludes unassigned vertices when the lower bound is 0", () => {
        const vertexCount = 300;
        const component = makeComponent(vertexCount);
        const positions = new Float32Array(vertexCount * 3);
        const boneAssignments = Array.from({ length: vertexCount }, (_, i) => (i < 200 ? [5] : []));
        const blendBytes = makeBlendBytesWWMI(vertexCount, boneAssignments);

        const result = analyzeComponentWithBones({
            component,
            positions,
            indices: new Uint32Array(),
            blendBytes,
            blendStride: 16,
            bones: [{ id: 5, vertexCount: 200 }],
            selections: [{ boneId: 5, channel: 0 }],
            weightThreshold: [0, 1],
            objectId: 1,
        });

        assert.equal(result.interactive, true);
        assert.equal(result.zones.length, 1);
        assert.equal(result.zones[0].seedVertices?.length, 200);
    });

    it("returns non-interactive for grade C components", () => {
        const component = { ...makeComponent(300), supportGrade: "C" as const };
        const result = analyzeComponentWithBones({
            component,
            positions: new Float32Array(900),
            indices: new Uint32Array(),
            blendBytes: makeBlendBytesWWMI(
                300,
                Array.from({ length: 300 }, () => [1]),
            ),
            blendStride: 16,
            bones: [{ id: 1, vertexCount: 300 }],
            selections: [{ boneId: 1, channel: 0 }],
            weightThreshold: [0.01, 1],
            objectId: 1,
        });

        assert.equal(result.interactive, false);
        assert.equal(result.zones.length, 0);
    });
});
