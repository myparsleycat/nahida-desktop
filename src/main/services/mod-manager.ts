import { createHash } from "node:crypto";
import path from "node:path";
import { getCharactersFolder, getMods, sendF10 } from "@native/native-mod";
import type { ArchiveExtractPathMode, ResolvedArchiveExtractPathMode } from "@shared/mod";
import type { ApplyPresetResult, FolderGroup, Preset } from "@shared/types.gen";
import { GAME_MATCH_CASES, getMatchingImporter } from "@shared/xxmi-match";
import { and, eq, ne } from "drizzle-orm";
import { trim } from "es-toolkit";
import fg from "fast-glob";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";
import { appState, gamePaths, modPresetItems, modPresets, setting } from "../internal/db/schema";

interface PresetSnapshotItemRecord {
    modKey: string;
    relativePath: string;
    groupRelativePath: string;
    folderName: string;
    isEnabled: boolean;
}

interface ScannedPresetItem extends PresetSnapshotItemRecord {
    actualPath: string;
}

interface PresetConflictCandidate {
    actualPath: string;
    relativePath: string;
    folderName: string;
    isEnabled: boolean;
}

interface PresetConflict {
    modKey: string;
    candidates: PresetConflictCandidate[];
}

interface ShaderFixesModManifestFile {
    file: string;
    targetPath: string;
    targetKey: string;
    hash: string;
}

interface ShaderFixesModManifest {
    modKey: string;
    files: ShaderFixesModManifestFile[];
}

interface ShaderFixesModMarker {
    version: number;
    modKey: string;
}

interface ShaderFixesTargetRecord {
    targetPath: string;
    hash: string;
    modKeys: string[];
}

interface ShaderFixesFileCandidate {
    file: string;
    sourcePath: string;
}

const MOD_PRESET_ITEM_INSERT_BATCH_SIZE = 100;
const MOD_PRESET_VERSION = 2;
const SHADER_FIXES_DIR_NAME = "ShaderFixes";
const SHADER_FIXES_MOD_MARKER_FILE = ".nahida-shader-fixes.json";
const SHADER_FIXES_MOD_MARKER_VERSION = 1;
const SHADER_FIXES_MOD_STATE_PREFIX = "mod_shader_fixes:mod:";
const SHADER_FIXES_TARGET_STATE_PREFIX = "mod_shader_fixes:target:";
const DISABLED_PREFIX_REGEX = /^disabled\s+/i;

export class ModManager {
    private readonly desktop: NahidaDesktop;
    private gameWatcherId: string | null = null;
    private characterWatcherId: string | null = null;
    private shaderOperationQueue: Promise<void> = Promise.resolve();

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    private async getGlobalShaderFixesPath(modPath: string): Promise<string | null> {
        const importers = this.desktop.service.xxmi.getEnabledImporters();
        const modImporter = this.getModImporter(modPath, importers);
        if (modImporter) {
            return path.join(modImporter.importerFolder, SHADER_FIXES_DIR_NAME);
        }

        const games = await this.get.games();
        const matchedGame = games.find((g) => this.isSameOrChildPath(g.modFolderPath, modPath));
        if (!matchedGame) return null;

        const importerKey =
            matchedGame.importer ??
            getMatchingImporter(
                matchedGame.game,
                importers.map((i) => i.key),
            );
        const importer = importers.find((i) => i.key.toUpperCase() === importerKey?.toUpperCase());

        if (!importer) return null;

        return path.join(importer.importerFolder, SHADER_FIXES_DIR_NAME);
    }

    private getModImporter<T extends { key: string; importerFolder: string }>(
        modPath: string,
        importers: T[],
    ): T | null {
        const importersByKey = new Map(importers.map((i) => [i.key.toUpperCase(), i]));

        let currentPath = path.resolve(modPath);
        let parentPath = path.dirname(currentPath);

        while (parentPath !== currentPath) {
            const importer = importersByKey.get(path.basename(parentPath).toUpperCase());
            if (importer) return importer;

            currentPath = parentPath;
            parentPath = path.dirname(currentPath);
        }

        return null;
    }

    private async withShaderOperationLock<T>(operation: () => Promise<T>): Promise<T> {
        const previousOperation = this.shaderOperationQueue;
        let releaseOperation!: () => void;

        this.shaderOperationQueue = new Promise<void>((resolve) => {
            releaseOperation = resolve;
        });

        await previousOperation.catch(() => undefined);

        try {
            return await operation();
        } finally {
            releaseOperation();
        }
    }

    private hashString(value: string): string {
        return createHash("sha256").update(value).digest("hex");
    }

    private async hashFile(filePath: string): Promise<string> {
        return createHash("sha256")
            .update(await fse.readFile(filePath))
            .digest("hex");
    }

    private getShaderFixesModMarkerPath(modPath: string): string {
        return path.join(modPath, SHADER_FIXES_MOD_MARKER_FILE);
    }

    private async readShaderFixesModMarker(modPath: string): Promise<ShaderFixesModMarker | null> {
        try {
            const marker = await fse.readJson(this.getShaderFixesModMarkerPath(modPath));
            if (
                marker &&
                marker.version === SHADER_FIXES_MOD_MARKER_VERSION &&
                typeof marker.modKey === "string" &&
                marker.modKey.length > 0
            ) {
                return marker;
            }
        } catch (error) {
            if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
                this.desktop.logger.error(error, `Mod:readShaderFixesModMarker:${modPath}`);
            }
        }

        return null;
    }

    private async writeShaderFixesModMarker(modPath: string, modKey: string): Promise<void> {
        await fse.writeJson(
            this.getShaderFixesModMarkerPath(modPath),
            {
                version: SHADER_FIXES_MOD_MARKER_VERSION,
                modKey,
            } satisfies ShaderFixesModMarker,
            { spaces: 2 },
        );
    }

    private async deleteShaderFixesModMarker(modPath: string): Promise<void> {
        await fse.remove(this.getShaderFixesModMarkerPath(modPath));
    }

    private async getShaderFixesModKey(modPath: string, create: true): Promise<string>;
    private async getShaderFixesModKey(modPath: string, create?: false): Promise<string | null>;
    private async getShaderFixesModKey(modPath: string, create = false): Promise<string | null> {
        const marker = await this.readShaderFixesModMarker(modPath);
        if (marker) return marker.modKey;

        if (!create) return null;

        const modKey = nanoid();
        await this.writeShaderFixesModMarker(modPath, modKey);
        return modKey;
    }

    private getShaderFixesModStateKey(modKey: string): string {
        return `${SHADER_FIXES_MOD_STATE_PREFIX}${modKey}`;
    }

    private getShaderFixesTargetKey(targetPath: string): string {
        return this.hashString(this.normalizeModPath(path.resolve(targetPath)));
    }

    private getShaderFixesTargetStateKey(targetKey: string): string {
        return `${SHADER_FIXES_TARGET_STATE_PREFIX}${targetKey}`;
    }

    private async readAppStateJson<T>(key: string): Promise<T | null> {
        const result = await this.desktop.lib.db.query.appState.findFirst({
            where: eq(appState.key, key),
        });
        if (!result?.value) return null;

        try {
            return JSON.parse(result.value) as T;
        } catch (error) {
            this.desktop.logger.error(error, `Mod:readAppStateJson:${key}`);
            return null;
        }
    }

    private async writeAppStateJson(key: string, value: unknown): Promise<void> {
        const serializedValue = JSON.stringify(value);
        await this.desktop.lib.db
            .insert(appState)
            .values({ key, value: serializedValue, updatedAt: new Date().toISOString() })
            .onConflictDoUpdate({
                target: appState.key,
                set: { value: serializedValue, updatedAt: new Date().toISOString() },
            });
    }

    private async deleteAppState(key: string): Promise<void> {
        await this.desktop.lib.db.delete(appState).where(eq(appState.key, key));
    }

    private async readShaderFixesModManifest(
        modKey: string,
    ): Promise<ShaderFixesModManifest | null> {
        const manifest = await this.readAppStateJson<Partial<ShaderFixesModManifest>>(
            this.getShaderFixesModStateKey(modKey),
        );

        if (!manifest || manifest.modKey !== modKey || !Array.isArray(manifest.files)) {
            return null;
        }

        const files = manifest.files.filter(
            (file): file is ShaderFixesModManifestFile =>
                typeof file.file === "string" &&
                typeof file.targetPath === "string" &&
                typeof file.targetKey === "string" &&
                typeof file.hash === "string",
        );

        return {
            modKey,
            files,
        };
    }

    private async writeShaderFixesModManifest(manifest: ShaderFixesModManifest): Promise<void> {
        await this.writeAppStateJson(this.getShaderFixesModStateKey(manifest.modKey), manifest);
    }

    private async deleteShaderFixesModManifest(modKey: string): Promise<void> {
        await this.deleteAppState(this.getShaderFixesModStateKey(modKey));
    }

    private async readShaderFixesTargetRecord(
        targetKey: string,
    ): Promise<ShaderFixesTargetRecord | null> {
        const record = await this.readAppStateJson<Partial<ShaderFixesTargetRecord>>(
            this.getShaderFixesTargetStateKey(targetKey),
        );

        if (
            !record ||
            typeof record.targetPath !== "string" ||
            typeof record.hash !== "string" ||
            !Array.isArray(record.modKeys)
        ) {
            return null;
        }

        return {
            targetPath: record.targetPath,
            hash: record.hash,
            modKeys: record.modKeys.filter(
                (modKey, index, modKeys): modKey is string =>
                    typeof modKey === "string" && modKeys.indexOf(modKey) === index,
            ),
        };
    }

    private async writeShaderFixesTargetRecord(
        targetKey: string,
        record: ShaderFixesTargetRecord,
    ): Promise<void> {
        await this.writeAppStateJson(this.getShaderFixesTargetStateKey(targetKey), record);
    }

    private async deleteShaderFixesTargetRecord(targetKey: string): Promise<void> {
        await this.deleteAppState(this.getShaderFixesTargetStateKey(targetKey));
    }

    private normalizeShaderFixesRelativePath(targetPath: string): string {
        return targetPath
            .split(/[\\/]+/)
            .filter(Boolean)
            .join("/");
    }

    private async getShaderFixesFileCandidates(
        modPath: string,
    ): Promise<ShaderFixesFileCandidate[]> {
        const shaderDirectories = await fg(`**/${SHADER_FIXES_DIR_NAME}`, {
            cwd: modPath,
            onlyDirectories: true,
            dot: true,
            caseSensitiveMatch: false,
        });

        if (shaderDirectories.length === 0) return [];

        const uniqueShaderDirectories = Array.from(new Set(shaderDirectories)).sort((a, b) => {
            const aIsRootShaderDirectory =
                this.normalizeShaderFixesRelativePath(a).toUpperCase() ===
                SHADER_FIXES_DIR_NAME.toUpperCase();
            const bIsRootShaderDirectory =
                this.normalizeShaderFixesRelativePath(b).toUpperCase() ===
                SHADER_FIXES_DIR_NAME.toUpperCase();

            if (aIsRootShaderDirectory && !bIsRootShaderDirectory) return -1;
            if (bIsRootShaderDirectory && !aIsRootShaderDirectory) return 1;
            return a.localeCompare(b);
        });
        const candidates: ShaderFixesFileCandidate[] = [];

        for (const shaderDirectory of uniqueShaderDirectories) {
            const shaderPath = path.join(modPath, shaderDirectory);
            const files = await fg("**/*", {
                cwd: shaderPath,
                onlyFiles: true,
                dot: true,
            });

            for (const file of files.sort((a, b) => a.localeCompare(b))) {
                candidates.push({
                    file: this.normalizeShaderFixesRelativePath(file),
                    sourcePath: path.join(shaderPath, file),
                });
            }
        }

        return candidates;
    }

    private async handleShaders(modPath: string, enable: boolean): Promise<string[]> {
        return await this.withShaderOperationLock(async () => {
            return await this.handleShadersLocked(modPath, enable);
        });
    }

    private async rollbackEnabledShaders(
        modPath: string,
        processedShaders: string[],
    ): Promise<void> {
        await this.withShaderOperationLock(async () => {
            let rollbackError: unknown = null;
            try {
                await this.handleShadersLocked(modPath, false);
            } catch (error) {
                rollbackError = error;
            }

            if (processedShaders.length === 0) {
                if (rollbackError) throw rollbackError;
                return;
            }

            const globalShaderPath = await this.getGlobalShaderFixesPath(modPath);
            if (!globalShaderPath) {
                if (rollbackError) throw rollbackError;
                return;
            }

            for (const file of processedShaders) {
                const target = path.join(globalShaderPath, file);
                const targetKey = this.getShaderFixesTargetKey(target);
                const targetRecord = await this.readShaderFixesTargetRecord(targetKey);

                if (!targetRecord) {
                    await fse.remove(target);
                }
            }

            if (rollbackError) throw rollbackError;
        });
    }

    private async handleShadersLocked(modPath: string, enable: boolean): Promise<string[]> {
        const shaderFiles = await this.getShaderFixesFileCandidates(modPath);
        if (shaderFiles.length === 0) return [];

        const globalShaderPath = await this.getGlobalShaderFixesPath(modPath);
        if (!globalShaderPath) return [];

        const processedFiles: string[] = [];

        try {
            if (enable) {
                const modKey = await this.getShaderFixesModKey(modPath, true);
                const manifest: ShaderFixesModManifest = {
                    modKey,
                    files: [],
                };

                await fse.ensureDir(globalShaderPath);
                for (const { file, sourcePath: source } of shaderFiles) {
                    const target = path.join(globalShaderPath, file);
                    const hash = await this.hashFile(source);
                    const targetKey = this.getShaderFixesTargetKey(target);
                    const targetRecord = await this.readShaderFixesTargetRecord(targetKey);
                    const targetExists = await fse.pathExists(target);

                    if (targetExists) {
                        const currentHash = await this.hashFile(target);
                        if (currentHash === hash) {
                            await this.writeShaderFixesTargetRecord(targetKey, {
                                targetPath: target,
                                hash,
                                modKeys:
                                    targetRecord?.hash === hash
                                        ? Array.from(new Set([...targetRecord.modKeys, modKey]))
                                        : [modKey],
                            });
                            manifest.files.push({ file, targetPath: target, targetKey, hash });
                            await this.writeShaderFixesModManifest(manifest);
                        }
                        continue;
                    }

                    processedFiles.push(file);
                    await fse.copy(source, target);
                    await this.writeShaderFixesTargetRecord(targetKey, {
                        targetPath: target,
                        hash,
                        modKeys:
                            targetRecord?.hash === hash
                                ? Array.from(new Set([...targetRecord.modKeys, modKey]))
                                : [modKey],
                    });
                    manifest.files.push({ file, targetPath: target, targetKey, hash });
                    await this.writeShaderFixesModManifest(manifest);
                }

                if (manifest.files.length > 0) {
                    await this.writeShaderFixesModManifest(manifest);
                } else {
                    await this.deleteShaderFixesModManifest(modKey);
                    await this.deleteShaderFixesModMarker(modPath);
                }
            } else {
                const modKey = await this.getShaderFixesModKey(modPath);
                if (!modKey) return [];

                const manifest = await this.readShaderFixesModManifest(modKey);
                if (!manifest) {
                    await this.deleteShaderFixesModMarker(modPath);
                    return [];
                }

                for (const file of manifest.files) {
                    const targetRecord = await this.readShaderFixesTargetRecord(file.targetKey);
                    if (targetRecord) {
                        const nextModKeys = targetRecord.modKeys.filter((key) => key !== modKey);
                        if (nextModKeys.length > 0) {
                            await this.writeShaderFixesTargetRecord(file.targetKey, {
                                ...targetRecord,
                                modKeys: nextModKeys,
                            });
                            continue;
                        }
                    }

                    if (await fse.pathExists(file.targetPath)) {
                        const currentHash = await this.hashFile(file.targetPath);
                        if (currentHash === file.hash) {
                            processedFiles.push(file.file);
                            await fse.remove(file.targetPath);
                        }
                    }

                    await this.deleteShaderFixesTargetRecord(file.targetKey);
                }

                await this.deleteShaderFixesModManifest(modKey);
                await this.deleteShaderFixesModMarker(modPath);
            }
        } catch (err) {
            const shaderError = err instanceof Error ? err : new Error(String(err));
            throw Object.assign(shaderError, { processedFiles });
        }

        return processedFiles;
    }

    public async watchGame(game: string) {
        const modFolderPath = await this.get.gamePath(game);
        if (!modFolderPath) return;

        if (this.gameWatcherId) {
            await this.desktop.lib.watcher.remove(this.gameWatcherId);
            this.gameWatcherId = null;
        }

        try {
            this.gameWatcherId = await this.desktop.lib.watcher.create(
                modFolderPath,
                { depth: 1 },
                (event) => {
                    if (event === "create" || event === "modify" || event === "remove") {
                        if (this.desktop.window.main.window) {
                            this.desktop.ipc.postMessageToWindow(
                                this.desktop.window.main.window,
                                "mod:update-game",
                            );
                        }
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:watchGame:${game}`);
        }
    }

    public async watchCharacter(characterPath: string) {
        if (this.characterWatcherId) {
            await this.desktop.lib.watcher.remove(this.characterWatcherId);
            this.characterWatcherId = null;
        }

        try {
            this.characterWatcherId = await this.desktop.lib.watcher.create(
                characterPath,
                { depth: 1 },
                (event) => {
                    if (event === "create" || event === "modify" || event === "remove") {
                        if (this.desktop.window.main.window) {
                            this.desktop.ipc.postMessageToWindow(
                                this.desktop.window.main.window,
                                "mod:update-mods",
                            );
                        }
                    }
                },
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:watchCharacter:${characterPath}`);
        }
    }

    private async renameWithUniqueName(modPath: string, baseFolderName: string): Promise<string> {
        const parentPath = path.dirname(modPath);
        const existingNames = await fse.readdir(parentPath);
        const newFolderName = this.desktop.lib.fs.getUniqueName(baseFolderName, existingNames);
        const newPath = path.join(parentPath, newFolderName);

        await this.desktop.lib.fs.rename(modPath, newPath);
        return newPath;
    }

    private normalizeModPath(modPath: string): string {
        return path.normalize(modPath).toLowerCase();
    }

    private isSameOrChildPath(parentPath: string, targetPath: string): boolean {
        const relativePath = path.relative(
            this.normalizeModPath(path.resolve(parentPath)),
            this.normalizeModPath(path.resolve(targetPath)),
        );

        return (
            relativePath === "" ||
            (!relativePath.startsWith("..") && !path.isAbsolute(relativePath))
        );
    }

    private normalizeRelativePath(targetPath: string): string {
        return targetPath
            .split(/[\\/]+/)
            .filter(Boolean)
            .map((segment) => this.stripDisabledPrefix(segment).toLowerCase())
            .join("/");
    }

    private stripDisabledPrefix(folderName: string): string {
        return trim(folderName.replace(DISABLED_PREFIX_REGEX, ""));
    }

    private restoreDisabledPrefix(sourceFolderName: string, folderName: string): string {
        if (DISABLED_PREFIX_REGEX.test(sourceFolderName)) {
            return `DISABLED ${folderName}`;
        }
        return folderName;
    }

    private toGameRelativePath(rootPath: string, targetPath: string): string {
        return this.normalizeRelativePath(path.relative(rootPath, targetPath));
    }

    private buildModKey(gamePath: string, groupPath: string, modPath: string): string {
        const groupRelativePath = this.toGameRelativePath(gamePath, groupPath);
        const modRelativePath = this.toGameRelativePath(gamePath, modPath);
        return `${groupRelativePath}::${modRelativePath}`;
    }

    private buildPresetSnapshotItem(
        gamePath: string,
        groupPath: string,
        modPath: string,
    ): ScannedPresetItem {
        const folderName = path.basename(modPath);
        return {
            modKey: this.buildModKey(gamePath, groupPath, modPath),
            relativePath: this.toGameRelativePath(gamePath, modPath),
            groupRelativePath: this.toGameRelativePath(gamePath, groupPath),
            folderName: this.stripDisabledPrefix(folderName),
            isEnabled: !/^disabled\s+/i.test(folderName),
            actualPath: modPath,
        };
    }

    private getPresetGroupPath(gamePath: string, modPath: string): string {
        const relativePath = path.relative(gamePath, modPath);
        const segments = relativePath.split(/[\\/]+/).filter(Boolean);

        if (segments.length <= 1) {
            return gamePath;
        }

        return path.join(gamePath, ...segments.slice(0, -1));
    }

    private async collectPresetSnapshotItems(gamePath: string): Promise<ScannedPresetItem[]> {
        const iniPaths = await this.desktop.lib.fs.findFiles(gamePath, {
            extensions: [".ini"],
        });
        const modPathMap = new Map<string, string>();

        for (const iniPath of iniPaths) {
            const modPath = path.dirname(iniPath);
            const normalizedModPath = this.normalizeModPath(modPath);
            if (!modPathMap.has(normalizedModPath)) {
                modPathMap.set(normalizedModPath, modPath);
            }
        }

        return Array.from(modPathMap.values())
            .sort((a, b) => a.localeCompare(b))
            .map((modPath) => {
                const groupPath = this.getPresetGroupPath(gamePath, modPath);
                return this.buildPresetSnapshotItem(gamePath, groupPath, modPath);
            });
    }

    private async getPresetSnapshot(game: string): Promise<ScannedPresetItem[]> {
        const gamePath = await this.get.gamePath(game);
        if (!gamePath) {
            throw new Error(`No mod folder path set for ${game}`);
        }

        const items = await this.collectPresetSnapshotItems(gamePath);
        return items
            .sort((a, b) => a.relativePath.localeCompare(b.relativePath))
            .map((item) => ({
                ...item,
                relativePath: item.relativePath,
            }));
    }

    private async getPresetSnapshotConflicts(game: string): Promise<PresetConflict[]> {
        const items = await this.getPresetSnapshot(game);
        const itemsByModKey = new Map<string, ScannedPresetItem[]>();

        for (const item of items) {
            const conflicts = itemsByModKey.get(item.modKey) ?? [];
            conflicts.push(item);
            itemsByModKey.set(item.modKey, conflicts);
        }

        return Array.from(itemsByModKey.entries())
            .filter(([, conflicts]) => conflicts.length > 1)
            .map(([modKey, conflicts]) => ({
                modKey,
                candidates: conflicts
                    .sort((a, b) => a.relativePath.localeCompare(b.relativePath))
                    .map((item) => ({
                        actualPath: item.actualPath,
                        relativePath: item.relativePath,
                        folderName: item.folderName,
                        isEnabled: item.isEnabled,
                    })),
            }))
            .sort((a, b) => a.modKey.localeCompare(b.modKey));
    }

    private async resolvePresetSnapshotConflicts(game: string): Promise<PresetConflict[]> {
        const conflicts = await this.getPresetSnapshotConflicts(game);

        for (const conflict of conflicts) {
            const candidates = conflict.candidates
                .map((candidate) =>
                    candidate.actualPath
                        ? {
                              ...candidate,
                              actualPath: candidate.actualPath,
                          }
                        : null,
                )
                .filter((candidate): candidate is PresetConflictCandidate => candidate !== null);
            const enabledCandidates = candidates.filter((candidate) => candidate.isEnabled);
            const disabledCandidates = candidates.filter((candidate) => !candidate.isEnabled);

            const renameTargets =
                enabledCandidates.length > 0
                    ? disabledCandidates
                    : disabledCandidates.length > 1
                      ? disabledCandidates.slice(1)
                      : [];

            for (const candidate of renameTargets) {
                await this.renameWithUniqueName(
                    candidate.actualPath,
                    this.restoreDisabledPrefix(
                        path.basename(candidate.actualPath),
                        candidate.folderName,
                    ),
                );
            }
        }

        return await this.getPresetSnapshotConflicts(game);
    }

    private toPresetRecord(item: ScannedPresetItem): PresetSnapshotItemRecord {
        return {
            modKey: item.modKey,
            relativePath: item.relativePath,
            groupRelativePath: item.groupRelativePath,
            folderName: item.folderName,
            isEnabled: item.isEnabled,
        };
    }

    get = {
        gamePath: async (game: string): Promise<string | null> => {
            const result = await this.desktop.lib.db.query.gamePaths.findFirst({
                where: eq(gamePaths.game, game),
            });
            return result?.modFolderPath || null;
        },

        characters: async (game: string, searchModPreview?: boolean): Promise<FolderGroup[]> => {
            const modFolderPath = await this.get.gamePath(game);
            if (!modFolderPath) {
                throw new Error(`No mod folder path set for ${game}`);
            }

            const shouldFallback =
                searchModPreview ?? (await this.desktop.setting.mod.getSearchModPreview());

            try {
                return await getCharactersFolder(modFolderPath, shouldFallback);
            } catch (error) {
                this.desktop.logger.error(error, `Mod:characters:${game}`);
                throw error;
            }
        },

        subGroups: async (
            folderPath: string,
            searchModPreview?: boolean,
        ): Promise<FolderGroup[]> => {
            const shouldFallback =
                searchModPreview ?? (await this.desktop.setting.mod.getSearchModPreview());
            try {
                return await getCharactersFolder(folderPath, shouldFallback);
            } catch (error) {
                this.desktop.logger.error(error, `Mod:subGroups:${folderPath}`);
                throw error;
            }
        },

        mods: async (groupPath: string): Promise<FolderGroup> => {
            try {
                return await getMods(groupPath);
            } catch (error) {
                this.desktop.logger.error(error, `Mod:mods:${groupPath}`);
                throw error;
            }
        },

        presets: async (game: string): Promise<Preset[]> => {
            const results = await this.desktop.lib.db.query.modPresets.findMany({
                where: eq(modPresets.game, game),
            });

            return results
                .sort((a, b) => a.name.localeCompare(b.name))
                .map((r) => ({
                    id: r.id,
                    game: r.game,
                    name: r.name,
                    description: r.description ?? null,
                    createdAt: r.createdAt,
                    updatedAt: r.updatedAt,
                    version: r.version,
                    isLegacy: r.version < MOD_PRESET_VERSION,
                }));
        },

        presetCreateConflicts: async (game: string): Promise<PresetConflict[]> => {
            return await this.getPresetSnapshotConflicts(game);
        },

        games: async () => {
            return await this.desktop.lib.db.select().from(gamePaths);
        },

        lastGame: async (): Promise<string | null> => {
            const result = await this.desktop.lib.db.query.setting.findFirst({
                where: eq(setting.key, "last_game"),
            });
            return result?.value || null;
        },

        expandedGroups: async (): Promise<string[]> => {
            const result = await this.desktop.lib.db.query.setting.findFirst({
                where: eq(setting.key, "expanded_groups"),
            });
            if (!result?.value) return [];
            try {
                return JSON.parse(result.value) as string[];
            } catch {
                return [];
            }
        },

        previousFocusedGame: async (): Promise<string | null> => {
            try {
                const currentPid = process.pid;

                let currentProcessName = this.desktop.lib.native.getProcessName(currentPid);
                if (currentProcessName) currentProcessName = currentProcessName.toLowerCase();

                const previousPids = this.desktop.lib.native.getPreviousPids(currentPid);
                if (previousPids.length === 0) return null;

                const games = await this.get.games();

                for (const pid of previousPids) {
                    const processName = this.desktop.lib.native.getProcessName(pid);
                    if (!processName) continue;

                    const lowerProcessName = processName.toLowerCase();

                    if (currentProcessName && lowerProcessName.includes(currentProcessName))
                        continue;
                    if (lowerProcessName.includes("explorer")) continue;

                    for (const [_, keywords] of Object.entries(GAME_MATCH_CASES)) {
                        const isGameProcess = keywords.some((k) => lowerProcessName.includes(k));

                        if (isGameProcess) {
                            const matchedGame = games.find((g) => {
                                const lowerGame = g.game.toLowerCase();
                                return keywords.some((k) => lowerGame.includes(k));
                            });

                            if (matchedGame) return matchedGame.game;
                        }
                    }
                }

                return null;
            } catch (error) {
                this.desktop.logger.error(error, "Mod:previousFocusedGame");
                return null;
            }
        },

        gamePid: async (game: string): Promise<number | null> => {
            try {
                const currentPid = process.pid;
                const previousPids = this.desktop.lib.native.getPreviousPids(currentPid);
                if (previousPids.length === 0) return null;

                const lowerGame = game.toLowerCase();
                let matchingKeywords: string[] | undefined;

                for (const [_, keywords] of Object.entries(GAME_MATCH_CASES)) {
                    if (keywords.some((k) => lowerGame.includes(k))) {
                        matchingKeywords = keywords;
                        break;
                    }
                }

                if (!matchingKeywords) return null;

                for (const pid of previousPids) {
                    const processName = this.desktop.lib.native.getProcessName(pid);
                    if (!processName) continue;

                    const lowerProcessName = processName.toLowerCase();
                    if (matchingKeywords.some((k) => lowerProcessName.includes(k))) {
                        return pid;
                    }
                }
                return null;
            } catch (error) {
                this.desktop.logger.error(error, `Mod:gamePid:${game}`);
                return null;
            }
        },
    };

    fn = {
        setGamePath: async (game: string, modFolderPath: string) => {
            await this.desktop.lib.db
                .insert(gamePaths)
                .values({ game, modFolderPath, importer: null })
                .onConflictDoUpdate({
                    target: gamePaths.game,
                    set: { modFolderPath },
                });
        },

        enable: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);

            if (DISABLED_PREFIX_REGEX.test(folderName)) {
                const baseFolderName = trim(folderName.replace(DISABLED_PREFIX_REGEX, ""));
                let processedShaders: string[] = [];
                const copyShaderFixes = await this.desktop.setting.mod.getCopyShaderFixesOnEnable();
                try {
                    if (copyShaderFixes) {
                        processedShaders = await this.handleShaders(modPath, true);
                    }
                    return await this.renameWithUniqueName(modPath, baseFolderName);
                } catch (err) {
                    processedShaders =
                        (err as { processedFiles?: string[] }).processedFiles ?? processedShaders;
                    if (copyShaderFixes) {
                        try {
                            await this.rollbackEnabledShaders(modPath, processedShaders);
                        } catch (rollbackError) {
                            this.desktop.logger.error(
                                rollbackError,
                                `Mod:enable:rollbackShaders:${modPath}`,
                            );
                        }
                    }

                    throw err;
                }
            }
            return modPath;
        },

        disable: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);

            if (!DISABLED_PREFIX_REGEX.test(folderName)) {
                const baseFolderName = `DISABLED ${folderName}`;
                try {
                    await this.handleShaders(modPath, false);
                    return await this.renameWithUniqueName(modPath, baseFolderName);
                } catch (err) {
                    try {
                        await this.handleShaders(modPath, true);
                    } catch (rollbackError) {
                        this.desktop.logger.error(
                            rollbackError,
                            `Mod:disable:rollbackShaders:${modPath}`,
                        );
                    }
                    throw err;
                }
            }
            return modPath;
        },

        toggle: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const isEnabled = !DISABLED_PREFIX_REGEX.test(folderName);

            let result: string;

            try {
                if (isEnabled) {
                    result = await this.fn.disable(modPath);
                } else {
                    result = await this.fn.enable(modPath);
                }
            } catch (err) {
                const lockInfo = await this.desktop.lib.fs.isLockedPathError(err, modPath);
                if (lockInfo.isLocked) {
                    console.log("lockInfo", lockInfo);
                    if (lockInfo.processes.length > 0) {
                        const processNames = lockInfo.processes.map((p) => p.name).join(", ");
                        throw new Error(`MOD_FOLDER_LOCKED|${processNames}`);
                    }
                    throw new Error("MOD_FOLDER_LOCKED");
                }
                throw err;
            }

            // await this.fn.triggerF10(modPath);
            return result;
        },

        exclusiveToggle: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const isEnabled = !DISABLED_PREFIX_REGEX.test(folderName);

            if (!isEnabled) {
                const groupPath = path.dirname(modPath);
                const modFolders = await fg("*", {
                    cwd: groupPath,
                    onlyDirectories: true,
                });

                const disablePromises = modFolders.map(async (modFolderName) => {
                    const currentModPath = path.join(groupPath, modFolderName);
                    if (currentModPath === modPath) return;

                    try {
                        const isOtherEnabled = !/^disabled\s+/i.test(modFolderName);
                        if (isOtherEnabled) {
                            await this.fn.disable(currentModPath);
                        }
                    } catch (error) {
                        this.desktop.logger.error(
                            error,
                            `Mod:exclusiveToggle:disable:${currentModPath}`,
                        );
                    }
                });

                await Promise.all(disablePromises);
                const result = await this.fn.enable(modPath);
                // await this.fn.triggerF10(modPath);
                return result;
            } else {
                const result = await this.fn.disable(modPath);
                // await this.fn.triggerF10(modPath);
                return result;
            }
        },

        rename: async (modPath: string, newName: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const trimmedName = this.stripDisabledPrefix(newName);

            if (!trimmedName) {
                throw new Error("INVALID_MOD_NAME");
            }

            this.desktop.lib.fs.assertValidWindowsFilename(trimmedName);

            const nextFolderName = this.restoreDisabledPrefix(folderName, trimmedName);
            if (folderName === nextFolderName) {
                return modPath;
            }

            const parentPath = path.dirname(modPath);
            const nextPath = path.join(parentPath, nextFolderName);

            if (this.normalizeModPath(modPath) !== this.normalizeModPath(nextPath)) {
                const exists = await this.desktop.lib.fs.pathExists(nextPath);
                if (exists) {
                    throw new Error(`ALREADY_EXISTS:${nextFolderName}`);
                }
            }

            try {
                await this.desktop.lib.fs.rename(modPath, nextPath);
                return nextPath;
            } catch (err) {
                const lockInfo = await this.desktop.lib.fs.isLockedPathError(err, modPath);
                if (lockInfo.isLocked) {
                    if (lockInfo.processes.length > 0) {
                        const processNames = lockInfo.processes.map((p) => p.name).join(", ");
                        throw new Error(`MOD_FOLDER_LOCKED|${processNames}`);
                    }
                    throw new Error("MOD_FOLDER_LOCKED");
                }
                throw err;
            }
        },

        enableAll: async (groupPath: string): Promise<void> => {
            try {
                const modFolders = await fg("*", {
                    cwd: groupPath,
                    onlyDirectories: true,
                });

                const enablePromises = modFolders.map(async (modFolderName) => {
                    const modPath = path.join(groupPath, modFolderName);
                    try {
                        await this.fn.enable(modPath);
                    } catch (error) {
                        this.desktop.logger.error(error, `Mod:enableAll:${modPath}`);
                    }
                });

                await Promise.all(enablePromises);
            } catch (error) {
                this.desktop.logger.error(error, `Mod:enableAll:${groupPath}`);
                throw error;
            }
        },

        disableAll: async (groupPath: string): Promise<void> => {
            try {
                const modFolders = await fg("*", {
                    cwd: groupPath,
                    onlyDirectories: true,
                });

                const disablePromises = modFolders.map(async (modFolderName) => {
                    const modPath = path.join(groupPath, modFolderName);
                    try {
                        await this.fn.disable(modPath);
                    } catch (error) {
                        this.desktop.logger.error(error, `Mod:disableAll:${modPath}`);
                    }
                });

                await Promise.all(disablePromises);
            } catch (error) {
                this.desktop.logger.error(error, `Mod:disableAll:${groupPath}`);
                throw error;
            }
        },

        updateToggleKey: async (
            iniPath: string,
            sectionName: string,
            variable: string,
            value: string,
        ): Promise<void> => {
            try {
                const content = await fse.readFile(iniPath, "utf-8");
                const lines = content.split("\n");
                const newLines: string[] = [];
                const variableLine = `${variable} = ${value}`;

                let currentSection: string | null = null;
                let updated = false;
                let foundVariableInSection = false;
                let sectionStartIndex = -1;

                const insertIntoCurrentSection = () => {
                    if (
                        currentSection?.toLowerCase() !== sectionName.toLowerCase() ||
                        foundVariableInSection ||
                        value === ""
                    ) {
                        return;
                    }

                    let insertIndex = newLines.length;
                    while (
                        insertIndex > sectionStartIndex + 1 &&
                        newLines[insertIndex - 1].trim() === ""
                    ) {
                        insertIndex -= 1;
                    }

                    newLines.splice(insertIndex, 0, variableLine);
                    updated = true;
                    foundVariableInSection = true;
                };

                for (let i = 0; i < lines.length; i++) {
                    const line = lines[i];
                    const trimmedLine = line.trim();

                    if (trimmedLine.startsWith("[") && trimmedLine.endsWith("]")) {
                        insertIntoCurrentSection();

                        currentSection = trimmedLine.slice(1, -1);
                        newLines.push(line);
                        sectionStartIndex = newLines.length - 1;
                        continue;
                    }

                    const lowerLine = trimmedLine.toLowerCase();
                    const lowerVar = variable.toLowerCase();
                    const isVariableLine =
                        lowerLine.startsWith(lowerVar + " =") ||
                        lowerLine.startsWith(lowerVar + "=");

                    if (
                        currentSection?.toLowerCase() === sectionName.toLowerCase() &&
                        isVariableLine
                    ) {
                        foundVariableInSection = true;
                        if (value === "") {
                            updated = true;
                        } else {
                            newLines.push(variableLine);
                            updated = true;
                        }
                    } else {
                        newLines.push(line);
                    }
                }

                insertIntoCurrentSection();

                if (updated) {
                    const newContent = newLines.join("\n");
                    try {
                        await fse.chmod(iniPath, 0o666);
                        await fse.writeFile(iniPath, newContent, "utf-8");
                    } catch (error) {
                        if (
                            (error as NodeJS.ErrnoException).code === "EPERM" ||
                            (error as NodeJS.ErrnoException).code === "EACCES"
                        ) {
                            await fse.unlink(iniPath);
                            await fse.writeFile(iniPath, newContent, "utf-8");
                        } else {
                            throw error;
                        }
                    }
                }
            } catch (error) {
                this.desktop.logger.error(error, `Mod:updateToggleKey:${iniPath}`);
                throw error;
            }
        },

        createPreset: async (
            game: string,
            name: string,
            description?: string,
            resolveConflicts = false,
        ): Promise<Preset> => {
            const trimmedName = trim(name);
            const trimmedDescription = trim(description ?? "");
            if (!trimmedName) {
                throw new Error("INVALID_PRESET_NAME");
            }

            const existingPreset = await this.desktop.lib.db.query.modPresets.findFirst({
                where: and(eq(modPresets.game, game), eq(modPresets.name, trimmedName)),
            });
            if (existingPreset) {
                throw new Error("PRESET_NAME_EXISTS");
            }

            if (resolveConflicts) {
                const remainingConflicts = await this.resolvePresetSnapshotConflicts(game);
                if (remainingConflicts.length > 0) {
                    throw new Error("PRESET_CONFLICT_RESOLUTION_FAILED");
                }
            } else {
                const conflicts = await this.getPresetSnapshotConflicts(game);
                if (conflicts.length > 0) {
                    throw new Error("PRESET_CONFLICTS_EXIST");
                }
            }

            const snapshot = await this.getPresetSnapshot(game);

            const id = nanoid();
            const now = new Date().toISOString();

            this.desktop.lib.db.transaction((tx) => {
                tx.insert(modPresets)
                    .values({
                        id,
                        game,
                        name: trimmedName,
                        description: trimmedDescription || null,
                        itemCount: 0,
                        createdAt: now,
                        updatedAt: now,
                        version: MOD_PRESET_VERSION,
                    })
                    .run();

                if (snapshot.length > 0) {
                    const presetItems = snapshot.map((item, index) => ({
                        presetId: id,
                        ...this.toPresetRecord(item),
                        itemOrder: index,
                    }));

                    for (
                        let startIndex = 0;
                        startIndex < presetItems.length;
                        startIndex += MOD_PRESET_ITEM_INSERT_BATCH_SIZE
                    ) {
                        tx.insert(modPresetItems)
                            .values(
                                presetItems.slice(
                                    startIndex,
                                    startIndex + MOD_PRESET_ITEM_INSERT_BATCH_SIZE,
                                ),
                            )
                            .run();
                    }
                }
            });

            return {
                id,
                game,
                name: trimmedName,
                description: trimmedDescription || null,
                createdAt: now,
                updatedAt: now,
                version: MOD_PRESET_VERSION,
                isLegacy: false,
            };
        },

        applyPreset: async (presetId: string): Promise<ApplyPresetResult> => {
            const preset = await this.desktop.lib.db.query.modPresets.findFirst({
                where: eq(modPresets.id, presetId),
            });

            if (!preset) {
                throw new Error(`Preset ${presetId} not found`);
            }

            if (preset.version < MOD_PRESET_VERSION) {
                throw new Error("LEGACY_PRESET_NOT_SUPPORTED");
            }

            const presetItems = await this.desktop.lib.db.query.modPresetItems.findMany({
                where: eq(modPresetItems.presetId, presetId),
            });
            const currentItems = await this.getPresetSnapshot(preset.game);
            const currentByKey = new Map(currentItems.map((item) => [item.modKey, item] as const));
            const currentByRelativePath = new Map(
                currentItems.map((item) => [item.relativePath.toLowerCase(), item] as const),
            );
            const result: ApplyPresetResult = {
                presetId,
                applied: [],
                skipped: [],
                missing: [],
            };

            for (const presetItem of presetItems.sort((a, b) => a.itemOrder - b.itemOrder)) {
                const currentItem =
                    currentByKey.get(presetItem.modKey) ??
                    currentByRelativePath.get(presetItem.relativePath.toLowerCase());

                if (!currentItem) {
                    result.missing.push({
                        modKey: presetItem.modKey,
                        expectedFolderName: presetItem.folderName,
                        expectedRelativePath: presetItem.relativePath,
                    });
                    continue;
                }

                if (currentItem.isEnabled === presetItem.isEnabled) {
                    result.skipped.push(currentItem.relativePath);
                    continue;
                }

                try {
                    if (presetItem.isEnabled) {
                        await this.fn.enable(currentItem.actualPath);
                    } else {
                        await this.fn.disable(currentItem.actualPath);
                    }
                    result.applied.push(currentItem.relativePath);
                } catch (error) {
                    this.desktop.logger.error(
                        error,
                        `Mod:applyPreset:${presetItem.isEnabled ? "enable" : "disable"}:${currentItem.actualPath}`,
                    );
                }
            }

            return result;
        },

        deletePreset: async (presetId: string): Promise<void> => {
            await this.desktop.lib.db.delete(modPresets).where(eq(modPresets.id, presetId));
        },

        updatePresetName: async (presetId: string, newName: string): Promise<void> => {
            const preset = await this.desktop.lib.db.query.modPresets.findFirst({
                where: eq(modPresets.id, presetId),
            });

            if (!preset) {
                throw new Error(`Preset ${presetId} not found`);
            }

            const trimmedName = trim(newName);
            if (!trimmedName) {
                throw new Error("INVALID_PRESET_NAME");
            }

            const existingPreset = await this.desktop.lib.db.query.modPresets.findFirst({
                where: and(eq(modPresets.game, preset.game), eq(modPresets.name, trimmedName)),
            });
            if (existingPreset && existingPreset.id !== presetId) {
                throw new Error("PRESET_NAME_EXISTS");
            }

            await this.desktop.lib.db
                .update(modPresets)
                .set({ name: trimmedName, updatedAt: new Date().toISOString() })
                .where(eq(modPresets.id, presetId));
        },

        addGame: async (game: string, modFolderPath: string) => {
            if (!game || !modFolderPath) {
                throw new Error("INVALID_PARAMS");
            }

            const exists = await this.desktop.lib.db.query.gamePaths.findFirst({
                where: (t, { eq, or }) => or(eq(t.game, game), eq(t.modFolderPath, modFolderPath)),
            });

            if (exists) {
                if (exists.game === game) {
                    throw new Error("DUPLICATE_GAME_NAME");
                } else if (exists.modFolderPath === modFolderPath) {
                    throw new Error("DUPLICATE_MOD_FOLDER_PATH");
                }
            }

            await this.desktop.lib.db
                .insert(gamePaths)
                .values({ game, modFolderPath, importer: null });
        },

        updateGame: async (
            game: string,
            updates: {
                modFolderPath: string;
                importer: string | null;
            },
        ) => {
            if (!game || !updates.modFolderPath) {
                throw new Error("Game and modFolderPath are required");
            }

            const existingGame = await this.desktop.lib.db.query.gamePaths.findFirst({
                where: (t, { eq }) => eq(t.game, game),
            });

            if (!existingGame) {
                throw new Error(`Game ${game} not found`);
            }

            const duplicatePath = await this.desktop.lib.db.query.gamePaths.findFirst({
                where: and(
                    eq(gamePaths.modFolderPath, updates.modFolderPath),
                    ne(gamePaths.game, game),
                ),
            });

            if (duplicatePath) {
                throw new Error("DUPLICATE_MOD_FOLDER_PATH");
            }

            await this.desktop.lib.db
                .update(gamePaths)
                .set({
                    modFolderPath: updates.modFolderPath,
                    importer: updates.importer,
                })
                .where(eq(gamePaths.game, game));
        },

        removeGame: async (game: string) => {
            await this.desktop.lib.db.delete(gamePaths).where(eq(gamePaths.game, game));
        },

        setLastGame: async (game: string) => {
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "last_game", value: game })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: game },
                });
        },

        setExpandedGroups: async (paths: string[]) => {
            const value = JSON.stringify(paths);
            await this.desktop.lib.db
                .insert(setting)
                .values({ key: "expanded_groups", value })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value },
                });
        },

        extractArchiveToGroup: async (
            archivePath: string,
            groupPath: string,
            mode?: ResolvedArchiveExtractPathMode,
        ): Promise<void> => {
            const deleteAfterExtract =
                await this.desktop.setting.mod.getDeleteArchiveAfterExtract();
            const extractMode: ArchiveExtractPathMode =
                mode ?? (await this.desktop.setting.mod.getArchiveExtractPathMode());

            if (extractMode === "ask_every_time") {
                throw new Error("ARCHIVE_EXTRACT_MODE_PROMPT_REQUIRED");
            }

            const flattenSingleRoot = extractMode !== "keep_archive_root";

            try {
                const finalTargetPath = await this.desktop.service.archive.extract(
                    archivePath,
                    groupPath,
                    {
                        flattenSingleRoot,
                    },
                );

                this.desktop.logger.info(
                    `Extracted archive ${archivePath} to ${finalTargetPath}`,
                    "Mod:extractArchiveToGroup",
                );

                if (deleteAfterExtract) {
                    await fse.remove(archivePath);
                }
            } catch (error) {
                this.desktop.logger.error(error, `Mod:extractArchiveToGroup:${archivePath}`);
                throw error;
            }
        },

        copyFolderToGroup: async (
            folderPath: string,
            groupPath: string,
            move: boolean,
        ): Promise<void> => {
            try {
                const folderName = path.basename(folderPath);
                const targetPath = path.join(groupPath, folderName);

                const exists = await fse.pathExists(targetPath);
                if (exists) {
                    throw new Error(`ALREADY_EXISTS:${folderName}`);
                }

                if (move) {
                    await fse.move(folderPath, targetPath);
                    this.desktop.logger.info(
                        `Moved folder ${folderPath} to ${targetPath}`,
                        "Mod:copyFolderToGroup",
                    );
                } else {
                    await fse.copy(folderPath, targetPath);
                    this.desktop.logger.info(
                        `Copied folder ${folderPath} to ${targetPath}`,
                        "Mod:copyFolderToGroup",
                    );
                }
            } catch (error) {
                this.desktop.logger.error(error, `Mod:copyFolderToGroup:${folderPath}`);
                throw error;
            }
        },

        pastePreview: async (
            modPath: string,
            data: string,
            type: "url" | "base64" | "path",
        ): Promise<void> => {
            try {
                let buffer: Buffer;
                let extension = ".png";

                if (type === "url") {
                    const response = await fetch(data);
                    if (!response.ok) {
                        throw new Error(`Failed to download image: ${response.statusText}`);
                    }
                    const contentType = response.headers.get("content-type");
                    if (contentType) {
                        const ext = contentType.split("/")[1];
                        if (ext) extension = `.${ext}`;
                    }
                    const arrayBuffer = await response.arrayBuffer();
                    buffer = Buffer.from(arrayBuffer);
                } else if (type === "base64") {
                    const matches = data.match(/^data:image\/([a-zA-Z0-9]+);base64,(.+)$/);
                    if (matches) {
                        extension = `.${matches[1]}`;
                        buffer = Buffer.from(matches[2], "base64");
                    } else {
                        buffer = Buffer.from(data, "base64");
                    }
                } else if (type === "path") {
                    extension = path.extname(data);
                    buffer = await fse.readFile(data);
                } else {
                    throw new Error(`Invalid paste type: ${type}`);
                }

                const fileName = `preview${extension}`;
                const filePath = path.join(modPath, fileName);

                await fse.writeFile(filePath, buffer);
                this.desktop.logger.info(`Saved preview image to ${filePath}`, "Mod:pastePreview");
            } catch (error) {
                this.desktop.logger.error(error, `Mod:pastePreview:${modPath}`);
                throw error;
            }
        },

        triggerF10: async (modPath: string) => {
            try {
                const groupPath = path.dirname(modPath);
                const group = await this.get.mods(groupPath);
                const activeCount = group.mods.filter((m) => m.isEnabled).length;

                if (activeCount <= 1) {
                    const games = await this.get.games();
                    const matchedGame = games.find((g) => modPath.startsWith(g.modFolderPath));
                    if (!matchedGame) return;

                    const pid = await this.get.gamePid(matchedGame.game);
                    if (pid) {
                        try {
                            const sent = await sendF10(pid);
                            if (sent) {
                                this.desktop.logger.info(
                                    `Sent F10 to ${matchedGame.game} (PID: ${pid})`,
                                    "Mod:triggerF10",
                                );
                            } else {
                                this.desktop.logger.warn(
                                    `Failed to send F10 to ${matchedGame.game} (PID: ${pid})`,
                                    "Mod:triggerF10",
                                );
                            }
                        } catch (e) {
                            this.desktop.logger.error(e, "Mod:triggerF10:native");
                        }
                    }
                }
            } catch (error) {
                this.desktop.logger.error(error, `Mod:triggerF10:${modPath}`);
            }
        },
    };
}

export default ModManager;
