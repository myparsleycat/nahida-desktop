import { readFloatAttribute as readFloatAttributeNative } from "@native/static-glb";
import type { GlbBuilder } from "./builder";
import {
    bufferToFloat32Array,
    ensureVec4,
    formatComponentCount,
    normalizeTangentArray,
    normalizeVec3Array,
    readDxgiValues,
    removeDegenerateTriangles,
} from "./dxgi-utils";
import type { FmtElement, FmtLayout } from "./types";

export function buildPrimitive(
    builder: GlbBuilder,
    vb: Buffer,
    stride: number,
    fmt: FmtLayout,
    indices: Uint32Array,
    options: { includeTangents: boolean; includeVertexColors: boolean; compactVertices?: boolean },
    warn: (message: string) => void,
): Record<string, unknown> | null {
    const mesh = extractPrimitiveGeometry(vb, stride, fmt, indices, options, warn);
    if (!mesh) {
        return null;
    }

    const attributes: Record<string, number> = {
        POSITION: builder.addAccessorFromFloat32(mesh.position, "VEC3", true),
    };
    if (mesh.normal) {
        attributes.NORMAL = builder.addAccessorFromFloat32(mesh.normal, "VEC3", false);
    }
    if (mesh.tangent) {
        attributes.TANGENT = builder.addAccessorFromFloat32(mesh.tangent, "VEC4", false);
    }
    if (mesh.texcoord0) {
        attributes.TEXCOORD_0 = builder.addAccessorFromFloat32(mesh.texcoord0, "VEC2", false);
    }
    if (mesh.color0) {
        attributes.COLOR_0 = builder.addAccessorFromFloat32(mesh.color0, "VEC4", false);
    }

    return {
        attributes,
        indices: builder.addAccessorFromIndices(mesh.indices, mesh.vertexCount),
        mode: 4,
    };
}

export function extractPrimitiveGeometry(
    vb: Buffer,
    stride: number,
    fmt: FmtLayout,
    indices: Uint32Array,
    options: { includeTangents: boolean; includeVertexColors?: boolean; compactVertices?: boolean },
    warn: (message: string) => void,
): {
    position: Float32Array;
    normal?: Float32Array;
    tangent?: Float32Array;
    texcoord0?: Float32Array;
    color0?: Float32Array;
    indices: Uint32Array;
    vertexCount: number;
} | null {
    if (fmt.topology.toLowerCase() !== "trianglelist") {
        throw new Error(`Unsupported topology: ${fmt.topology}`);
    }

    const vertexCount = Math.floor(vb.length / stride);
    const position = findElement(fmt, "POSITION");
    if (!position) {
        return null;
    }
    const meshIndices = removeDegenerateTriangles(indices, warn);
    if (options.compactVertices && shouldCompactGeometry(meshIndices, vertexCount)) {
        const compacted = extractCompactedPrimitiveGeometry(
            vb,
            stride,
            fmt,
            meshIndices,
            vertexCount,
            options,
            warn,
        );
        if (compacted) {
            return compacted;
        }
    }
    const positionData = readRequiredFloatAttribute(
        vb,
        stride,
        vertexCount,
        position,
        3,
        "POSITION",
        warn,
    );
    if (!positionData) {
        return null;
    }

    const mesh: {
        position: Float32Array;
        normal?: Float32Array;
        tangent?: Float32Array;
        texcoord0?: Float32Array;
        color0?: Float32Array;
        indices: Uint32Array;
        vertexCount: number;
    } = {
        position: positionData,
        indices: meshIndices,
        vertexCount,
    };

    const normal = findElement(fmt, "NORMAL");
    if (normal) {
        const normalData = readOptionalFloatAttribute(
            vb,
            stride,
            vertexCount,
            normal,
            3,
            "NORMAL",
            warn,
        );
        if (normalData) {
            mesh.normal = normalizeVec3Array(normalData);
        }
    }

    const tangent = findElement(fmt, "TANGENT");
    if (tangent && options.includeTangents) {
        const width = formatComponentCount(tangent.format);
        const tangentData = readOptionalFloatAttribute(
            vb,
            stride,
            vertexCount,
            tangent,
            Math.min(width, 4),
            "TANGENT",
            warn,
        );
        if (tangentData) {
            mesh.tangent = normalizeTangentArray(ensureVec4(tangentData, vertexCount, width));
        }
    }

    const texcoord0 = findElement(fmt, "TEXCOORD", 0);
    if (texcoord0) {
        mesh.texcoord0 = readOptionalFloatAttribute(
            vb,
            stride,
            vertexCount,
            texcoord0,
            2,
            "TEXCOORD_0",
            warn,
        );
    }

    const color0 = findElement(fmt, "COLOR", 0);
    if (color0 && options.includeVertexColors) {
        const colorWidth = Math.min(formatComponentCount(color0.format), 4);
        const colorData = readOptionalFloatAttribute(
            vb,
            stride,
            vertexCount,
            color0,
            colorWidth,
            "COLOR_0",
            warn,
        );
        if (colorData) {
            mesh.color0 = ensureVec4(colorData, vertexCount, colorWidth, 1);
        }
    }

    return mesh;
}

function shouldCompactGeometry(indices: Uint32Array, sourceVertexCount: number): boolean {
    if (indices.length === 0 || sourceVertexCount <= 0) {
        return false;
    }

    return indices.length < sourceVertexCount * 0.75;
}

function extractCompactedPrimitiveGeometry(
    vb: Buffer,
    stride: number,
    fmt: FmtLayout,
    indices: Uint32Array,
    sourceVertexCount: number,
    options: { includeTangents: boolean; includeVertexColors?: boolean; compactVertices?: boolean },
    warn: (message: string) => void,
): {
    position: Float32Array;
    normal?: Float32Array;
    tangent?: Float32Array;
    texcoord0?: Float32Array;
    color0?: Float32Array;
    indices: Uint32Array;
    vertexCount: number;
} | null {
    const compactIndex = buildCompactIndexMap(indices, sourceVertexCount, warn);
    if (!compactIndex) {
        return null;
    }

    const position = findElement(fmt, "POSITION");
    if (!position) {
        return null;
    }

    const positionData = readIndexedRequiredFloatAttribute(
        vb,
        stride,
        sourceVertexCount,
        compactIndex.sourceIndices,
        position,
        3,
        "POSITION",
        warn,
    );
    if (!positionData) {
        return null;
    }

    const mesh: {
        position: Float32Array;
        normal?: Float32Array;
        tangent?: Float32Array;
        texcoord0?: Float32Array;
        color0?: Float32Array;
        indices: Uint32Array;
        vertexCount: number;
    } = {
        position: positionData,
        indices: compactIndex.indices,
        vertexCount: compactIndex.sourceIndices.length,
    };

    const normal = findElement(fmt, "NORMAL");
    if (normal) {
        const normalData = readIndexedOptionalFloatAttribute(
            vb,
            stride,
            sourceVertexCount,
            compactIndex.sourceIndices,
            normal,
            3,
            "NORMAL",
            warn,
        );
        if (normalData) {
            mesh.normal = normalizeVec3Array(normalData);
        }
    }

    const tangent = findElement(fmt, "TANGENT");
    if (tangent && options.includeTangents) {
        const width = Math.min(formatComponentCount(tangent.format), 4);
        const tangentData = readIndexedOptionalFloatAttribute(
            vb,
            stride,
            sourceVertexCount,
            compactIndex.sourceIndices,
            tangent,
            width,
            "TANGENT",
            warn,
        );
        if (tangentData) {
            mesh.tangent = normalizeTangentArray(
                ensureVec4(tangentData, compactIndex.sourceIndices.length, width),
            );
        }
    }

    const texcoord0 = findElement(fmt, "TEXCOORD", 0);
    if (texcoord0) {
        mesh.texcoord0 = readIndexedOptionalFloatAttribute(
            vb,
            stride,
            sourceVertexCount,
            compactIndex.sourceIndices,
            texcoord0,
            2,
            "TEXCOORD_0",
            warn,
        );
    }

    const color0 = findElement(fmt, "COLOR", 0);
    if (color0 && options.includeVertexColors) {
        const colorWidth = Math.min(formatComponentCount(color0.format), 4);
        const colorData = readIndexedOptionalFloatAttribute(
            vb,
            stride,
            sourceVertexCount,
            compactIndex.sourceIndices,
            color0,
            colorWidth,
            "COLOR_0",
            warn,
        );
        if (colorData) {
            mesh.color0 = ensureVec4(colorData, compactIndex.sourceIndices.length, colorWidth, 1);
        }
    }

    return mesh;
}

export function buildCompactIndexMap(
    indices: Uint32Array,
    sourceVertexCount: number,
    warn: (message: string) => void,
): { indices: Uint32Array; sourceIndices: number[] } | null {
    const sourceToCompact = new Map<number, number>();
    const sourceIndices: number[] = [];
    const remapped = new Uint32Array(indices.length);

    for (let index = 0; index < indices.length; index += 1) {
        const sourceIndex = indices[index];
        if (sourceIndex >= sourceVertexCount) {
            warn(
                `Skipping compacted animation geometry: index ${sourceIndex} exceeds vertex count ${sourceVertexCount}`,
            );
            return null;
        }

        let compactIndex = sourceToCompact.get(sourceIndex);
        if (compactIndex === undefined) {
            compactIndex = sourceIndices.length;
            sourceToCompact.set(sourceIndex, compactIndex);
            sourceIndices.push(sourceIndex);
        }
        remapped[index] = compactIndex;
    }

    return { indices: remapped, sourceIndices };
}

export function findElement(
    fmt: FmtLayout,
    semantic: string,
    index?: number,
): FmtElement | undefined {
    return fmt.elements.find((element) => {
        if (element.semanticName.toUpperCase() !== semantic) return false;
        return index === undefined || element.semanticIndex === index;
    });
}

export function readFloatAttribute(
    bytes: Buffer,
    stride: number,
    vertexCount: number,
    element: FmtElement,
    width: number,
): Float32Array {
    return bufferToFloat32Array(
        readFloatAttributeNative(
            bytes,
            stride,
            vertexCount,
            element.alignedByteOffset,
            element.format,
            width,
        ),
    );
}

export function readRequiredFloatAttribute(
    bytes: Buffer,
    stride: number,
    vertexCount: number,
    element: FmtElement,
    width: number,
    label: string,
    warn: (message: string) => void,
): Float32Array | null {
    if (!validateFloatAttributeRead(bytes, stride, vertexCount, element, label, warn)) {
        return null;
    }

    try {
        return readFloatAttribute(bytes, stride, vertexCount, element, width);
    } catch (error) {
        warn(
            `Skipping primitive: failed to read ${label} (${element.format} @ ${element.alignedByteOffset}): ${
                error instanceof Error ? error.message : String(error)
            }`,
        );
        return null;
    }
}

export function readOptionalFloatAttribute(
    bytes: Buffer,
    stride: number,
    vertexCount: number,
    element: FmtElement,
    width: number,
    label: string,
    warn: (message: string) => void,
): Float32Array | undefined {
    if (!validateFloatAttributeRead(bytes, stride, vertexCount, element, label, warn)) {
        return undefined;
    }

    try {
        return readFloatAttribute(bytes, stride, vertexCount, element, width);
    } catch (error) {
        warn(
            `Skipping ${label}: failed to read ${element.format} @ ${element.alignedByteOffset}: ${
                error instanceof Error ? error.message : String(error)
            }`,
        );
        return undefined;
    }
}

export function readIndexedRequiredFloatAttribute(
    bytes: Buffer,
    stride: number,
    vertexCount: number,
    sourceIndices: number[],
    element: FmtElement,
    width: number,
    label: string,
    warn: (message: string) => void,
): Float32Array | null {
    if (!validateFloatAttributeRead(bytes, stride, vertexCount, element, label, warn)) {
        return null;
    }

    try {
        return readIndexedFloatAttribute(bytes, stride, sourceIndices, element, width);
    } catch (error) {
        warn(
            `Skipping primitive: failed to read compacted ${label} (${element.format} @ ${element.alignedByteOffset}): ${
                error instanceof Error ? error.message : String(error)
            }`,
        );
        return null;
    }
}

export function readIndexedOptionalFloatAttribute(
    bytes: Buffer,
    stride: number,
    vertexCount: number,
    sourceIndices: number[],
    element: FmtElement,
    width: number,
    label: string,
    warn: (message: string) => void,
): Float32Array | undefined {
    if (!validateFloatAttributeRead(bytes, stride, vertexCount, element, label, warn)) {
        return undefined;
    }

    try {
        return readIndexedFloatAttribute(bytes, stride, sourceIndices, element, width);
    } catch (error) {
        warn(
            `Skipping compacted ${label}: failed to read ${element.format} @ ${element.alignedByteOffset}: ${
                error instanceof Error ? error.message : String(error)
            }`,
        );
        return undefined;
    }
}

function readIndexedFloatAttribute(
    bytes: Buffer,
    stride: number,
    sourceIndices: number[],
    element: FmtElement,
    width: number,
): Float32Array {
    const output = new Float32Array(sourceIndices.length * width);
    for (let index = 0; index < sourceIndices.length; index += 1) {
        const values = readDxgiValues(
            bytes,
            sourceIndices[index] * stride + element.alignedByteOffset,
            element.format,
        );
        const outputOffset = index * width;
        for (let component = 0; component < width; component += 1) {
            output[outputOffset + component] = values[component] ?? 0;
        }
    }
    return output;
}

export function validateFloatAttributeRead(
    bytes: Buffer,
    stride: number,
    vertexCount: number,
    element: FmtElement,
    label: string,
    warn: (message: string) => void,
): boolean {
    const byteSize = formatByteSize(element.format);
    if (byteSize <= 0) {
        warn(`Skipping ${label}: unsupported attribute format ${element.format}`);
        return false;
    }

    const endOffset = element.alignedByteOffset + byteSize;
    if (element.alignedByteOffset < 0 || endOffset > stride) {
        warn(
            `Skipping ${label}: ${element.format} @ ${element.alignedByteOffset} exceeds vertex stride ${stride}`,
        );
        return false;
    }

    if (vertexCount <= 0) {
        return true;
    }

    const lastReadEnd = (vertexCount - 1) * stride + endOffset;
    if (lastReadEnd > bytes.length) {
        warn(
            `Skipping ${label}: ${element.format} @ ${element.alignedByteOffset} exceeds vertex buffer length ${bytes.length}`,
        );
        return false;
    }

    return true;
}

function formatByteSize(format: string): number {
    const upper = format.toUpperCase();
    if (upper === "DXGI_FORMAT_R10G10B10A2_UNORM") return 4;
    const count = formatComponentCount(upper);
    if (upper.includes("32")) return count * 4;
    if (upper.includes("16")) return count * 2;
    if (upper.includes("8")) return count;
    return 0;
}
