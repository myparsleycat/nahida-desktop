import { NahidaDesktop } from "..";
import { trim } from "es-toolkit";
import path from "path";

import fse from "fs-extra";
import fg from "fast-glob";
import { eq } from "drizzle-orm";
import { db } from "../internal/db";
import { gamePaths, modPresets, setting } from "../internal/db/schema";
import { nanoid } from "nanoid";

const PREVIEW_EXTENSIONS = [
    ".png",
    ".jpg",
    ".jpeg",
    ".gif",
    ".webp",
    ".bmp",
    ".mp4",
    ".webm",
    ".avi",
    ".mkv",
    ".mov",
];
const MOD_FILE_EXTENSIONS = [".ini", ...PREVIEW_EXTENSIONS];
const MOD_FILE_GLOB = `**/*.{${MOD_FILE_EXTENSIONS.map((e) => e.slice(1)).join(",")}}`;

interface ToggleKey {
    sectionName: string;
    iniFileName: string;
    key?: string;
    back?: string;
    type?: string;
    variable: string;
    values: string[];
    currentValue?: string;
}

interface ModInfo {
    name: string;
    path: string;
    isEnabled: boolean;
    toggleKeys: ToggleKey[];
    preview?: string;
    mtime: number;
    size: number;
    inis: {
        name: string;
        path: string;
    }[];
}

interface FolderGroup {
    name: string;
    path: string;
    mods: ModInfo[];
    preview?: string;
    modCount?: number;
}

interface Preset {
    id: string;
    game: string;
    name: string;
    mods: string[];
}

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

    private async parseIni(iniPath: string): Promise<ToggleKey[]> {
        try {
            const iniFileName = path.basename(iniPath);
            const content = await fse.readFile(iniPath, "utf-8");
            const toggleKeys: ToggleKey[] = [];
            const lines = content.split("\n");

            let currentSection: string | null = null;
            let sectionData: any = {};

            for (let line of lines) {
                line = line.trim();

                if (line.startsWith("[") && line.endsWith("]")) {
                    if (currentSection && currentSection.toLowerCase().startsWith("key")) {
                        const toggleKey = this.extractToggleKey(
                            currentSection,
                            sectionData,
                            iniFileName,
                        );
                        if (toggleKey) {
                            toggleKeys.push(toggleKey);
                        }
                    }

                    currentSection = line.slice(1, -1);
                    sectionData = {};
                    continue;
                }

                if (currentSection && line.includes("=")) {
                    const [key, ...valueParts] = line.split("=");
                    const value = valueParts.join("=").trim();
                    sectionData[key.trim()] = value;
                }
            }

            if (currentSection && currentSection.toLowerCase().startsWith("key")) {
                const toggleKey = this.extractToggleKey(currentSection, sectionData, iniFileName);
                if (toggleKey) {
                    toggleKeys.push(toggleKey);
                }
            }

            return toggleKeys;
        } catch (error) {
            this.desktop.logger.error(error, `Mod:parseIni:${iniPath}`);
            return [];
        }
    }

    private extractToggleKey(
        sectionName: string,
        data: any,
        iniFileName: string,
    ): ToggleKey | null {
        const entries = Object.entries(data);
        const variableEntry = entries.find(([key]) => key.startsWith("$"));
        if (!variableEntry) return null;

        const [variable, valuesStr] = variableEntry;
        const values = (valuesStr as string).split(",").map((v) => v.trim());

        if (values.length < 2) return null;

        const getCaseInsensitive = (obj: any, target: string) => {
            const key = Object.keys(obj).find((k) => k.toLowerCase() === target.toLowerCase());
            return key ? obj[key] : undefined;
        };

        return {
            sectionName,
            iniFileName,
            key: getCaseInsensitive(data, "key"),
            back: getCaseInsensitive(data, "back"),
            type: getCaseInsensitive(data, "type"),
            variable,
            values,
            currentValue: values[0],
        };
    }

    private async findPreview(modPath: string, files?: string[]): Promise<string | null> {
        try {
            if (!files) {
                files = await fg(
                    PREVIEW_EXTENSIONS.map((ext) => `**/*${ext}`),
                    {
                        cwd: modPath,
                        onlyFiles: true,
                        caseSensitiveMatch: false,
                        dot: true,
                    },
                );
            }

            const imageExtensions = [".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"];
            const videoExtensions = [".mp4", ".webm", ".avi", ".mkv", ".mov"];
            const excludedKeywords = ["normal", "light", "material", "diffuse"];

            const isExcludedFile = (file: string): boolean => {
                const lowerFilename = path.basename(file).toLowerCase();
                if (lowerFilename.includes("preview")) return false;
                return excludedKeywords.some((keyword) => lowerFilename.includes(keyword));
            };

            const getScore = (file: string): number => {
                const lowerFile = file.toLowerCase();
                const filename = path.basename(lowerFile);
                const isRoot = !file.includes("/") && !file.includes("\\");
                const isVideo = videoExtensions.some((ext) => lowerFile.endsWith(ext));

                let score = 0;

                if (filename.startsWith("preview")) {
                    score += 1000;
                } else if (filename.includes("preview")) {
                    score += 500;
                }

                if (isRoot) score += 200;

                if (isVideo) score += 10;

                return score;
            };

            const isMediaFile = (file: string): boolean => {
                const lowerFile = file.toLowerCase();
                return (
                    imageExtensions.some((ext) => lowerFile.endsWith(ext)) ||
                    videoExtensions.some((ext) => lowerFile.endsWith(ext))
                );
            };

            const candidateFiles = files.filter(
                (file) => isMediaFile(file) && !isExcludedFile(file),
            );

            if (candidateFiles.length === 0) return null;

            candidateFiles.sort((a, b) => {
                const scoreA = getScore(a);
                const scoreB = getScore(b);

                if (scoreA !== scoreB) {
                    return scoreB - scoreA;
                }

                return a.localeCompare(b, undefined, { numeric: true, sensitivity: "base" });
            });

            return path.join(modPath, candidateFiles[0]);
        } catch (error) {
            this.desktop.logger.error(error, `Mod:findPreview:${modPath}`);
            return null;
        }
    }

    private isModEnabled(folderName: string): boolean {
        return !/^disabled\s+/i.test(folderName);
    }

    private async scanModFolder(modPath: string): Promise<ModInfo | null> {
        try {
            const folderName = path.basename(modPath);
            const isEnabled = this.isModEnabled(folderName);

            const files = (await fg(MOD_FILE_GLOB, {
                cwd: modPath,
                onlyFiles: true,
                caseSensitiveMatch: false,
                dot: true,
                stats: true,
            })) as any[];

            let maxMtime = 0;

            for (const file of files) {
                if (file.stats.mtimeMs > maxMtime) {
                    maxMtime = file.stats.mtimeMs;
                }
            }

            const mtime = maxMtime || (await fse.stat(modPath)).mtimeMs;
            const size = await this.desktop.lib.fs.getFolderSize(modPath);

            const collator = new Intl.Collator(undefined, {
                numeric: true,
                sensitivity: "base",
            });

            const rawIniFiles = files
                .map((f) => f.name)
                .filter(
                    (f) =>
                        f.toLowerCase().endsWith(".ini") && !f.toLowerCase().startsWith("disabled"),
                );

            const iniData = await Promise.all(
                rawIniFiles.map(async (iniFile) => {
                    const iniPath = path.join(modPath, iniFile);
                    const toggleKeys = await this.parseIni(iniPath);
                    toggleKeys.sort((a, b) => {
                        if (a.key && !b.key) return -1;
                        if (!a.key && b.key) return 1;
                        return 0;
                    });
                    return {
                        name: iniFile,
                        path: iniPath,
                        toggleKeys,
                        hasToggleKey: toggleKeys.some((tk) => !!tk.key),
                    };
                }),
            );

            iniData.sort((a, b) => {
                if (a.hasToggleKey && !b.hasToggleKey) return -1;
                if (!a.hasToggleKey && b.hasToggleKey) return 1;
                return collator.compare(a.name, b.name);
            });

            const toggleKeys = iniData.flatMap((d) => d.toggleKeys);

            const preview = await this.findPreview(
                modPath,
                files.map((f) => f.name),
            );

            const inis = iniData.map((d) => ({
                name: d.name,
                path: d.path,
            }));

            return {
                name: folderName,
                path: modPath,
                isEnabled,
                toggleKeys,
                preview: preview || undefined,
                mtime,
                size,
                inis,
            };
        } catch (error) {
            this.desktop.logger.error(error, `Mod:scanModFolder:${modPath}`);
            return null;
        }
    }

    get = {
        gamePath: async (game: string): Promise<string | null> => {
            const result = await db.query.gamePaths.findFirst({
                where: eq(gamePaths.game, game),
            });
            return result?.modFolderPath || null;
        },

        characters: async (game: string): Promise<FolderGroup[]> => {
            const modFolderPath = await this.get.gamePath(game);
            if (!modFolderPath) {
                throw new Error(`No mod folder path set for ${game}`);
            }

            try {
                const groupFolders = await fg("*", {
                    cwd: modFolderPath,
                    onlyDirectories: true,
                });

                const groups = await Promise.all(
                    groupFolders.map(async (groupFolderName) => {
                        const groupPath = path.join(modFolderPath, groupFolderName);
                        const modFolders = await fg("*", {
                            cwd: groupPath,
                            onlyDirectories: true,
                        });

                        const preview = await this.findPreview(groupPath);

                        return {
                            name: groupFolderName,
                            path: groupPath,
                            mods: [],
                            preview: preview || undefined,
                            modCount: modFolders.length,
                        };
                    }),
                );

                return groups;
            } catch (error) {
                this.desktop.logger.error(error, `Mod:characters:${game}`);
                throw error;
            }
        },

        mods: async (groupPath: string): Promise<FolderGroup> => {
            try {
                const groupName = path.basename(groupPath);
                const modFolders = await fg("*", {
                    cwd: groupPath,
                    onlyDirectories: true,
                });

                const modsPromises = modFolders.map((modFolderName) => {
                    const modPath = path.join(groupPath, modFolderName);
                    return this.scanModFolder(modPath);
                });

                const [mods, preview] = await Promise.all([
                    Promise.all(modsPromises),
                    this.findPreview(groupPath),
                ]);

                const validMods = mods.filter((m): m is ModInfo => m !== null);

                return {
                    name: groupName,
                    path: groupPath,
                    mods: validMods,
                    preview: preview || undefined,
                    modCount: validMods.length,
                };
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
                } catch (error: any) {
                    if (error.code === "ENOENT") {
                        await fse.rename(modPath, newPath);
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
                } catch (error: any) {
                    if (error.code === "ENOENT") {
                        await fse.rename(modPath, newPath);
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

                for (let line of lines) {
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
                    await fse.writeFile(iniPath, newLines.join("\n"), "utf-8");
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
