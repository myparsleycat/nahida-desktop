import os from "node:os";
import path from "node:path";

import fse from "fs-extra";
import { afterEach, describe, expect, it } from "vitest";

import {
    isRealcuganRuntimeInstalled,
    REALCUGAN_BINARY_NAME,
    REQUIRED_REALCUGAN_MODEL_FILES,
} from "./realcugan-runtime-status";

const temporaryDirectories: string[] = [];

afterEach(async () => {
    await Promise.all(temporaryDirectories.splice(0).map((dirPath) => fse.remove(dirPath)));
});

describe("realcugan runtime install check", () => {
    it("requires the executable and all bundled model files", async () => {
        const root = await fse.mkdtemp(path.join(os.tmpdir(), "realcugan-runtime-"));
        temporaryDirectories.push(root);
        const binaryPath = path.join(root, REALCUGAN_BINARY_NAME);
        await fse.outputFile(binaryPath, "");

        expect(await isRealcuganRuntimeInstalled(binaryPath, root)).toBe(false);

        for (const model of REQUIRED_REALCUGAN_MODEL_FILES) {
            await fse.outputFile(path.join(root, `${model}.param`), "");
            await fse.outputFile(path.join(root, `${model}.bin`), "");
        }

        expect(await isRealcuganRuntimeInstalled(binaryPath, root)).toBe(true);
        expect(await isRealcuganRuntimeInstalled(path.join(root, "missing.exe"), root)).toBe(false);
    });
});
