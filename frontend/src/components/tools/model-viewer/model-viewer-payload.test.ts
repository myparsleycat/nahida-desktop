import type { EvaluatedViewerState } from "@shared/mod-viewer/types";
import {
    BufferAttribute,
    BufferGeometry,
    Group,
    Matrix3,
    Mesh,
    MeshStandardMaterial,
    RED_GREEN_RGTC2_Format,
    ShaderMaterial,
    Texture,
} from "three";
import { describe, expect, it, vi } from "vitest";

import type { ModelViewerPositionGeometry } from "./model-viewer-position-codec";
import type { PositionVariantLoader } from "./model-viewer-position-loader";

import { DEFAULT_EFMI_TOON_STYLE, markEFMIHeadMeshes } from "./material-profiles/efmi";
import { DEFAULT_GIMI_TOON_STYLE } from "./material-profiles/gimi";
import {
    parseModelViewerToonStyles,
    serializeModelViewerToonStyles,
} from "./material-profiles/registry";
import { DEFAULT_SRMI_TOON_STYLE } from "./material-profiles/srmi";
import {
    applyPayloadEval,
    clearPayloadModelData,
    commitPayloadEval,
    finalizePayloadGeometry,
    preparePayloadEval,
    setPayloadGameTextureHighlight,
    setPayloadNormalMaps,
    setPayloadToonShadows,
    setPayloadToonStyle,
} from "./model-viewer-payload";

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
        baseNormals: new Float32Array(
            (geometry.attributes.normal?.array as Float32Array | undefined) ?? [],
        ),
        lastPositionVariantIndex: null,
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
                cutoutMapKey: null,
                shapeWeights,
                positionVariantIndex: null,
            },
        ],
    };
}

function variantEval(positionVariantIndex: number | null): EvaluatedViewerState {
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
                cutoutMapKey: null,
                shapeWeights: {},
                positionVariantIndex,
            },
        ],
    };
}

describe("applyPayloadEval midpoint targets", () => {
    it("skips geometry work when the ordered shape signature is unchanged", () => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [{ var: "shape", positions: new Float32Array([1, 0, 0]) }],
        );
        const root = new Group();
        root.add(mesh);
        const normals = vi.spyOn(mesh.geometry, "computeVertexNormals");
        const bounds = vi.spyOn(mesh.geometry, "computeBoundingSphere");

        applyPayloadEval(root, evalState({ shape: 0.5 }));
        normals.mockClear();
        bounds.mockClear();
        applyPayloadEval(root, evalState({ shape: 0.5 }));

        expect(normals).not.toHaveBeenCalled();
        expect(bounds).not.toHaveBeenCalled();
        expect(mesh.userData.lastShapeSignature).toBe("shape:0.5");
    });

    it("recomputes geometry once when a shape weight changes", () => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [{ var: "shape", positions: new Float32Array([1, 0, 0]) }],
        );
        const root = new Group();
        root.add(mesh);
        applyPayloadEval(root, evalState({ shape: 0.25 }));
        const normals = vi.spyOn(mesh.geometry, "computeVertexNormals");

        applyPayloadEval(root, evalState({ shape: 0.75 }));
        applyPayloadEval(root, evalState({ shape: 0.75 }));

        expect(normals).toHaveBeenCalledOnce();
    });

    it("defers normals and bounds until the geometry is finalized", () => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [{ var: "shape", positions: new Float32Array([1, 0, 0]) }],
        );
        const root = new Group();
        root.add(mesh);
        const normals = vi.spyOn(mesh.geometry, "computeVertexNormals");
        const bounds = vi.spyOn(mesh.geometry, "computeBoundingSphere");

        commitPayloadEval(
            root,
            { evalResult: evalState({ shape: 0.5 }), positions: new Map() },
            { deferGeometryUpdates: true },
        );
        expect(mesh.geometry.attributes.position.array[0]).toBe(0.5);
        expect(normals).not.toHaveBeenCalled();
        expect(bounds).not.toHaveBeenCalled();

        expect(finalizePayloadGeometry(root)).toBe(true);
        expect(normals).toHaveBeenCalledOnce();
        expect(bounds).toHaveBeenCalledOnce();
        expect(finalizePayloadGeometry(root)).toBe(false);
        expect(normals).toHaveBeenCalledOnce();

        // A settled deformation that repeats its signature stays finalized.
        applyPayloadEval(root, evalState({ shape: 0.5 }));
        expect(normals).toHaveBeenCalledOnce();
    });

    it("restores shape geometry after applying a position variant", () => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [{ var: "shape", positions: new Float32Array([2, 0, 0]) }],
        );
        mesh.userData.positionVariants = [{ conditions: [], geometryUrl: "/variant.buf" }];
        const root = new Group();
        root.add(mesh);
        applyPayloadEval(root, evalState({ shape: 1 }));
        commitPayloadEval(root, {
            evalResult: variantEval(0),
            positions: new Map([["mesh", { variantIndex: 0, ...preparedGeometry([9, 0, 0]) }]]),
        });
        const normals = vi.spyOn(mesh.geometry, "computeVertexNormals");

        applyPayloadEval(root, evalState({ shape: 1 }));

        expect(Array.from(mesh.geometry.attributes.position.array)).toEqual([2, 0, 0]);
        expect(normals).toHaveBeenCalledOnce();
    });

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

describe("applyPayloadEval packed maps", () => {
    it("uses ZZMI material channels without binding packed light maps as AO", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("uv", new BufferAttribute(new Float32Array(6), 2));
        geometry.setAttribute(
            "tangent",
            new BufferAttribute(new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]), 4),
        );
        const material = new MeshStandardMaterial();
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            baseNormals: new Float32Array(
                (geometry.attributes.normal?.array as Float32Array | undefined) ?? [],
            ),
            lastPositionVariantIndex: null,
            materialProfile: "zzmi",
            normalXYReconstruct: true,
        };
        const diffuse = new Texture();
        const normal = new Texture();
        const light = new Texture();
        const packedMaterial = new Texture();
        diffuse.userData.modelViewerDDS = { format: "bc4-unorm", direct: true };
        diffuse.userData.modelViewerInvertAlpha = true;
        normal.userData.modelViewerDDS = { format: "bc5-snorm", direct: true };
        light.userData.modelViewerDDS = { format: "bc4-unorm", direct: true };
        packedMaterial.userData.modelViewerDDS = { format: "bc4-unorm", direct: true };
        const root = new Group();
        root.userData.payloadTextures = new Map([
            ["diffuse", diffuse],
            ["normal", normal],
            ["light", light],
            ["material", packedMaterial],
        ]);
        root.add(mesh);

        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: "diffuse",
                    normalMapKey: "normal",
                    lightMapKey: "light",
                    materialMapKey: "material",
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        expect(material.map).toBe(diffuse);
        expect(material.normalMap).toBe(normal);
        expect(material.normalScale.y).toBe(-1);
        expect(material.aoMap).toBeNull();
        expect(material.metalnessMap).toBe(light);
        expect(material.metalness).toBe(1);
        expect(material.roughnessMap).toBe(packedMaterial);
        expect(material.roughness).toBe(1);

        const shader = {
            fragmentShader:
                "#include <common>\n#include <map_fragment>\n#include <normal_fragment_maps>\n#include <roughnessmap_fragment>\n#include <metalnessmap_fragment>",
            uniforms: {},
        };
        material.onBeforeCompile(shader as never, {} as never);
        expect(shader.fragmentShader).toContain("float modelViewerRoughnessSample = mix(");
        expect(shader.fragmentShader).toContain("float modelViewerMetalnessSample = mix(");
        expect(shader.fragmentShader).not.toContain("#include <roughnessmap_fragment>");
        expect(shader.fragmentShader).not.toContain("#include <metalnessmap_fragment>");
        expect(shader.fragmentShader).toContain(
            "sampledDiffuseColor = vec4( sampledDiffuseColor.rrr, 1.0 )",
        );
        expect(shader.fragmentShader).toContain("mapN = modelViewerNormalTexel");
        const uniforms = shader.uniforms as Record<string, { value: number }>;
        expect(uniforms.modelViewerInvertAlpha.value).toBe(1);
        expect(uniforms.modelViewerMapBC4.value).toBe(1);
        expect(uniforms.modelViewerNormalMode.value).toBe(2);
        expect(uniforms.modelViewerMetalnessBC4.value).toBe(1);
        expect(uniforms.modelViewerRoughnessBC4.value).toBe(1);
    });

    it("blends coverage masks only while one is bound", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        const material = new MeshStandardMaterial();
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            baseNormals: new Float32Array(0),
            lastPositionVariantIndex: null,
            materialProfile: "generic",
        };
        const mask = new Texture();
        mask.userData.modelViewerCoverageMask = true;
        const root = new Group();
        root.userData.payloadTextures = new Map([
            ["mask", mask],
            ["diffuse", new Texture()],
        ]);
        root.add(mesh);
        const apply = (texKey: string) =>
            applyPayloadEval(root, {
                state: {},
                meshes: [
                    {
                        id: "mesh",
                        visible: true,
                        texKey,
                        normalMapKey: null,
                        lightMapKey: null,
                        materialMapKey: null,
                        cutoutMapKey: null,
                        shapeWeights: {},
                        positionVariantIndex: null,
                    },
                ],
            });

        apply("mask");
        expect(material.transparent).toBe(true);
        expect(material.depthWrite).toBe(false);
        const { version } = material;

        apply("diffuse");
        expect(material.transparent).toBe(false);
        expect(material.depthWrite).toBe(true);
        expect(material.version).toBeGreaterThan(version);
    });

    it("recompiles when a normal-map variant changes the packed-normal define", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("normal", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("uv", new BufferAttribute(new Float32Array(6), 2));
        geometry.setAttribute(
            "tangent",
            new BufferAttribute(new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]), 4),
        );
        const material = new MeshStandardMaterial();
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            baseNormals: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            lastPositionVariantIndex: null,
            materialProfile: "zzmi",
        };
        const packed = new Texture();
        packed.format = RED_GREEN_RGTC2_Format;
        packed.userData.modelViewerDDS = { format: "bc5-unorm", direct: true };
        const rgba = new Texture();
        const root = new Group();
        root.userData.payloadTextures = new Map([
            ["packed", packed],
            ["rgba", rgba],
        ]);
        root.add(mesh);

        const evaluated = (normalMapKey: string): EvaluatedViewerState => ({
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: null,
                    normalMapKey,
                    lightMapKey: null,
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });
        applyPayloadEval(root, evaluated("packed"));
        const packedVersion = material.version;

        applyPayloadEval(root, evaluated("rgba"));

        expect(material.version).toBeGreaterThan(packedVersion);
    });

    it("does not bind tangent-space normal maps without authored tangents", () => {
        const mesh = meshWithTargets([0, 0, 0], []);
        const normal = new Texture();
        const root = new Group();
        root.userData.payloadTextures = new Map([["normal", normal]]);
        root.add(mesh);

        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: null,
                    normalMapKey: "normal",
                    lightMapKey: null,
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        expect((mesh.material as MeshStandardMaterial).normalMap).toBeNull();
    });

    it("does not bind normal maps through outline normals stored as tangents", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute(
            "normal",
            new BufferAttribute(new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]), 3),
        );
        geometry.setAttribute(
            "tangent",
            new BufferAttribute(new Float32Array([0, 0, 1, 1, 0, 0, 1, -1, 0, 0, -1, 1]), 4),
        );
        const mesh = new Mesh(geometry, new MeshStandardMaterial());
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            baseNormals: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            lastPositionVariantIndex: null,
        };
        const root = new Group();
        root.userData.payloadTextures = new Map([["normal", new Texture()]]);
        root.add(mesh);

        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: null,
                    normalMapKey: "normal",
                    lightMapKey: null,
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        expect((mesh.material as MeshStandardMaterial).normalMap).toBeNull();
    });

    it.each([
        [0, 0, 0],
        [NaN, 0, 0],
        [0, Infinity, 0],
        [0, 0, -Infinity],
    ])("does not bind normal maps with invalid tangent (%s, %s, %s)", (x, y, z) => {
        const mesh = meshWithTargets([0, 0, 0], []);
        mesh.geometry.setAttribute(
            "tangent",
            new BufferAttribute(new Float32Array([x, y, z, 1]), 4),
        );
        const root = new Group();
        root.add(mesh);
        root.userData.payloadTextures = new Map([["normal", new Texture()]]);
        const evaluated = evalState({});
        evaluated.meshes[0].normalMapKey = "normal";

        applyPayloadEval(root, evaluated);

        expect((mesh.material as MeshStandardMaterial).normalMap).toBeNull();
    });

    it("does not let invalid samples dilute outline-tangent alignment", () => {
        const mesh = meshWithTargets(new Array(15).fill(0), []);
        mesh.geometry.attributes.normal.array.set([0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1, 0, 0, 1]);
        mesh.geometry.setAttribute(
            "tangent",
            new BufferAttribute(
                new Float32Array([0, 0, 1, 1, 0, 0, 0, 1, NaN, 0, 0, 1, 0, 0, 0, 1, 0, 0, 0, 1]),
                4,
            ),
        );
        const root = new Group();
        root.add(mesh);
        root.userData.payloadTextures = new Map([["normal", new Texture()]]);
        const evaluated = evalState({});
        evaluated.meshes[0].normalMapKey = "normal";

        applyPayloadEval(root, evaluated);

        expect((mesh.material as MeshStandardMaterial).normalMap).toBeNull();
    });

    it.each(["normal", "tangent"] as const)(
        "updates normal-map eligibility when the %s attribute changes with unchanged texture keys",
        (attributeName) => {
            const mesh = meshWithTargets([0, 0, 0], []);
            mesh.geometry.attributes.normal.array.set([0, 0, 1]);
            mesh.geometry.setAttribute(
                "tangent",
                new BufferAttribute(new Float32Array([1, 0, 0, 1]), 4),
            );
            const material = mesh.material as MeshStandardMaterial;
            const root = new Group();
            root.add(mesh);
            const normalMap = new Texture();
            root.userData.payloadTextures = new Map([["normal", normalMap]]);
            const evaluated = evalState({});
            evaluated.meshes[0].normalMapKey = "normal";
            const attribute = mesh.geometry.attributes[attributeName];
            const original = attribute.array.slice();
            const unusable = attributeName === "normal" ? [1, 0, 0] : [0, 0, 1, 1];

            applyPayloadEval(root, evaluated);
            expect(material.normalMap).toBe(normalMap);
            const version = material.version;
            applyPayloadEval(root, evaluated);
            expect(material.version).toBe(version);

            attribute.array.set(unusable);
            attribute.needsUpdate = true;
            applyPayloadEval(root, evaluated);
            expect(material.normalMap).toBeNull();
            expect(material.version).toBeGreaterThan(version);
            const disabledVersion = material.version;

            attribute.array.set(original);
            attribute.needsUpdate = true;
            applyPayloadEval(root, evaluated);
            expect(material.normalMap).toBe(normalMap);
            expect(material.version).toBeGreaterThan(disabledVersion);

            mesh.geometry.setAttribute(
                attributeName,
                new BufferAttribute(new Float32Array(unusable), attribute.itemSize),
            );
            applyPayloadEval(root, evaluated);
            expect(material.normalMap).toBeNull();

            mesh.geometry.setAttribute(attributeName, attribute);
            applyPayloadEval(root, evaluated);
            expect(material.normalMap).toBe(normalMap);

            if (attributeName === "tangent") {
                mesh.geometry.deleteAttribute("tangent");
                applyPayloadEval(root, evaluated);
                expect(material.normalMap).toBeNull();

                mesh.geometry.setAttribute("tangent", attribute);
                applyPayloadEval(root, evaluated);
                expect(material.normalMap).toBe(normalMap);
            }
        },
    );

    it.each([false, true])(
        "rechecks normal maps after shape normals change (deferred: %s)",
        (deferred) => {
            const mesh = meshWithTargets(
                [0, 0, 0, 1, 0, 0, 0, 1, 0],
                [{ var: "shape", positions: new Float32Array([0, 0, 0, 0, 1, 0, 0, 0, 1]) }],
            );
            mesh.userData.baseNormals = new Float32Array([0, 0, 1, 0, 0, 1, 0, 0, 1]);
            mesh.geometry.attributes.normal.array.set(mesh.userData.baseNormals);
            mesh.geometry.setAttribute(
                "tangent",
                new BufferAttribute(new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]), 4),
            );
            const root = new Group();
            root.add(mesh);
            const normalMap = new Texture();
            root.userData.payloadTextures = new Map([["normal", normalMap]]);
            const evaluated = evalState({ shape: 0 });
            evaluated.meshes[0].normalMapKey = "normal";
            const material = mesh.material as MeshStandardMaterial;
            applyPayloadEval(root, evaluated);
            expect(material.normalMap).toBe(normalMap);

            evaluated.meshes[0].shapeWeights.shape = 1;
            commitPayloadEval(
                root,
                { evalResult: evaluated, positions: new Map() },
                { deferGeometryUpdates: deferred },
            );
            if (deferred) finalizePayloadGeometry(root);
            expect(material.normalMap).toBeNull();

            evaluated.meshes[0].shapeWeights.shape = 0;
            applyPayloadEval(root, evaluated);
            expect(material.normalMap).toBe(normalMap);
        },
    );

    it("uses Three.js's derivative tangent frame for RabbitFX normals", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("normal", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("uv", new BufferAttribute(new Float32Array(6), 2));
        const material = new MeshStandardMaterial({ metalness: 0.5, roughness: 0.5 });
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            baseNormals: new Float32Array(
                (geometry.attributes.normal?.array as Float32Array | undefined) ?? [],
            ),
            lastPositionVariantIndex: null,
            materialProfile: "wuwa:rabbitfx",
            toonShadows: false,
        };
        const normal = new Texture();
        const root = new Group();
        root.userData.payloadTextures = new Map([["normal", normal]]);
        root.add(mesh);

        const evaluated: EvaluatedViewerState = {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: null,
                    normalMapKey: "normal",
                    lightMapKey: null,
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        };
        applyPayloadEval(root, evaluated);

        expect(material.normalMap).toBe(normal);
        expect(material.normalScale.y).toBe(-1);
        expect(material.metalness).toBe(0);
        expect(material.roughness).toBe(1);
        expect(geometry.attributes.tangent).toBeUndefined();

        geometry.deleteAttribute("normal");
        applyPayloadEval(root, evaluated);
        expect(material.normalMap).toBeNull();

        geometry.setAttribute("normal", new BufferAttribute(new Float32Array(9), 3));
        applyPayloadEval(root, evaluated);
        expect(material.normalMap).toBe(normal);
    });

    it("draws EFMI normals without tangents and follows the normal map toggle", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("normal", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("uv", new BufferAttribute(new Float32Array(6), 2));
        const material = new MeshStandardMaterial();
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            baseNormals: new Float32Array(9),
            lastPositionVariantIndex: null,
            materialProfile: "efmi",
            toonShadows: false,
        };
        const normal = new Texture();
        const root = new Group();
        root.userData.payloadTextures = new Map([["normal", normal]]);
        root.add(mesh);

        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: null,
                    normalMapKey: "normal",
                    lightMapKey: null,
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });
        expect(material.normalMap).toBe(normal);

        setPayloadNormalMaps(root, false);
        expect(material.normalMap).toBeNull();

        setPayloadNormalMaps(root, true);
        expect(material.normalMap).toBe(normal);
    });

    it("adapts RabbitFX LightMap.G without occupying generic PBR map slots", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("uv", new BufferAttribute(new Float32Array(6), 2));
        geometry.setAttribute(
            "tangent",
            new BufferAttribute(new Float32Array([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]), 4),
        );
        const material = new MeshStandardMaterial();
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            baseNormals: new Float32Array(
                (geometry.attributes.normal?.array as Float32Array | undefined) ?? [],
            ),
            lastPositionVariantIndex: null,
            materialProfile: "wuwa:rabbitfx",
            toonShadows: true,
        };
        const diffuse = new Texture();
        const normal = new Texture();
        const firstLight = new Texture();
        const secondLight = new Texture();
        const cutout = new Texture();
        const root = new Group();
        root.userData.payloadTextures = new Map([
            ["diffuse", diffuse],
            ["normal", normal],
            ["light-a", firstLight],
            ["light-b", secondLight],
            ["cutout", cutout],
        ]);
        root.add(mesh);

        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: "diffuse",
                    normalMapKey: "normal",
                    lightMapKey: "light-a",
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        expect(material.map).toBe(diffuse);
        expect(material.normalMap).toBe(normal);
        expect(material.aoMap).toBeNull();
        expect(material.metalnessMap).toBeNull();
        expect(material.roughnessMap).toBeNull();
        expect(material.customProgramCacheKey()).toBe("wuwa-rabbitfx-v3|model-viewer-dds-v1");

        const shader = {
            vertexShader: "#include <common>\nvoid main() {\n#include <uv_vertex>\n}",
            fragmentShader:
                "#include <common>\nvoid main() {\n#include <map_fragment>\n#include <lights_fragment_begin>\n}",
            uniforms: {},
        };
        material.onBeforeCompile(shader as never, {} as never);
        expect(shader.vertexShader).toContain("vRabbitFXUv = uv");
        expect(shader.fragmentShader).toContain("mask <= 0.01 || mask >= 0.99");
        expect(shader.fragmentShader).toContain("step( 0.1, mask )");
        expect(shader.fragmentShader).toContain("rabbitFXDirectDiffuseContribution");
        expect(
            shader.fragmentShader.match(/vec3 rabbitFXDirectDiffuseContribution;/g),
        ).toHaveLength(1);
        expect(shader.fragmentShader).not.toContain("#include <lights_fragment_begin>");
        expect(
            (shader.uniforms as Record<string, { value: unknown }>).rabbitFXLightMap?.value,
        ).toBe(firstLight);
        expect(
            (shader.uniforms as Record<string, { value: unknown }>).rabbitFXToonEnabled?.value,
        ).toBe(1);

        const version = material.version;
        secondLight.repeat.y = -1;
        secondLight.offset.y = 1;
        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: "diffuse",
                    normalMapKey: "normal",
                    lightMapKey: "light-b",
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });
        expect(material.version).toBe(version);
        expect(
            (shader.uniforms as Record<string, { value: unknown }>).rabbitFXLightMap?.value,
        ).toBe(secondLight);
        expect(
            (shader.uniforms as Record<string, { value: Matrix3 }>).rabbitFXLightMapTransform?.value
                .elements[4],
        ).toBe(-1);

        const applyCutout = (cutoutMapKey: string | null) =>
            applyPayloadEval(root, {
                state: {},
                meshes: [
                    {
                        id: "mesh",
                        visible: true,
                        texKey: "diffuse",
                        normalMapKey: "normal",
                        lightMapKey: "light-b",
                        materialMapKey: null,
                        cutoutMapKey,
                        shapeWeights: {},
                        positionVariantIndex: null,
                    },
                ],
            });
        const uniforms = shader.uniforms as Record<string, { value: unknown }>;
        applyCutout("cutout");
        expect(uniforms.rabbitFXCutoutMap?.value).toBe(cutout);
        expect(uniforms.rabbitFXCutoutEnabled?.value).toBe(1);
        applyCutout(null);
        expect(uniforms.rabbitFXCutoutEnabled?.value).toBe(0);
        expect(material.version).toBe(version);

        setPayloadToonShadows(root, false);
        expect(
            (shader.uniforms as Record<string, { value: unknown }>).rabbitFXToonEnabled?.value,
        ).toBe(0);
        expect(material.version).toBe(version);
    });

    it("switches EFMI toon shading and its outline without recompiling or changing picks", () => {
        const mesh = meshWithTargets([0, 0, 0, 1, 0, 0, 0, 1, 0], []);
        mesh.userData.materialProfile = "efmi";
        mesh.userData.toonShadows = false;
        const diffuse = new Texture();
        const light = new Texture();
        for (const texture of [diffuse, light]) {
            texture.repeat.y = -1;
            texture.offset.y = 1;
        }
        const material = mesh.material as MeshStandardMaterial;
        const root = new Group();
        root.userData.payloadTextures = new Map([
            ["diffuse", diffuse],
            ["light", light],
        ]);
        root.add(mesh);
        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: "diffuse",
                    normalMapKey: null,
                    lightMapKey: "light",
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });
        const state = material.userData.efmiToonMaterial as {
            enabled: { value: number };
            lightMapTransform: { value: Matrix3 };
        };
        const version = material.version;
        expect(state.lightMapTransform.value.elements[4]).toBe(-1);
        expect(state.enabled.value).toBe(0);
        expect(mesh.children).toHaveLength(0);

        setPayloadToonStyle(root, { ...DEFAULT_EFMI_TOON_STYLE, selfShadow: true });
        setPayloadToonShadows(root, true);
        expect(state.enabled.value).toBe(1);
        expect(mesh.castShadow).toBe(true);
        const outline = mesh.children[0] as Mesh<BufferGeometry, ShaderMaterial>;
        expect(outline.material.uniforms.mapTransform.value.elements[4]).toBe(-1);
        expect(outline.geometry).toBe(mesh.geometry);
        expect(mesh.geometry.attributes.outlineNormal?.count).toBe(3);
        expect(outline.visible).toBe(true);
        expect(outline.userData.meshId).toBeUndefined();
        const hits: unknown[] = [];
        outline.raycast({} as never, hits as never);
        expect(hits).toHaveLength(0);

        setPayloadToonStyle(root, {
            ...DEFAULT_EFMI_TOON_STYLE,
            outlines: false,
            selfShadow: false,
        });
        expect(outline.visible).toBe(false);
        expect(mesh.castShadow).toBe(false);
        setPayloadToonShadows(root, false);
        expect(state.enabled.value).toBe(0);
        expect(material.version).toBe(version);
    });

    it("lights only head meshes without a light map as a flat EFMI face", () => {
        const mesh = meshWithTargets([0, 0, 0, 1, 0, 0, 0, 1, 0], []);
        mesh.userData.materialProfile = "efmi";
        mesh.userData.toonShadows = true;
        mesh.userData.efmiHeadMesh = true;
        const material = mesh.material as MeshStandardMaterial;
        const root = new Group();
        root.userData.payloadTextures = new Map([["light", new Texture()]]);
        root.add(mesh);
        const apply = (lightMapKey: string | null) =>
            applyPayloadEval(root, {
                state: {},
                meshes: [
                    {
                        id: "mesh",
                        visible: true,
                        texKey: null,
                        normalMapKey: null,
                        lightMapKey,
                        materialMapKey: null,
                        cutoutMapKey: null,
                        shapeWeights: {},
                        positionVariantIndex: null,
                    },
                ],
            });

        apply(null);
        const state = material.userData.efmiToonMaterial as { face: { value: number } };
        expect(state.face.value).toBe(1);

        apply("light");
        expect(state.face.value).toBe(0);

        apply(null);
        setPayloadToonStyle(root, { ...DEFAULT_EFMI_TOON_STYLE, flatFace: false });
        expect(state.face.value).toBe(0);
    });

    it("classifies wide EFMI faces and hides only small untextured head meshes", () => {
        const headMesh = (id: string, indices: number) => {
            const mesh = meshWithTargets([0, 0, 0, 1, 0, 0, 0, 1, 0], []);
            mesh.userData.meshId = id;
            mesh.userData.materialProfile = "efmi";
            mesh.geometry.setIndex(Array.from({ length: indices }, () => 0));
            return mesh;
        };
        const overlay = headMesh("overlay", 1023);
        const face = headMesh("face", 8040);
        const feet = headMesh("feet", 3);
        const transportMesh = (id: string, width: number, minZ: number, maxZ: number) => ({
            id,
            bounds: { min: [-width / 2, 0, minZ], max: [width / 2, 0, maxZ] },
            texKey: null,
            textureVariants: [],
            lightMapKey: null,
            lightMapVariants: [],
            materialMapKey: null,
            highlightMapKey: null,
        });
        markEFMIHeadMeshes(
            {
                meshes: [
                    transportMesh("overlay", 0.14, 1.4, 1.6),
                    transportMesh("face", 0.233, 1.4, 1.6),
                    transportMesh("feet", 0.3, 0, 0.1),
                ],
            } as never,
            [overlay, face, feet],
        );
        expect(face.userData.efmiHeadMesh).toBe(true);
        expect(feet.userData.efmiHeadMesh).toBeFalsy();

        const root = new Group();
        root.userData.payloadTextures = new Map();
        root.add(overlay, face);
        const entry = (id: string) => ({
            id,
            visible: true,
            texKey: null,
            normalMapKey: null,
            lightMapKey: null,
            materialMapKey: null,
            cutoutMapKey: null,
            shapeWeights: {},
            positionVariantIndex: null,
        });
        applyPayloadEval(root, { state: {}, meshes: [entry("overlay"), entry("face")] });
        expect((overlay.material as MeshStandardMaterial).visible).toBe(false);
        expect((face.material as MeshStandardMaterial).visible).toBe(true);

        setPayloadGameTextureHighlight(root, true);
        expect((overlay.material as MeshStandardMaterial).visible).toBe(true);
    });

    it("measures the EFMI head from the face when a prop inflates the model height", () => {
        const headMesh = (indices: number) => {
            const mesh = meshWithTargets([0, 0, 0, 1, 0, 0, 0, 1, 0], []);
            mesh.geometry.setIndex(Array.from({ length: indices }, () => 0));
            return mesh;
        };
        const transportMesh = (minZ: number, maxZ: number, width = 0.2, lit = false) => ({
            bounds: { min: [-width / 2, 0, minZ], max: [width / 2, 0, maxZ] },
            texKey: "diffuse",
            textureVariants: [],
            lightMapKey: lit ? "light" : null,
            lightMapVariants: [],
            materialMapKey: null,
            highlightMapKey: null,
        });
        const face = headMesh(11538);
        const overlay = headMesh(900);
        const iris = headMesh(1032);
        const chest = headMesh(12000);
        const hair = headMesh(30000);
        const prop = headMesh(3);
        markEFMIHeadMeshes(
            {
                meshes: [
                    transportMesh(1.5, 1.68),
                    { ...transportMesh(1.45, 1.7), texKey: null },
                    { ...transportMesh(1.52, 1.545), texKey: null },
                    transportMesh(1.15, 1.34),
                    transportMesh(1.4, 1.75, 0.28, true),
                    transportMesh(0, 2.2, 1.5),
                ],
            } as never,
            [face, overlay, iris, chest, hair, prop],
        );
        expect(face.userData.efmiHeadMesh).toBe(true);
        expect(overlay.userData.overlayShadow).toBe(true);
        expect(iris.userData.efmiHeadMesh).toBe(true);
        expect(iris.userData.overlayShadow).toBe(false);
        expect(iris.userData.efmiEyeMesh).toBe(true);
        expect(face.userData.efmiEyeMesh).toBe(false);
        expect(chest.userData.efmiHeadMesh).toBeFalsy();
        expect(prop.userData.efmiHeadMesh).toBeFalsy();
    });

    it("reads an EFMI outline mask as outline width instead of a shadow mask", () => {
        const mesh = meshWithTargets([0, 0, 0, 1, 0, 0, 0, 1, 0], []);
        mesh.userData.materialProfile = "efmi";
        mesh.userData.toonShadows = true;
        const mask = new Texture();
        mask.userData.modelViewerOutlineMask = true;
        const root = new Group();
        root.userData.payloadTextures = new Map([["mask", mask]]);
        root.add(mesh);
        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: null,
                    normalMapKey: null,
                    lightMapKey: "mask",
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        const material = mesh.material as MeshStandardMaterial;
        const state = material.userData.efmiToonMaterial as {
            lightMapEnabled: { value: number };
        };
        expect(state.lightMapEnabled.value).toBe(0);
        const outline = mesh.children[0] as Mesh<BufferGeometry, ShaderMaterial>;
        expect(outline.material.uniforms.widthMask.value).toBe(mask);
        expect(outline.material.uniforms.widthMaskEnabled.value).toBe(1);
    });

    it("restores only the toon flags and asset choices from stored settings", () => {
        const defaults = {
            efmi: DEFAULT_EFMI_TOON_STYLE,
            gimi: DEFAULT_GIMI_TOON_STYLE,
            srmi: DEFAULT_SRMI_TOON_STYLE,
        };
        expect(parseModelViewerToonStyles("")).toEqual(defaults);
        expect(parseModelViewerToonStyles("null")).toEqual(defaults);
        expect(parseModelViewerToonStyles('{"efmi":null,"gimi":[]}')).toEqual(defaults);

        const styles = parseModelViewerToonStyles(
            JSON.stringify({
                efmi: { shadowBrightness: 0.2, outlines: false, lightFollowsCamera: 1 },
                gimi: {
                    toneMapping: true,
                    daytime: false,
                    emission: 2,
                    outlines: false,
                    ramp: "Avatar_Loli_Tex_Body_Shadow_Ramp",
                    faceType: "loli",
                    assetDir: "D:\\Genshin",
                },
            }),
        );
        expect(styles.efmi).toEqual({ ...DEFAULT_EFMI_TOON_STYLE, outlines: false });
        expect(styles.gimi).toEqual({
            ...DEFAULT_GIMI_TOON_STYLE,
            outlines: false,
            ramp: "Avatar_Loli_Tex_Body_Shadow_Ramp",
            faceType: "loli",
            assetDir: "D:\\Genshin",
        });

        const stored = serializeModelViewerToonStyles(styles);
        expect(JSON.parse(stored).gimi).not.toHaveProperty("emission");
        expect(JSON.parse(stored).efmi).not.toHaveProperty("shadowBrightness");
        expect(parseModelViewerToonStyles(stored)).toEqual(styles);
    });

    it("keeps RabbitFX materials on the stock shader when primary UVs are absent", () => {
        const mesh = meshWithTargets([0, 0, 0], []);
        mesh.userData.materialProfile = "wuwa:rabbitfx";
        mesh.userData.toonShadows = true;
        const light = new Texture();
        const root = new Group();
        root.userData.payloadTextures = new Map([["light", light]]);
        root.add(mesh);

        applyPayloadEval(root, {
            state: {},
            meshes: [
                {
                    id: "mesh",
                    visible: true,
                    texKey: null,
                    normalMapKey: null,
                    lightMapKey: "light",
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        expect((mesh.material as MeshStandardMaterial).userData.rabbitFXMaterial).toBeUndefined();
    });
});

describe("lazy payload position variants", () => {
    it("requests only the active variant of visible meshes and commits atomically", async () => {
        const visible = meshWithTargets([0, 0, 0], []);
        visible.userData.meshId = "visible";
        visible.userData.positionVariants = [{ conditions: [], geometryUrl: "/visible.buf" }];
        const hidden = meshWithTargets([0, 0, 0], []);
        hidden.userData.meshId = "hidden";
        hidden.userData.positionVariants = [{ conditions: [], geometryUrl: "/hidden.buf" }];
        const root = new Group();
        root.add(visible, hidden);
        const requested: string[] = [];
        const loader: PositionVariantLoader = {
            load: async (variant) => {
                requested.push(variant.geometryUrl);
                return preparedGeometry([9, 8, 7]);
            },
        };
        const evaluated: EvaluatedViewerState = {
            state: {},
            meshes: [
                {
                    id: "visible",
                    visible: true,
                    texKey: null,
                    normalMapKey: null,
                    lightMapKey: null,
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: 0,
                },
                {
                    id: "hidden",
                    visible: false,
                    texKey: null,
                    normalMapKey: null,
                    lightMapKey: null,
                    materialMapKey: null,
                    cutoutMapKey: null,
                    shapeWeights: {},
                    positionVariantIndex: 0,
                },
            ],
        };

        const prepared = await preparePayloadEval(root, evaluated, loader);
        expect(requested).toEqual(["/visible.buf"]);
        expect(Array.from(visible.geometry.attributes.position.array)).toEqual([0, 0, 0]);
        expect(hidden.visible).toBe(true);

        commitPayloadEval(root, prepared);
        expect(Array.from(visible.geometry.attributes.position.array)).toEqual([9, 8, 7]);
        expect(hidden.visible).toBe(false);
        expect(Array.from(visible.geometry.attributes.normal.array)).toEqual([0, 0, 1]);
    });

    it("does not mutate geometry when loading the requested variant fails", async () => {
        const mesh = meshWithTargets([1, 2, 3], []);
        mesh.userData.positionVariants = [{ conditions: [], geometryUrl: "/broken.buf" }];
        const root = new Group();
        root.add(mesh);
        const loader: PositionVariantLoader = {
            load: async () => {
                throw new Error("decode failed");
            },
        };

        await expect(preparePayloadEval(root, variantEval(0), loader)).rejects.toThrow(
            "decode failed",
        );
        expect(Array.from(mesh.geometry.attributes.position.array)).toEqual([1, 2, 3]);
        expect(mesh.userData.lastPositionVariantIndex).toBeNull();
    });

    it("ignores prepared positions whose length does not match the geometry", () => {
        const mesh = meshWithTargets([1, 2, 3], []);
        const root = new Group();
        root.add(mesh);

        commitPayloadEval(root, {
            evalResult: variantEval(0),
            positions: new Map([["mesh", { variantIndex: 0, ...preparedGeometry([9, 8]) }]]),
        });

        expect(Array.from(mesh.geometry.attributes.position.array)).toEqual([1, 2, 3]);
        expect(mesh.userData.lastPositionVariantIndex).toBeNull();
        expect(mesh.userData.baseNormals).toHaveLength(3);
    });

    it("applies prepared normals and bounds without recomputation across repeated variants", () => {
        const mesh = meshWithTargets([0, 0, 0], []);
        const root = new Group();
        root.add(mesh);
        const normals = vi.spyOn(mesh.geometry, "computeVertexNormals");
        const bounds = vi.spyOn(mesh.geometry, "computeBoundingSphere");
        for (const variantIndex of [0, 1, 2, 0, 1, 2]) {
            commitPayloadEval(root, {
                evalResult: variantEval(variantIndex),
                positions: new Map([
                    ["mesh", { variantIndex, ...preparedGeometry([variantIndex, 0, 0]) }],
                ]),
            });
        }
        expect(normals).not.toHaveBeenCalled();
        expect(bounds).not.toHaveBeenCalled();
        expect(Array.from(mesh.geometry.attributes.normal.array)).toEqual([0, 0, 1]);
        expect(mesh.geometry.boundingSphere?.radius).toBe(2);
    });

    it("preserves authored base normals on initial application and after restoring a variant", () => {
        const mesh = meshWithTargets([0, 0, 0], []);
        mesh.userData.baseNormals = new Float32Array([1, 0, 0]);
        mesh.geometry.attributes.normal.array.set([1, 0, 0]);
        const root = new Group();
        root.add(mesh);
        const compute = vi.spyOn(mesh.geometry, "computeVertexNormals");
        applyPayloadEval(root, variantEval(null));
        expect(Array.from(mesh.geometry.attributes.normal.array)).toEqual([1, 0, 0]);
        commitPayloadEval(root, {
            evalResult: variantEval(0),
            positions: new Map([["mesh", { variantIndex: 0, ...preparedGeometry([2, 0, 0]) }]]),
        });
        applyPayloadEval(root, variantEval(null));
        expect(Array.from(mesh.geometry.attributes.normal.array)).toEqual([1, 0, 0]);
        expect(compute).not.toHaveBeenCalled();
    });

    it("clears payload arrays, caches, and every root texture during disposal", () => {
        const mesh = meshWithTargets(
            [1, 2, 3],
            [{ var: "shape", positions: new Float32Array([4, 5, 6]) }],
        );
        mesh.userData.positionVariants = [{ conditions: [], geometryUrl: "/0.buf" }];
        mesh.userData.baseNormals = new Float32Array(3);
        mesh.userData.lastShapeSignature = "shape:1";
        const texture = new Texture();
        const dispose = vi.spyOn(texture, "dispose");
        const root = new Group();
        root.userData.payloadTextures = new Map([["unused-variant", texture]]);
        root.add(mesh);

        clearPayloadModelData(root);

        expect(dispose).toHaveBeenCalledOnce();
        expect(root.userData.payloadTextures).toBeUndefined();
        expect(mesh.userData.basePositions).toHaveLength(0);
        expect(mesh.userData.shapeTargets).toHaveLength(0);
        expect(mesh.userData.positionVariants).toHaveLength(0);
        expect(mesh.userData.baseNormals).toHaveLength(0);
        expect(mesh.userData.lastShapeSignature).toBeUndefined();
    });
});

function preparedGeometry(values: number[]): ModelViewerPositionGeometry {
    return {
        positions: new Float32Array(values),
        normals: Float32Array.from(values, (_, index) => (index % 3 === 2 ? 1 : 0)),
        bounds: { min: [-2, -2, -2], max: [2, 2, 2], center: [0, 0, 0], radius: 2 },
    };
}

it.each(["linear", "midpoint_pair"] as const)(
    "preserves authored normals for neutral %s shape weights",
    (mode) => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [
                {
                    var: "shape",
                    positions: new Float32Array([2, 0, 0]),
                    mode: mode === "midpoint_pair" ? mode : undefined,
                },
            ],
        );
        mesh.userData.baseNormals = new Float32Array([1, 0, 0]);
        mesh.geometry.attributes.normal.array.set([1, 0, 0]);
        const root = new Group();
        root.add(mesh);
        const normal = vi.spyOn(mesh.geometry, "computeVertexNormals");
        applyPayloadEval(root, evalState({ shape: mode === "midpoint_pair" ? 0.5 : 0 }));
        expect(normal).not.toHaveBeenCalled();
        expect(Array.from(mesh.geometry.attributes.normal.array)).toEqual([1, 0, 0]);
        applyPayloadEval(root, evalState({ shape: 1 }));
        normal.mockClear();
        applyPayloadEval(root, evalState({ shape: mode === "midpoint_pair" ? 0.5 : 0 }));
        expect(normal).not.toHaveBeenCalled();
        expect(Array.from(mesh.geometry.attributes.normal.array)).toEqual([1, 0, 0]);
    },
);
