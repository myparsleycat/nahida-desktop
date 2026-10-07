import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("generated Wails binding contract", () => {
    it("does not expose large numeric or base64 mesh payloads", () => {
        const models = [
            "bindings/nahida.live/desktop/internal/tools/models.ts",
            "bindings/nahida.live/desktop/internal/tools/body_shape/models.ts",
            "bindings/nahida.live/desktop/internal/tools/fix_inspection/models.ts",
            "bindings/nahida.live/desktop/internal/tools/fix_tool/models.ts",
            "bindings/nahida.live/desktop/internal/tools/fixer_4001/models.ts",
            "bindings/nahida.live/desktop/internal/tools/mod_bisect/models.ts",
            "bindings/nahida.live/desktop/internal/tools/model_viewer/models.ts",
            "bindings/nahida.live/desktop/internal/tools/modmesh/models.ts",
            "bindings/nahida.live/desktop/internal/tools/texture/models.ts",
            "bindings/nahida.live/desktop/internal/tools/toggle_persist/models.ts",
            "bindings/nahida.live/desktop/internal/tools/touch_profile/models.ts",
            "bindings/nahida.live/desktop/internal/tools/wuwa_fixer/models.ts",
            "bindings/nahida.live/desktop/internal/tools/zzmi_fixer/models.ts",
        ].map((path) => readFileSync(path, "utf8"));
        for (const source of models) {
            expect(source).not.toMatch(/"(?:positions|indices|weights)": number\[\]/);
            expect(source).not.toMatch(/"blendBytes": string/);
        }
    });

    it("keeps protocol memory session internals out of Wails bindings", () => {
        const protocol = readFileSync(
            "bindings/nahida.live/desktop/internal/infra/protocol.ts",
            "utf8",
        );
        expect(protocol).not.toMatch(
            /CreateMemorySession|StoreMemoryBuffer|RemoveMemoryBuffer|CreateMemoryUpload|TakeMemoryUpload|CleanupMemorySession/,
        );
        expect(protocol).toContain("LocalFileURL");
    });

    it("exposes the Menu Maker contract through the Tools service", () => {
        const service = readFileSync(
            "bindings/nahida.live/desktop/internal/tools/tools.ts",
            "utf8",
        );
        const models = readFileSync(
            "bindings/nahida.live/desktop/internal/tools/models.ts",
            "utf8",
        );
        for (const method of [
            "MenuMakerApplyBundle",
            "MenuMakerGenerate",
            "MenuMakerLoadSource",
            "MenuMakerParse",
            "MenuMakerSaveINI",
            "MenuMakerSaveZIP",
            "MenuMakerScanFolder",
        ]) {
            expect(service).toContain(`export function ${method}(`);
        }
        for (const model of ["MenuMakerDocument", "MenuMakerGenerateRequest", "MenuMakerSource"]) {
            expect(models).toContain(`export type ${model} =`);
        }
    });

    it("exposes the character classification contract through the Mod service", () => {
        const service = readFileSync("bindings/nahida.live/desktop/internal/mod/mod.ts", "utf8");
        const models = readFileSync("bindings/nahida.live/desktop/internal/mod/models.ts", "utf8");
        for (const method of [
            "GetClassifications",
            "SaveClassification",
            "DeleteClassification",
            "SetActiveClassification",
            "SetCharacterClassification",
        ]) {
            expect(service).toContain(`export function ${method}(`);
        }
        expect(models).toMatch(/"classifications"\?: \{ \[_ in string\]\?: string \}/);
    });
});
