import type { BufferGeometry, Matrix3, Texture } from "three";
import { BufferAttribute, DataTexture, NoColorSpace, RGBAFormat } from "three";

// Kept apart from Three.js "color", which the body-shape editor writes and which
// would tint the albedo. GIMI stores AO in R, ramp width in G, outline width in A.
export const TOON_VERTEX_COLOR_ATTRIBUTE = "toonVertexColor";

/** Bound to texture uniforms that have no map, so samplers never read an unbound unit. */
export const fallbackWhiteTexture = new DataTexture(
    new Uint8Array([255, 255, 255, 255]),
    1,
    1,
    RGBAFormat,
);
fallbackWhiteTexture.colorSpace = NoColorSpace;
fallbackWhiteTexture.needsUpdate = true;

export function replaceShaderAnchor(source: string, anchor: string, replacement: string): string {
    const first = source.indexOf(anchor);
    if (first < 0 || source.indexOf(anchor, first + anchor.length) >= 0) {
        throw new Error(`Three.js shader anchor changed: ${anchor}`);
    }
    return source.slice(0, first) + replacement + source.slice(first + anchor.length);
}

// Direct DDS textures flip V through this transform, which three applies only to
// a material's own map slots.
export function textureUvTransform(texture: Texture): Matrix3 {
    texture.updateMatrix();
    return texture.matrix;
}

// Vertices split along UV seams carry different normals, which would tear the hull
// open along every seam. The game stores an averaged normal for the same reason.
export function averagedNormals(geometry: BufferGeometry): BufferAttribute {
    const position = geometry.attributes.position;
    const normal = geometry.attributes.normal;
    const sums = new Map<string, [number, number, number]>();
    const key = (index: number) =>
        `${position.getX(index).toFixed(5)},${position.getY(index).toFixed(5)},${position.getZ(index).toFixed(5)}`;
    for (let index = 0; index < position.count; index++) {
        const sum = sums.get(key(index)) ?? [0, 0, 0];
        sum[0] += normal.getX(index);
        sum[1] += normal.getY(index);
        sum[2] += normal.getZ(index);
        sums.set(key(index), sum);
    }

    const output = new Float32Array(position.count * 3);
    for (let index = 0; index < position.count; index++) {
        const [x, y, z] = sums.get(key(index)) ?? [0, 0, 0];
        const length = Math.hypot(x, y, z) || 1;
        output.set([x / length, y / length, z / length], index * 3);
    }
    return new BufferAttribute(output, 3);
}
