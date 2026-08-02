import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { describe, expect, it } from "vitest";

import { MOD_DOWNLOAD_METADATA_FILE_NAME, readGameBananaModId } from "./mod-download-metadata";

describe("readGameBananaModId", () => {
    it.each([
        [{ source: "gamebanana", mod: { id: 123 } }, 123],
        [{ source: "mod" }, undefined],
        [{ source: "gamebanana", mod: { id: -1 } }, undefined],
        [{ source: "gamebanana", mod: { id: "123" } }, undefined],
    ])("reads a valid GameBanana mod ID from %j", async (metadata, expected) => {
        const dirPath = await fse.mkdtemp(path.join(os.tmpdir(), "nahida-mod-metadata-"));
        await fse.writeJson(path.join(dirPath, MOD_DOWNLOAD_METADATA_FILE_NAME), metadata);

        const result = await readGameBananaModId(dirPath);

        await fse.remove(dirPath);
        expect(result).toBe(expected);
    });
});
