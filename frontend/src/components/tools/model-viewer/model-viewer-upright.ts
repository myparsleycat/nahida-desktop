import { Box3, Euler, Object3D, Vector3 } from "three";

/**
 * Rotation applied to a freshly loaded model root when the model is authored
 * Z-up (character height along +Z), which is how 3DMigoto-dumped ZZMI/WWMI
 * vertex data is frequently stored. Rotating by -90° about X maps the model's
 * dominant Z axis onto the viewer's Y axis, matching the reference mod viewer
 * (scene.js fitTo: setFromAxisAngle((1, 0, 0), -PI / 2)).
 */
export const MODEL_VIEWER_UPRIGHT_ROTATION = new Euler(-Math.PI / 2, 0, 0);

/** Thresholds mirrored from the reference mod viewer's fitTo() heuristic. */
export const UPRIGHT_Z_OVER_Y = 1.5;
export const UPRIGHT_Z_OVER_X = 1.15;

/**
 * Detect whether a model root lies on its back in the viewer's Y-up space.
 *
 * A clearly dominant Z extent is the signal for a Z-up model; anything else
 * (Y-up or X-dominant) is left untouched. Measurement happens in the model's
 * own space, so run this on the root before it inherits any group transform.
 */
export function needsUprightCorrection(root: Object3D): boolean {
    root.updateMatrixWorld(true);
    const bounds = new Box3().setFromObject(root);
    if (bounds.isEmpty()) {
        return false;
    }

    const size = bounds.getSize(new Vector3());
    if (!Number.isFinite(size.x) || !Number.isFinite(size.y) || !Number.isFinite(size.z)) {
        return false;
    }

    return size.z > size.y * UPRIGHT_Z_OVER_Y && size.z > size.x * UPRIGHT_Z_OVER_X;
}
