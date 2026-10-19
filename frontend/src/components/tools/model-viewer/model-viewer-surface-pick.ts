import { fetchUint32 } from "@renderer/wails/binary-memory";
import {
    type Camera,
    type Intersection,
    Mesh,
    type Object3D,
    Raycaster,
    Triangle,
    Vector2,
    Vector3,
} from "three";

import type { ModelViewerSurfacePick, ModelViewerVec3 } from "./model-viewer-contract";

const raycaster = new Raycaster();
const pointer = new Vector2();

function isShown(object: Object3D, root: Object3D): boolean {
    for (let current: Object3D | null = object; current; current = current.parent) {
        if (!current.visible) return false;
        if (current === root) return true;
    }
    return true;
}

/** The nearest visible payload mesh under a canvas-relative pointer in NDC. */
export function raycastModelViewerSurface(
    root: Object3D,
    camera: Camera,
    ndcX: number,
    ndcY: number,
): Intersection<Mesh> | undefined {
    raycaster.setFromCamera(pointer.set(ndcX, ndcY), camera);
    return raycaster
        .intersectObject(root, true)
        .find(
            (hit): hit is Intersection<Mesh> =>
                hit.object instanceof Mesh &&
                typeof hit.object.userData.meshId === "string" &&
                hit.faceIndex != null &&
                isShown(hit.object, root),
        );
}

/**
 * Resolves a hit to source-buffer vertex ids. Compacted payload meshes carry
 * a source index buffer; without one the local index already is the source.
 */
export async function resolveModelViewerSurfacePick(
    hit: Intersection<Mesh>,
    sourceIndexCache: Map<string, Promise<Uint32Array>>,
): Promise<ModelViewerSurfacePick | null> {
    const mesh = hit.object;
    const faceIndex = hit.faceIndex;
    const position = mesh.geometry.attributes.position;
    if (faceIndex == null || !position) return null;

    const index = mesh.geometry.index;
    const corners = [0, 1, 2].map((corner) =>
        index ? index.getX(faceIndex * 3 + corner) : faceIndex * 3 + corner,
    );
    const [a, b, c] = corners.map((corner) => new Vector3().fromBufferAttribute(position, corner));
    const point = mesh.worldToLocal(hit.point.clone());
    const barycentric = Triangle.getBarycoord(point, a, b, c, new Vector3());
    if (!barycentric) return null;

    const sourceUrl: unknown = mesh.userData.sourceIndicesUrl;
    let vertices = corners;
    if (typeof sourceUrl === "string" && sourceUrl) {
        let request = sourceIndexCache.get(sourceUrl);
        if (!request) {
            request = fetchUint32(sourceUrl);
            sourceIndexCache.set(sourceUrl, request);
        }
        const sources = await request;
        vertices = corners.map((corner) => sources[corner]);
    }

    return {
        meshId: mesh.userData.meshId as string,
        faceIndex,
        vertices: vertices as ModelViewerVec3,
        barycentric: barycentric.toArray() as ModelViewerVec3,
        triangle: [
            a.toArray() as ModelViewerVec3,
            b.toArray() as ModelViewerVec3,
            c.toArray() as ModelViewerVec3,
        ],
        point: point.toArray() as ModelViewerVec3,
        normal: new Triangle(a, b, c).getNormal(new Vector3()).toArray() as ModelViewerVec3,
    };
}
