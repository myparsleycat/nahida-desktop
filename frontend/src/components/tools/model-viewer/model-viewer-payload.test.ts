import type { EvaluatedViewerState } from "@shared/mod-viewer/types";
import { BufferAttribute, BufferGeometry, Group, Mesh, MeshStandardMaterial, Texture } from "three";
import { describe, expect, it, vi } from "vitest";

import type { PositionVariantLoader } from "./model-viewer-position-loader";

import {
    applyPayloadEval,
    clearPayloadModelData,
    commitPayloadEval,
    preparePayloadEval,
    setPayloadToonShadows,
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
        normalCache: [],
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

    it("restores shape geometry after applying a position variant", () => {
        const mesh = meshWithTargets(
            [0, 0, 0],
            [{ var: "shape", positions: new Float32Array([2, 0, 0]) }],
        );
        mesh.userData.positionVariants = [
            { conditions: [], sourceUrl: "/variant.buf", stride: 40, sourceBytes: 12 },
        ];
        const root = new Group();
        root.add(mesh);
        applyPayloadEval(root, evalState({ shape: 1 }));
        commitPayloadEval(root, {
            evalResult: variantEval(0),
            positions: new Map([
                ["mesh", { variantIndex: 0, positions: new Float32Array([9, 0, 0]) }],
            ]),
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
        geometry.setAttribute("tangent", new BufferAttribute(new Float32Array(12), 4));
        const material = new MeshStandardMaterial();
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            normalCache: [],
            materialProfile: "zzmi",
        };
        const diffuse = new Texture();
        const normal = new Texture();
        const light = new Texture();
        const packedMaterial = new Texture();
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
            fragmentShader: "#include <roughnessmap_fragment>\n#include <metalnessmap_fragment>",
            uniforms: {},
        };
        material.onBeforeCompile(shader as never, {} as never);
        expect(shader.fragmentShader).toContain("1.0 - texelRoughness.g");
        expect(shader.fragmentShader).toContain("metalnessFactor *= texelMetalness.g");
        expect(shader.fragmentShader).not.toContain("#include <roughnessmap_fragment>");
        expect(shader.fragmentShader).not.toContain("#include <metalnessmap_fragment>");
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
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        expect((mesh.material as MeshStandardMaterial).normalMap).toBeNull();
    });

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
            normalCache: [],
            materialProfile: "wuwa:rabbitfx",
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
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });

        expect(material.normalMap).toBe(normal);
        expect(material.normalScale.y).toBe(-1);
        expect(material.metalness).toBe(0);
        expect(material.roughness).toBe(1);
        expect(geometry.attributes.tangent).toBeUndefined();
    });

    it("adapts RabbitFX LightMap.G without occupying generic PBR map slots", () => {
        const geometry = new BufferGeometry();
        geometry.setAttribute("position", new BufferAttribute(new Float32Array(9), 3));
        geometry.setAttribute("uv", new BufferAttribute(new Float32Array(6), 2));
        geometry.setAttribute("tangent", new BufferAttribute(new Float32Array(12), 4));
        const material = new MeshStandardMaterial();
        const mesh = new Mesh(geometry, material);
        mesh.userData = {
            meshId: "mesh",
            basePositions: new Float32Array(9),
            shapeTargets: [],
            positionVariants: [],
            normalCache: [],
            materialProfile: "wuwa:rabbitfx",
            toonShadows: true,
        };
        const diffuse = new Texture();
        const normal = new Texture();
        const firstLight = new Texture();
        const secondLight = new Texture();
        const root = new Group();
        root.userData.payloadTextures = new Map([
            ["diffuse", diffuse],
            ["normal", normal],
            ["light-a", firstLight],
            ["light-b", secondLight],
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
        expect(material.customProgramCacheKey()).toBe("wuwa-rabbitfx-v1");

        const shader = {
            vertexShader: "#include <common>\nvoid main() {\n#include <uv_vertex>\n}",
            fragmentShader: "#include <common>\nvoid main() {\n#include <lights_fragment_begin>\n}",
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
                    shapeWeights: {},
                    positionVariantIndex: null,
                },
            ],
        });
        expect(material.version).toBe(version);
        expect(
            (shader.uniforms as Record<string, { value: unknown }>).rabbitFXLightMap?.value,
        ).toBe(secondLight);

        setPayloadToonShadows(root, false);
        expect(
            (shader.uniforms as Record<string, { value: unknown }>).rabbitFXToonEnabled?.value,
        ).toBe(0);
        expect(material.version).toBe(version);
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
        visible.userData.positionVariants = [
            { conditions: [], sourceUrl: "/visible.buf", stride: 40, sourceBytes: 40 },
        ];
        const hidden = meshWithTargets([0, 0, 0], []);
        hidden.userData.meshId = "hidden";
        hidden.userData.positionVariants = [
            { conditions: [], sourceUrl: "/hidden.buf", stride: 40, sourceBytes: 40 },
        ];
        const root = new Group();
        root.add(visible, hidden);
        const requested: string[] = [];
        const loader: PositionVariantLoader = {
            load: async (variant) => {
                requested.push(variant.sourceUrl);
                return new Float32Array([9, 8, 7]);
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
        expect(visible.userData.normalCache).toHaveLength(1);
    });

    it("does not mutate geometry when loading the requested variant fails", async () => {
        const mesh = meshWithTargets([1, 2, 3], []);
        mesh.userData.positionVariants = [
            { conditions: [], sourceUrl: "/broken.buf", stride: 40, sourceBytes: 40 },
        ];
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
        expect(mesh.userData.lastPositionVariantIndex).toBeUndefined();
    });

    it("ignores prepared positions whose length does not match the geometry", () => {
        const mesh = meshWithTargets([1, 2, 3], []);
        const root = new Group();
        root.add(mesh);

        commitPayloadEval(root, {
            evalResult: variantEval(0),
            positions: new Map([
                ["mesh", { variantIndex: 0, positions: new Float32Array([9, 8]) }],
            ]),
        });

        expect(Array.from(mesh.geometry.attributes.position.array)).toEqual([1, 2, 3]);
        expect(mesh.userData.lastPositionVariantIndex).toBeUndefined();
        expect(mesh.userData.normalCache).toHaveLength(0);
    });

    it("bounds each mesh normal cache to the current and previous position", () => {
        const mesh = meshWithTargets([0, 0, 0], []);
        mesh.userData.positionVariants = [
            { conditions: [], sourceUrl: "/0.buf", stride: 40, sourceBytes: 40 },
            { conditions: [], sourceUrl: "/1.buf", stride: 40, sourceBytes: 40 },
            { conditions: [], sourceUrl: "/2.buf", stride: 40, sourceBytes: 40 },
        ];
        const root = new Group();
        root.add(mesh);
        for (let variantIndex = 0; variantIndex < 3; variantIndex++) {
            commitPayloadEval(root, {
                evalResult: variantEval(variantIndex),
                positions: new Map([
                    [
                        "mesh",
                        {
                            variantIndex,
                            positions: new Float32Array([variantIndex, 0, 0]),
                        },
                    ],
                ]),
            });
        }
        expect(mesh.userData.normalCache).toHaveLength(2);
        expect(mesh.userData.normalCache.map((entry: { key: number }) => entry.key)).toEqual([
            2, 1,
        ]);
    });

    it("clears payload arrays, caches, and every root texture during disposal", () => {
        const mesh = meshWithTargets(
            [1, 2, 3],
            [{ var: "shape", positions: new Float32Array([4, 5, 6]) }],
        );
        mesh.userData.positionVariants = [
            { conditions: [], sourceUrl: "/0.buf", stride: 40, sourceBytes: 40 },
        ];
        mesh.userData.normalCache = [{ key: 0, normal: new Float32Array(3) }];
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
        expect(mesh.userData.normalCache).toHaveLength(0);
        expect(mesh.userData.lastShapeSignature).toBeUndefined();
    });
});
