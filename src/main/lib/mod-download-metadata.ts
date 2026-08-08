import { execFile } from "child_process";
import crypto from "node:crypto";
import path from "node:path";
import { promisify } from "util";

import { toErrorMessage } from "@shared/utils";
import fse from "fs-extra";

const execFileAsync = promisify(execFile);

export const MOD_DOWNLOAD_METADATA_FILE_NAME = "nhd.json";

type DownloadSource = "mod" | "gamebanana";

interface ModDownloadMetadataBase {
    id: string;
    source: DownloadSource;
    downloadedAt: string;
}

export interface GameBananaModDownloadMetadata extends ModDownloadMetadataBase {
    source: "gamebanana";
    mod: {
        id: number;
        pageUrl: string;
        version: string | null;
    };
    author: {
        name: string | null;
        url: string | null;
    };
    file: {
        downloadUrl: string;
        md5: string | null;
    };
}

export interface DirectModDownloadMetadata extends ModDownloadMetadataBase {
    source: "mod";
}

export type ModDownloadMetadata = DirectModDownloadMetadata | GameBananaModDownloadMetadata;

export type ModDownloadMetadataInput =
    | Omit<DirectModDownloadMetadata, "id">
    | Omit<GameBananaModDownloadMetadata, "id">;

export async function readGameBananaModId(dirPath: string) {
    const metadata: unknown = await fse
        .readJson(path.join(dirPath, MOD_DOWNLOAD_METADATA_FILE_NAME))
        .catch(() => null);
    if (
        !metadata ||
        typeof metadata !== "object" ||
        !("source" in metadata) ||
        metadata.source !== "gamebanana" ||
        !("mod" in metadata) ||
        !metadata.mod ||
        typeof metadata.mod !== "object" ||
        !("id" in metadata.mod) ||
        typeof metadata.mod.id !== "number" ||
        !Number.isSafeInteger(metadata.mod.id) ||
        metadata.mod.id <= 0
    ) {
        return undefined;
    }

    return metadata.mod.id;
}

export async function writeModDownloadMetadata(
    dirPath: string,
    metadata: ModDownloadMetadataInput,
) {
    await fse.ensureDir(dirPath);
    const data = { id: crypto.randomUUID(), ...metadata } as ModDownloadMetadata;
    const metadataPath = path.join(dirPath, MOD_DOWNLOAD_METADATA_FILE_NAME);
    let backupPath: string | null = null;
    if (await fse.pathExists(metadataPath)) {
        backupPath = `${metadataPath}.backup-${crypto.randomUUID()}`;
        await fse.move(metadataPath, backupPath, { overwrite: true });
    }
    try {
        await fse.writeJson(metadataPath, data, { spaces: 2 });
        await hideFile(metadataPath);
    } catch (error) {
        let restoreError: unknown;
        try {
            if (await fse.pathExists(metadataPath)) {
                await fse.remove(metadataPath);
            }
            if (backupPath && (await fse.pathExists(backupPath))) {
                await fse.move(backupPath, metadataPath, { overwrite: true });
            }
        } catch (e) {
            restoreError = e;
        }
        if (restoreError) {
            (error as Error & { cleanupError?: string }).cleanupError =
                toErrorMessage(restoreError);
            (error as Error & { cleanupErrors?: unknown[] }).cleanupErrors = [restoreError];
        }
        throw error;
    }
    if (backupPath) {
        try {
            await fse.remove(backupPath);
        } catch (cleanupError) {
            const err =
                cleanupError instanceof Error
                    ? cleanupError
                    : new Error(toErrorMessage(cleanupError));
            (err as Error & { cleanupError?: string }).cleanupError = err.message;
            throw err;
        }
    }
}

export async function writeModDownloadMetadataToDirectories(
    paths: string[],
    metadata: ModDownloadMetadataInput,
) {
    const directories = new Set<string>();

    for (const targetPath of paths) {
        const stat = await fse.stat(targetPath);
        directories.add(stat.isDirectory() ? targetPath : path.dirname(targetPath));
    }

    const dirArray = Array.from(directories);
    const backups = new Map<string, string | null>();

    try {
        for (const directoryPath of dirArray) {
            const metadataPath = path.join(directoryPath, MOD_DOWNLOAD_METADATA_FILE_NAME);
            if (await fse.pathExists(metadataPath)) {
                const backupPath = `${metadataPath}.backup-${crypto.randomUUID()}`;
                await fse.move(metadataPath, backupPath, { overwrite: true });
                backups.set(directoryPath, backupPath);
            } else {
                backups.set(directoryPath, null);
            }
        }
    } catch (error) {
        const cleanupErrors: unknown[] = [];
        for (const [directoryPath, backupPath] of backups) {
            if (!backupPath) continue;
            const metadataPath = path.join(directoryPath, MOD_DOWNLOAD_METADATA_FILE_NAME);
            try {
                if (await fse.pathExists(metadataPath)) {
                    await fse.remove(metadataPath);
                }
                if (await fse.pathExists(backupPath)) {
                    await fse.move(backupPath, metadataPath, { overwrite: true });
                }
            } catch (e) {
                cleanupErrors.push(e);
            }
        }
        if (cleanupErrors.length > 0) {
            (error as Error & { cleanupError?: string }).cleanupError = cleanupErrors
                .map((e) => toErrorMessage(e))
                .join("; ");
            (error as Error & { cleanupErrors?: unknown[] }).cleanupErrors = cleanupErrors;
        }
        throw error;
    }

    const results = await Promise.allSettled(
        dirArray.map(async (directoryPath) => {
            const metadataPath = path.join(directoryPath, MOD_DOWNLOAD_METADATA_FILE_NAME);
            const data = { id: crypto.randomUUID(), ...metadata } as ModDownloadMetadata;
            await fse.ensureDir(directoryPath);
            await fse.writeJson(metadataPath, data, { spaces: 2 });
            await hideFile(metadataPath);
            return directoryPath;
        }),
    );

    const fulfilled: string[] = [];
    const rejected: PromiseRejectedResult[] = [];
    for (const result of results) {
        if (result.status === "fulfilled") fulfilled.push(result.value);
        else rejected.push(result as PromiseRejectedResult);
    }

    if (rejected.length > 0) {
        const firstError = rejected[0].reason as Error;
        (firstError as Error & { writtenDirectories?: string[] }).writtenDirectories = [
            ...fulfilled,
        ];

        const cleanupErrors: unknown[] = [];

        for (const directoryPath of fulfilled) {
            const metadataPath = path.join(directoryPath, MOD_DOWNLOAD_METADATA_FILE_NAME);
            const backupPath = backups.get(directoryPath) ?? null;
            try {
                if (await fse.pathExists(metadataPath)) {
                    await fse.remove(metadataPath);
                }
                if (backupPath && (await fse.pathExists(backupPath))) {
                    await fse.move(backupPath, metadataPath, { overwrite: true });
                }
            } catch (e) {
                cleanupErrors.push(e);
            }
        }

        for (let i = 0; i < dirArray.length; i++) {
            const result = results[i];
            if (result.status === "rejected") {
                const directoryPath = dirArray[i];
                const metadataPath = path.join(directoryPath, MOD_DOWNLOAD_METADATA_FILE_NAME);
                const backupPath = backups.get(directoryPath) ?? null;
                try {
                    if (await fse.pathExists(metadataPath)) {
                        await fse.remove(metadataPath);
                    }
                    if (backupPath && (await fse.pathExists(backupPath))) {
                        await fse.move(backupPath, metadataPath, { overwrite: true });
                    }
                } catch (e) {
                    cleanupErrors.push(e);
                }
            }
        }

        if (cleanupErrors.length > 0) {
            (firstError as Error & { cleanupError?: string }).cleanupError = cleanupErrors
                .map((e) => toErrorMessage(e))
                .join("; ");
            (firstError as Error & { cleanupErrors?: unknown[] }).cleanupErrors = cleanupErrors;
        }

        throw firstError;
    }

    const backupCleanupErrors: unknown[] = [];
    for (const directoryPath of dirArray) {
        const backupPath = backups.get(directoryPath);
        if (backupPath) {
            try {
                await fse.remove(backupPath);
            } catch (e) {
                backupCleanupErrors.push(e);
            }
        }
    }
    if (backupCleanupErrors.length > 0) {
        const err =
            backupCleanupErrors[0] instanceof Error
                ? (backupCleanupErrors[0] as Error)
                : new Error(toErrorMessage(backupCleanupErrors[0]));
        (err as Error & { cleanupError?: string }).cleanupError = backupCleanupErrors
            .map((e) => toErrorMessage(e))
            .join("; ");
        (err as Error & { cleanupErrors?: unknown[] }).cleanupErrors = backupCleanupErrors;
        throw err;
    }
}

async function hideFile(filePath: string) {
    await execFileAsync("attrib", ["+h", filePath], { windowsHide: true });
}
