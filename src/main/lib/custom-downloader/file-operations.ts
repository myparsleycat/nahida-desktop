import path from "node:path";

import { toErrorMessage } from "@shared/utils";
import fse from "fs-extra";
import { nanoid } from "nanoid";

import { getArchiveRootName } from "./utils";

export async function moveWithOverwrite(sourcePath: string, destinationPath: string) {
    if (await fse.pathExists(destinationPath)) {
        await fse.remove(destinationPath);
    }

    await fse.move(sourcePath, destinationPath, { overwrite: true });
}

export async function finalizeStagedDownload(stagingPath: string, destinationDir: string) {
    await fse.ensureDir(destinationDir);

    const stagedEntries = await fse.readdir(stagingPath);
    if (stagedEntries.length === 0) {
        throw new Error("Downloaded file did not produce staged content.");
    }

    const destinationPaths: string[] = [];
    const backups: Array<{ destinationPath: string; backupPath: string | null }> = [];

    try {
        for (const entry of stagedEntries) {
            const sourcePath = path.join(stagingPath, entry);
            const destinationPath = path.join(destinationDir, entry);
            let backupPath: string | null = null;
            if (await fse.pathExists(destinationPath)) {
                backupPath = `${destinationPath}.nhd-backup-${nanoid()}`;
                await fse.move(destinationPath, backupPath, { overwrite: true });
            }
            backups.push({ destinationPath, backupPath });
            await fse.move(sourcePath, destinationPath, { overwrite: true });
            destinationPaths.push(destinationPath);
        }
    } catch (error) {
        (error as Error & { partialDestinationPaths?: string[] }).partialDestinationPaths = [
            ...destinationPaths,
        ];
        const restoreErrors: unknown[] = [];
        for (const { destinationPath, backupPath } of backups) {
            const wasMoved = destinationPaths.includes(destinationPath);
            const isFailedEntry = !wasMoved && backupPath !== null;
            try {
                if (wasMoved || isFailedEntry) {
                    if (await fse.pathExists(destinationPath)) {
                        await fse.remove(destinationPath);
                    }
                }
                if (backupPath && (await fse.pathExists(backupPath))) {
                    await fse.move(backupPath, destinationPath, { overwrite: true });
                }
            } catch (restoreError) {
                restoreErrors.push(restoreError);
            }
        }
        if (restoreErrors.length > 0) {
            (error as Error & { cleanupError?: string }).cleanupError = restoreErrors
                .map((e) => toErrorMessage(e))
                .join("; ");
            (error as Error & { cleanupErrors?: unknown[] }).cleanupErrors = restoreErrors;
        }
        (error as Error & { restoreCompleted?: boolean }).restoreCompleted = true;
        throw error;
    }

    for (const { backupPath } of backups) {
        if (backupPath) {
            try {
                await fse.remove(backupPath);
            } catch {}
        }
    }

    return destinationPaths;
}

export async function applySelectedExtractedName(props: {
    extractedPath: string;
    stagingPath: string;
    requestedFileName: string;
    originalSuggestedFileName: string;
    sanitizeWindowsFilename: (name: string) => string;
}) {
    const {
        extractedPath,
        stagingPath,
        requestedFileName,
        originalSuggestedFileName,
        sanitizeWindowsFilename,
    } = props;

    if (requestedFileName === originalSuggestedFileName || extractedPath === stagingPath) {
        return extractedPath;
    }

    const stats = await fse.stat(extractedPath);
    const desiredName = stats.isDirectory()
        ? getArchiveRootName(requestedFileName, sanitizeWindowsFilename)
        : requestedFileName;

    if (!desiredName || path.basename(extractedPath) === desiredName) {
        return extractedPath;
    }

    const renamedPath = path.join(path.dirname(extractedPath), desiredName);
    await moveWithOverwrite(extractedPath, renamedPath);
    return renamedPath;
}
