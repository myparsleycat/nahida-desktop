import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";
import { describe, expect, it } from "vitest";

import { computeCyclicPackedFrame } from "./model-viewer-compute-cyclic";

function source(byteLength: number, stride: number) {
    return { url: "/fixture.buf", byteLength, stride };
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

function identityPose(): ArrayBuffer {
    return new Float32Array([1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0]).buffer;
}

function translatedPose(row: 0 | 1 | 2, amount: number): ArrayBuffer {
    const values = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0];
    values[row * 4 + 3] = amount;
    return new Float32Array(values).buffer;
}

function blendBuffer(): ArrayBuffer {
    const buffer = new ArrayBuffer(32);
    const view = new DataView(buffer);
    view.setFloat32(0, 1, true);
    return buffer;
}

function concat(buffers: ArrayBuffer[]): ArrayBuffer {
    const size = buffers.reduce((total, buffer) => total + buffer.byteLength, 0);
    const output = new Uint8Array(size);
    let offset = 0;
    for (const buffer of buffers) {
        output.set(new Uint8Array(buffer), offset);
        offset += buffer.byteLength;
    }
    return output.buffer;
}

function descriptor(frameCount = 2): ViewerComputeDeformer {
    return {
        kind: "gimi_cyclic_packed_v1",
        id: "closet",
        meshIds: ["mesh"],
        vertexCount: 1,
        base: source(20, 20),
        shapePasses: [],
        shapeStages: [],
        pose: {
            blend: source(32, 32),
            frames: source(frameCount * 48, 48),
            boneCount: 1,
            frameCount,
        },
    };
}

describe("cyclic packed compute kernel", () => {
    it("keeps bind-pose game coordinates through an identity palette", () => {
        const frame = computeCyclicPackedFrame(
            descriptor(),
            {
                base: packedVertex(1, 2, 3),
                shapeTargets: [],
                blend: blendBuffer(),
                pose: concat([identityPose(), identityPose()]),
            },
            0,
        );
        expect([...frame.positions].map((value) => Number(value.toFixed(5)))).toEqual([1, 2, 3]);
        expect([...frame.normals].map((value) => Number(value.toFixed(5)))).toEqual([0, 0, 1]);
        expect(frame.tangents).toBeUndefined();
    });

    it("applies a 3x4 translation in shader space then writes game coordinates", () => {
        const frame = computeCyclicPackedFrame(
            descriptor(),
            {
                base: packedVertex(0, 0, 1),
                shapeTargets: [],
                blend: blendBuffer(),
                pose: concat([translatedPose(2, 4), translatedPose(2, 4)]),
            },
            0,
        );
        expect([...frame.positions].map((value) => Number(value.toFixed(5)))).toEqual([0, 4, 1]);
    });
});
