export function decodeModelViewerMesh(buffer: ArrayBuffer) {
    if (buffer.byteLength < 28 || buffer.byteLength % 4 !== 0) {
        throw new RangeError("Invalid model viewer mesh buffer length");
    }
    const magic = new Uint32Array(buffer, 0, 1)[0];
    const version3 = magic === 0x3347564d;
    const headerBytes = version3 ? 32 : 28;
    if ((magic !== 0x3247564d && !version3) || buffer.byteLength < headerBytes) {
        throw new RangeError("Invalid model viewer mesh header");
    }
    const counts = new Uint32Array(buffer, 4, version3 ? 7 : 6);
    const [positionsCount, normalsCount, tangentsCount, uvsCount] = counts;
    const uvs1Count = version3 ? counts[4] : 0;
    const colorsCount = counts[version3 ? 5 : 4];
    const indicesCount = counts[version3 ? 6 : 5];
    const vertexCount = positionsCount / 3;
    if (
        !Number.isInteger(vertexCount) ||
        (normalsCount !== 0 && normalsCount !== positionsCount) ||
        (tangentsCount !== 0 && tangentsCount !== vertexCount * 4) ||
        (uvsCount !== 0 && uvsCount !== vertexCount * 2) ||
        (uvs1Count !== 0 && uvs1Count !== vertexCount * 2) ||
        (colorsCount !== 0 && colorsCount !== vertexCount * 4) ||
        buffer.byteLength !==
            headerBytes +
                colorsCount +
                (positionsCount +
                    normalsCount +
                    tangentsCount +
                    uvsCount +
                    uvs1Count +
                    indicesCount) *
                    4
    ) {
        throw new RangeError("Invalid model viewer mesh header");
    }

    // Windows uses little-endian typed arrays. Keep views on the response buffer.
    const normalsOffset = headerBytes + positionsCount * 4;
    const tangentsOffset = normalsOffset + normalsCount * 4;
    const uvsOffset = tangentsOffset + tangentsCount * 4;
    const uvs1Offset = uvsOffset + uvsCount * 4;
    const colorsOffset = uvs1Offset + uvs1Count * 4;
    return {
        positions: new Float32Array(buffer, headerBytes, positionsCount),
        normals: normalsCount ? new Float32Array(buffer, normalsOffset, normalsCount) : undefined,
        tangents: tangentsCount
            ? new Float32Array(buffer, tangentsOffset, tangentsCount)
            : undefined,
        uvs: uvsCount ? new Float32Array(buffer, uvsOffset, uvsCount) : undefined,
        uvs1: uvs1Count ? new Float32Array(buffer, uvs1Offset, uvs1Count) : undefined,
        colors: colorsCount ? new Uint8Array(buffer, colorsOffset, colorsCount) : undefined,
        indices: new Uint32Array(buffer, colorsOffset + colorsCount, indicesCount),
    };
}
