import { describe, expect, it } from "vitest";

import { mapFixToolPresets, mapFixToolScripts } from "./fix-tools";

describe("mapFixToolScripts", () => {
    it("maps generated script rows onto the UI field names", () => {
        expect(
            mapFixToolScripts([
                { ID: "script-1", Name: "normalize.py", Type: "python", Size: 2048 },
            ]),
        ).toEqual([{ id: "script-1", name: "normalize.py", type: "python", size: 2048 }]);
    });

    it("treats a missing script list as empty", () => {
        expect(mapFixToolScripts(null)).toEqual([]);
        expect(mapFixToolScripts(undefined)).toEqual([]);
    });
});

describe("mapFixToolPresets", () => {
    it("maps generated preset rows and nested script ids onto the UI field names", () => {
        expect(
            mapFixToolPresets([
                {
                    ID: "preset-1",
                    Name: "cleanup",
                    Scripts: [
                        { Order: 2, PresetID: "preset-1", ScriptID: "script-2" },
                        { Order: 1, PresetID: "preset-1", ScriptID: "script-1" },
                    ],
                },
            ]),
        ).toEqual([
            {
                id: "preset-1",
                name: "cleanup",
                scripts: [
                    { order: 2, presetId: "preset-1", scriptId: "script-2" },
                    { order: 1, presetId: "preset-1", scriptId: "script-1" },
                ],
            },
        ]);
    });

    it("treats missing preset scripts as an empty list", () => {
        expect(mapFixToolPresets([{ ID: "preset-empty", Name: "empty", Scripts: null }])).toEqual([
            { id: "preset-empty", name: "empty", scripts: [] },
        ]);
    });
});
