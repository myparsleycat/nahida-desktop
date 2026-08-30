import { describe, expect, it } from "vitest";

import { decodeModelViewerPositions, ModelViewerByteLRU } from "./model-viewer-position-codec";

function positionSource(values: Array<[number, number, number]>, stride: number) {
    const buffer = new ArrayBuffer(values.length * stride);
    const view = new DataView(buffer);
    for (const [index, value] of values.entries()) {
        view.setFloat32(index * stride, value[0], true);
        view.setFloat32(index * stride + 4, value[1], true);
        view.setFloat32(index * stride + 8, value[2], true);
    }
    return buffer;
}

describe("model viewer position decoder", () => {
    it("applies the source stride and compact source-index mapping", () => {
        const source = positionSource(
            [
                [1, 2, 3],
                [4, 5, 6],
                [7, 8, 9],
            ],
            40,
        );
        expect(
            Array.from(decodeModelViewerPositions(source, 40, new Uint32Array([2, 0]), 2)),
        ).toEqual([7, 8, 9, 1, 2, 3]);
        expect(Array.from(decodeModelViewerPositions(source, 40, undefined, 3))).toEqual([
            1, 2, 3, 4, 5, 6, 7, 8, 9,
        ]);
    });

    it("keeps the combined raw cache within its byte limit", () => {
        const cache = new ModelViewerByteLRU(10);
        cache.set("first", new ArrayBuffer(6));
        cache.set("second", new ArrayBuffer(6));
        expect(cache.sizeBytes).toBe(6);
        expect(cache.get("first")).toBeUndefined();
        expect(cache.get("second")?.byteLength).toBe(6);

        cache.set("oversized", new ArrayBuffer(11));
        expect(cache.sizeBytes).toBe(6);
        expect(cache.get("oversized")).toBeUndefined();
    });
});
