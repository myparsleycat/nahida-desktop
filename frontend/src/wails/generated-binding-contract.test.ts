import { readFileSync } from "node:fs";

import { describe, expect, it } from "vitest";

describe("generated Wails binding contract", () => {
    it("does not expose large numeric or base64 mesh payloads", () => {
        const models = readFileSync(
            "bindings/nahida.live/desktop/internal/tools/models.ts",
            "utf8",
        );
        expect(models).not.toMatch(/"(?:positions|indices|weights)": number\[\]/);
        expect(models).not.toMatch(/"blendBytes": string/);
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

    it("exposes the focused Menu Maker service contract", () => {
        const service = readFileSync(
            "bindings/nahida.live/desktop/internal/menumaker/menumaker.ts",
            "utf8",
        );
        const models = readFileSync(
            "bindings/nahida.live/desktop/internal/menumaker/models.ts",
            "utf8",
        );
        expect(service).toMatch(/ApplyBundle|Generate|LoadSource|Parse|SaveINI|SaveZIP|ScanFolder/);
        expect(models).toMatch(/MenuMakerDocument|MenuMakerGenerateRequest|MenuMakerSource/);
    });
});
