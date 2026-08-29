import { BufferAttribute, BufferGeometry, Group, Mesh, MeshStandardMaterial } from "three";
import { describe, expect, it } from "vitest";

import {
    MODEL_VIEWER_UPRIGHT_ROTATION,
    needsUprightCorrection,
} from "./model-viewer-upright";

function boxMesh(size: [number, number, number]): Mesh {
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
        const root = new Group();
        root.add(boxMesh([6, 6, 6]));
        root.add(boxMesh([12, 14, 94]));
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