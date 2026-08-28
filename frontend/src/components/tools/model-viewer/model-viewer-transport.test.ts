import type { ModelViewerTransport as WailsModelViewerTransport } from "@bindings/tools";
import { describe, expect, it } from "vitest";

import { normalizeModelViewerTransport } from "./model-viewer-transport";

describe("normalizeModelViewerTransport", () => {
    it("turns nullable Wails collections into frontend collections", () => {
        const input: WailsModelViewerTransport = {
            memorySessionId: "session",
            iniPath: "mod.ini",
            modPath: "mod",
            name: "Example",
            meshes: [
                {
                    id: "mesh",
                    component: "body",
                    positionsUrl: "positions",
                    indicesUrl: "indices",
                    conditions: null,
                    texKey: null,
                    textureVariants: null,
                    normalMapKey: null,
                    normalMapVariants: null,
                    lightMapKey: null,
                    lightMapVariants: null,
                    materialMapKey: null,
                    materialMapVariants: null,
                    shapeTargets: null,
                    positionVariants: null,
                },
            ],
            textures: null,
            variables: [
                {
                    id: "toggle",
                    label: "Toggle",
                    defaultValue: 0,
                    values: null,
                    order: 0,
                    effects: null,
                },
            ],
            defaultState: null,
            stateRules: null,
            uiAssets: {},
            animations: [
                {
                    id: "animation",
                    label: "Animation",
                    variableIds: null,
                    fps: 30,
                    frameStart: 0,
                    frameEnd: 0,
                    loop: false,
                    frames: [
                        {
                            index: 0,
                            time: 0,
                            values: null,
                        },
                    ],
                },
            ],
        };

        const result = normalizeModelViewerTransport(input);

        expect(result.meshes[0]).toMatchObject({
            conditions: [],
            textureVariants: [],
            normalMapVariants: [],
            lightMapVariants: [],
            materialMapVariants: [],
            shapeTargets: [],
            positionVariants: [],
        });
        expect(result.textures).toEqual({});
        expect(result.variables[0]?.values).toEqual([]);
        expect(result.variables[0]?.effects).toBeUndefined();
        expect(result.defaultState).toEqual({});
        expect(result.stateRules).toEqual([]);
        expect(result.animations[0]?.variableIds).toEqual([]);
        expect(result.animations[0]?.frames[0]?.values).toEqual({});
    });

    it("turns a missing animation list into an empty list", () => {
        const input: WailsModelViewerTransport = {
            memorySessionId: "session",
            iniPath: "mod.ini",
            modPath: "mod",
            name: "Example",
            meshes: null,
            textures: null,
            variables: null,
            defaultState: null,
            stateRules: null,
            uiAssets: {},
            animations: null,
        };

        expect(normalizeModelViewerTransport(input).animations).toEqual([]);
    });
});
