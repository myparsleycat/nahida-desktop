import { expect, it } from "vitest";

import type { ViewerEvalInput } from "./types";

import { applyStateRules, evaluateViewerState } from "./eval";

it("sees a key added by an earlier rule through a case-insensitive lookup", () => {
    const rules = [
        { var: "Skirt", value: "1", conditions: [[{ var: "skirt", value: "1", negate: true }]] },
        { var: "Shoes", value: "1", conditions: [[{ var: "skirt", value: "2", negate: false }]] },
    ];
    expect(applyStateRules({ Outfit: 1 }, rules)).toEqual({ Outfit: 1, Skirt: "1" });
});

it("keeps folder-shared meshes visible in an INI form without showing sibling forms", () => {
    const payload: ViewerEvalInput = {
        defaultState: {},
        stateRules: [],
        meshes: [
            "",
            "bundle",
            "bundle::mod.ini::Human",
            "bundle::mod.ini::Mermaid",
            "bundle-other",
        ].map((form) => ({
            id: form || "shared",
            form,
            texKey: null,
            normalMapKey: null,
            lightMapKey: null,
            materialMapKey: null,
            shapeTargets: [],
        })),
    };

    expect(
        evaluateViewerState(payload, {}, "bundle::mod.ini::Human").meshes.map(
            (mesh) => mesh.visible,
        ),
    ).toEqual([true, true, true, false, false]);
});
