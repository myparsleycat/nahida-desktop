import type { BufferGeometry, Matrix3, Mesh, ShaderMaterial, Texture } from "three";
import { BufferAttribute, DataTexture, NoColorSpace, RGBAFormat, Vector3 } from "three";

import { modelViewerDDSFormatOf } from "./model-viewer-dds";

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

export const TOON_STEP_GLSL = `// A step whose edge spans one screen pixel, so toon boundaries do not alias.
float toonStep( float edge, float x ) {
    float width = max( fwidth( x ), 1e-4 );
    return smoothstep( edge - width, edge + width, x );
}`;

/** The light map a toon shader may read; one- and two-channel containers have no material ID or specular mask. */
export function toonLightMapOf(texture: Texture | undefined): Texture | undefined {
    const format = modelViewerDDSFormatOf(texture);
    return format.startsWith("bc4-") || format.startsWith("bc5-") ? undefined : texture;
}

/** The mean of a mesh's normals, which points out of a face in whatever space the mod's buffers use. */
export function meanNormal(geometry: BufferGeometry): Vector3 | undefined {
    const normal = geometry.attributes.normal;
    const sum = new Vector3();
    for (let vertex = 0; vertex < normal.count; vertex++) {
        sum.x += normal.getX(vertex);
        sum.y += normal.getY(vertex);
        sum.z += normal.getZ(vertex);
    }
    return sum.lengthSq() > 0 ? sum.normalize() : undefined;
}

/**
 * Keeps an outline hull whose extrusion source matches the tangent check, rebuilding it
 * when the check flips. The check is unknown until the maps are applied and is redone
 * when the normal or tangent attributes change; until then the current hull stays.
 */
export function syncToonOutlineSource<Outline extends Mesh<BufferGeometry, ShaderMaterial>>(
    current: Outline | undefined,
    tangentFrameUsable: boolean | undefined,
    create: (tangentHoldsOutlineNormal: boolean) => Outline,
): Outline | undefined {
    if (tangentFrameUsable === undefined) {
        return current;
    }
    const tangentOutline = !tangentFrameUsable;
    if (current?.userData.toonTangentOutline === tangentOutline) {
        return current;
    }
    current?.removeFromParent();
    current?.material.dispose();
    const outline = create(tangentOutline);
    outline.userData.toonTangentOutline = tangentOutline;
    return outline;
}

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
