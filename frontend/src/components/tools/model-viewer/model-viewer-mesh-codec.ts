export function decodeModelViewerMesh(buffer: ArrayBuffer) {
    if (buffer.byteLength < 28 || buffer.byteLength % 4 !== 0) {
        throw new RangeError("Invalid model viewer mesh buffer length");
    }
    const [
        magic,
        positionsCount,
        normalsCount,
        tangentsCount,
        uvsCount,
        colorsCount,
        indicesCount,
    ] = new Uint32Array(buffer, 0, 7);
    const vertexCount = positionsCount / 3;
    if (
        magic !== 0x3247564d ||
        !Number.isInteger(vertexCount) ||
        (normalsCount !== 0 && normalsCount !== positionsCount) ||
        (tangentsCount !== 0 && tangentsCount !== vertexCount * 4) ||
        (uvsCount !== 0 && uvsCount !== vertexCount * 2) ||
        (colorsCount !== 0 && colorsCount !== vertexCount * 4) ||
        buffer.byteLength !==
            28 +
                colorsCount +
                (positionsCount + normalsCount + tangentsCount + uvsCount + indicesCount) * 4
    ) {
        throw new RangeError("Invalid model viewer mesh header");
    }

    // Windows uses little-endian typed arrays. Keep views on the response buffer.
    const normalsOffset = 28 + positionsCount * 4;
    const tangentsOffset = normalsOffset + normalsCount * 4;
    const uvsOffset = tangentsOffset + tangentsCount * 4;
    const colorsOffset = uvsOffset + uvsCount * 4;
    return {
        positions: new Float32Array(buffer, 28, positionsCount),
        normals: normalsCount ? new Float32Array(buffer, normalsOffset, normalsCount) : undefined,
        tangents: tangentsCount
            ? new Float32Array(buffer, tangentsOffset, tangentsCount)
            : undefined,
        uvs: uvsCount ? new Float32Array(buffer, uvsOffset, uvsCount) : undefined,
        colors: colorsCount ? new Uint8Array(buffer, colorsOffset, colorsCount) : undefined,
        indices: new Uint32Array(buffer, colorsOffset + colorsCount, indicesCount),
    };
}
