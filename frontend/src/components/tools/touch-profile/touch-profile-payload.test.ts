import { afterEach, describe, expect, it, vi } from "vitest";

import { loadTouchProfileMesh, loadTouchProfilePreview } from "./touch-profile-payload";

const settings = {
    maskStrength: 1,
    maskCurve: 1,
    maskRadiusScale: 1,
    maskCoreAttenuation: "off",
    strengthPreset: "normal",
    physicsPreset: "normal",
    advanced: { radius: 1, strength: 1, damping: 1, spring: 1, maxOffset: 1, falloff: 1 },
};

afterEach(() => vi.unstubAllGlobals());

describe("touch profile binary payloads", () => {
    it("loads topology and blend data from binary URLs", async () => {
        const payloads = new Map<string, ArrayBuffer>([
            ["/positions", new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]).buffer],
            ["/indices", new Uint32Array([0, 1, 2]).buffer],
            ["/blend", new Uint8Array([3, 0, 0, 0]).buffer],
        ]);
        vi.stubGlobal(
            "fetch",
            vi.fn(async (url: string) => new Response(payloads.get(url), { status: 200 })),
        );

        const mesh = await loadTouchProfileMesh({
            sessionId: "session",
            componentId: "mesh",
            topologyRevision: "r1",
            vertexCount: 3,
            positionsUrl: "/positions",
            positionsCount: 9,
            indicesUrl: "/indices",
            indexCount: 3,
            bones: [{ id: 3, vertexCount: 1 }],
            blendStride: 16,
            blendUrl: "/blend",
            blendBytes: 4,
        });

        expect(mesh.positions).toEqual(new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]));
        expect(mesh.indices).toEqual(new Uint32Array([0, 1, 2]));
        expect(mesh.blendBytes).toEqual(new Uint8Array([3, 0, 0, 0]));
    });

    it("uses zero-copy subarray views for packed zone weights", async () => {
        const mesh = {
            sessionId: "session",
            componentId: "mesh",
            vertexCount: 3,
            positions: new Float32Array(9),
            indices: new Uint32Array([0, 1, 2]),
            bones: [],
        };
        vi.stubGlobal(
            "fetch",
            vi.fn(async () => new Response(new Float32Array([0, 0.5, 1, 1, 0.5, 0]).buffer)),
        );

        const preview = await loadTouchProfilePreview(
            {
                sessionId: "session",
                componentId: "mesh",
                previewRevision: 2,
                vertexCount: 3,
                weightsUrl: "/weights",
                weightsCount: 6,
                zones: [
                    {
                        id: "left",
                        label: "Left",
                        channel: 0,
                        confidence: 1,
                        center: [0, 0, 0],
                        radius: [1, 1, 1],
                        source: "bone",
                        settings,
                        weightOffset: 0,
                    },
                    {
                        id: "right",
                        label: "Right",
                        channel: 1,
                        confidence: 1,
                        center: [0, 0, 0],
                        radius: [1, 1, 1],
                        source: "bone",
                        settings,
                        weightOffset: 3,
                    },
                ],
            },
            mesh,
        );

        expect(preview.zones[0]?.weights).toEqual(new Float32Array([0, 0.5, 1]));
        expect(preview.zones[1]?.weights).toEqual(new Float32Array([1, 0.5, 0]));
        expect(preview.zones[0]?.weights.buffer).toBe(preview.zones[1]?.weights.buffer);
    });
});
