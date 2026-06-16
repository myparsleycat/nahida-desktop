import path from "node:path";
import { trim } from "es-toolkit";
import type { NahidaDesktop } from "../..";

export const DISABLED_PREFIX_REGEX = /^disabled\s+/i;

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

export function stripDisabledPrefix(folderName: string): string {
    return trim(folderName.replace(DISABLED_PREFIX_REGEX, ""));
}

export function restoreDisabledPrefix(sourceFolderName: string, folderName: string): string {
    if (DISABLED_PREFIX_REGEX.test(sourceFolderName)) {
        return `DISABLED ${folderName}`;
    }
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

export async function manualSubGroupPathExists(
    modFolderPath: string,
    storedRelativePath: string,
    pathExists: (targetPath: string) => Promise<boolean>,
    readDirectory: (targetPath: string) => Promise<string[]>,
    statPath: (targetPath: string) => Promise<{ isDirectory: () => boolean } | null>,
) {
    const segments = storedRelativePath.split("/").filter(Boolean);

    const walk = async (currentPath: string, segmentIndex: number): Promise<boolean> => {
        if (segmentIndex >= segments.length) return true;
        if (!(await pathExists(currentPath))) return false;

        const storedSegment = segments[segmentIndex];
        const entries = await readDirectory(currentPath);
        const matchingPaths = (
            await Promise.all(
                entries.map(async (entry) => {
                    const entryPath = path.join(currentPath, entry);
                    const stat = await statPath(entryPath);
                    if (!stat?.isDirectory()) return null;
                    if (!manualSubGroupSegmentMatches(entry, storedSegment)) return null;
                    return entryPath;
                }),
            )
        ).filter((entryPath): entryPath is string => entryPath !== null);

        if (matchingPaths.length === 0) return false;

        return (
            await Promise.all(matchingPaths.map((entryPath) => walk(entryPath, segmentIndex + 1)))
        ).some(Boolean);
    };

    return walk(modFolderPath, 0);
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
