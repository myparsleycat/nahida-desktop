import path from "node:path";

import type { NahidaDesktop } from "@/main";

import { NcnnVulkanRuntime, type NcnnVulkanRuntimeConfig } from "./ncnn-vulkan-runtime";
import { isRealesrganRuntimeInstalled, REALESRGAN_BINARY_NAME } from "./realesrgan-runtime-status";

export const REALESRGAN_RUNTIME_VERSION = "20220424";
export const REALESRGAN_RUNTIME_DIR_NAME = "realesrgan-ncnn-vulkan";
export { REALESRGAN_BINARY_NAME };
export const REALESRGAN_DOWNLOAD_URL =
    "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-windows.zip";
export const REALESRGAN_ARCHIVE_SHA256 =
    "abc02804e17982a3be33675e4d471e91ea374e65b70167abc09e31acb412802d";

export const REALESRGAN_RUNTIME_CONFIG: NcnnVulkanRuntimeConfig = {
    dirName: REALESRGAN_RUNTIME_DIR_NAME,
    binaryName: REALESRGAN_BINARY_NAME,
    version: REALESRGAN_RUNTIME_VERSION,
    downloadUrl: REALESRGAN_DOWNLOAD_URL,
    archiveSha256: REALESRGAN_ARCHIVE_SHA256,
    settingKeyPrefix: "mod_tools:realesrgan-ncnn-vulkan",
    displayName: "Real-ESRGAN",
    modelDirNames: ["models"],
    isInstalled: isRealesrganRuntimeInstalled,
    resolveModelsPath: (runtimeRoot) => path.join(runtimeRoot, "models"),
};

export class RealesrganRuntime extends NcnnVulkanRuntime {
    constructor(desktop: NahidaDesktop) {
        super(desktop, REALESRGAN_RUNTIME_CONFIG);
    }
}
