import { fetchFloat32, fetchUint32 } from "@renderer/wails/binary-memory";
import type { ModViewerTransport } from "@shared/mod-viewer/types";
import { BufferGeometry, Mesh } from "three";
import { afterEach, expect, it, vi } from "vitest";

import {
    buildPayloadModel,
    clearPayloadModelData,
    commitPayloadEval,
} from "./model-viewer-payload";

vi.mock("@renderer/wails/binary-memory", () => ({ fetchFloat32: vi.fn(), fetchUint32: vi.fn() }));
afterEach(() => vi.restoreAllMocks());

it("preserves backend normals and bounds through initial load and variant restoration", async () => {
    const positions = new Float32Array([0, 0, 0, 1, 0, 0, 0, 1, 0]);
    const authored = new Float32Array([1, 0, 0, 1, 0, 0, 1, 0, 0]);
    vi.mocked(fetchFloat32).mockImplementation(async (url) =>
        url === "positions" ? positions.slice() : authored.slice(),
    );
    vi.mocked(fetchUint32).mockResolvedValue(new Uint32Array([0, 1, 2]));
    const normals = vi.spyOn(BufferGeometry.prototype, "computeVertexNormals");
    const sphere = vi.spyOn(BufferGeometry.prototype, "computeBoundingSphere");
    const bounds = { min: [0, 0, 0], max: [1, 1, 0], center: [0.5, 0.5, 0], radius: Math.SQRT1_2 };
    const mesh = {
        id: "mesh",
        component: "body",
        positionsUrl: "positions",
        normalsUrl: "normals",
        indicesUrl: "indices",
        bounds,
        conditions: [[]],
        texKey: null,
        textureVariants: [],
        normalMapKey: null,
        normalMapVariants: [],
        lightMapKey: null,
        lightMapVariants: [],
        materialMapKey: null,
        materialMapVariants: [],
        shapeTargets: [],
        positionVariants: [],
    };
    const transport: ModViewerTransport = {
        memorySessionId: "test",
        iniPath: "mod.ini",
        modPath: "mod",
        name: "model",
        meshes: [mesh],
        textures: {},
        variables: [],
        defaultState: {},
        stateRules: [],
        uiAssets: {},
        animations: [],
        computeDeformers: [],
    };
    const evaluated = {
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
                positionVariantIndex: null,
            },
        ],
    };
    const root = await buildPayloadModel(transport, evaluated, true, { load: vi.fn() });
    const geometry = (root.children[0] as Mesh).geometry;
    expect(geometry.attributes.normal.array).toEqual(authored);
    expect(geometry.boundingSphere?.radius).toBe(bounds.radius);
    commitPayloadEval(root, {
        evalResult: { state: {}, meshes: [{ ...evaluated.meshes[0], positionVariantIndex: 0 }] },
        positions: new Map([
            [
                "mesh",
                {
                    variantIndex: 0,
                    positions: new Float32Array(9),
                    normals: new Float32Array(9),
                    bounds: { ...bounds, radius: 9 },
                },
            ],
        ]),
    });
    expect(geometry.boundingSphere?.radius).toBe(9);
    commitPayloadEval(root, { evalResult: evaluated, positions: new Map() });
    expect(geometry.attributes.normal.array).toEqual(authored);
    expect(geometry.boundingSphere?.radius).toBe(bounds.radius);
    expect(normals).not.toHaveBeenCalled();
    expect(sphere).not.toHaveBeenCalled();
    clearPayloadModelData(root);
});
