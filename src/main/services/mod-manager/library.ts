import { getCharactersFolder, getMods } from "@native/native-mod";
import type { FolderGroup, Preset } from "@shared/types";
import { GAME_MATCH_CASES } from "@shared/xxmi-match";
import { and, eq, ne } from "drizzle-orm";
import type { NahidaDesktop } from "../..";
import { gamePaths, modPresets, setting } from "../../internal/db/schema";

const MOD_PRESET_VERSION = 2;

export class ModLibraryService {
    constructor(private readonly desktop: NahidaDesktop) {}

    public async gamePath(game: string): Promise<string | null> {
        const result = await this.desktop.lib.db.query.gamePaths.findFirst({
            where: eq(gamePaths.game, game),
        });
        return result?.modFolderPath || null;
    }

    public async characters(game: string, searchModPreview?: boolean): Promise<FolderGroup[]> {
        const modFolderPath = await this.gamePath(game);
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
    }

    public async subGroups(folderPath: string, searchModPreview?: boolean): Promise<FolderGroup[]> {
        const shouldFallback =
            searchModPreview ?? (await this.desktop.setting.mod.getSearchModPreview());
        try {
            return await getCharactersFolder(folderPath, shouldFallback);
        } catch (error) {
            this.desktop.logger.error(error, `Mod:subGroups:${folderPath}`);
            throw error;
        }
    }

    public async mods(groupPath: string): Promise<FolderGroup> {
        try {
            return await getMods(groupPath);
        } catch (error) {
            this.desktop.logger.error(error, `Mod:mods:${groupPath}`);
            throw error;
        }
    }

    public async presets(game: string): Promise<Preset[]> {
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
    }

    public async games() {
        return await this.desktop.lib.db.select().from(gamePaths);
    }

    public async lastGame(): Promise<string | null> {
        const result = await this.desktop.lib.db.query.setting.findFirst({
            where: eq(setting.key, "last_game"),
        });
        return result?.value || null;
    }

    public async expandedGroups(): Promise<string[]> {
        const result = await this.desktop.lib.db.query.setting.findFirst({
            where: eq(setting.key, "expanded_groups"),
        });
        if (!result?.value) return [];
        try {
            return JSON.parse(result.value) as string[];
        } catch {
            return [];
        }
    }

    public async previousFocusedGame(): Promise<string | null> {
        try {
            const currentPid = process.pid;

            let currentProcessName = this.desktop.lib.native.getProcessName(currentPid);
            if (currentProcessName) currentProcessName = currentProcessName.toLowerCase();

            const previousPids = this.desktop.lib.native.getPreviousPids(currentPid);
            if (previousPids.length === 0) return null;

            const games = await this.games();

            for (const pid of previousPids) {
                const processName = this.desktop.lib.native.getProcessName(pid);
                if (!processName) continue;

                const lowerProcessName = processName.toLowerCase();

                if (currentProcessName && lowerProcessName.includes(currentProcessName)) continue;
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
    }

    public async gamePid(game: string): Promise<number | null> {
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
    }

    public async setGamePath(game: string, modFolderPath: string) {
        await this.desktop.lib.db
            .insert(gamePaths)
            .values({ game, modFolderPath, importer: null })
            .onConflictDoUpdate({
                target: gamePaths.game,
                set: { modFolderPath },
            });
    }

    public async addGame(game: string, modFolderPath: string, importer: string | null) {
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

        await this.desktop.lib.db.insert(gamePaths).values({ game, modFolderPath, importer });
    }

    public async updateGame(
        game: string,
        updates: {
            modFolderPath: string;
            importer: string | null;
        },
    ) {
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
    }

    public async removeGame(game: string) {
        await this.desktop.lib.db.delete(gamePaths).where(eq(gamePaths.game, game));
    }

    public async setLastGame(game: string) {
        await this.desktop.lib.db
            .insert(setting)
            .values({ key: "last_game", value: game })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value: game },
            });
    }

    public async setExpandedGroups(paths: string[]) {
        const value = JSON.stringify(paths);
        await this.desktop.lib.db
            .insert(setting)
            .values({ key: "expanded_groups", value })
            .onConflictDoUpdate({
                target: setting.key,
                set: { value },
            });
    }
}
