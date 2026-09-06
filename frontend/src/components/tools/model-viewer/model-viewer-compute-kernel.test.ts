import type { ViewerComputeDeformer } from "@shared/mod-viewer/types";
import { describe, expect, it } from "vitest";

import {
    compactGIMIShapePoseFrame,
    computeGIMIShapePoseFrame,
} from "./model-viewer-compute-kernel";

function source(byteLength: number, stride: number) {
    return { url: "/fixture.buf", byteLength, stride };
}

function vertex(position: number[], normal = [0, 1, 0], tangent = [1, 0, 0, 1]): ArrayBuffer {
    return new Float32Array([...position, ...normal, ...tangent]).buffer;
}

function poseBuffer(
    scale = [1, 1, 1],
    translation = [0, 0, 0],
    qr = [0, 0, 0, 1],
    qd = [0, 0, 0, 0],
): ArrayBuffer {
    return new Float32Array([...scale, ...translation, ...qr, ...qd]).buffer;
}

function blendBuffer(): ArrayBuffer {
    const buffer = new ArrayBuffer(32);
    const view = new DataView(buffer);
    view.setFloat32(0, 1, true);
    for (let index = 0; index < 4; index += 1) {
        view.setInt32(16 + index * 4, 0, true);
    }
    return buffer;
}

function descriptor(shapeTargets = 0, withPose = true): ViewerComputeDeformer {
    return {
        kind: "gimi_shape_pose_v1",
        id: "cloth",
        meshIds: ["mesh"],
        vertexCount: 1,
        base: source(40, 40),
        shapePasses: Array.from({ length: shapeTargets }, () => ({
            target: source(40, 40),
            phaseRate: 1,
            phaseOffset: 0,
            angularScale: 1,
            amplitude: 0,
            bias: 0.5,
        })),
        pose: withPose
            ? {
                  blend: source(32, 32),
                  frames: source(56, 56),
                  boneCount: 1,
                  frameCount: 1,
              }
            : undefined,
    };
}

describe("GIMI shape/pose compute kernel", () => {
    it("accumulates shape deltas in pass order", () => {
        const frame = computeGIMIShapePoseFrame(
            descriptor(2, false),
            {
                base: vertex([1, 0, 0]),
                shapeTargets: [vertex([3, 0, 0]), vertex([5, 0, 0])],
            },
            0,
            0,
        );
        expect([...frame.positions]).toEqual([4, 0, 0]);
    });

    it("applies scale, bias, and dual-quaternion translation", () => {
        const frame = computeGIMIShapePoseFrame(
            descriptor(),
            {
                base: vertex([1, 1, 1]),
                shapeTargets: [],
                blend: blendBuffer(),
                pose: poseBuffer([2, 3, 4], [1, 1, 1], [0, 0, 0, 1], [1, 0, 0, 0]),
            },
            0,
            0,
        );
        expect([...frame.positions]).toEqual([5, 4, 5]);
        expect([...frame.normals]).toEqual([0, 1, 0]);
    });

    it("rotates and normalizes positions and normals", () => {
        const half = Math.SQRT1_2;
        const frame = computeGIMIShapePoseFrame(
            descriptor(),
            {
                base: vertex([1, 0, 0], [1, 0, 0]),
                shapeTargets: [],
                blend: blendBuffer(),
                pose: poseBuffer([1, 1, 1], [0, 0, 0], [0, 0, half, half]),
            },
            0,
            0,
        );
        expect(frame.positions[0]).toBeCloseTo(0);
        expect(frame.positions[1]).toBeCloseTo(1);
        expect(frame.normals[0]).toBeCloseTo(0);
        expect(frame.normals[1]).toBeCloseTo(1);
        expect(Math.hypot(...frame.normals)).toBeCloseTo(1);
    });

    it("rejects invalid bone indices and source-index mappings", () => {
        const blend = blendBuffer();
        new DataView(blend).setInt32(16, 2, true);
        expect(() =>
            computeGIMIShapePoseFrame(
                descriptor(),
                { base: vertex([0, 0, 0]), shapeTargets: [], blend, pose: poseBuffer() },
                0,
                0,
            ),
        ).toThrow("bone index 2");
        expect(() =>
            compactGIMIShapePoseFrame(
                {
                    positions: new Float32Array(3),
                    normals: new Float32Array(3),
                    tangents: new Float32Array(4),
                },
                new Uint32Array([1]),
            ),
        ).toThrow("source index 1");
    });
});
