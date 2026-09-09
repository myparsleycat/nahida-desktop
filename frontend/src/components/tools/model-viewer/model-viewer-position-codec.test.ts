import { describe, expect, it } from "vitest";

import { decodeModelViewerGeometry } from "./model-viewer-position-codec";

describe("prepared model viewer geometry", () => {
    it("views compact positions and normals without copying the backend result", () => {
        const buffer = new ArrayBuffer(80 + 2 * 24);
        new Float64Array(buffer, 0, 10).set([0, 0, 0, 4, 5, 6, 2, 2.5, 3, 5]);
        new Float32Array(buffer, 80).set([1, 2, 3, 4, 5, 6, 0, 0, 1, 0, 1, 0]);
        const result = decodeModelViewerGeometry(buffer, 2);
        expect(Array.from(result.positions)).toEqual([1, 2, 3, 4, 5, 6]);
        expect(Array.from(result.normals)).toEqual([0, 0, 1, 0, 1, 0]);
        expect(result.bounds).toEqual({
            min: [0, 0, 0],
            max: [4, 5, 6],
            center: [2, 2.5, 3],
            radius: 5,
        });
        expect(result.positions.buffer).toBe(buffer);
        expect(result.normals.buffer).toBe(buffer);
    });
    it("rejects malformed lengths and bounds", () => {
        expect(() => decodeModelViewerGeometry(new ArrayBuffer(80), 1)).toThrow(RangeError);
        expect(() => decodeModelViewerGeometry(new ArrayBuffer(80), -1)).toThrow(RangeError);
        const buffer = new ArrayBuffer(104);
        new Float64Array(buffer, 0, 10)[9] = NaN;
        expect(() => decodeModelViewerGeometry(buffer, 1)).toThrow(RangeError);
    });
});
