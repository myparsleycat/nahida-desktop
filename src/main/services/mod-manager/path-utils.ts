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

export function toGameRelativePath(rootPath: string, targetPath: string): string {
    return normalizeRelativePath(path.relative(rootPath, targetPath));
}

export async function renameWithUniqueName(
    fsLib: NahidaDesktop["lib"]["fs"],
    modPath: string,
    baseFolderName: string,
): Promise<string> {
    const parentPath = path.dirname(modPath);
    const existingNames = await fsLib.readdir(parentPath);
    const newFolderName = fsLib.getUniqueName(baseFolderName, existingNames);
    const newPath = path.join(parentPath, newFolderName);

    await fsLib.rename(modPath, newPath);
    return newPath;
}
