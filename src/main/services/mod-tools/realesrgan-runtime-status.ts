import path from "node:path";

import fse from "fs-extra";

export const REALESRGAN_BINARY_NAME = "realesrgan-ncnn-vulkan.exe";

export const REQUIRED_REALESRGAN_MODEL_FILES = [
    "realesr-animevideov3-x2",
    "realesr-animevideov3-x3",
    "realesr-animevideov3-x4",
    "realesrgan-x4plus-anime",
    "realesrgan-x4plus",
] as const;

export async function isRealesrganRuntimeInstalled(binaryPath: string, modelsPath: string) {
    if (!(await fse.pathExists(binaryPath)) || !(await fse.pathExists(modelsPath))) {
        return false;
    }

    const modelReady = await Promise.all(
        REQUIRED_REALESRGAN_MODEL_FILES.map(async (model) => {
            const paramPath = path.join(modelsPath, `${model}.param`);
            const binPath = path.join(modelsPath, `${model}.bin`);
            return (await fse.pathExists(paramPath)) && (await fse.pathExists(binPath));
        }),
    );

    return modelReady.every(Boolean);
}
