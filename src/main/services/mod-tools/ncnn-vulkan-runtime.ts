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

export interface NcnnVulkanRuntimeConfig {
    dirName: string;
    binaryName: string;
    version: string;
    downloadUrl: string;
    archiveSha256: string;
    settingKeyPrefix: string;
    displayName: string;
    modelDirNames: readonly string[];
    isInstalled: (binaryPath: string, modelsPath: string) => Promise<boolean>;
    resolveModelsPath: (runtimeRoot: string) => string;
}

const DOWNLOAD_STALL_TIMEOUT_MS = 30_000;

export class NcnnVulkanRuntime {
    private installPromise: Promise<TextureUpscaleRuntimeStatus> | null = null;

    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly config: NcnnVulkanRuntimeConfig,
    ) {}

    public getToolDir() {
        return path.join(app.getPath("userData"), "tools", this.config.dirName);
    }

    public async getStatus(): Promise<TextureUpscaleRuntimeStatus> {
        const binaryPath = await this.resolveBinaryPath();
        const modelsPath = await this.resolveModelsPath(binaryPath);
        const installed = await this.isInstalled(binaryPath, modelsPath);

        return {
            installed,
            version: installed
                ? ((await this.getSettingValue(
                      `${this.config.settingKeyPrefix}:installed-version`,
                  )) ?? this.config.version)
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
        const zipPath = path.join(targetDir, `${this.config.dirName}.zip.download`);
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
            if (digest.digest("hex") !== this.config.archiveSha256) {
                throw new Error(`Downloaded ${this.config.displayName} archive checksum mismatch.`);
            }
            onProgress?.("extract", null);
            await this.desktop.service.archive.extract(zipPath, extractDir);
            const layout = await this.resolveExtractedLayout(extractDir);
            await this.promoteExtractedLayout(layout, targetDir);
            await this.saveSettingValue(
                `${this.config.settingKeyPrefix}:installed-version`,
                this.config.version,
            );
            await this.saveSettingValue(
                `${this.config.settingKeyPrefix}:binary-path`,
                path.join(targetDir, this.config.binaryName),
            );
            await this.saveSettingValue(
                `${this.config.settingKeyPrefix}:models-path`,
                this.config.resolveModelsPath(targetDir),
            );
        } catch (error) {
            await fse.remove(zipPath).catch(() => {});
            await fse.remove(extractDir).catch(() => {});
            throw error;
        }

        await fse.remove(zipPath).catch(() => {});
        await fse.remove(extractDir).catch(() => {});

        const status = await this.getStatus();
        if (!status.installed) {
            throw new Error(`${this.config.displayName} runtime installation is incomplete.`);
        }

        return status;
    }

    private async downloadArchive(zipPath: string, onPercent: (percent: number | null) => void) {
        const abortController = new AbortController();
        const response = await ky.get(this.config.downloadUrl, {
            timeout: 10 * 60 * 1000,
            signal: abortController.signal,
            throwHttpErrors: false,
            headers: {
                "User-Agent": "Nahida Desktop",
            },
        });

        if (!response.ok) {
            await drainWebStream(response.body).catch(() => {});
            throw new Error(
                `Failed to download ${this.config.displayName} runtime: HTTP ${response.status}`,
            );
        }
        if (!response.body) {
            throw new Error(
                `Failed to download ${this.config.displayName} runtime: empty response body.`,
            );
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
                source.destroy(
                    new Error(`Failed to download ${this.config.displayName} runtime: stalled.`),
                );
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
        const binaryPath = await findFileByName(extractDir, this.config.binaryName);
        if (!binaryPath) {
            throw new Error(
                `Extracted ${this.config.displayName} archive is missing the executable.`,
            );
        }

        const layoutRoot = path.dirname(binaryPath);
        for (const dirName of this.config.modelDirNames) {
            const modelDir = await findDirectoryByName(layoutRoot, dirName);
            if (!modelDir) {
                throw new Error(
                    `Extracted ${this.config.displayName} archive is missing the ${dirName} directory.`,
                );
            }
        }

        return {
            binaryPath,
            layoutRoot,
        };
    }

    private async promoteExtractedLayout(
        layout: { binaryPath: string; layoutRoot: string },
        targetDir: string,
    ) {
        const finalBinaryPath = path.join(targetDir, this.config.binaryName);
        const sidecarNames = await fse.readdir(layout.layoutRoot);

        await fse.remove(finalBinaryPath);
        await fse.move(layout.binaryPath, finalBinaryPath, { overwrite: true });
        for (const dirName of this.config.modelDirNames) {
            const sourcePath = path.join(layout.layoutRoot, dirName);
            const finalPath = path.join(targetDir, dirName);
            await fse.remove(finalPath);
            await fse.move(sourcePath, finalPath, { overwrite: true });
        }

        await Promise.all(
            sidecarNames
                .filter((name) => name.toLowerCase().endsWith(".dll"))
                .map(async (name) => {
                    const sourcePath = path.join(layout.layoutRoot, name);
                    if (!(await fse.pathExists(sourcePath))) {
                        return;
                    }

                    await fse.move(sourcePath, path.join(targetDir, name), { overwrite: true });
                }),
        );
    }

    private async resolveBinaryPath() {
        const stored = await this.getSettingValue(`${this.config.settingKeyPrefix}:binary-path`);
        if (stored && (await fse.pathExists(stored))) {
            return stored;
        }

        return path.join(this.getToolDir(), this.config.binaryName);
    }

    private async resolveModelsPath(binaryPath: string) {
        const stored = await this.getSettingValue(`${this.config.settingKeyPrefix}:models-path`);
        if (stored && (await fse.pathExists(stored))) {
            return stored;
        }

        return this.config.resolveModelsPath(path.dirname(binaryPath));
    }

    private async isInstalled(binaryPath: string, modelsPath: string) {
        return await this.config.isInstalled(binaryPath, modelsPath);
    }

    private async getSettingValue(key: string) {
        return await this.desktop.lib.db.settings.getValue(key);
    }

    private async saveSettingValue(key: string, value: string) {
        await this.desktop.lib.db.settings.upsert(key, value);
    }
}

export async function findFileByName(root: string, fileName: string): Promise<string | null> {
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

export async function findDirectoryByName(
    root: string,
    directoryName: string,
): Promise<string | null> {
    const entries = await fse.readdir(root, { withFileTypes: true });
    const match = entries.find(
        (entry) => entry.isDirectory() && entry.name.toLowerCase() === directoryName.toLowerCase(),
    );
    return match ? path.join(root, match.name) : null;
}
