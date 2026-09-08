import type { ViewerComputeDeformer, ViewerComputeShapeStage } from "@shared/mod-viewer/types";
import { describe, expect, it } from "vitest";

import { computePackedShapeFrame, packedShapeStageAt } from "./model-viewer-compute-packed-shape";

function source(byteLength: number, stride: number, url = "/fixture.buf") {
    return { url, byteLength, stride };
}

function floatToHalf(value: number): number {
    const view = new DataView(new ArrayBuffer(4));
    view.setFloat32(0, value, true);
    const bits = view.getUint32(0, true);
    const sign = (bits >>> 16) & 0x8000;
    const exp = ((bits >>> 23) & 0xff) - 127 + 15;
    const frac = (bits >>> 13) & 0x3ff;
    if (exp <= 0) {
        return sign;
    }
    if (exp >= 31) {
        return sign | 0x7c00;
    }
    return sign | (exp << 10) | frac;
}

function packedVertex(x: number, y: number, z: number, nx = 0, ny = 0, nz = 127): ArrayBuffer {
    const buffer = new ArrayBuffer(20);
    const view = new DataView(buffer);
    view.setUint16(0, floatToHalf(x), true);
    view.setUint16(2, floatToHalf(y), true);
    view.setUint16(4, floatToHalf(z), true);
    view.setUint16(6, floatToHalf(1), true);
    view.setInt8(8, nx);
    view.setInt8(9, ny);
    view.setInt8(10, nz);
    return buffer;
}

function stage(overrides: Partial<ViewerComputeShapeStage> = {}): ViewerComputeShapeStage {
    return {
        base: source(20, 20, "/base"),
        target: source(20, 20, "/target"),
        phaseRate: 1,
        wrapAt: 1,
        phaseStart: 0,
        phaseOffset: 0,
        angularScale: 1,
        amplitude: 0.5,
        bias: 0.5,
        duration: 1,
        ...overrides,
    };
}

function descriptor(shapeStages: ViewerComputeShapeStage[]): ViewerComputeDeformer {
    return {
        kind: "gimi_cyclic_packed_shape_v1",
        id: "kimono",
        meshIds: ["mesh"],
        vertexCount: 1,
        base: source(20, 20, "/base"),
        shapePasses: [],
        shapeStages,
    };
}

describe("packed cyclic shapekey kernel", () => {
    it("lerps packed positions and normals at the rest-phase weight", () => {
        const frame = computePackedShapeFrame(
            descriptor([stage()]),
            [{ base: packedVertex(0, 0, 0, 127, 0, 0), target: packedVertex(10, 0, 0, 0, 127, 0) }],
            0,
        );
        expect(frame.positions[0]).toBeCloseTo(5);
        expect(frame.positions[1]).toBeCloseTo(0);
        expect(frame.positions[2]).toBeCloseTo(0);
        const length = Math.hypot(frame.normals[0]!, frame.normals[1]!, frame.normals[2]!);
        expect(length).toBeCloseTo(1);
        expect(frame.normals[0]).toBeCloseTo(frame.normals[1]!);
    });

    it("selects the later sequential stage using backend durations", () => {
        const deformer = descriptor([
            stage({ duration: 1 }),
            stage({
                base: source(20, 20, "/base-2"),
                target: source(20, 20, "/target-2"),
                duration: 1,
            }),
        ]);
        expect(packedShapeStageAt(deformer, 0.25).index).toBe(0);
        expect(packedShapeStageAt(deformer, 1.25).index).toBe(1);
        const frame = computePackedShapeFrame(
            deformer,
            [
                { base: packedVertex(0, 0, 0), target: packedVertex(10, 0, 0) },
                { base: packedVertex(0, 0, 0), target: packedVertex(0, 10, 0) },
            ],
            1,
        );
        expect(frame.positions[0]).toBeCloseTo(0);
        expect(frame.positions[1]).toBeCloseTo(5);
    });
});
