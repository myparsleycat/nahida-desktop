const GL_COMPONENT = {
    FLOAT: 5126,
    UNSIGNED_SHORT: 5123,
    UNSIGNED_INT: 5125,
} as const;

const GL_BUFFER_TARGET = {
    ARRAY_BUFFER: 34962,
    ELEMENT_ARRAY_BUFFER: 34963,
} as const;

const GLB_HEADER_MAGIC = 0x46546c67;
const GLB_VERSION = 2;
const GLB_CHUNK_TYPE_JSON = 0x4e4f534a;
const GLB_CHUNK_TYPE_BIN = 0x004e4942;

type GlTf = {
    asset: { version: "2.0"; generator: string };
    scene: number;
    scenes: Array<{ nodes: number[] }>;
    nodes: Array<{ mesh: number; name: string }>;
    meshes: Array<{ name: string; primitives: unknown[] }>;
    buffers: Array<{ byteLength: number }>;
    bufferViews: Array<Record<string, unknown>>;
    accessors: Array<Record<string, unknown>>;
    materials?: Array<Record<string, unknown>>;
    images?: Array<Record<string, unknown>>;
    textures?: Array<Record<string, unknown>>;
};

export class GlbBuilder {
    private chunks: Buffer[] = [];
    private gltf: GlTf = {
        asset: { version: "2.0", generator: "Nahida Desktop static mod GLB converter" },
        scene: 0,
        scenes: [{ nodes: [] }],
        nodes: [],
        meshes: [],
        buffers: [{ byteLength: 0 }],
        bufferViews: [],
        accessors: [],
    };

    addMesh(name: string, primitive: Record<string, unknown>) {
        const meshIndex = this.gltf.meshes.length;
        this.gltf.meshes.push({ name, primitives: [primitive] });
        const nodeIndex = this.gltf.nodes.length;
        this.gltf.nodes.push({ mesh: meshIndex, name });
        this.gltf.scenes[0].nodes.push(nodeIndex);
    }

    meshCount(): number {
        return this.gltf.meshes.length;
    }

    addAccessorFromFloat32(
        data: Float32Array,
        type: "VEC2" | "VEC3" | "VEC4",
        withMinMax: boolean,
    ): number {
        const buffer = Buffer.from(data.buffer, data.byteOffset, data.byteLength);
        const bufferView = this.addBufferView(buffer, GL_BUFFER_TARGET.ARRAY_BUFFER);
        const accessor: Record<string, unknown> = {
            bufferView,
            byteOffset: 0,
            componentType: GL_COMPONENT.FLOAT,
            count: data.length / typeWidth(type),
            type,
        };
        if (withMinMax) {
            const { min, max } = minMax(data, typeWidth(type));
            accessor.min = min;
            accessor.max = max;
        }
        this.gltf.accessors.push(accessor);
        return this.gltf.accessors.length - 1;
    }

    addAccessorFromIndices(indices: Uint32Array, vertexCount?: number): number {
        const useUint16 = (vertexCount ?? Number.POSITIVE_INFINITY) <= 65535;
        let buffer: Buffer;
        let componentType: number;
        if (useUint16) {
            const compact = new Uint16Array(indices.length);
            compact.set(indices);
            buffer = Buffer.from(compact.buffer);
            componentType = GL_COMPONENT.UNSIGNED_SHORT;
        } else {
            buffer = Buffer.from(indices.buffer, indices.byteOffset, indices.byteLength);
            componentType = GL_COMPONENT.UNSIGNED_INT;
        }
        const bufferView = this.addBufferView(buffer, GL_BUFFER_TARGET.ELEMENT_ARRAY_BUFFER);
        this.gltf.accessors.push({
            bufferView,
            byteOffset: 0,
            componentType,
            count: indices.length,
            type: "SCALAR",
        });
        return this.gltf.accessors.length - 1;
    }

    addImage(data: Buffer, mimeType: string, name?: string): number {
        const bufferView = this.addBufferView(data);
        this.gltf.images ??= [];
        this.gltf.images.push({
            ...(name ? { name } : {}),
            bufferView,
            mimeType,
        });
        return this.gltf.images.length - 1;
    }

    addTexture(source: number): number {
        this.gltf.textures ??= [];
        this.gltf.textures.push({ source });
        return this.gltf.textures.length - 1;
    }

    addMaterial(material: Record<string, unknown>): number {
        this.gltf.materials ??= [];
        this.gltf.materials.push(material);
        return this.gltf.materials.length - 1;
    }

    toGlb(): Buffer {
        const bin = Buffer.concat(this.chunks);
        this.gltf.buffers[0].byteLength = bin.length;
        const json = Buffer.from(JSON.stringify(this.gltf), "utf8");
        const jsonPadded = padBuffer(json, 0x20);
        const binPadded = padBuffer(bin, 0x00);

        const totalLength = 12 + 8 + jsonPadded.length + 8 + binPadded.length;
        const out = Buffer.alloc(totalLength);
        let offset = 0;
        out.writeUInt32LE(GLB_HEADER_MAGIC, offset);
        offset += 4;
        out.writeUInt32LE(GLB_VERSION, offset);
        offset += 4;
        out.writeUInt32LE(totalLength, offset);
        offset += 4;
        out.writeUInt32LE(jsonPadded.length, offset);
        offset += 4;
        out.writeUInt32LE(GLB_CHUNK_TYPE_JSON, offset);
        offset += 4;
        jsonPadded.copy(out, offset);
        offset += jsonPadded.length;
        out.writeUInt32LE(binPadded.length, offset);
        offset += 4;
        out.writeUInt32LE(GLB_CHUNK_TYPE_BIN, offset);
        offset += 4;
        binPadded.copy(out, offset);
        return out;
    }

    private addBufferView(data: Buffer, target?: number): number {
        const aligned = padBuffer(data, 0x00);
        const byteOffset = this.chunks.reduce((sum, chunk) => sum + chunk.length, 0);
        this.chunks.push(aligned);
        this.gltf.bufferViews.push({
            buffer: 0,
            byteOffset,
            byteLength: data.length,
            ...(target ? { target } : {}),
        });
        return this.gltf.bufferViews.length - 1;
    }
}

function typeWidth(type: "SCALAR" | "VEC2" | "VEC3" | "VEC4"): number {
    if (type === "SCALAR") return 1;
    if (type === "VEC2") return 2;
    if (type === "VEC3") return 3;
    return 4;
}

function minMax(data: Float32Array, width: number): { min: number[]; max: number[] } {
    const min = Array(width).fill(Number.POSITIVE_INFINITY);
    const max = Array(width).fill(Number.NEGATIVE_INFINITY);
    for (let i = 0; i < data.length; i += width) {
        for (let c = 0; c < width; c++) {
            const value = data[i + c];
            min[c] = Math.min(min[c], value);
            max[c] = Math.max(max[c], value);
        }
    }
    return { min, max };
}

function padBuffer(buffer: Buffer, fill: number): Buffer {
    const paddedLength = (buffer.length + 3) & ~3;
    if (paddedLength === buffer.length) return buffer;
    const out = Buffer.alloc(paddedLength, fill);
    buffer.copy(out);
    return out;
}
