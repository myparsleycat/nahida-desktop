import type { EvaluatedViewerState } from "@shared/mod-viewer/types";
import { BufferAttribute, BufferGeometry, Group, Mesh, MeshStandardMaterial } from "three";
import { describe, expect, it } from "vitest";

import { applyPayloadEval } from "./model-viewer-payload";

function meshWithTargets(
    base: number[],
    shapeTargets: Array<{
        var: string;
        positions: Float32Array;
        mode?: "midpoint_pair";
        lowPositions?: Float32Array;
    }>,
) {
    const geometry = new BufferGeometry();
    geometry.setAttribute("position", new BufferAttribute(new Float32Array(base), 3));
    geometry.setAttribute("normal", new BufferAttribute(new Float32Array(base.length), 3));
    const mesh = new Mesh(geometry, new MeshStandardMaterial());
    mesh.userData = {
        meshId: "mesh",
        basePositions: new Float32Array(base),
        shapeTargets,
        positionVariants: [],
        normalCache: new Map(),
    };
    return mesh;
}

function evalState(shapeWeights: Record<string, number>): EvaluatedViewerState {
    return {
        state: {},
        meshes: [
            {
                id: "mesh",
                visible: true,
                texKey: null,
                normalMapKey: null,
                lightMapKey: null,
                materialMapKey: null,
                shapeWeights,
                positionVariantIndex: null,
            },
        ],
    };
}

describe("applyPayloadEval midpoint targets", () => {
    it("excludes malformed midpoint endpoints from the deformation divisor", () => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [
                {
                    var: "valid",
                    positions: new Float32Array([10, 0, 0]),
                    mode: "midpoint_pair",
                },
                {
                    var: "malformed",
                    positions: new Float32Array([1, 2]),
                    mode: "midpoint_pair",
                },
            ],
        );
        const root = new Group();
        root.add(mesh);

        applyPayloadEval(root, evalState({ valid: 1, malformed: 1 }));

        // weight 1 → factor 2, so a lone valid target must contribute the full delta.
        expect(Array.from(mesh.geometry.attributes.position.array)).toEqual([20, 0, 0]);
    });

    it("counts only the selected endpoint that matches the base length", () => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [
                {
                    var: "low",
                    positions: new Float32Array([10, 0, 0]),
                    lowPositions: new Float32Array([4, 0, 0]),
                    mode: "midpoint_pair",
                },
                {
                    var: "bad-low",
                    positions: new Float32Array([10, 0, 0]),
                    lowPositions: new Float32Array([1, 2]),
                    mode: "midpoint_pair",
                },
            ],
        );
        const root = new Group();
        root.add(mesh);

        applyPayloadEval(root, evalState({ low: 0.25, "bad-low": 0.25 }));

        // weight 0.25 → factor 1; only the valid low endpoint is counted.
        expect(Array.from(mesh.geometry.attributes.position.array)).toEqual([4, 0, 0]);
    });

    it("leaves base positions when every midpoint target is malformed", () => {
        const mesh = meshWithTargets(
            [1, 2, 3],
            [
                {
                    var: "broken",
                    positions: new Float32Array([1, 2]),
                    mode: "midpoint_pair",
                },
            ],
        );
        const root = new Group();
        root.add(mesh);

        applyPayloadEval(root, evalState({ broken: 1 }));

        expect(Array.from(mesh.geometry.attributes.position.array)).toEqual([1, 2, 3]);
    });
});
