export const TOUCH_PREVIEW_SIZE = 768;
export const TOUCH_PREVIEW_PAD = 32;

export type TouchViewName = "front" | "back" | "left" | "right" | "top";

export const TOUCH_VIEW_NAMES: TouchViewName[] = ["front", "back", "left", "right", "top"];

export type TouchViewProjector = {
    view: TouchViewName;
    project: (x: number, y: number, z: number) => readonly [number, number];
    /** Camera depth where smaller values are closer to the view. */
    depth: (x: number, y: number, z: number) => number;
};

export const TOUCH_VIEW_PROJECTORS: TouchViewProjector[] = [
    // Mesh axes: +X character-right, +Y back, +Z up. Smaller depth = closer to camera.
    { view: "front", project: (x, _y, z) => [x, z], depth: (_x, y, _z) => y },
    { view: "back", project: (x, _y, z) => [-x, z], depth: (_x, y, _z) => -y },
    // From character-left (-X): front (-Y) appears on image-right → screen x = -y.
    { view: "left", project: (_x, y, z) => [-y, z], depth: (x, _y, _z) => x },
    // From character-right (+X): front (-Y) appears on image-left → screen x = y.
    { view: "right", project: (_x, y, z) => [y, z], depth: (x, _y, _z) => -x },
    { view: "top", project: (x, y, _z) => [x, -y], depth: (_x, _y, z) => -z },
];

export type TouchViewTransform = {
    view: TouchViewName;
    /** Projected XY before image framing. */
    projected: Float32Array;
    minX: number;
    maxX: number;
    minY: number;
    maxY: number;
    scale: number;
    centerX: number;
    centerY: number;
    size: number;
    depth: Float32Array;
    visibleVertices?: Uint8Array;
};

export function buildViewTransform(
    positions: Float32Array,
    projector: TouchViewProjector,
    size = TOUCH_PREVIEW_SIZE,
    pad = TOUCH_PREVIEW_PAD,
): TouchViewTransform {
    const vertexCount = positions.length / 3;
    const projected = new Float32Array(vertexCount * 2);
    let minX = Number.POSITIVE_INFINITY;
    let maxX = Number.NEGATIVE_INFINITY;
    let minY = Number.POSITIVE_INFINITY;
    let maxY = Number.NEGATIVE_INFINITY;
    const depth = new Float32Array(vertexCount);

    for (let i = 0; i < vertexCount; i++) {
        const x = positions[i * 3];
        const y = positions[i * 3 + 1];
        const z = positions[i * 3 + 2];
        const [projectedX, projectedY] = projector.project(x, y, z);
        projected[i * 2] = projectedX;
        projected[i * 2 + 1] = projectedY;
        depth[i] = projector.depth(x, y, z);
        minX = Math.min(minX, projectedX);
        maxX = Math.max(maxX, projectedX);
        minY = Math.min(minY, projectedY);
        maxY = Math.max(maxY, projectedY);
    }

    if (!Number.isFinite(minX)) {
        minX = -1;
        maxX = 1;
        minY = -1;
        maxY = 1;
    }

    const scale = Math.min(
        (size - pad * 2) / Math.max(maxX - minX, 1e-6),
        (size - pad * 2) / Math.max(maxY - minY, 1e-6),
    );

    return {
        view: projector.view,
        projected,
        minX,
        maxX,
        minY,
        maxY,
        scale,
        centerX: (minX + maxX) * 0.5,
        centerY: (minY + maxY) * 0.5,
        size,
        depth,
    };
}

export function buildAllViewTransforms(positions: Float32Array) {
    return Object.fromEntries(
        TOUCH_VIEW_PROJECTORS.map((projector) => [
            projector.view,
            buildViewTransform(positions, projector),
        ]),
    ) as Record<TouchViewName, TouchViewTransform>;
}

/** Map mesh vertex to normalized image coords (x right, y down, 0..1). */
export function vertexToNormalized(
    transform: TouchViewTransform,
    vertexIndex: number,
): [number, number] {
    const x =
        ((transform.projected[vertexIndex * 2] - transform.centerX) * transform.scale +
            transform.size / 2) /
        transform.size;
    const y =
        (transform.size / 2 -
            (transform.projected[vertexIndex * 2 + 1] - transform.centerY) * transform.scale) /
        transform.size;
    return [x, y];
}

export function normalizePolygonPoint(
    point: [number, number],
    size = TOUCH_PREVIEW_SIZE,
): [number, number] {
    const [x, y] = point;
    // Accept either normalized 0..1 or pixel coordinates from the preview size.
    if (x > 1.5 || y > 1.5 || x < -0.05 || y < -0.05) {
        return [x / size, y / size];
    }
    return [x, y];
}
