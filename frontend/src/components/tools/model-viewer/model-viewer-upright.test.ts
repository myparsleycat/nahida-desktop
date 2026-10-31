import { BufferAttribute, BufferGeometry, Group, Mesh, MeshStandardMaterial, Vector3 } from "three";
import { describe, expect, it } from "vitest";

import {
    applyModelViewerInitialRotation,
    MODEL_VIEWER_UPRIGHT_ROTATION,
    needsUprightCorrection,
} from "./model-viewer-upright";

function boxMesh(size: readonly [number, number, number]): Mesh {
    const geometry = new BufferGeometry();
    geometry.setAttribute(
        "position",
        new BufferAttribute(new Float32Array([0, 0, 0, size[0], size[1]!, size[2]!]), 3),
    );
    return new Mesh(geometry, new MeshStandardMaterial());
}

describe("needsUprightCorrection", () => {
    it("detects a Z-up model", () => {
        const root = new Group();
        root.add(boxMesh([20, 20, 100]));
        expect(needsUprightCorrection(root)).toBe(true);
    });

    it("detects a Z-up model from the combined bounds of several meshes", () => {
        // Neither cube is Z-dominant alone; only their union spans the Z axis.
        const feet = boxMesh([10, 10, 10]);
        const head = boxMesh([10, 10, 10]);
        head.position.z = 90;
        expect(needsUprightCorrection(new Group().add(feet))).toBe(false);
        expect(needsUprightCorrection(new Group().add(head))).toBe(false);

        const root = new Group();
        root.add(feet, head);
        expect(needsUprightCorrection(root)).toBe(true);
    });

    it("leaves a Y-up model untouched", () => {
        const root = new Group();
        root.add(boxMesh([20, 100, 20]));
        expect(needsUprightCorrection(root)).toBe(false);
    });

    it("leaves an X-dominant model untouched", () => {
        const root = new Group();
        root.add(boxMesh([100, 20, 20]));
        expect(needsUprightCorrection(root)).toBe(false);
    });

    it("keeps a model at exactly the reference Y threshold unrotated", () => {
        // z == y * 1.5 must fail the strict comparison, mirroring fitTo().
        const root = new Group();
        root.add(boxMesh([20, 100, 150]));
        expect(needsUprightCorrection(root)).toBe(false);
    });

    it("returns false for an empty root", () => {
        expect(needsUprightCorrection(new Group())).toBe(false);
    });

    it("returns false when positions are not finite", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute(
            "position",
            new BufferAttribute(new Float32Array([0, 0, 0, Number.NaN, 0, 100]), 3),
        );
        const root = new Group();
        root.add(new Mesh(geometry, new MeshStandardMaterial()));
        expect(needsUprightCorrection(root)).toBe(false);
    });

    it("exposes the reference -90° pitch about X", () => {
        expect(MODEL_VIEWER_UPRIGHT_ROTATION.x).toBeCloseTo(-Math.PI / 2);
        expect(MODEL_VIEWER_UPRIGHT_ROTATION.y).toBe(0);
        expect(MODEL_VIEWER_UPRIGHT_ROTATION.z).toBe(0);
    });
});

describe("applyModelViewerInitialRotation", () => {
    it.each([
        {
            name: "Z-up WWMI",
            size: [20, 20, 100],
            up: [0, 0, 1],
            front: [0, 1, 0],
            yaw: 180,
            facing: 1,
        },
        {
            name: "Y-up WWMI",
            size: [20, 100, 20],
            up: [0, 1, 0],
            front: [0, 0, -1],
            yaw: 180,
            facing: 1,
        },
        {
            name: "Z-up default",
            size: [20, 20, 100],
            up: [0, 0, 1],
            front: [0, 1, 0],
            yaw: undefined,
            facing: -1,
        },
        {
            name: "Y-up default",
            size: [20, 100, 20],
            up: [0, 1, 0],
            front: [0, 0, 1],
            yaw: undefined,
            facing: 1,
        },
    ] as const)(
        "preserves upright and expected facing for $name",
        ({ size, up, front, yaw, facing }) => {
            const root = new Group().add(boxMesh(size));

            applyModelViewerInitialRotation(root, yaw);

            const rotatedUp = new Vector3(...up).applyQuaternion(root.quaternion);
            const rotatedFront = new Vector3(...front).applyQuaternion(root.quaternion);
            expect(rotatedUp.distanceTo(new Vector3(0, 1, 0))).toBeLessThan(1e-10);
            expect(rotatedFront.distanceTo(new Vector3(0, 0, facing))).toBeLessThan(1e-10);
        },
    );
});
