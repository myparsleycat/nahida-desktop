import type { ModelViewerBounds } from "@bindings/tools";

export type ModelViewerPositionGeometry = {
    positions: Float32Array;
    normals: Float32Array;
    bounds: ModelViewerBounds;
};

export function decodeModelViewerGeometry(
    buffer: ArrayBuffer,
    vertexCount: number,
): ModelViewerPositionGeometry {
    if (
        !Number.isSafeInteger(vertexCount) ||
        vertexCount < 0 ||
        buffer.byteLength !== 80 + vertexCount * 24
    ) {
        throw new RangeError("Invalid model viewer geometry buffer length");
    }
    const header = new Float64Array(buffer, 0, 10);
    if (!header.every(Number.isFinite) || header[9] < 0)
        throw new RangeError("Invalid model viewer geometry bounds");
    return {
        bounds: {
            min: Array.from(header.subarray(0, 3)),
            max: Array.from(header.subarray(3, 6)),
            center: Array.from(header.subarray(6, 9)),
            radius: header[9],
        },
        positions: new Float32Array(buffer, 80, vertexCount * 3),
        normals: new Float32Array(buffer, 80 + vertexCount * 12, vertexCount * 3),
    };
}
