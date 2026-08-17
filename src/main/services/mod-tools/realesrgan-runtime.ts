import { createHash } from "node:crypto";
import { createReadStream, createWriteStream } from "node:fs";
import path from "node:path";
import { pipeline } from "node:stream/promises";

import type { TextureUpscaleRuntimeStatus } from "@shared/types";
import { app } from "electron";
import fse from "fs-extra";
import ky from "ky";

import type { NahidaDesktop } from "@/main";

import { drainWebStream, webStreamToNodeReadable } from "@/main/lib/web-stream-to-readable";

import { isRealesrganRuntimeInstalled, REALESRGAN_BINARY_NAME } from "./realesrgan-runtime-status";

export const REALESRGAN_RUNTIME_VERSION = "20220424";
export const REALESRGAN_RUNTIME_DIR_NAME = "realesrgan-ncnn-vulkan";
export { REALESRGAN_BINARY_NAME };
export const REALESRGAN_DOWNLOAD_URL =
    "https://github.com/xinntao/Real-ESRGAN/releases/download/v0.2.5.0/realesrgan-ncnn-vulkan-20220424-windows.zip";
export const REALESRGAN_ARCHIVE_SHA256 =
    "abc02804e17982a3be33675e4d471e91ea374e65b70167abc09e31acb412802d";

const INSTALLED_VERSION_KEY = "mod_tools:realesrgan-ncnn-vulkan:installed-version";
const BINARY_PATH_KEY = "mod_tools:realesrgan-ncnn-vulkan:binary-path";
const MODELS_PATH_KEY = "mod_tools:realesrgan-ncnn-vulkan:models-path";
const DOWNLOAD_STALL_TIMEOUT_MS = 30_000;

export class RealesrganRuntime {
    private installPromise: Promise<TextureUpscaleRuntimeStatus> | null = null;

    constructor(private readonly desktop: NahidaDesktop) {}

    public getToolDir() {
        return path.join(app.getPath("userData"), "tools", REALESRGAN_RUNTIME_DIR_NAME);
    }

    public async getStatus(): Promise<TextureUpscaleRuntimeStatus> {
        const binaryPath = await this.resolveBinaryPath();
        const modelsPath = await this.resolveModelsPath(binaryPath);
        const installed = await this.isInstalled(binaryPath, modelsPath);

        return {
            installed,
            version: installed
                ? ((await this.getSettingValue(INSTALLED_VERSION_KEY)) ??
                  REALESRGAN_RUNTIME_VERSION)
                : null,
            binaryPath: installed ? binaryPath : null,
            modelsPath: installed ? modelsPath : null,
            needsInstall: !installed,
        };
    }

    public async ensureInstalled(
        onProgress?: (phase: "download" | "extract", percent: number | null) => void,
    ) {
        const current = await this.getStatus();
        if (current.installed) {
            return current;
        }

        if (!this.installPromise) {
            this.installPromise = this.install(onProgress).finally(() => {
                this.installPromise = null;
            });
        }

        return await this.installPromise;
    }

    private async install(
        onProgress?: (phase: "download" | "extract", percent: number | null) => void,
    ) {
        const targetDir = this.getToolDir();
        const zipPath = path.join(targetDir, `${REALESRGAN_RUNTIME_DIR_NAME}.zip.download`);
        const extractDir = path.join(targetDir, "extract");

        await fse.ensureDir(targetDir);
        await fse.remove(zipPath);
        await fse.remove(extractDir);

        try {
            onProgress?.("download", 0);
            await this.downloadArchive(zipPath, (percent) => onProgress?.("download", percent));
            const digest = createHash("sha256");
            for await (const chunk of createReadStream(zipPath)) {
                digest.update(chunk);
            }
            if (digest.digest("hex") !== REALESRGAN_ARCHIVE_SHA256) {
                throw new Error("Downloaded Real-ESRGAN archive checksum mismatch.");
            }
            onProgress?.("extract", null);
            await this.desktop.service.archive.extract(zipPath, extractDir);
            const layout = await this.resolveExtractedLayout(extractDir);
            await this.promoteExtractedLayout(layout, targetDir);
            await this.saveSettingValue(INSTALLED_VERSION_KEY, REALESRGAN_RUNTIME_VERSION);
            await this.saveSettingValue(
                BINARY_PATH_KEY,
                path.join(targetDir, REALESRGAN_BINARY_NAME),
            );
            await this.saveSettingValue(MODELS_PATH_KEY, path.join(targetDir, "models"));
        } catch (error) {
            await fse.remove(zipPath).catch(() => {});
            await fse.remove(extractDir).catch(() => {});
            throw error;
        }

        await fse.remove(zipPath).catch(() => {});
        await fse.remove(extractDir).catch(() => {});

        const status = await this.getStatus();
        if (!status.installed) {
            throw new Error("Real-ESRGAN runtime installation is incomplete.");
        }

        return status;
    }

    private async downloadArchive(zipPath: string, onPercent: (percent: number | null) => void) {
        const abortController = new AbortController();
        const response = await ky.get(REALESRGAN_DOWNLOAD_URL, {
            timeout: 10 * 60 * 1000,
            signal: abortController.signal,
            throwHttpErrors: false,
            headers: {
                "User-Agent": "Nahida Desktop",
            },
        });

        if (!response.ok) {
            await drainWebStream(response.body).catch(() => {});
            throw new Error(`Failed to download Real-ESRGAN runtime: HTTP ${response.status}`);
        }
        if (!response.body) {
            throw new Error("Failed to download Real-ESRGAN runtime: empty response body.");
        }

        const contentLength = Number.parseInt(response.headers.get("content-length") ?? "", 10);
        const hasLength = Number.isFinite(contentLength) && contentLength > 0;
        let received = 0;
        const source = webStreamToNodeReadable(response.body);
        let stallTimer: ReturnType<typeof setTimeout> | undefined;
        const resetStallTimer = () => {
            clearTimeout(stallTimer);
            stallTimer = setTimeout(() => {
                abortController.abort();
                source.destroy(new Error("Failed to download Real-ESRGAN runtime: stalled."));
            }, DOWNLOAD_STALL_TIMEOUT_MS);
        };

        source.on("data", (chunk: Buffer) => {
            resetStallTimer();
            received += chunk.byteLength;
            onPercent(hasLength ? Math.min(100, (received / contentLength) * 100) : null);
        });
        resetStallTimer();

        await pipeline(source, createWriteStream(zipPath)).finally(() => {
            clearTimeout(stallTimer);
        });
        onPercent(100);
    }

    private async resolveExtractedLayout(extractDir: string) {
        const binaryPath = await findFileByName(extractDir, REALESRGAN_BINARY_NAME);
        if (!binaryPath) {
            throw new Error("Extracted Real-ESRGAN archive is missing the executable.");
        }

        const modelsPath = await findDirectoryByName(path.dirname(binaryPath), "models");
        if (!modelsPath) {
            throw new Error("Extracted Real-ESRGAN archive is missing the models directory.");
        }

        return {
            binaryPath,
            modelsPath,
            rootDir: path.dirname(binaryPath),
        };
    }

    private async promoteExtractedLayout(
        layout: { binaryPath: string; modelsPath: string; rootDir: string },
        targetDir: string,
    ) {
        const finalBinaryPath = path.join(targetDir, REALESRGAN_BINARY_NAME);
        const finalModelsPath = path.join(targetDir, "models");
        const sidecarNames = await fse.readdir(layout.rootDir);

        await fse.remove(finalBinaryPath);
        await fse.move(layout.binaryPath, finalBinaryPath, { overwrite: true });
        await fse.remove(finalModelsPath);
        await fse.move(layout.modelsPath, finalModelsPath, { overwrite: true });

        await Promise.all(
            sidecarNames
                .filter((name) => name.toLowerCase().endsWith(".dll"))
                .map(async (name) => {
                    const sourcePath = path.join(layout.rootDir, name);
                    if (!(await fse.pathExists(sourcePath))) {
                        return;
                    }

                    await fse.move(sourcePath, path.join(targetDir, name), { overwrite: true });
                }),
        );
    }

    private async resolveBinaryPath() {
        const stored = await this.getSettingValue(BINARY_PATH_KEY);
        if (stored && (await fse.pathExists(stored))) {
            return stored;
        }

        return path.join(this.getToolDir(), REALESRGAN_BINARY_NAME);
    }

    private async resolveModelsPath(binaryPath: string) {
        const stored = await this.getSettingValue(MODELS_PATH_KEY);
        if (stored && (await fse.pathExists(stored))) {
            return stored;
        }

        return path.join(path.dirname(binaryPath), "models");
    }

    private async isInstalled(binaryPath: string, modelsPath: string) {
        return await isRealesrganRuntimeInstalled(binaryPath, modelsPath);
    }

    private async getSettingValue(key: string) {
        return await this.desktop.lib.db.settings.getValue(key);
    }

    private async saveSettingValue(key: string, value: string) {
        await this.desktop.lib.db.settings.upsert(key, value);
    }
}

async function findFileByName(root: string, fileName: string): Promise<string | null> {
    const entries = await fse.readdir(root, { withFileTypes: true });
    const files = entries.filter(
        (entry) => entry.isFile() && entry.name.toLowerCase() === fileName.toLowerCase(),
    );
    if (files[0]) {
        return path.join(root, files[0].name);
    }

    const nested = await Promise.all(
        entries
            .filter((entry) => entry.isDirectory())
            .map((entry) => findFileByName(path.join(root, entry.name), fileName)),
    );
    return nested.find((value): value is string => value != null) ?? null;
}

async function findDirectoryByName(root: string, directoryName: string): Promise<string | null> {
    const entries = await fse.readdir(root, { withFileTypes: true });
    const match = entries.find(
        (entry) => entry.isDirectory() && entry.name.toLowerCase() === directoryName.toLowerCase(),
    );
    return match ? path.join(root, match.name) : null;
}
