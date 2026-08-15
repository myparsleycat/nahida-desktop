import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, expect, it } from "vitest";

import { isRealesrganRuntimeInstalled, REALESRGAN_BINARY_NAME } from "./realesrgan-runtime-status";

const temporaryDirectories: string[] = [];

afterEach(async () => {
    await Promise.all(temporaryDirectories.splice(0).map((dirPath) => fse.remove(dirPath)));
});

describe("realesrgan runtime install check", () => {
    it("requires the executable and all bundled model files", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "realesrgan-runtime-"));
        temporaryDirectories.push(root);
        const binaryPath = path.join(root, REALESRGAN_BINARY_NAME);
        const modelsPath = path.join(root, "models");
        await fse.outputFile(binaryPath, "");
        await fse.ensureDir(modelsPath);

        expect(await isRealesrganRuntimeInstalled(binaryPath, modelsPath)).toBe(false);

        for (const model of [
            "realesr-animevideov3-x2",
            "realesr-animevideov3-x3",
            "realesr-animevideov3-x4",
            "realesrgan-x4plus-anime",
            "realesrgan-x4plus",
        ]) {
            await fse.outputFile(path.join(modelsPath, `${model}.param`), "");
            await fse.outputFile(path.join(modelsPath, `${model}.bin`), "");
        }

        expect(await isRealesrganRuntimeInstalled(binaryPath, modelsPath)).toBe(true);
        expect(await isRealesrganRuntimeInstalled(path.join(root, "missing.exe"), modelsPath)).toBe(
            false,
        );
    });
});
