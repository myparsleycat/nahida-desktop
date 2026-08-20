import path from "node:path";

import fse from "fs-extra";

export const REALCUGAN_BINARY_NAME = "realcugan-ncnn-vulkan.exe";

// Model files the app can use with the fixed no-denoise (-n 0) setting,
// relative to the runtime root. models-pro has no up4x and models-nose only
// ships up2x-no-denoise, so the exposed scale options are limited accordingly.
export const REQUIRED_REALCUGAN_MODEL_FILES = [
    "models-pro/up2x-no-denoise",
    "models-pro/up3x-no-denoise",
    "models-se/up2x-no-denoise",
    "models-se/up3x-no-denoise",
    "models-se/up4x-no-denoise",
    "models-nose/up2x-no-denoise",
] as const;

export async function isRealcuganRuntimeInstalled(binaryPath: string, runtimeRoot: string) {
    if (!(await fse.pathExists(binaryPath)) || !(await fse.pathExists(runtimeRoot))) {
        return false;
    }

    const modelReady = await Promise.all(
        REQUIRED_REALCUGAN_MODEL_FILES.map(async (model) => {
            const paramPath = path.join(runtimeRoot, `${model}.param`);
            const binPath = path.join(runtimeRoot, `${model}.bin`);
            return (await fse.pathExists(paramPath)) && (await fse.pathExists(binPath));
        }),
    );

    return modelReady.every(Boolean);
}
