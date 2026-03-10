import path from "node:path";
import { getCharactersFolder, getMods, sendF10 } from "@native/native-mod";
import type { ApplyPresetResult, FolderGroup, Preset } from "@shared/types.gen";
import { GAME_MATCH_CASES } from "@shared/xxmi-match";
import { and, eq } from "drizzle-orm";
import { trim } from "es-toolkit";
import fg from "fast-glob";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";
import { gamePaths, modPresetItems, modPresets, setting } from "../internal/db/schema";

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

const MOD_PRESET_ITEM_INSERT_BATCH_SIZE = 100;
const MOD_PRESET_VERSION = 2;

export class ModManager {
    private readonly desktop: NahidaDesktop;
    private gameWatcherId: string | null = null;
    private characterWatcherId: string | null = null;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    public async watchGame(game: string) {
        const modFolderPath = await this.get.gamePath(game);
        if (!modFolderPath) return;

        if (this.gameWatcherId) {
            await this.desktop.lib.watcher.removeWatcher(this.gameWatcherId);
            this.gameWatcherId = null;
        }

        try {
            this.gameWatcherId = await this.desktop.lib.watcher.createWatcher(
                modFolderPath,
                { depth: 0 },
                (event) => {
                    if (event === "create" || event === "remove") {
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
            await this.desktop.lib.watcher.removeWatcher(this.characterWatcherId);
            this.characterWatcherId = null;
        }

        try {
            this.characterWatcherId = await this.desktop.lib.watcher.createWatcher(
                characterPath,
                { depth: 0 },
                (event) => {
                    if (event === "modify" || event === "remove") {
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

    private normalizeRelativePath(targetPath: string): string {
        return targetPath
            .split(/[\\/]+/)
            .filter(Boolean)
            .map((segment) => this.stripDisabledPrefix(segment).toLowerCase())
            .join("/");
    }

    private stripDisabledPrefix(folderName: string): string {
        return trim(folderName.replace(/^disabled\s+/i, ""));
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
                .values({ game, modFolderPath })
                .onConflictDoUpdate({
                    target: gamePaths.game,
                    set: { modFolderPath },
                });
        },

        enable: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const regex = /^disabled\s+/i;

            if (regex.test(folderName)) {
                const baseFolderName = trim(folderName.replace(regex, ""));
                return this.renameWithUniqueName(modPath, baseFolderName);
            }

            return modPath;
        },

        disable: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const regex = /^disabled\s+/i;

            if (!regex.test(folderName)) {
                const baseFolderName = `DISABLED ${folderName}`;
                return this.renameWithUniqueName(modPath, baseFolderName);
            }

            return modPath;
        },

        toggle: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const isEnabled = !/^disabled\s+/i.test(folderName);

            let result: string;
            if (isEnabled) {
                result = await this.fn.disable(modPath);
            } else {
                result = await this.fn.enable(modPath);
            }

            // await this.fn.triggerF10(modPath);
            return result;
        },

        exclusiveToggle: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const isEnabled = !/^disabled\s+/i.test(folderName);

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

        createPreset: async (game: string, name: string, description?: string): Promise<Preset> => {
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
                throw new Error("Game and modFolderPath are required");
            }
            await this.desktop.lib.db
                .insert(gamePaths)
                .values({ game, modFolderPath })
                .onConflictDoUpdate({
                    target: gamePaths.game,
                    set: { modFolderPath },
                });
        },

        removeGame: async (game: string) => {
            await this.desktop.lib.db.delete(gamePaths).where(eq(gamePaths.game, game));
            await this.desktop.lib.db.delete(modPresets).where(eq(modPresets.game, game));
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

        extractArchiveToGroup: async (archivePath: string, groupPath: string): Promise<void> => {
            const deleteAfterExtract =
                await this.desktop.setting.mod.getDeleteArchiveAfterExtract();

            try {
                const finalTargetPath = await this.desktop.service.archive.extract(
                    archivePath,
                    groupPath,
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
