import { exec } from "node:child_process";
import crypto from "node:crypto";
import { createReadStream } from "node:fs";
import os from "node:os";
import path from "node:path";
import { pipeline } from "node:stream/promises";
import { promisify } from "node:util";

import { diversifyDllPadding } from "@native/pe-padding-diversifier";
import { toErrorMessage } from "@shared/utils";
import fse from "fs-extra";
import ky from "ky";
import ms from "ms";
import { nanoid } from "nanoid";

import type { NahidaDesktop } from "@/main";

import {
    copyFilesElevated,
    isFsPermissionError,
    removeFilesElevated,
} from "@/main/lib/elevated-fs";

const execAsync = promisify(exec);

type ExecBuildError = Error & {
    code?: number | string;
    signal?: NodeJS.Signals;
    stdout?: string | Buffer;
    stderr?: string | Buffer;
};

type BuildD3DResult = {
    success: boolean;
    errorMessage?: string;
    backupPath?: string;
};

type DiversificationState = {
    hasBackup: boolean;
    backupPath: string | null;
};

type GitHubRelease = {
    tag_name?: unknown;
};

const TARGET_DLL_NAME = "d3d11.dll";
const NON_RELEASE_VERSION_NAMES = new Set(["main", "master"]);
const D3D_BUILD_STATE_KEY_PREFIX = "mod_tools:d3d_build:";
const D3D_BUILD_TEMP_DIR_NAME = "nahida-tools-d3d-build";
const D3D_BUILD_ID_PATTERN = /^[A-Za-z0-9_-]+$/;
const DIVERSIFY_TEMP_SUFFIX = ".nahida-diversified.tmp";
const DIVERSIFIER_BACKUP_PREFIX = `${TARGET_DLL_NAME}.pepd-backup-`;
const DIVERSIFIER_BACKUP_HASH_PREFIX_LENGTH = 7;
const BACKUP_HASH_PATTERN = /^d3d11\.dll\.pepd-backup-([a-f0-9]{7})-\d+\.bak$/;

type FourThousandOneFixerTask = "build-dll" | "diversify-dll" | "restore-dll";

type D3DBuildState = {
    id: string;
    tempDir: string;
};

export class FourThousandOneFixer {
    private readonly VS_EDITIONS = ["Community", "Professional", "Enterprise", "Insiders"];
    private readonly VS_VERSIONS = ["2025", "2022", "18", "17"];
    private readonly RELEASES_FETCH_COOLDOWN_MS = ms("1m");

    private activeTask: FourThousandOneFixerTask | null = null;
    private currentProgress = "";
    private currentErrorMessage = "";

    private releasesCache: Partial<Record<string, string[]>> = {};
    private readonly releasesFetchedAt: Partial<Record<string, number>> = {};
    private readonly releasesFetchInFlight: Partial<Record<string, Promise<boolean>>> = {};

    constructor(private readonly desktop: NahidaDesktop) {
        this.desktop.service.startupCleanup.register({
            name: "mod-tools:d3d-build",
            run: () => this.cleanupStaleBuildDirs(),
        });
        void this.updateReleases();
    }

    public getState() {
        return {
            isBuilding: this.activeTask !== null,
            activeTask: this.activeTask,
            progress: this.currentProgress,
            errorMessage: this.currentErrorMessage,
        };
    }

    public async updateReleases() {
        await this.fetchProviderReleases("SpectrumQT");
    }

    private async fetchProviderReleases(provider: string) {
        const inFlight = this.releasesFetchInFlight[provider];
        if (inFlight) {
            return inFlight;
        }

        const now = Date.now();
        const lastFetchedAt = this.releasesFetchedAt[provider] ?? 0;
        if (now - lastFetchedAt < this.RELEASES_FETCH_COOLDOWN_MS) {
            return true;
        }

        const fetchPromise = this.fetchProviderReleasesInternal(provider);
        this.releasesFetchInFlight[provider] = fetchPromise;

        try {
            const success = await fetchPromise;
            if (success) {
                this.releasesFetchedAt[provider] = Date.now();
            }
            return success;
        } finally {
            delete this.releasesFetchInFlight[provider];
        }
    }

    private async fetchProviderReleasesInternal(provider: string) {
        try {
            const url = `https://api.github.com/repos/${provider}/XXMI-Libs-Package/releases`;
            const resp = await ky.get(url, {
                headers: {
                    "User-Agent":
                        "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
                },
                throwHttpErrors: false,
            });

            if (!resp.ok) {
                this.desktop.logger.warn(
                    `Failed to fetch releases for ${provider}: ${resp.status} ${resp.statusText}`,
                    "4001Fixer:fetchProviderReleases",
                );
                return false;
            }

            const releases = (await resp.json()) as GitHubRelease[];
            this.releasesCache[provider] = releases
                .map((release) => release.tag_name)
                .filter(
                    (tagName): tagName is string =>
                        typeof tagName === "string" &&
                        !NON_RELEASE_VERSION_NAMES.has(tagName.toLowerCase()),
                );
            return true;
        } catch (error) {
            this.desktop.logger.error(error, "4001Fixer:fetchProviderReleases");
            return false;
        }
    }

    public async getProviderReleases(provider: string) {
        if (!this.releasesCache[provider]) {
            await this.fetchProviderReleases(provider);
        }

        return this.releasesCache[provider] ?? [];
    }

    public async getDiversificationState({
        importerPath,
    }: {
        importerPath?: string;
    }): Promise<DiversificationState> {
        if (!importerPath || !(await fse.pathExists(importerPath))) {
            return { hasBackup: false, backupPath: null };
        }

        const backupPath = await this.findDiversifierBackup(importerPath);
        return {
            hasBackup: !!backupPath,
            backupPath,
        };
    }

    public async checkImporterWriteAccess({ importerPath }: { importerPath?: string }) {
        if (!importerPath || !(await fse.pathExists(importerPath))) {
            return {
                writable: false,
                locked: false,
                requiresElevation: false,
                processes: [] as { name: string; pid: number }[],
            };
        }

        const destinationCheck = await this.desktop.lib.fs.isPathWritable(
            path.join(importerPath, TARGET_DLL_NAME),
            {
                detailed: true,
                parentPath: importerPath,
            },
        );

        return {
            writable: destinationCheck.writable,
            locked: destinationCheck.locked,
            requiresElevation: !destinationCheck.writable && !destinationCheck.locked,
            processes: destinationCheck.processes,
        };
    }

    private updateProgress(code: string, errorMessage = "") {
        this.currentProgress = code;
        this.currentErrorMessage = errorMessage;
        this.desktop.ipc.broadcast("tools:4001FixerProgress", {
            task: this.activeTask,
            code,
            errorMessage,
        });
    }

    public async buildD3D11Dll({
        provider,
        version,
        importerKey,
        importerPath,
    }: {
        provider: string;
        version: string;
        importerKey: string;
        importerPath?: string;
    }): Promise<BuildD3DResult> {
        if (this.activeTask) {
            return { success: false };
        }

        this.activeTask = "build-dll";
        this.updateProgress("XXMI_INIT");
        if (!importerPath || !(await fse.pathExists(importerPath))) {
            this.updateProgress("XXMI_ERR_GIMI_NOT_FOUND");
            this.activeTask = null;
            return { success: false };
        }

        const finalDestination = path.join(importerPath, TARGET_DLL_NAME);
        const destinationCheck = await this.desktop.lib.fs.isPathWritable(finalDestination, {
            detailed: true,
            parentPath: importerPath,
        });
        if (destinationCheck.locked) {
            const errorMessage = this.desktop.lib.fs.formatProcessList(destinationCheck.processes);
            this.updateProgress("XXMI_ERR_DLL_IN_USE", errorMessage);
            this.activeTask = null;
            return { success: false, errorMessage };
        }
        const useElevated = !destinationCheck.writable;

        this.updateProgress("XXMI_FIND_VS");
        const vcvarsPath = await this.findVsDevCmd();
        if (!vcvarsPath) {
            this.updateProgress("XXMI_ERR_VS_NOT_FOUND");
            this.activeTask = null;
            return { success: false };
        }

        const buildId = nanoid();
        const tempDir = this.getBuildTempDir(buildId);

        try {
            await this.trackBuildTempDir(buildId, tempDir);
            await fse.ensureDir(tempDir);

            const projectPath = await this.prepareSourceCode(tempDir, provider, version);

            this.updateProgress("XXMI_BUILDING");
            this.desktop.logger.info("Building D3D11 DLL...", "4001Fixer:buildD3D11Dll");

            const buildSuccess = await this.executeMsBuild(vcvarsPath, projectPath);
            if (!buildSuccess) {
                this.activeTask = null;
                return { success: false };
            }

            const builtDllPath = path.join(projectPath, "x64", "Release", TARGET_DLL_NAME);
            if (!(await fse.pathExists(builtDllPath))) {
                this.updateProgress("XXMI_ERR_DLL_NOT_FOUND");
                this.activeTask = null;
                return { success: false };
            }

            const installResult = await this.installFilesWithElevation(
                [{ sourcePath: builtDllPath, targetPath: finalDestination }],
                finalDestination,
                useElevated,
            );
            if (!installResult.success) {
                this.updateProgress(installResult.errorCode, installResult.errorMessage);
                this.activeTask = null;
                return { success: false, errorMessage: installResult.errorMessage };
            }

            const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
            if (xxmiPath) {
                await this.enableUnsafeMode(xxmiPath, importerKey);
            }

            await this.removeDiversifierBackups(importerPath, useElevated);

            this.updateProgress("XXMI_BUILD_SUCCESS");
            this.desktop.logger.info(
                `Successfully built and installed d3d11.dll to ${finalDestination}`,
                "4001Fixer:buildD3D11Dll",
            );

            this.activeTask = null;
            return { success: true };
        } catch (error) {
            this.desktop.logger.error(error, "4001Fixer:buildD3D11Dll");
            const elevated = this.mapElevatedError(error);
            if (elevated) {
                this.updateProgress(elevated.code, elevated.message);
                this.activeTask = null;
                return { success: false, errorMessage: elevated.message };
            }
            const errorMessage = this.extractBuildErrorMessage(error);
            this.updateProgress("XXMI_ERR_BUILD_FAILED", errorMessage);
            this.activeTask = null;
            return { success: false, errorMessage };
        } finally {
            await fse.remove(tempDir).catch(() => {});
            await this.untrackBuildTempDir(buildId).catch((error) => {
                this.desktop.logger.error(error, "4001Fixer:untrackBuildTempDir");
            });
        }
    }

    public async diversifyD3D11DllPadding({
        importerKey,
        importerPath,
    }: {
        importerKey: string;
        importerPath?: string;
    }): Promise<BuildD3DResult> {
        if (this.activeTask) {
            return { success: false };
        }

        this.activeTask = "diversify-dll";
        this.updateProgress("XXMI_OBFUSCATE_INIT");

        if (!importerPath || !(await fse.pathExists(importerPath))) {
            this.updateProgress("XXMI_ERR_GIMI_NOT_FOUND");
            this.activeTask = null;
            return { success: false };
        }

        const backupPath = await this.findDiversifierBackup(importerPath);
        if (backupPath) {
            this.updateProgress("XXMI_OBFUSCATE_BACKUP_EXISTS");
            this.activeTask = null;
            return { success: false, backupPath };
        }

        const targetDllPath = path.join(importerPath, TARGET_DLL_NAME);
        if (!(await fse.pathExists(targetDllPath))) {
            this.updateProgress("XXMI_ERR_DLL_NOT_FOUND");
            this.activeTask = null;
            return { success: false };
        }

        const destinationCheck = await this.desktop.lib.fs.isPathWritable(targetDllPath, {
            detailed: true,
            parentPath: importerPath,
        });
        if (destinationCheck.locked) {
            const errorMessage = this.desktop.lib.fs.formatProcessList(destinationCheck.processes);
            this.updateProgress("XXMI_ERR_DLL_IN_USE", errorMessage);
            this.activeTask = null;
            return { success: false, errorMessage };
        }
        const useElevated = !destinationCheck.writable;

        const tempPath = path.join(
            os.tmpdir(),
            D3D_BUILD_TEMP_DIR_NAME,
            `${TARGET_DLL_NAME}.${nanoid()}${DIVERSIFY_TEMP_SUFFIX}`,
        );

        try {
            await fse.ensureDir(path.dirname(tempPath));
            this.updateProgress("XXMI_OBFUSCATING");
            const result = await diversifyDllPadding(targetDllPath, tempPath);

            if (result.candidates === 0) {
                this.updateProgress("XXMI_ERR_OBFUSCATE_NO_CANDIDATES");
                this.activeTask = null;
                return { success: false };
            }
            if (result.patchedCandidates === 0) {
                const errorMessage = `Found ${result.candidates} JMP-rel8 candidate(s), but none were safe to patch.`;
                this.updateProgress("XXMI_ERR_OBFUSCATE_NO_CANDIDATES", errorMessage);
                this.activeTask = null;
                return { success: false, errorMessage };
            }
            if (result.mutations === 0 || result.hashBefore === result.hashAfter) {
                this.updateProgress("XXMI_OBFUSCATE_ALREADY_APPLIED");
                this.activeTask = null;
                return { success: false };
            }

            const diversifiedHash = await this.hashDllFile(tempPath);
            const hashPrefix = diversifiedHash.slice(0, DIVERSIFIER_BACKUP_HASH_PREFIX_LENGTH);
            const newBackupPath = this.getBackupPath(importerPath, hashPrefix);

            const installResult = await this.installFilesWithElevation(
                [
                    { sourcePath: targetDllPath, targetPath: newBackupPath },
                    { sourcePath: tempPath, targetPath: targetDllPath },
                ],
                targetDllPath,
                useElevated,
            );
            if (!installResult.success) {
                await this.removePathsBestEffort([newBackupPath], useElevated);
                this.updateProgress(installResult.errorCode, installResult.errorMessage);
                this.activeTask = null;
                return { success: false, errorMessage: installResult.errorMessage };
            }

            const xxmiPath = await this.desktop.service.xxmi.getXXMIPath();
            if (xxmiPath) {
                await this.enableUnsafeMode(xxmiPath, importerKey);
            }

            this.updateProgress("XXMI_OBFUSCATE_SUCCESS");
            this.desktop.logger.info(
                `Successfully diversified padding in ${targetDllPath}; backup=${newBackupPath}; candidates=${result.candidates}; mutations=${result.mutations}; hashBefore=${result.hashBefore}; hashAfter=${result.hashAfter}`,
                "4001Fixer:diversifyD3D11DllPadding",
            );

            this.activeTask = null;
            return { success: true, backupPath: newBackupPath };
        } catch (error) {
            this.desktop.logger.error(error, "4001Fixer:diversifyD3D11DllPadding");
            const elevated = this.mapElevatedError(error);
            if (elevated) {
                this.updateProgress(elevated.code, elevated.message);
                this.activeTask = null;
                return { success: false, errorMessage: elevated.message };
            }
            const errorMessage = toErrorMessage(error);
            this.updateProgress("XXMI_ERR_OBFUSCATE_FAILED", errorMessage);
            this.activeTask = null;
            return { success: false, errorMessage };
        } finally {
            await fse.remove(tempPath).catch(() => {});
        }
    }

    public async restoreDiversifiedD3D11Dll({
        importerPath,
    }: {
        importerPath?: string;
    }): Promise<BuildD3DResult> {
        if (this.activeTask) {
            return { success: false };
        }

        this.activeTask = "restore-dll";
        this.updateProgress("XXMI_RESTORE_INIT");

        if (!importerPath || !(await fse.pathExists(importerPath))) {
            this.updateProgress("XXMI_ERR_GIMI_NOT_FOUND");
            this.activeTask = null;
            return { success: false };
        }

        const backupPath = await this.findDiversifierBackup(importerPath);
        if (!backupPath) {
            this.updateProgress("XXMI_ERR_RESTORE_BACKUP_NOT_FOUND");
            this.activeTask = null;
            return { success: false };
        }

        const targetDllPath = path.join(importerPath, TARGET_DLL_NAME);
        const destinationCheck = await this.desktop.lib.fs.isPathWritable(targetDllPath, {
            detailed: true,
            parentPath: importerPath,
        });
        if (destinationCheck.locked) {
            const errorMessage = this.desktop.lib.fs.formatProcessList(destinationCheck.processes);
            this.updateProgress("XXMI_ERR_DLL_IN_USE", errorMessage);
            this.activeTask = null;
            return { success: false, errorMessage };
        }
        const useElevated = !destinationCheck.writable;

        try {
            this.updateProgress("XXMI_RESTORING");
            const installResult = await this.installFilesWithElevation(
                [{ sourcePath: backupPath, targetPath: targetDllPath }],
                targetDllPath,
                useElevated,
            );
            if (!installResult.success) {
                this.updateProgress(installResult.errorCode, installResult.errorMessage);
                this.activeTask = null;
                return { success: false, errorMessage: installResult.errorMessage };
            }
            await this.removeDiversifierBackups(importerPath, useElevated);

            this.updateProgress("XXMI_RESTORE_SUCCESS");
            this.desktop.logger.info(
                `Successfully restored ${targetDllPath} from ${backupPath}`,
                "4001Fixer:restoreDiversifiedD3D11Dll",
            );

            this.activeTask = null;
            return { success: true, backupPath };
        } catch (error) {
            this.desktop.logger.error(error, "4001Fixer:restoreDiversifiedD3D11Dll");
            const elevated = this.mapElevatedError(error);
            if (elevated) {
                this.updateProgress(elevated.code, elevated.message);
                this.activeTask = null;
                return { success: false, errorMessage: elevated.message };
            }
            const lockInfo = await this.desktop.lib.fs.isLockedPathError(error, targetDllPath);
            const errorMessage = lockInfo.isLocked
                ? this.desktop.lib.fs.formatProcessList(lockInfo.processes)
                : error instanceof Error
                  ? error.message
                  : String(error);
            this.updateProgress(
                lockInfo.isLocked ? "XXMI_ERR_DLL_IN_USE" : "XXMI_ERR_RESTORE_FAILED",
                errorMessage,
            );
            this.activeTask = null;
            return { success: false, errorMessage };
        }
    }

    private async installFilesWithElevation(
        fileCopies: readonly { sourcePath: string; targetPath: string }[],
        lockTargetPath: string,
        useElevated: boolean,
    ): Promise<{ success: true } | { success: false; errorCode: string; errorMessage: string }> {
        if (useElevated) {
            try {
                await copyFilesElevated(fileCopies, "XXMI_ERR_ELEVATED_COPY_FAILED");
                return { success: true };
            } catch (error) {
                const elevated = this.mapElevatedError(error);
                return {
                    success: false,
                    errorCode: elevated?.code ?? "XXMI_ERR_ELEVATION_FAILED",
                    errorMessage: elevated?.message ?? toErrorMessage(error),
                };
            }
        }

        try {
            for (const fileCopy of fileCopies) {
                await fse.copy(fileCopy.sourcePath, fileCopy.targetPath, { overwrite: true });
            }
            return { success: true };
        } catch (error) {
            const lockInfo = await this.desktop.lib.fs.isLockedPathError(error, lockTargetPath);
            if (lockInfo.isLocked) {
                return {
                    success: false,
                    errorCode: "XXMI_ERR_DLL_IN_USE",
                    errorMessage: this.desktop.lib.fs.formatProcessList(lockInfo.processes),
                };
            }
            if (!isFsPermissionError(error) && !lockInfo.isPermissionError) {
                throw error;
            }

            try {
                await copyFilesElevated(fileCopies, "XXMI_ERR_ELEVATED_COPY_FAILED");
                return { success: true };
            } catch (elevatedError) {
                const elevated = this.mapElevatedError(elevatedError);
                return {
                    success: false,
                    errorCode: elevated?.code ?? "XXMI_ERR_ELEVATION_FAILED",
                    errorMessage: elevated?.message ?? toErrorMessage(elevatedError),
                };
            }
        }
    }

    private mapElevatedError(error: unknown) {
        const message = toErrorMessage(error);
        if (
            message.startsWith("XXMI_ERR_ELEVATED_COPY_FAILED:") ||
            message.startsWith("XXMI_ERR_ELEVATED_REMOVE_FAILED:") ||
            message.startsWith("ELEVATED_COPY_FAILED:") ||
            message.startsWith("ELEVATED_REMOVE_FAILED:") ||
            message.startsWith("ELEVATED_PS_SPAWN_FAILED:")
        ) {
            return { code: "XXMI_ERR_ELEVATION_FAILED", message };
        }
        return null;
    }

    private getBackupPath(importerPath: string, hashPrefix: string) {
        return path.join(
            importerPath,
            `${DIVERSIFIER_BACKUP_PREFIX}${hashPrefix}-${Math.floor(Date.now() / 1000)}.bak`,
        );
    }

    private async hashDllFile(filePath: string): Promise<string> {
        return new Promise((resolve, reject) => {
            const hash = crypto.createHash("sha256");
            const stream = createReadStream(filePath);
            stream.on("data", (chunk) => hash.update(chunk));
            stream.on("end", () => resolve(hash.digest("hex")));
            stream.on("error", reject);
        });
    }

    private async findDiversifierBackup(importerPath: string): Promise<string | null> {
        const entries = await fse.readdir(importerPath).catch(() => []);
        const candidates = entries
            .filter((entry) => entry.startsWith(DIVERSIFIER_BACKUP_PREFIX))
            .filter((entry) => entry.endsWith(".bak"))
            .sort()
            .reverse()
            .map((entry) => path.join(importerPath, entry));

        if (candidates.length === 0) {
            return null;
        }

        const targetDllPath = path.join(importerPath, TARGET_DLL_NAME);
        const currentHash = await fse.pathExists(targetDllPath).then(async (exists) => {
            if (!exists) return null;
            try {
                return await this.hashDllFile(targetDllPath);
            } catch (error) {
                this.desktop.logger.warn(
                    `Failed to hash d3d11.dll for backup validation, accepting backup as-is: ${String(error)}`,
                    "4001Fixer:findDiversifierBackup",
                );
                return null;
            }
        });

        for (const candidate of candidates) {
            const match = path.basename(candidate).match(BACKUP_HASH_PATTERN);
            if (!match) {
                this.desktop.logger.warn(
                    `Removing invalid PE padding diversifier backup: ${candidate}`,
                    "4001Fixer:findDiversifierBackup",
                );
                await fse.remove(candidate).catch((error) => {
                    this.desktop.logger.error(error, "4001Fixer:findDiversifierBackup");
                });
                continue;
            }
            if (currentHash === null) {
                return candidate;
            }
            const backedUpHashPrefix = match[1];
            if (currentHash.startsWith(backedUpHashPrefix)) {
                return candidate;
            }
            this.desktop.logger.warn(
                `Removing stale PE padding diversifier backup (hash mismatch): ${candidate} (expected=${backedUpHashPrefix} actual=${currentHash.slice(0, DIVERSIFIER_BACKUP_HASH_PREFIX_LENGTH)})`,
                "4001Fixer:findDiversifierBackup",
            );
            await fse.remove(candidate).catch((error) => {
                this.desktop.logger.error(error, "4001Fixer:findDiversifierBackup");
            });
        }

        return null;
    }

    private async removeDiversifierBackups(importerPath: string, useElevated = false) {
        const entries = await fse.readdir(importerPath).catch(() => []);
        const backupPaths = entries
            .filter((entry) => entry.startsWith(DIVERSIFIER_BACKUP_PREFIX))
            .filter((entry) => entry.endsWith(".bak"))
            .map((entry) => path.join(importerPath, entry));
        await this.removePathsBestEffort(backupPaths, useElevated);
    }

    private async removePathsBestEffort(paths: readonly string[], useElevated: boolean) {
        if (paths.length === 0) return;

        try {
            if (useElevated) {
                await removeFilesElevated(paths, "XXMI_ERR_ELEVATED_REMOVE_FAILED");
                return;
            }
            await Promise.all(paths.map((filePath) => fse.remove(filePath)));
        } catch (error) {
            if (!useElevated && isFsPermissionError(error)) {
                try {
                    await removeFilesElevated(paths, "XXMI_ERR_ELEVATED_REMOVE_FAILED");
                    return;
                } catch (elevatedError) {
                    this.desktop.logger.warn(
                        elevatedError,
                        "4001Fixer:removePathsBestEffort:elevated",
                    );
                    return;
                }
            }
            this.desktop.logger.warn(error, "4001Fixer:removePathsBestEffort");
        }
    }

    private getBuildStateKey(buildId: string) {
        return `${D3D_BUILD_STATE_KEY_PREFIX}${buildId}`;
    }

    private getBuildTempRoot() {
        return path.join(os.tmpdir(), D3D_BUILD_TEMP_DIR_NAME);
    }

    private getBuildTempDir(buildId: string) {
        return path.join(this.getBuildTempRoot(), buildId);
    }

    private async trackBuildTempDir(buildId: string, tempDir: string) {
        const state: D3DBuildState = { id: buildId, tempDir };
        await this.desktop.lib.db.appState.upsert(
            this.getBuildStateKey(buildId),
            JSON.stringify(state),
            new Date().toISOString(),
        );
    }

    private async untrackBuildTempDir(buildId: string) {
        await this.desktop.lib.db.appState.delete(this.getBuildStateKey(buildId));
    }

    private async cleanupStaleBuildDirs() {
        const states = await this.desktop.lib.db.appState.listByPrefix(D3D_BUILD_STATE_KEY_PREFIX);

        for (const state of states) {
            const buildId = state.key.slice(D3D_BUILD_STATE_KEY_PREFIX.length);
            if (!D3D_BUILD_ID_PATTERN.test(buildId)) {
                this.desktop.logger.warn(
                    `Skipping invalid D3D build state key: ${state.key}`,
                    "4001Fixer:cleanupStaleBuildDirs",
                );
                await this.desktop.lib.db.appState.delete(state.key);
                continue;
            }

            await fse.remove(this.getBuildTempDir(buildId)).catch((error) => {
                this.desktop.logger.warn(
                    `Failed to remove stale D3D build temp dir for ${buildId}: ${error}`,
                    "4001Fixer:cleanupStaleBuildDirs",
                );
            });
            await this.desktop.lib.db.appState.delete(state.key);
        }
    }

    private async findVsDevCmd(): Promise<string | null> {
        const baseDir = "C:\\Program Files\\Microsoft Visual Studio";

        for (const version of this.VS_VERSIONS) {
            for (const edition of this.VS_EDITIONS) {
                const candidatePath = path.join(
                    baseDir,
                    version,
                    edition,
                    "VC",
                    "Auxiliary",
                    "Build",
                    "vcvars64.bat",
                );
                if (await fse.pathExists(candidatePath)) {
                    return candidatePath;
                }
            }
        }
        return null;
    }

    private async prepareSourceCode(
        workDir: string,
        provider: string,
        version: string,
    ): Promise<string> {
        this.updateProgress("XXMI_DOWNLOAD_REPO");
        this.desktop.logger.info("Downloading XXMI Repo...", "4001Fixer:prepareSourceCode");

        const zipPath = await this.downloadXXMIRepo(workDir, provider, version);

        this.updateProgress("XXMI_EXTRACT_REPO");
        this.desktop.logger.info("Extracting Repo...", "4001Fixer:prepareSourceCode");

        const extractDir = await this.desktop.service.archive.extract(zipPath, workDir);

        const entries = await fse.readdir(extractDir);
        const repoDirName = entries.find((e) => e.startsWith("XXMI-Libs-Package"));

        return repoDirName ? path.join(extractDir, repoDirName) : extractDir;
    }

    private async downloadXXMIRepo(
        targetDir: string,
        provider: string,
        version: string,
    ): Promise<string> {
        const selectedVersion = version?.trim();
        if (!selectedVersion) {
            throw new Error("No version selected");
        }

        const url = `https://github.com/${provider}/XXMI-Libs-Package/archive/refs/tags/${selectedVersion}.zip`;

        const zipPath = path.join(targetDir, "repo.zip");

        const resp = await ky.get(url, {
            headers: {
                "User-Agent":
                    "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36",
                Referer: `https://github.com/${provider}/XXMI-Libs-Package`,
            },
        });

        if (!resp.ok) {
            throw new Error(`Failed to download repo: ${resp.statusText}`);
        }

        await pipeline(resp.body as ReadableStream, fse.createWriteStream(zipPath));

        return zipPath;
    }

    private async executeMsBuild(vcvarsPath: string, projectPath: string): Promise<boolean> {
        const buildCommand = [
            `"${vcvarsPath}"`,
            `cd /d "${projectPath}"`,
            "msbuild StereovisionHacks.sln /nologo /verbosity:minimal /consoleloggerparameters:ErrorsOnly /p:Configuration=Release /p:Platform=x64",
        ].join(" && ");

        try {
            await execAsync(buildCommand, { maxBuffer: 1024 * 1024 * 20 });
            return true;
        } catch (e) {
            const error = e as ExecBuildError;
            const stderr = this.formatBuildOutput(error.stderr);
            const stdout = this.formatBuildOutput(error.stdout);
            const details = [
                `Build failed: ${error.message}`,
                `Command: ${buildCommand}`,
                `Project path: ${projectPath}`,
                `Exit code: ${error.code ?? "unknown"}`,
                `Signal: ${error.signal ?? "none"}`,
                stderr ? `stderr:\n${stderr}` : "stderr: <empty>",
                stdout ? `stdout:\n${stdout}` : "stdout: <empty>",
            ];

            throw new Error(details.join("\n"));
        }
    }

    private formatBuildOutput(output?: string | Buffer): string {
        if (!output) {
            return "";
        }

        const text = Buffer.isBuffer(output) ? output.toString("utf8") : output;
        const trimmed = text.trim();
        if (!trimmed) {
            return "";
        }

        const lines = trimmed.split(/\r?\n/);
        const maxLines = 120;
        const omitted = lines.length - maxLines;
        const tail = omitted > 0 ? lines.slice(-maxLines) : lines;

        return omitted > 0
            ? `[showing last ${maxLines} lines, omitted ${omitted} earlier lines]\n${tail.join("\n")}`
            : tail.join("\n");
    }

    private extractBuildErrorMessage(error: unknown): string {
        const lines = toErrorMessage(error)
            .split(/\r?\n/)
            .map((line) => line.trim())
            .filter(Boolean);
        const errorLines = lines.filter((line) => /\berror\s+[A-Z]+\d+:/i.test(line));

        if (errorLines.length > 0) {
            return errorLines.slice(0, 12).join("\n");
        }

        const outputIndex = lines.findIndex((line) => line === "stdout:" || line === "stderr:");
        const fallbackLines = outputIndex >= 0 ? lines.slice(outputIndex + 1) : lines;

        return fallbackLines.slice(-12).join("\n");
    }

    private async generateUnsafeModeSignature(xxmiPath: string) {
        const privateKeyPath = path.join(xxmiPath, "Resources", "Security", "private_key.der");
        const privateKeyBase64 = await fse.readFile(privateKeyPath, "utf8");
        const privateKeyBuffer = Buffer.from(privateKeyBase64, "base64");

        const privateKey = crypto.createPrivateKey({
            key: privateKeyBuffer,
            format: "der",
            type: "pkcs8",
        });

        const sign = crypto.createSign("SHA256");
        sign.update(os.userInfo().username);
        sign.end();

        const signature = sign.sign(privateKey);
        return signature.toString("base64");
    }

    private async enableUnsafeMode(xxmiPath: string, importerKey: string) {
        try {
            const configPath = path.join(xxmiPath, "XXMI Launcher Config.json");

            if (!(await fse.pathExists(configPath))) {
                this.desktop.logger.warn(
                    `Config file not found at ${configPath}`,
                    "4001Fixer:enableUnsafeMode",
                );
                return;
            }

            this.desktop.logger.info(
                `configPath found: ${configPath}`,
                "4001Fixer:enableUnsafeMode",
            );

            const config = await fse.readJson(configPath);

            const importerConfig = config?.Importers?.[importerKey]?.Migoto;
            if (importerConfig) {
                if (importerConfig.unsafe_mode === false) {
                    this.updateProgress("XXMI_ENABLE_UNSAFE_MODE");

                    importerConfig.unsafe_mode = true;
                    importerConfig.unsafe_mode_signature =
                        await this.generateUnsafeModeSignature(xxmiPath);

                    await fse.writeJson(configPath, config, { spaces: 4 });

                    this.desktop.logger.info(
                        `Enabled unsafe_mode for ${importerKey}`,
                        "4001Fixer:enableUnsafeMode",
                    );
                }
            }
        } catch (error) {
            this.desktop.logger.error(
                `Failed to update config for ${importerKey}: ${String(error)}`,
                "4001Fixer:enableUnsafeMode",
            );
        }
    }
}
