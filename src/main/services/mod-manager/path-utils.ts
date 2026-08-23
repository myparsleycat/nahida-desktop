import path from "node:path";

import { DISABLED_PREFIX_REGEX, stripDisabledPrefix } from "@shared/mod";

import type { NahidaDesktop } from "../..";

export { DISABLED_PREFIX_REGEX, stripDisabledPrefix } from "@shared/mod";

export const DISABLED_FILE_SUFFIX = ".disabled";

export function isDisabledFile(fileName: string) {
    return fileName.toLowerCase().endsWith(DISABLED_FILE_SUFFIX);
}

export function stripDisabledFileSuffix(fileName: string) {
    if (!isDisabledFile(fileName)) return fileName;
    return fileName.slice(0, -DISABLED_FILE_SUFFIX.length);
}

export function isDisabledFolderName(folderName: string) {
    const trimmed = folderName.trim();
    return stripDisabledPrefix(trimmed) !== trimmed;
}

export function normalizeModPath(modPath: string): string {
    return path.normalize(modPath).toLowerCase();
}

export function isSameOrChildPath(parentPath: string, targetPath: string): boolean {
    const relativePath = path.relative(
        normalizeModPath(path.resolve(parentPath)),
        normalizeModPath(path.resolve(targetPath)),
    );

    return (
        relativePath === "" || (!relativePath.startsWith("..") && !path.isAbsolute(relativePath))
    );
}

export function restoreDisabledPrefix(sourceFolderName: string, folderName: string): string {
    const match = sourceFolderName.match(DISABLED_PREFIX_REGEX);
    if (match) return `${match[0]}${folderName}`;
    return folderName;
}

export function normalizeRelativePath(targetPath: string): string {
    return targetPath
        .split(/[\\/]+/)
        .filter(Boolean)
        .map((segment) => stripDisabledPrefix(segment).toLowerCase())
        .join("/");
}

export function manualSubGroupRelativePath(targetPath: string): string {
    return targetPath
        .split(/[\\/]+/)
        .filter(Boolean)
        .map((segment) => segment.toLowerCase())
        .join("/");
}

export function manualSubGroupSegmentMatches(entryName: string, storedSegment: string) {
    const lowerEntryName = entryName.toLowerCase();
    if (lowerEntryName === storedSegment) return true;

    return stripDisabledPrefix(entryName).toLowerCase() === storedSegment;
}

type ManualSubGroupFsAccessors = {
    pathExists: (targetPath: string) => Promise<boolean>;
    readDirectory: (targetPath: string) => Promise<string[]>;
    statPath: (
        targetPath: string,
    ) => Promise<{ isDirectory: () => boolean; isFile: () => boolean } | null>;
};

async function walkManualSubGroupDiskPaths(
    currentPath: string,
    storedRelativePath: string,
    fs: ManualSubGroupFsAccessors,
    segmentIndex: number,
): Promise<string[]> {
    const segments = storedRelativePath.split("/").filter(Boolean);
    if (segmentIndex >= segments.length) return [currentPath];
    if (!(await fs.pathExists(currentPath))) return [];

    const storedSegment = segments[segmentIndex];
    const entries = await fs.readDirectory(currentPath);
    return (
        await Promise.all(
            entries.map(async (entry) => {
                const entryPath = path.join(currentPath, entry);
                const stat = await fs.statPath(entryPath);
                if (!stat?.isDirectory()) return [];
                if (!manualSubGroupSegmentMatches(entry, storedSegment)) return [];
                return walkManualSubGroupDiskPaths(
                    entryPath,
                    storedRelativePath,
                    fs,
                    segmentIndex + 1,
                );
            }),
        )
    ).flat();
}

export async function resolveManualSubGroupDiskPaths(
    modFolderPath: string,
    storedRelativePath: string,
    fs: ManualSubGroupFsAccessors,
) {
    return walkManualSubGroupDiskPaths(modFolderPath, storedRelativePath, fs, 0);
}

export async function folderHasAnyFile(dirPath: string, fs: ManualSubGroupFsAccessors) {
    if (!(await fs.pathExists(dirPath))) return false;

    const entries = await fs.readDirectory(dirPath);
    for (const entry of entries) {
        const entryPath = path.join(dirPath, entry);
        const stat = await fs.statPath(entryPath);
        if (!stat) continue;
        if (stat.isFile()) return true;
        if (stat.isDirectory() && (await folderHasAnyFile(entryPath, fs))) return true;
    }

    return false;
}

export async function manualSubGroupPathExists(
    modFolderPath: string,
    storedRelativePath: string,
    pathExists: (targetPath: string) => Promise<boolean>,
    readDirectory: (targetPath: string) => Promise<string[]>,
    statPath: (targetPath: string) => Promise<{ isDirectory: () => boolean } | null>,
) {
    const fs: ManualSubGroupFsAccessors = {
        pathExists,
        readDirectory,
        statPath: async (targetPath) => {
            const stat = await statPath(targetPath);
            if (!stat) return null;
            return {
                isDirectory: () => stat.isDirectory(),
                isFile: () => false,
            };
        },
    };

    return (await resolveManualSubGroupDiskPaths(modFolderPath, storedRelativePath, fs)).length > 0;
}

export function toGameRelativePath(rootPath: string, targetPath: string): string {
    return normalizeRelativePath(path.relative(rootPath, targetPath));
}

export async function renameWithUniqueName(
    fsLib: NahidaDesktop["lib"]["fs"],
    modPath: string,
    baseFolderName: string,
): Promise<string> {
    const parentPath = path.dirname(modPath);
    const existingFolderNames = await fsLib.listDirectories(parentPath);
    const newFolderName = fsLib.getUniqueName(baseFolderName, existingFolderNames);
    const newPath = path.join(parentPath, newFolderName);

    await fsLib.rename(modPath, newPath);
    return newPath;
}
