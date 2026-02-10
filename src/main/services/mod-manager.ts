import path from "node:path";
import { getCharactersFolder, getMods } from "@native/native-mod";
import type { FolderGroup, Preset } from "@shared/types.gen";
import { eq } from "drizzle-orm";
import { trim } from "es-toolkit";
import fg from "fast-glob";
import fse from "fs-extra";
import { nanoid } from "nanoid";
import type { NahidaDesktop } from "..";
import { db } from "../internal/db";
import { gamePaths, modPresets, setting } from "../internal/db/schema";

export class ModManager {
    private desktop: NahidaDesktop;
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
                    if (event === "add" || event === "unlink") {
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
                    if (event === "update" || event === "unlink") {
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

    get = {
        gamePath: async (game: string): Promise<string | null> => {
            const result = await db.query.gamePaths.findFirst({
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
                return getCharactersFolder(modFolderPath, shouldFallback);
            } catch (error) {
                this.desktop.logger.error(error, `Mod:characters:${game}`);
                throw error;
            }
        },

        mods: async (groupPath: string): Promise<FolderGroup> => {
            try {
                return getMods(groupPath);
            } catch (error) {
                this.desktop.logger.error(error, `Mod:mods:${groupPath}`);
                throw error;
            }
        },

        presets: async (game: string): Promise<Preset[]> => {
            const results = await db.query.modPresets.findMany({
                where: eq(modPresets.game, game),
            });

            return results.map((r) => ({
                id: r.id,
                game: r.game,
                name: r.name,
                mods: JSON.parse(r.mods),
            }));
        },

        games: async () => {
            return await db.select().from(gamePaths);
        },

        lastGame: async (): Promise<string | null> => {
            const result = await db.query.setting.findFirst({
                where: eq(setting.key, "last_game"),
            });
            return result?.value || null;
        },

        previousFocusedGame: async (): Promise<string | null> => {
            try {
                const currentPid = process.pid;

                let currentProcessName = this.desktop.lib.native.getProcessName(currentPid);
                if (currentProcessName) currentProcessName = currentProcessName.toLowerCase();

                const previousPids = this.desktop.lib.native.getPreviousPids(currentPid);
                if (previousPids.length === 0) return null;

                const games = await this.get.games();

                const genshinCase = ["원신", "genshin", "gimi"];
                const starrailCase = ["스타레일", "붕스", "열차", "starrail", "srmi"];
                const zenlessCase = ["젠레스", "젠존제", "찢", "zzz", "zenless", "zzmi"];
                const wuwaCase = ["명조", "묑조", "wuwa", "wuthering", "wwmi"];
                const endfieldCase = ["엔드필드", "엔필", "endfield", "efmi"];

                const allCases = [genshinCase, starrailCase, zenlessCase, wuwaCase, endfieldCase];

                for (const pid of previousPids) {
                    const processName = this.desktop.lib.native.getProcessName(pid);
                    if (!processName) continue;

                    const lowerProcessName = processName.toLowerCase();

                    if (currentProcessName && lowerProcessName.includes(currentProcessName))
                        continue;
                    if (lowerProcessName.includes("explorer")) continue;

                    for (const gameCase of allCases) {
                        const isGameProcess = gameCase.some((k) => lowerProcessName.includes(k));

                        if (isGameProcess) {
                            const matchedGame = games.find((g) => {
                                const lowerGame = g.game.toLowerCase();
                                return gameCase.some((k) => lowerGame.includes(k));
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
    };

    fn = {
        setGamePath: async (game: string, modFolderPath: string) => {
            await db.insert(gamePaths).values({ game, modFolderPath }).onConflictDoUpdate({
                target: gamePaths.game,
                set: { modFolderPath },
            });
        },

        enable: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const regex = /^disabled\s+/i;

            if (regex.test(folderName)) {
                const newFolderName = trim(folderName.replace(regex, ""));
                const newPath = path.join(path.dirname(modPath), newFolderName);

                try {
                    await fse.access(newPath);
                    throw new Error(`ALREADY_EXISTS:${newFolderName}`);
                } catch (error) {
                    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
                        await this.desktop.lib.fs.rename(modPath, newPath);
                        return newPath;
                    }
                    throw error;
                }
            }

            return modPath;
        },

        disable: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const regex = /^disabled\s+/i;

            if (!regex.test(folderName)) {
                const newFolderName = `DISABLED ${folderName}`;
                const newPath = path.join(path.dirname(modPath), newFolderName);

                try {
                    await fse.access(newPath);
                    throw new Error(`ALREADY_EXISTS:${newFolderName}`);
                } catch (error) {
                    if ((error as NodeJS.ErrnoException).code === "ENOENT") {
                        await this.desktop.lib.fs.rename(modPath, newPath);
                        return newPath;
                    }
                    throw error;
                }
            }

            return modPath;
        },

        toggle: async (modPath: string): Promise<string> => {
            const folderName = path.basename(modPath);
            const isEnabled = !/^disabled\s+/i.test(folderName);

            if (isEnabled) {
                return await this.fn.disable(modPath);
            } else {
                return await this.fn.enable(modPath);
            }
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
                return await this.fn.enable(modPath);
            } else {
                return await this.fn.disable(modPath);
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

                let currentSection: string | null = null;
                let updated = false;

                for (const line of lines) {
                    const trimmedLine = line.trim();

                    if (trimmedLine.startsWith("[") && trimmedLine.endsWith("]")) {
                        currentSection = trimmedLine.slice(1, -1);
                        newLines.push(line);
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
                        newLines.push(`${variable} = ${value}`);
                        updated = true;
                    } else {
                        newLines.push(line);
                    }
                }

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

        createPreset: async (game: string, name: string): Promise<Preset> => {
            const enabledMods: string[] = [];

            const characterGroups = await this.get.characters(game);
            for (const charGroup of characterGroups) {
                const fullGroup = await this.get.mods(charGroup.path);
                for (const mod of fullGroup.mods) {
                    if (mod.isEnabled) {
                        enabledMods.push(mod.path);
                    }
                }
            }

            const id = nanoid();
            const preset: Preset = {
                id,
                game,
                name,
                mods: enabledMods,
            };

            await db.insert(modPresets).values({
                id,
                game,
                name,
                mods: JSON.stringify(enabledMods),
            });

            return preset;
        },

        applyPreset: async (presetId: string): Promise<void> => {
            const preset = await db.query.modPresets.findFirst({
                where: eq(modPresets.id, presetId),
            });

            if (!preset) {
                throw new Error(`Preset ${presetId} not found`);
            }

            const modPaths = JSON.parse(preset.mods) as string[];

            await Promise.all(
                modPaths.map(async (modPath) => {
                    try {
                        const folderName = path.basename(modPath);
                        const parentPath = path.dirname(modPath);
                        const isEnabledInPreset = !/^disabled\s+/i.test(folderName);

                        let actualModPath: string | null = null;

                        try {
                            await fse.access(modPath);
                            actualModPath = modPath;
                        } catch {
                            let alternativePath: string;
                            if (isEnabledInPreset) {
                                alternativePath = path.join(parentPath, `DISABLED ${folderName}`);
                            } else {
                                const regex = /^disabled\s+/i;
                                const cleanName = trim(folderName.replace(regex, ""));
                                alternativePath = path.join(parentPath, cleanName);
                            }

                            try {
                                await fse.access(alternativePath);
                                actualModPath = alternativePath;
                            } catch {
                                this.desktop.logger.warn(
                                    `Mod ${modPath} not found, skipping`,
                                    "Mod:applyPreset",
                                );
                                return;
                            }
                        }

                        if (actualModPath) {
                            if (isEnabledInPreset) {
                                await this.fn.enable(actualModPath);
                            } else {
                                await this.fn.disable(actualModPath);
                            }
                        }
                    } catch (error) {
                        this.desktop.logger.error(error, `Mod:applyPreset:${modPath}`);
                    }
                }),
            );
        },

        deletePreset: async (presetId: string): Promise<void> => {
            await db.delete(modPresets).where(eq(modPresets.id, presetId));
        },

        updatePresetName: async (presetId: string, newName: string): Promise<void> => {
            await db.update(modPresets).set({ name: newName }).where(eq(modPresets.id, presetId));
        },

        addGame: async (game: string, modFolderPath: string) => {
            if (!game || !modFolderPath) {
                throw new Error("Game and modFolderPath are required");
            }
            await db.insert(gamePaths).values({ game, modFolderPath }).onConflictDoUpdate({
                target: gamePaths.game,
                set: { modFolderPath },
            });
        },

        removeGame: async (game: string) => {
            await db.delete(gamePaths).where(eq(gamePaths.game, game));
            await db.delete(modPresets).where(eq(modPresets.game, game));
        },

        setLastGame: async (game: string) => {
            await db
                .insert(setting)
                .values({ key: "last_game", value: game })
                .onConflictDoUpdate({
                    target: setting.key,
                    set: { value: game },
                });
        },

        extractArchiveToGroup: async (archivePath: string, groupPath: string): Promise<void> => {
            const deleteAfterExtract =
                await this.desktop.setting.mod.getDeleteArchiveAfterExtract();

            const tempDir = path.join(groupPath, `.tmp_${nanoid()}`);
            await fse.ensureDir(tempDir);

            try {
                await this.desktop.service.archive.extract(archivePath, tempDir);

                let sourcePath = tempDir;
                let targetFolderName = path.basename(archivePath, path.extname(archivePath));

                let currentPath = tempDir;
                while (true) {
                    const items = await fse.readdir(currentPath);
                    const validItems = items.filter((item) => {
                        const lower = item.toLowerCase();
                        return ![".ds_store", "__macosx", "desktop.ini", "thumbs.db"].includes(
                            lower,
                        );
                    });

                    if (validItems.length === 1) {
                        const singleItemPath = path.join(currentPath, validItems[0]);
                        const stats = await fse.stat(singleItemPath);
                        if (stats.isDirectory()) {
                            currentPath = singleItemPath;
                            targetFolderName = validItems[0];
                            continue;
                        }
                    }
                    break;
                }
                sourcePath = currentPath;

                const finalTargetPath = path.join(groupPath, targetFolderName);

                if (await fse.pathExists(finalTargetPath)) {
                    throw new Error(`ALREADY_EXISTS:${targetFolderName}`);
                }

                await fse.move(sourcePath, finalTargetPath);

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
            } finally {
                await fse.remove(tempDir);
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
    };
}

export default ModManager;
