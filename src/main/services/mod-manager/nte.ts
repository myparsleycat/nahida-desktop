import path from "node:path";
import type { FolderGroup, GameConfig, ModInfo } from "@shared/types";
import fg from "fast-glob";
import fse from "fs-extra";
import type { NahidaDesktop } from "../..";
import {
    DISABLED_PREFIX_REGEX,
    isSameOrChildPath,
    normalizeModPath,
    renameWithUniqueName,
    stripDisabledPrefix,
} from "./path-utils";

const NTE_EXE_NAME = "HTGame.exe";
const NTE_COMMON_EXE_RELATIVE_PATH = path.join(
    "Neverness To Everness",
    "Client",
    "WindowsNoEditor",
    "HT",
    "Binaries",
    "Win64",
    NTE_EXE_NAME,
);
const NTE_MODS_RELATIVE_PATH = path.join("Content", "Paks", "Mods");
const NTE_DEFAULT_MOD_SUBFOLDERS = ["Character", "UI", "Enemy", "NPC"] as const;
const DISABLED_FILE_SUFFIX = ".disabled";
const PREVIEW_FILE_REGEX = /^preview\.(jpeg|jpg|gif|png|webp|bmp|mp4|webm|ogg)$/i;

export interface NtePathResolution {
    gameRootPath: string;
    executablePath: string;
    modFolderPath: string;
    linkedModFolderPath: string;
}

type NteGameRoots = {
    modRoot: string;
};

export async function resolveNteInstallPath(inputPath: string): Promise<NtePathResolution | null> {
    const trimmedPath = inputPath.trim();
    if (!trimmedPath || !(await fse.pathExists(trimmedPath))) return null;

    const executablePath = await findNteExecutable(trimmedPath);
    if (!executablePath) return null;

    const htRootPath = path.resolve(path.dirname(executablePath), "..", "..");
    const modFolderPath = path.join(htRootPath, NTE_MODS_RELATIVE_PATH);

    return {
        gameRootPath: path.resolve(htRootPath, "..", ".."),
        executablePath,
        modFolderPath,
        linkedModFolderPath: modFolderPath,
    };
}

export function deriveNteGameInstallPath(modOrLinkedPath: string) {
    const htRootPath = path.resolve(modOrLinkedPath, "..", "..", "..");
    return path.resolve(htRootPath, "..", "..");
}

export function getNteRoots(game: Pick<GameConfig, "modFolderPath">) {
    return { modRoot: game.modFolderPath };
}

export async function ensureNteModFolders(modFolderPath: string) {
    await fse.ensureDir(modFolderPath);
    await Promise.all(
        NTE_DEFAULT_MOD_SUBFOLDERS.map((name) => fse.ensureDir(path.join(modFolderPath, name))),
    );
}

export async function configureNteModFolder(
    modFolderPath: string,
    linkedModFolderPath: string | null,
) {
    if (!linkedModFolderPath || isSameNtePath(modFolderPath, linkedModFolderPath)) {
        await unlinkNteModsFolder(modFolderPath);
        await ensureNteModFolders(modFolderPath);
        return;
    }

    if (
        isSameOrChildPath(linkedModFolderPath, modFolderPath) ||
        isSameOrChildPath(modFolderPath, linkedModFolderPath)
    ) {
        throw new Error("NTE_CUSTOM_MOD_FOLDER_INSIDE_LINK_PATH");
    }

    await fse.ensureDir(modFolderPath);
    await linkNteModsFolder(modFolderPath, linkedModFolderPath);
    await ensureNteModFolders(modFolderPath);
}

export function findNteGameByPath(games: GameConfig[], targetPath: string) {
    return games.find(
        (game) =>
            game.importer === "NTE" && isSameOrChildPath(getNteRoots(game).modRoot, targetPath),
    );
}

export async function getNteCharacters(
    desktop: NahidaDesktop,
    roots: NteGameRoots,
): Promise<FolderGroup[]> {
    return await getNteSubGroups(desktop, roots, roots.modRoot);
}

export async function getNteSubGroups(
    _desktop: NahidaDesktop,
    roots: NteGameRoots,
    groupPath: string,
): Promise<FolderGroup[]> {
    const relativePath = getNteRelativePath(roots, groupPath);
    const groupDir = path.join(roots.modRoot, relativePath);
    const groupNames = await listDirectoryNames(groupDir);

    const groups = await Promise.all(
        groupNames.map(async (groupName) => {
            const nextPath = path.join(groupDir, groupName);

            if (await hasDirectPak(nextPath)) return null;

            const preview = await findPreview(nextPath);
            return {
                name: groupName,
                path: nextPath,
                mods: [],
                ...(preview ? { preview } : {}),
                modCount: await countDirectMods(nextPath),
                hasManualSubGroups: false,
            } satisfies FolderGroup;
        }),
    );

    return groups.filter((group) => group !== null).sort((a, b) => a.name.localeCompare(b.name));
}

export async function getNteMods(
    desktop: NahidaDesktop,
    roots: NteGameRoots,
    groupPath: string,
): Promise<FolderGroup> {
    const relativePath = getNteRelativePath(roots, groupPath);
    const groupDir = path.join(roots.modRoot, relativePath);
    const modNames = await listDirectoryNames(groupDir);
    const mods = (
        await Promise.all(
            modNames.map(async (modName) => {
                const modPath = path.join(groupDir, modName);
                if (!(await hasDirectPak(modPath))) return null;

                return await createNteModInfo(desktop, modPath, await isNteModEnabled(modPath));
            }),
        )
    )
        .filter((mod) => mod !== null)
        .sort((a, b) => a.name.localeCompare(b.name));

    const preview = await findPreview(groupDir);
    return {
        name: path.basename(groupPath),
        path: groupDir,
        mods,
        ...(preview ? { preview } : {}),
        modCount: mods.length,
    };
}

export async function cleanupNteModFolder(
    modFolderPath: string,
    linkedModFolderPath: string | null,
) {
    if (linkedModFolderPath && !isSameNtePath(linkedModFolderPath, modFolderPath)) {
        await unlinkNteModsFolder(linkedModFolderPath);
        return;
    }

    await unlinkNteModsFolder(modFolderPath);
}

export async function isNteModEnabled(modPath: string) {
    if (DISABLED_PREFIX_REGEX.test(path.basename(modPath))) return false;

    const pakFiles = (await fse.readdir(modPath, { withFileTypes: true }))
        .filter((entry) => entry.isFile())
        .filter((entry) => isPakModFile(entry.name));

    if (pakFiles.length === 0) return true;

    return pakFiles.some((entry) => !isDisabledFile(entry.name));
}

export async function setNteModEnabled(
    desktop: NahidaDesktop,
    modPath: string,
    enabled: boolean,
): Promise<string> {
    const folderName = path.basename(modPath);
    if ((await isNteModEnabled(modPath)) === enabled) return modPath;

    const entries = (await fse.readdir(modPath, { withFileTypes: true })).filter((entry) =>
        entry.isFile(),
    );

    if (enabled) {
        await Promise.all(
            entries
                .filter((entry) => isDisabledFile(entry.name))
                .map((entry) =>
                    fse.rename(
                        path.join(modPath, entry.name),
                        path.join(modPath, entry.name.slice(0, -DISABLED_FILE_SUFFIX.length)),
                    ),
                ),
        );
        return await renameWithUniqueName(desktop.lib.fs, modPath, stripDisabledPrefix(folderName));
    }

    await Promise.all(
        entries
            .filter((entry) => !isDisabledFile(entry.name))
            .map((entry) =>
                fse.rename(
                    path.join(modPath, entry.name),
                    path.join(modPath, `${entry.name}${DISABLED_FILE_SUFFIX}`),
                ),
            ),
    );
    return await renameWithUniqueName(
        desktop.lib.fs,
        modPath,
        `DISABLED ${stripDisabledPrefix(folderName)}`,
    );
}

export async function hasNteDirectPak(dirPath: string) {
    return await hasDirectPak(dirPath);
}

export function getNteGroupRelativePath(roots: NteGameRoots, groupPath: string) {
    return getNteRelativePath(roots, groupPath);
}

async function findNteExecutable(inputPath: string) {
    const commonPath = path.join(inputPath, NTE_COMMON_EXE_RELATIVE_PATH);
    if (await fse.pathExists(commonPath)) return commonPath;

    if (path.basename(inputPath).toLowerCase() === NTE_EXE_NAME.toLowerCase()) {
        return inputPath;
    }

    const matches = await fg([`**/${NTE_EXE_NAME}`], {
        cwd: inputPath,
        absolute: true,
        caseSensitiveMatch: false,
        dot: false,
        onlyFiles: true,
        suppressErrors: true,
    });

    return matches.sort((a, b) => a.length - b.length)[0] ?? null;
}

async function linkNteModsFolder(targetPath: string, linkPath: string) {
    await fse.ensureDir(path.dirname(linkPath));

    if (await fse.pathExists(linkPath)) {
        const stat = await fse.lstat(linkPath);
        if (stat.isSymbolicLink()) {
            if (isSameNtePath(await resolveLinkTarget(linkPath), targetPath)) return;
            await fse.remove(linkPath);
        } else {
            await moveExistingNteModsFolder(linkPath, targetPath);
        }
    }

    await fse.symlink(targetPath, linkPath, "junction");
}

async function unlinkNteModsFolder(modFolderPath: string) {
    if (!(await fse.pathExists(modFolderPath))) return;

    const stat = await fse.lstat(modFolderPath);
    if (!stat.isSymbolicLink()) return;

    const targetPath = await resolveLinkTarget(modFolderPath);
    const shouldMoveTargetEntries =
        (await fse.pathExists(targetPath)) && (await directoryHasAnyFile(targetPath));

    await fse.remove(modFolderPath);
    await fse.ensureDir(modFolderPath);

    if (!shouldMoveTargetEntries) return;

    await Promise.all(
        (await fse.readdir(targetPath)).map((entry) =>
            fse.move(path.join(targetPath, entry), path.join(modFolderPath, entry), {
                overwrite: false,
            }),
        ),
    );
}

async function moveExistingNteModsFolder(sourcePath: string, targetPath: string) {
    if (!(await fse.stat(sourcePath)).isDirectory()) {
        throw new Error("NTE_MODS_LINK_PATH_OCCUPIED");
    }

    if (!(await directoryHasAnyFile(sourcePath))) {
        await fse.remove(sourcePath);
        return;
    }

    if (await directoryHasAnyFile(targetPath)) {
        throw new Error("NTE_MODS_LINK_CONFLICT");
    }

    await Promise.all(
        (await fse.readdir(targetPath)).map((entry) => fse.remove(path.join(targetPath, entry))),
    );

    await Promise.all(
        (await fse.readdir(sourcePath)).map((entry) =>
            fse.move(path.join(sourcePath, entry), path.join(targetPath, entry), {
                overwrite: false,
            }),
        ),
    );
    await fse.remove(sourcePath);
}

async function resolveLinkTarget(linkPath: string) {
    const target = await fse.readlink(linkPath);
    return path.isAbsolute(target) ? target : path.resolve(path.dirname(linkPath), target);
}

async function directoryHasAnyFile(dirPath: string): Promise<boolean> {
    if (!(await fse.pathExists(dirPath))) return false;

    const entries = await fse.readdir(dirPath, { withFileTypes: true });
    return (
        await Promise.all(
            entries.map(async (entry) => {
                if (entry.isFile()) return true;
                if (!entry.isDirectory()) return true;
                return await directoryHasAnyFile(path.join(dirPath, entry.name));
            }),
        )
    ).some(Boolean);
}

function isSameNtePath(left: string, right: string) {
    return normalizeModPath(path.resolve(left)) === normalizeModPath(path.resolve(right));
}

function getNteRelativePath(roots: NteGameRoots, targetPath: string) {
    const relativePath = path.relative(roots.modRoot, targetPath);

    return relativePath.startsWith("..") ? "" : relativePath;
}

async function listDirectoryNames(dirPath: string) {
    if (!(await fse.pathExists(dirPath))) return [];

    return (await fse.readdir(dirPath, { withFileTypes: true }))
        .filter((entry) => entry.isDirectory())
        .map((entry) => entry.name)
        .sort((a, b) => a.localeCompare(b));
}

function isDisabledFile(fileName: string) {
    return fileName.toLowerCase().endsWith(DISABLED_FILE_SUFFIX);
}

function isPakModFile(fileName: string) {
    const baseName = isDisabledFile(fileName)
        ? fileName.slice(0, -DISABLED_FILE_SUFFIX.length)
        : fileName;

    return baseName.toLowerCase().endsWith(".pak");
}

async function hasDirectPak(dirPath: string) {
    if (!(await fse.pathExists(dirPath))) return false;

    return (await fse.readdir(dirPath, { withFileTypes: true })).some(
        (entry) => entry.isFile() && isPakModFile(entry.name),
    );
}

async function countDirectMods(groupDir: string) {
    return (
        await Promise.all(
            (
                await listDirectoryNames(groupDir)
            ).map(
                async (name): Promise<number> =>
                    (await hasDirectPak(path.join(groupDir, name))) ? 1 : 0,
            ),
        )
    ).reduce((total, count) => total + count, 0);
}

async function findPreview(dirPath: string) {
    if (!(await fse.pathExists(dirPath))) return undefined;

    const preview = (await fse.readdir(dirPath)).find((entry) =>
        PREVIEW_FILE_REGEX.test(
            isDisabledFile(entry) ? entry.slice(0, -DISABLED_FILE_SUFFIX.length) : entry,
        ),
    );
    return preview ? path.join(dirPath, preview) : undefined;
}

async function createNteModInfo(desktop: NahidaDesktop, modPath: string, isEnabled: boolean) {
    const stat = await fse.stat(modPath);
    const preview = await findPreview(modPath);

    return {
        id: modPath,
        name: path.basename(modPath),
        path: modPath,
        isEnabled,
        ...(preview ? { preview } : {}),
        mtime: stat.mtimeMs,
        size: await desktop.lib.fs.getFolderSize(modPath),
        inis: [],
    } satisfies ModInfo;
}
