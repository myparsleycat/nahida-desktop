import type { NahidaDesktop } from "@/main";

import { NcnnVulkanRuntime, type NcnnVulkanRuntimeConfig } from "./ncnn-vulkan-runtime";
import { isRealcuganRuntimeInstalled, REALCUGAN_BINARY_NAME } from "./realcugan-runtime-status";

export const REALCUGAN_RUNTIME_VERSION = "20220728";
export const REALCUGAN_RUNTIME_DIR_NAME = "realcugan-ncnn-vulkan";
export { REALCUGAN_BINARY_NAME };
export const REALCUGAN_DOWNLOAD_URL =
    "https://github.com/nihui/realcugan-ncnn-vulkan/releases/download/20220728/realcugan-ncnn-vulkan-20220728-windows.zip";
export const REALCUGAN_ARCHIVE_SHA256 =
    "c6e08d46c11704b1e3a1ada9ddd591cb5005f52f132136c8633ba25def400e01";

export const REALCUGAN_RUNTIME_CONFIG: NcnnVulkanRuntimeConfig = {
    dirName: REALCUGAN_RUNTIME_DIR_NAME,
    binaryName: REALCUGAN_BINARY_NAME,
    version: REALCUGAN_RUNTIME_VERSION,
    downloadUrl: REALCUGAN_DOWNLOAD_URL,
    archiveSha256: REALCUGAN_ARCHIVE_SHA256,
    settingKeyPrefix: "mod_tools:realcugan-ncnn-vulkan",
    displayName: "Real-CUGAN",
    modelDirNames: ["models-pro", "models-se", "models-nose"],
    isInstalled: isRealcuganRuntimeInstalled,
    resolveModelsPath: (runtimeRoot) => runtimeRoot,
};

export class RealcuganRuntime extends NcnnVulkanRuntime {
    constructor(desktop: NahidaDesktop) {
        super(desktop, REALCUGAN_RUNTIME_CONFIG);
    }
}
