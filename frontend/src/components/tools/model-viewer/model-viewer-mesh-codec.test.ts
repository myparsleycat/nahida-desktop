import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

import { decodeModelViewerMesh } from "./model-viewer-mesh-codec";

const fixture = Buffer.from(
    readFileSync(
        new URL("../../../../../internal/tools/testdata/model_viewer_mesh.hex", import.meta.url),
        "utf8",
    ).trim(),
    "hex",
);

describe("model viewer mesh wire format", () => {
    it("reads the Go encoder fixture without copying attributes", () => {
        const buffer = Uint8Array.from(fixture).buffer;
        const mesh = decodeModelViewerMesh(buffer);
        expect([...mesh.positions]).toEqual([1, -2, 3]);
        expect([...mesh.normals!]).toEqual([0, 0, 1]);
        expect([...mesh.tangents!]).toEqual([1, 0, 0, -1]);
        expect([...mesh.uvs!]).toEqual([0.25, 0.75]);
        expect([...mesh.indices]).toEqual([0, 0, 0]);
        for (const attribute of Object.values(mesh)) {
            expect(attribute?.buffer).toBe(buffer);
        }
    });

    it("accepts omitted optional attributes", () => {
        const buffer = new ArrayBuffer(48);
        new Uint32Array(buffer, 0, 6).set([0x3147564d, 3, 0, 0, 0, 3]);
        const mesh = decodeModelViewerMesh(buffer);
        expect(mesh.normals).toBeUndefined();
        expect(mesh.tangents).toBeUndefined();
        expect(mesh.uvs).toBeUndefined();
        expect(mesh.positions.length).toBe(3);
        expect(mesh.indices.length).toBe(3);
    });

    it.each([
        [0, 0],
        [1, 4],
        [2, 2],
        [3, 3],
        [4, 1],
        [5, 0xffffffff],
    ])("rejects malformed header field %i", (field, value) => {
        const buffer = Uint8Array.from(fixture).buffer;
        new Uint32Array(buffer, 0, 6)[field] = value;
        expect(() => decodeModelViewerMesh(buffer)).toThrow(RangeError);
    });

    it.each([0, 23, 25, fixture.length - 4, fixture.length + 4])(
        "rejects invalid length %i",
        (length) => {
            const buffer = new ArrayBuffer(length);
            new Uint8Array(buffer).set(fixture.subarray(0, length));
            expect(() => decodeModelViewerMesh(buffer)).toThrow(RangeError);
        },
    );
});
