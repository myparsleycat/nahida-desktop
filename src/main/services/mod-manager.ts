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
const MOD_FILE_GLOB = `*.{${MOD_FILE_EXTENSIONS.map((e) => e.slice(1)).join(",")}}`;

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
}

interface Preset {
    id: string;
    game: string;
    name: string;
    mods: string[];
}

export class ModManager {
    private desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
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
                    if (currentSection && currentSection.startsWith("Key")) {
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

            if (currentSection && currentSection.startsWith("Key")) {
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
        const variableEntry = Object.entries(data).find(([key]) => key.startsWith("$"));
        if (!variableEntry) return null;

        const [variable, valuesStr] = variableEntry;
        const values = (valuesStr as string).split(",").map((v) => v.trim());

        if (values.length < 2) return null;

        return {
            sectionName,
            iniFileName,
            key: data.key,
            back: data.back,
            type: data.type,
            variable,
            values,
            currentValue: values[0],
        };
    }

    private async findPreview(modPath: string, files?: string[]): Promise<string | null> {
        try {
            if (!files) {
                files = await fg(
                    PREVIEW_EXTENSIONS.map((ext) => `*${ext}`),
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

            const isExcludedFile = (filename: string): boolean => {
                const lowerFilename = filename.toLowerCase();
                return excludedKeywords.some((keyword) => lowerFilename.includes(keyword));
            };

            const imageFiles = files.filter(
                (file) =>
                    imageExtensions.some((ext) => file.toLowerCase().endsWith(ext)) &&
                    !isExcludedFile(file),
            );
            const videoFiles = files.filter(
                (file) =>
                    videoExtensions.some((ext) => file.toLowerCase().endsWith(ext)) &&
                    !isExcludedFile(file),
            );

            // 프리뷰 붙은 영상
            const previewVideoFile = videoFiles.find((file) =>
                file.toLowerCase().includes("preview"),
            );
            if (previewVideoFile) return path.join(modPath, previewVideoFile);

            // 프리뷰 붙은 이미지
            const previewImageFile = imageFiles.find((file) =>
                file.toLowerCase().includes("preview"),
            );
            if (previewImageFile) return path.join(modPath, previewImageFile);

            // 첫번째 영상
            if (videoFiles.length > 0) return path.join(modPath, videoFiles[0]);

            // 첫번째 이미지
            if (imageFiles.length > 0) return path.join(modPath, imageFiles[0]);

            return null;
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

            const files = await fg(MOD_FILE_GLOB, {
                cwd: modPath,
                onlyFiles: true,
                caseSensitiveMatch: false,
                dot: true,
            });

            const iniFiles = files.filter(
                (f) => f.toLowerCase().endsWith(".ini") && !f.toLowerCase().startsWith("disabled"),
            );

            const toggleKeysPromises = iniFiles.map((iniFile) => {
                const iniPath = path.join(modPath, iniFile);
                return this.parseIni(iniPath);
            });
            const toggleKeysResults = await Promise.all(toggleKeysPromises);
            const toggleKeys = toggleKeysResults.flat();

            const preview = await this.findPreview(modPath, files);

            const inis = iniFiles.map((iniFile) => ({
                name: iniFile,
                path: path.join(modPath, iniFile),
            }));

            return {
                name: folderName,
                path: modPath,
                isEnabled,
                toggleKeys,
                preview: preview || undefined,
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

        list: async (game: string): Promise<FolderGroup[]> => {
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

                        const modsPromises = modFolders.map((modFolderName) => {
                            const modPath = path.join(groupPath, modFolderName);
                            return this.scanModFolder(modPath);
                        });

                        const [mods, preview] = await Promise.all([
                            Promise.all(modsPromises),
                            this.findPreview(groupPath),
                        ]);

                        return {
                            name: groupFolderName,
                            path: groupPath,
                            mods: mods.filter((m): m is ModInfo => m !== null),
                            preview: preview || undefined,
                        };
                    }),
                );

                return groups;
            } catch (error) {
                this.desktop.logger.error(error, `Mod:list:${game}`);
                throw error;
            }
        },

        scanGroup: async (groupPath: string): Promise<FolderGroup> => {
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

                return {
                    name: groupName,
                    path: groupPath,
                    mods: mods.filter((m): m is ModInfo => m !== null),
                    preview: preview || undefined,
                };
            } catch (error) {
                this.desktop.logger.error(error, `Mod:scanGroup:${groupPath}`);
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

                    if (currentSection === sectionName && trimmedLine.startsWith(variable + " =")) {
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

        createPreset: async (game: string, name: string, modPaths: string[]): Promise<Preset> => {
            const id = nanoid();
            const preset: Preset = {
                id,
                game,
                name,
                mods: modPaths,
            };

            await db.insert(modPresets).values({
                id,
                game,
                name,
                mods: JSON.stringify(modPaths),
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
