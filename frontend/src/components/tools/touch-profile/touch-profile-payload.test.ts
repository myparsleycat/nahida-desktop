import { describe, expect, it } from "vitest";

import { toTouchProfileMeshPreview, toTouchProfilePreview } from "./touch-profile-payload";

const settings = {
    maskStrength: 1,
    maskCurve: 1,
    maskRadiusScale: 1,
    maskCoreAttenuation: "off",
    strengthPreset: "normal",
    physicsPreset: "normal",
    advanced: {
        radius: 1,
        strength: 1,
        damping: 1,
        spring: 1,
        maxOffset: 1,
        falloff: 1,
    },
};

describe("touch profile payloads", () => {
    it("restores mesh arrays and base64 bytes from Wails payloads", () => {
        const preview = toTouchProfileMeshPreview({
            sessionId: "session",
            componentId: "mesh",
            vertexCount: 3,
            positions: [0, 0, 0, 1, 0, 0, 0, 1, 0],
            indices: [0, 1, 2],
            bones: [{ id: 3, vertexCount: 1 }],
            blendStride: 16,
            blendBytes: "AwAAAAAAAAD/AAAAAAAAAA==",
        });

        expect(preview.positions).toBeInstanceOf(Float32Array);
        expect(preview.indices).toEqual(new Uint32Array([0, 1, 2]));
        expect(preview.blendBytes).toBeInstanceOf(Uint8Array);
    });

    it("restores preview positions, indices, and zone weights", () => {
        const preview = toTouchProfilePreview({
            sessionId: "session",
            componentId: "mesh",
            vertexCount: 3,
            positions: [0, 0, 0, 1, 0, 0, 0, 1, 0],
            indices: [0, 1, 2],
            zones: [
                {
                    id: "zone",
                    label: "Zone",
                    channel: 0,
                    confidence: 1,
                    center: [0, 0, 0],
                    radius: [1, 1, 1],
                    source: "bone",
                    settings,
                    weights: [0, 0.5, 1],
                },
            ],
        });

        expect(preview.positions).toBeInstanceOf(Float32Array);
        expect(preview.indices).toBeInstanceOf(Uint32Array);
        expect(preview.zones[0]?.weights).toEqual(new Float32Array([0, 0.5, 1]));
    });
});
