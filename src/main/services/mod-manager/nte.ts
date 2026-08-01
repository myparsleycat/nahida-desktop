import os from "node:os";
import path from "node:path";

import { disabledPrefixString } from "@shared/mod";
import type { FolderGroup, GameConfig, ModInfo, NteBootstrapProgress } from "@shared/types";
import { toErrorMessage } from "@shared/utils";
import fg from "fast-glob";
import fse from "fs-extra";
import { nanoid } from "nanoid";

import type { NahidaDesktop } from "../..";

import {
    copyFilesWithBackupElevated,
    isFsPermissionError,
    rollbackFilesElevated,
    runElevatedPowerShell,
    toPowerShellString,
} from "../../lib/elevated-fs";
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
const NTE_USER_MODS_FOLDER_NAME = "NTE-Mods";
const DISABLED_FILE_SUFFIX = ".disabled";
const PREVIEW_FILE_REGEX = /^preview\.(jpeg|jpg|gif|png|webp|bmp|mp4|webm|ogg)$/i;

export interface NtePathResolution {
    gameRootPath: string;
    executablePath: string;
    modFolderPath: string;
    linkedModFolderPath: string;
    requiresElevation: boolean;
}

type NteGameRoots = {
    modRoot: string;
    linkedRoot: string | null;
};

type BootstrapFileCopy = {
    sourcePath: string;
    targetPath: string;
};

type PreparedBootstrapFileCopy = BootstrapFileCopy & {
    backupPath: string;
    existed: boolean;
};

type BootstrapRollbackFile = {
    targetPath: string;
    backupPath: string;
    existed: boolean;
};

export async function resolveNteInstallPath(inputPath: string): Promise<NtePathResolution | null> {
    const trimmedPath = inputPath.trim();
    if (!trimmedPath || !(await fse.pathExists(trimmedPath))) return null;

    const executablePath = await findNteExecutable(trimmedPath);
    if (!executablePath) return null;

    const htRootPath = path.resolve(path.dirname(executablePath), "..", "..");
    const gameModFolderPath = path.join(htRootPath, NTE_MODS_RELATIVE_PATH);
    const existingLinkedModFolderPath = await getExistingNteModsLinkTarget(gameModFolderPath);
    const canWriteGameModFolderPath = existingLinkedModFolderPath
        ? true
        : await canWriteNteDirectoryPath(gameModFolderPath);
    const modFolderPath =
        existingLinkedModFolderPath ??
        (canWriteGameModFolderPath ? gameModFolderPath : getDefaultNteModFolderPath());

    return {
        gameRootPath: path.resolve(htRootPath, "..", ".."),
        executablePath,
        modFolderPath,
        linkedModFolderPath: gameModFolderPath,
        requiresElevation: !canWriteGameModFolderPath,
    };
}

export function getDefaultNteModFolderPath() {
    return path.join(
        process.env.USERPROFILE || os.homedir(),
        ".nahida-desktop",
        NTE_USER_MODS_FOLDER_NAME,
    );
}

export function deriveNteGameInstallPath(modOrLinkedPath: string) {
    const htRootPath = path.resolve(modOrLinkedPath, "..", "..", "..");
    return path.resolve(htRootPath, "..", "..");
}

export function getNteRoots(game: Pick<GameConfig, "modFolderPath" | "linkedModFolderPath">) {
    return { modRoot: game.modFolderPath, linkedRoot: game.linkedModFolderPath };
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
    return games.find((game) => {
        if (game.importer !== "NTE") return false;

        const roots = getNteRoots(game);
        if (isSameOrChildPath(roots.modRoot, targetPath)) return true;
        if (roots.linkedRoot && isSameOrChildPath(roots.linkedRoot, targetPath)) return true;

        return false;
    });
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
            const modCounts = await countDirectMods(nextPath);
            return {
                name: groupName,
                path: nextPath,
                mods: [],
                ...(preview ? { preview } : {}),
                modCount: modCounts.total,
                enabledModCount: modCounts.enabled,
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
        enabledModCount: mods.filter((mod) => mod.isEnabled).length,
    };
}

export function hasNtePathChanges(
    existing: Pick<GameConfig, "modFolderPath" | "linkedModFolderPath">,
    updates: Pick<GameConfig, "modFolderPath" | "linkedModFolderPath">,
) {
    if (!isSameNtePath(existing.modFolderPath, updates.modFolderPath)) return true;

    const existingLinked = existing.linkedModFolderPath;
    const updatedLinked = updates.linkedModFolderPath;
    if (existingLinked === updatedLinked) return false;
    if (!existingLinked || !updatedLinked) return true;

    return !isSameNtePath(existingLinked, updatedLinked);
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
                .filter((entry) => isDisabledFile(entry.name) && isPakModFile(entry.name))
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
            .filter((entry) => !isDisabledFile(entry.name) && isPakModFile(entry.name))
            .map((entry) =>
                fse.rename(
                    path.join(modPath, entry.name),
                    path.join(modPath, `${entry.name}${DISABLED_FILE_SUFFIX}`),
                ),
            ),
    );
    const style = await desktop.setting.mod.getDisabledPrefixStyle();
    return await renameWithUniqueName(
        desktop.lib.fs,
        modPath,
        `${disabledPrefixString(style)}${stripDisabledPrefix(folderName)}`,
    );
}

export async function hasNteDirectPak(dirPath: string) {
    return await hasDirectPak(dirPath);
}

export function getNteGroupRelativePath(roots: NteGameRoots, groupPath: string) {
    return getNteRelativePath(roots, groupPath);
}

const NTE_SIG_BYPASSER_URL =
    "https://github.com/rm-NoobInCoding/UniversalSigBypasser/releases/download/v1.2/SigBypasser_v1.2.zip";
const NTE_SIG_BYPASSER_FILES = ["dsound.dll", "UniversalSigBypasser.asi"] as const;
const NTE_ASI_LOADER_URLS = {
    x64: "https://github.com/ThirteenAG/Ultimate-ASI-Loader/releases/download/x64-latest/winhttp-x64.zip",
    ia32: "https://github.com/ThirteenAG/Ultimate-ASI-Loader/releases/download/Win32-latest/winhttp-Win32.zip",
} as const;
const NTE_ASI_LOADER_FILE = "winhttp.dll";

export async function ensureNteBootstrapFiles(
    desktop: NahidaDesktop,
    executablePath: string,
    onProgress: (payload: NteBootstrapProgress) => void,
): Promise<(() => Promise<void>) | undefined> {
    const targetDir = path.dirname(executablePath);
    if (!(await fse.pathExists(targetDir))) {
        throw new Error("NTE_BOOTSTRAP_INVALID_TARGET_DIR");
    }

    const arch = process.arch as keyof typeof NTE_ASI_LOADER_URLS;
    const asiLoaderUrl = NTE_ASI_LOADER_URLS[arch];
    if (!asiLoaderUrl) {
        onProgress({
            phase: "failed",
            progress: null,
            message: `Unsupported architecture: ${arch}`,
        });
        throw new Error("NTE_BOOTSTRAP_UNSUPPORTED_ARCH");
    }

    if (await areBootstrapFilesInstalled(targetDir)) {
        onProgress({ phase: "completed", progress: 100 });
        return undefined;
    }

    const useElevatedBootstrapCopy = !(await canWriteNteDirectoryPath(targetDir));

    const installTracker = createBootstrapInstallTracker();
    const tempDir = path.join(os.tmpdir(), `nte-bootstrap-${process.pid}-${nanoid(8)}`);
    await fse.ensureDir(tempDir);

    try {
        onProgress({
            phase: "fetching-release",
            progress: null,
            archiveName: "SigBypasser_v1.2.zip",
        });
        await downloadAndExtract(
            desktop,
            NTE_SIG_BYPASSER_URL,
            "SigBypasser_v1.2.zip",
            tempDir,
            onProgress,
        );
        const sigBypasserCopies = await resolveFileCopiesFromExtracted(
            tempDir,
            targetDir,
            NTE_SIG_BYPASSER_FILES,
        );

        const archLabel = arch === "x64" ? "winhttp-x64" : "winhttp-Win32";
        const loaderZipName = `${archLabel}.zip`;
        onProgress({ phase: "fetching-release", progress: 93, archiveName: loaderZipName });
        await downloadAndExtract(desktop, asiLoaderUrl, loaderZipName, tempDir, onProgress);
        onProgress({ phase: "installing", progress: 96 });
        await installTracker.copyFiles(
            [
                ...sigBypasserCopies,
                ...(await resolveFileCopiesFromExtracted(tempDir, targetDir, [
                    NTE_ASI_LOADER_FILE,
                ])),
                ...(await resolveSha512FileCopiesFromExtracted(tempDir, targetDir)),
            ],
            useElevatedBootstrapCopy,
        );

        onProgress({ phase: "completed", progress: 100 });
        return installTracker.hasWrittenFiles() ? installTracker.rollback : undefined;
    } catch (error) {
        if (installTracker.hasWrittenFiles()) {
            await installTracker.rollback();
        }
        onProgress({ phase: "failed", progress: null, message: toErrorMessage(error) });
        throw error;
    } finally {
        await fse.remove(tempDir).catch(() => {});
    }
}

function createBootstrapInstallTracker() {
    const rollbackDir = path.join(
        os.tmpdir(),
        `nte-bootstrap-rollback-${process.pid}-${nanoid(8)}`,
    );
    const snapshots = new Map<string, { existed: boolean; backupPath: string }>();
    const writtenFiles: string[] = [];

    const prepareCopy = async (fileCopy: BootstrapFileCopy, backupExisting: boolean) => {
        const snapshot = snapshots.get(fileCopy.targetPath);
        if (snapshot) return { ...fileCopy, ...snapshot };

        const backupPath = path.join(
            rollbackDir,
            `${nanoid(6)}-${path.basename(fileCopy.targetPath)}`,
        );
        const existed = await fse.pathExists(fileCopy.targetPath);
        snapshots.set(fileCopy.targetPath, { existed, backupPath });

        if (existed && backupExisting) {
            await fse.ensureDir(rollbackDir);
            await fse.copy(fileCopy.targetPath, backupPath);
        }

        return { ...fileCopy, existed, backupPath };
    };

    const markWritten = (filePath: string) => {
        if (!writtenFiles.includes(filePath)) {
            writtenFiles.push(filePath);
        }
    };

    const markAllWritten = (fileCopies: readonly BootstrapFileCopy[]) => {
        fileCopies.forEach((fileCopy) => markWritten(fileCopy.targetPath));
    };

    const copyFilesDirect = async (preparedCopies: readonly PreparedBootstrapFileCopy[]) => {
        for (const fileCopy of preparedCopies) {
            await fse.copy(fileCopy.sourcePath, fileCopy.targetPath, { overwrite: true });
            markWritten(fileCopy.targetPath);
        }
    };

    const copyFiles = async (fileCopies: readonly BootstrapFileCopy[], useElevated: boolean) => {
        if (useElevated) {
            const preparedCopies = await Promise.all(
                fileCopies.map((fileCopy) => prepareCopy(fileCopy, false)),
            );
            markAllWritten(fileCopies);
            await copyBootstrapFilesElevated(preparedCopies);
            return;
        }

        try {
            await copyFilesDirect(
                await Promise.all(fileCopies.map((fileCopy) => prepareCopy(fileCopy, true))),
            );
        } catch (error) {
            if (!isFsPermissionError(error)) throw error;
            const preparedCopies = await Promise.all(
                fileCopies.map((fileCopy) => prepareCopy(fileCopy, false)),
            );
            markAllWritten(fileCopies);
            await copyBootstrapFilesElevated(preparedCopies);
        }
    };

    const rollbackDirect = async () => {
        for (const filePath of writtenFiles.toReversed()) {
            const snapshot = snapshots.get(filePath);
            if (snapshot?.existed) {
                if (await fse.pathExists(snapshot.backupPath)) {
                    await fse.copy(snapshot.backupPath, filePath, { overwrite: true });
                }
                continue;
            }

            await fse.remove(filePath).catch(() => {});
        }
    };

    const rollback = async () => {
        try {
            await rollbackDirect();
        } catch (error) {
            if (!isFsPermissionError(error)) throw error;
            await rollbackBootstrapFilesElevated(
                writtenFiles.toReversed().flatMap((filePath) => {
                    const snapshot = snapshots.get(filePath);
                    return snapshot ? [{ targetPath: filePath, ...snapshot }] : [];
                }),
            );
        }
        await fse.remove(rollbackDir).catch(() => {});
    };

    return {
        copyFiles,
        rollback,
        hasWrittenFiles: () => writtenFiles.length > 0,
    };
}

async function areBootstrapFilesInstalled(targetDir: string): Promise<boolean> {
    const requiredFiles = [...NTE_SIG_BYPASSER_FILES, NTE_ASI_LOADER_FILE];
    const checks = await Promise.all(
        requiredFiles.map((file) => fse.pathExists(path.join(targetDir, file))),
    );
    return checks.every(Boolean);
}

async function downloadAndExtract(
    desktop: NahidaDesktop,
    url: string,
    archiveName: string,
    tempDir: string,
    onProgress: (payload: NteBootstrapProgress) => void,
): Promise<void> {
    const zipPath = path.join(tempDir, archiveName);
    const extractDir = path.join(tempDir, `extract-${nanoid(6)}`);
    await fse.ensureDir(extractDir);

    onProgress({ phase: "downloading", progress: null, archiveName });
    const response = await desktop.httpService.fetcher(url);
    if (!response.ok) {
        throw new Error(`NTE_BOOTSTRAP_DOWNLOAD_FAILED:${url} (HTTP ${response.status})`);
    }
    const buffer = Buffer.from(await response.arrayBuffer());
    await fse.writeFile(zipPath, buffer);

    onProgress({ phase: "extracting", progress: null, archiveName });
    try {
        await desktop.service.archive.extract(zipPath, extractDir);
    } catch (error) {
        throw new Error(`NTE_BOOTSTRAP_EXTRACT_FAILED:${archiveName}: ${toErrorMessage(error)}`);
    }
}

async function resolveFileCopiesFromExtracted(
    tempDir: string,
    targetDir: string,
    fileNames: readonly string[],
): Promise<BootstrapFileCopy[]> {
    const candidates = await collectFilesRecursively(tempDir);
    return fileNames.map((fileName) => {
        const match = candidates.find(
            (candidate) => path.basename(candidate).toLowerCase() === fileName.toLowerCase(),
        );
        if (!match) {
            throw new Error(`NTE_BOOTSTRAP_FILE_MISSING:${fileName}`);
        }
        return { sourcePath: match, targetPath: path.join(targetDir, fileName) };
    });
}

async function resolveSha512FileCopiesFromExtracted(
    tempDir: string,
    targetDir: string,
): Promise<BootstrapFileCopy[]> {
    const candidates = await collectFilesRecursively(tempDir);
    return candidates
        .filter((candidate) => /\.sha512$/i.test(path.basename(candidate)))
        .map((file) => ({
            sourcePath: file,
            targetPath: path.join(targetDir, path.basename(file)),
        }));
}

async function collectFilesRecursively(dirPath: string): Promise<string[]> {
    const results: string[] = [];
    const entries = await fse.readdir(dirPath, { withFileTypes: true });
    for (const entry of entries) {
        const fullPath = path.join(dirPath, entry.name);
        if (entry.isFile()) {
            results.push(fullPath);
        } else if (entry.isDirectory()) {
            results.push(...(await collectFilesRecursively(fullPath)));
        }
    }
    return results;
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
    try {
        await linkNteModsFolderDirect(targetPath, linkPath);
    } catch (error) {
        if (!isFsPermissionError(error)) throw error;
        await linkNteModsFolderElevated(targetPath, linkPath);
    }
}

async function linkNteModsFolderDirect(targetPath: string, linkPath: string) {
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
    try {
        await unlinkNteModsFolderDirect(modFolderPath);
    } catch (error) {
        if (!isFsPermissionError(error)) throw error;
        await unlinkNteModsFolderElevated(modFolderPath);
    }
}

async function unlinkNteModsFolderDirect(modFolderPath: string) {
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

async function linkNteModsFolderElevated(targetPath: string, linkPath: string) {
    const exitCode = await runElevatedPowerShell(
        `
$ErrorActionPreference = 'Stop'
$TargetPath = ${toPowerShellString(targetPath)}
$LinkPath = ${toPowerShellString(linkPath)}

function Normalize-PathValue([string]$PathValue) {
    return [System.IO.Path]::GetFullPath($PathValue).TrimEnd([char[]]@([System.IO.Path]::DirectorySeparatorChar, [System.IO.Path]::AltDirectorySeparatorChar))
}

function Same-Path([string]$Left, [string]$Right) {
    return (Normalize-PathValue $Left).Equals((Normalize-PathValue $Right), [System.StringComparison]::OrdinalIgnoreCase)
}

function Has-Any-FileSystemContent([string]$PathValue) {
    foreach ($Child in Get-ChildItem -LiteralPath $PathValue -Force -ErrorAction SilentlyContinue) {
        if (-not $Child.PSIsContainer) { return $true }
        if (($Child.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { return $true }
        if (Has-Any-FileSystemContent $Child.FullName) { return $true }
    }
    return $false
}

[System.IO.Directory]::CreateDirectory($TargetPath) | Out-Null
[System.IO.Directory]::CreateDirectory([System.IO.Path]::GetDirectoryName($LinkPath)) | Out-Null

if (Test-Path -LiteralPath $LinkPath) {
    $Item = Get-Item -LiteralPath $LinkPath -Force
    if (($Item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) {
        $ExistingTarget = if ($Item.Target -is [array]) { [string]$Item.Target[0] } else { [string]$Item.Target }
        if ((Same-Path $ExistingTarget $TargetPath)) { exit 0 }
        $RmdirCommand = 'rmdir "' + $LinkPath + '"'
        cmd.exe /d /c $RmdirCommand | Out-Null
        if ($LASTEXITCODE -ne 0) { exit 35 }
    } elseif (-not $Item.PSIsContainer) {
        exit 33
    } elseif (-not (Has-Any-FileSystemContent $LinkPath)) {
        Remove-Item -LiteralPath $LinkPath -Force
    } elseif (Has-Any-FileSystemContent $TargetPath) {
        exit 32
    } else {
        Get-ChildItem -LiteralPath $TargetPath -Force | Remove-Item -Recurse -Force
        Get-ChildItem -LiteralPath $LinkPath -Force | Move-Item -Destination $TargetPath -Force
        Remove-Item -LiteralPath $LinkPath -Force
    }
}

$MklinkCommand = 'mklink /J "' + $LinkPath + '" "' + $TargetPath + '"'
cmd.exe /d /c $MklinkCommand | Out-Null
if ($LASTEXITCODE -ne 0) { exit 34 }
`,
        "NTE_MODS_LINK_ELEVATION_FAILED",
    );
    if (exitCode === 0) return;
    if (exitCode === 32) throw new Error("NTE_MODS_LINK_CONFLICT");
    if (exitCode === 33) throw new Error("NTE_MODS_LINK_PATH_OCCUPIED");
    if (exitCode === 34) throw new Error("NTE_MODS_LINK_JUNCTION_FAILED");
    if (exitCode === 35) throw new Error("NTE_MODS_UNLINK_JUNCTION_FAILED");
    throw new Error(`NTE_MODS_LINK_ELEVATED_FAILED:${exitCode}`);
}

async function unlinkNteModsFolderElevated(modFolderPath: string) {
    const exitCode = await runElevatedPowerShell(
        `
$ErrorActionPreference = 'Stop'
$ModFolderPath = ${toPowerShellString(modFolderPath)}

function Has-Any-FileSystemContent([string]$PathValue) {
    foreach ($Child in Get-ChildItem -LiteralPath $PathValue -Force -ErrorAction SilentlyContinue) {
        if (-not $Child.PSIsContainer) { return $true }
        if (($Child.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -ne 0) { return $true }
        if (Has-Any-FileSystemContent $Child.FullName) { return $true }
    }
    return $false
}

if (-not (Test-Path -LiteralPath $ModFolderPath)) { exit 0 }

$Item = Get-Item -LiteralPath $ModFolderPath -Force
if (($Item.Attributes -band [System.IO.FileAttributes]::ReparsePoint) -eq 0) { exit 0 }

$TargetPath = if ($Item.Target -is [array]) { [string]$Item.Target[0] } else { [string]$Item.Target }
$ShouldMoveTargetEntries = (Test-Path -LiteralPath $TargetPath) -and (Has-Any-FileSystemContent $TargetPath)

$RmdirCommand = 'rmdir "' + $ModFolderPath + '"'
cmd.exe /d /c $RmdirCommand | Out-Null
if ($LASTEXITCODE -ne 0) { exit 35 }

[System.IO.Directory]::CreateDirectory($ModFolderPath) | Out-Null

if ($ShouldMoveTargetEntries) {
    Get-ChildItem -LiteralPath $TargetPath -Force | Move-Item -Destination $ModFolderPath -Force
}
`,
        "NTE_MODS_UNLINK_ELEVATION_FAILED",
    );
    if (exitCode === 0) return;
    if (exitCode === 35) throw new Error("NTE_MODS_UNLINK_JUNCTION_FAILED");
    throw new Error(`NTE_MODS_UNLINK_ELEVATED_FAILED:${exitCode}`);
}

async function copyBootstrapFilesElevated(fileCopies: readonly PreparedBootstrapFileCopy[]) {
    await copyFilesWithBackupElevated(fileCopies, "NTE_BOOTSTRAP_ELEVATED_COPY_FAILED");
}

async function rollbackBootstrapFilesElevated(fileCopies: readonly BootstrapRollbackFile[]) {
    await rollbackFilesElevated(fileCopies, "NTE_BOOTSTRAP_ELEVATED_ROLLBACK_FAILED");
}

async function getExistingNteModsLinkTarget(modFolderPath: string) {
    if (!(await fse.pathExists(modFolderPath))) return null;

    const stat = await fse.lstat(modFolderPath);
    return stat.isSymbolicLink() ? await resolveLinkTarget(modFolderPath) : null;
}

async function canWriteNteDirectoryPath(dirPath: string) {
    const testRoot = (await fse.pathExists(dirPath)) ? dirPath : path.dirname(dirPath);
    const stat = await fse.stat(testRoot).catch(() => null);
    if (!stat?.isDirectory()) return false;

    const testPath = path.join(testRoot, `.nahida-write-test-${process.pid}-${nanoid(8)}`);
    try {
        await fse.mkdir(testPath);
        await fse.remove(testPath);
        return true;
    } catch {
        await fse.remove(testPath).catch(() => {});
        return false;
    }
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
    const relativeFromModRoot = path.relative(roots.modRoot, targetPath);
    if (!relativeFromModRoot.startsWith("..") && !path.isAbsolute(relativeFromModRoot)) {
        return relativeFromModRoot;
    }

    if (!roots.linkedRoot) return "";

    const relativeFromLinkedRoot = path.relative(roots.linkedRoot, targetPath);
    if (relativeFromLinkedRoot.startsWith("..") || path.isAbsolute(relativeFromLinkedRoot)) {
        return "";
    }

    return relativeFromLinkedRoot;
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
    const mods = (
        await Promise.all(
            (
                await listDirectoryNames(groupDir)
            ).map(async (name) => {
                const modPath = path.join(groupDir, name);
                if (!(await hasDirectPak(modPath))) return null;
                return await isNteModEnabled(modPath);
            }),
        )
    ).filter((enabled): enabled is boolean => enabled !== null);

    return {
        total: mods.length,
        enabled: mods.filter(Boolean).length,
    };
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
