import { NahidaDesktop } from "..";
import { trim } from "es-toolkit";
import path from "path";
import fs from "fs/promises";
import { eq } from "drizzle-orm";
import { db } from "../internal/db";
import { gamePaths, modPresets, setting } from "../internal/db/schema";
import { nanoid } from "nanoid";

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
    ini?: {
        name: string;
        path: string;
    };
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

export class Mod {
    private desktop: NahidaDesktop;

    constructor(desktop: NahidaDesktop) {
        this.desktop = desktop;
    }

    private async parseIni(iniPath: string): Promise<ToggleKey[]> {
        try {
            const iniFileName = path.basename(iniPath);
            const content = await fs.readFile(iniPath, "utf-8");
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

    private async findPreview(modPath: string): Promise<string | null> {
        try {
            const files = await fs.readdir(modPath);
            const imageExtensions = [".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"];
            const videoExtensions = [".mp4", ".webm", ".avi", ".mkv", ".mov"];

            const imageFiles = files.filter((file) =>
                imageExtensions.some((ext) => file.toLowerCase().endsWith(ext)),
            );
            const videoFiles = files.filter((file) =>
                videoExtensions.some((ext) => file.toLowerCase().endsWith(ext)),
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

            const files = await fs.readdir(modPath);
            const iniFiles = files.filter(
                (f) => f.toLowerCase().endsWith(".ini") && !f.toLowerCase().startsWith("disabled"),
            );

            let toggleKeys: ToggleKey[] = [];
            for (const iniFile of iniFiles) {
                const iniPath = path.join(modPath, iniFile);
                const keys = await this.parseIni(iniPath);
                toggleKeys.push(...keys);
            }

            const preview = await this.findPreview(modPath);

            let selectedIni: string | undefined;
            if (iniFiles.length > 0) {
                const mergedIni = iniFiles.find(
                    (f) => f.toLowerCase().replace(".ini", "") === "merged",
                );
                selectedIni = mergedIni || iniFiles[0];
            }

            const ini = selectedIni
                ? {
                      name: selectedIni,
                      path: path.join(modPath, selectedIni),
                  }
                : undefined;

            return {
                name: folderName,
                path: modPath,
                isEnabled,
                toggleKeys,
                preview: preview || undefined,
                ini,
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
                const groupFolders = await fs.readdir(modFolderPath, { withFileTypes: true });
                const groups: FolderGroup[] = [];

                for (const groupFolder of groupFolders) {
                    if (!groupFolder.isDirectory()) continue;

                    const groupPath = path.join(modFolderPath, groupFolder.name);
                    const modFolders = await fs.readdir(groupPath, { withFileTypes: true });

                    const mods: ModInfo[] = [];
                    for (const modFolder of modFolders) {
                        if (!modFolder.isDirectory()) continue;

                        const modPath = path.join(groupPath, modFolder.name);
                        const modInfo = await this.scanModFolder(modPath);
                        if (modInfo) {
                            mods.push(modInfo);
                        }
                    }

                    const preview = await this.findPreview(groupPath);

                    groups.push({
                        name: groupFolder.name,
                        path: groupPath,
                        mods,
                        preview: preview || undefined,
                    });
                }

                return groups;
            } catch (error) {
                this.desktop.logger.error(error, `Mod:list:${game}`);
                throw error;
            }
        },

        scanGroup: async (groupPath: string): Promise<FolderGroup> => {
            try {
                const groupName = path.basename(groupPath);
                const modFolders = await fs.readdir(groupPath, { withFileTypes: true });

                const mods: ModInfo[] = [];
                for (const modFolder of modFolders) {
                    if (!modFolder.isDirectory()) continue;

                    const modPath = path.join(groupPath, modFolder.name);
                    const modInfo = await this.scanModFolder(modPath);
                    if (modInfo) {
                        mods.push(modInfo);
                    }
                }

                const preview = await this.findPreview(groupPath);

                return {
                    name: groupName,
                    path: groupPath,
                    mods,
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
                    await fs.access(newPath);
                    throw new Error(`ALREADY_EXISTS:${newFolderName}`);
                } catch (error: any) {
                    if (error.code === "ENOENT") {
                        await fs.rename(modPath, newPath);
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
                    await fs.access(newPath);
                    throw new Error(`ALREADY_EXISTS:${newFolderName}`);
                } catch (error: any) {
                    if (error.code === "ENOENT") {
                        await fs.rename(modPath, newPath);
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

        updateToggleKey: async (
            iniPath: string,
            sectionName: string,
            variable: string,
            value: string,
        ): Promise<void> => {
            try {
                const content = await fs.readFile(iniPath, "utf-8");
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
                    await fs.writeFile(iniPath, newLines.join("\n"), "utf-8");
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

            for (const modPath of modPaths) {
                try {
                    const folderName = path.basename(modPath);
                    const parentPath = path.dirname(modPath);
                    const isEnabledInPreset = !/^disabled\s+/i.test(folderName);

                    let actualModPath: string | null = null;

                    try {
                        await fs.access(modPath);
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
                            await fs.access(alternativePath);
                            actualModPath = alternativePath;
                        } catch {
                            this.desktop.logger.warn(
                                `Mod ${modPath} not found, skipping`,
                                "Mod:applyPreset",
                            );
                            continue;
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
            }
        },

        deletePreset: async (presetId: string): Promise<void> => {
            await db.delete(modPresets).where(eq(modPresets.id, presetId));
        },

        updatePresetName: async (presetId: string, newName: string): Promise<void> => {
            await db.update(modPresets).set({ name: newName }).where(eq(modPresets.id, presetId));
        },

        addGame: async (game: string, modFolderPath: string) => {
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
    };
}

export default Mod;
