import { spawn } from "node:child_process";
import path from "node:path";

import type { GamePathRow } from "@main/internal/db/schema";
import { getCharactersFolder, getMods } from "@native/mod-manager";
import { findBestFuzzyMatch } from "@shared/fuzzy-match";
import { isNteImporter } from "@shared/mod";
import type { FolderGroup, NteBootstrapProgress, Preset } from "@shared/types";
import { GAME_MATCH_CASES } from "@shared/xxmi-match";
import fse from "fs-extra";

import type { NahidaDesktop } from "../..";

import {
    cleanupNteModFolder,
    configureNteModFolder,
    deriveNteGameInstallPath,
    ensureNteBootstrapFiles,
    findNteGameByPath,
    hasNtePathChanges,
    getNteCharacters,
    getNteMods,
    getNteRoots,
    getNteSubGroups,
    resolveNteInstallPath,
} from "./nte";
import {
    folderHasAnyFile,
    manualSubGroupPathExists,
    manualSubGroupRelativePath,
    resolveManualSubGroupDiskPaths,
} from "./path-utils";

const MOD_PRESET_VERSION = 2;
const MANUAL_SUBGROUPS_SETTING_KEY = "manual_subgroups";
const DOWNLOAD_TARGET_MATCH_THRESHOLD = 0.85;

type ManualSubGroups = Record<string, string[]>;

export class ModLibraryService {
    private manualSubGroupWriteLock = Promise.resolve();

    constructor(private readonly desktop: NahidaDesktop) {}

    private broadcastNteBootstrapProgress(payload: NteBootstrapProgress) {
        this.desktop.ipc.broadcast("mod:nte-bootstrap-progress", payload);
    }

    public async gamePath(game: string): Promise<string | null> {
        const result = await this.desktop.lib.db.gamePaths.getByGame(game);
        return result?.modFolderPath || null;
    }

    public async characters(game: string, searchModPreview?: boolean): Promise<FolderGroup[]> {
        const gameConfig = await this.desktop.lib.db.gamePaths.getByGame(game);
        if (!gameConfig?.modFolderPath) {
            throw new Error(`No mod folder path set for ${game}`);
        }

        if (isNteImporter(gameConfig.importer)) {
            return await getNteCharacters(this.desktop, getNteRoots(gameConfig));
        }

        const shouldFallback =
            searchModPreview ?? (await this.desktop.setting.mod.getSearchModPreview());

        try {
            return await this.addManualSubGroupFlags(
                game,
                "",
                await getCharactersFolder(gameConfig.modFolderPath, shouldFallback),
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:characters:${game}`);
            throw error;
        }
    }

    public async subGroups(folderPath: string, searchModPreview?: boolean): Promise<FolderGroup[]> {
        const shouldFallback =
            searchModPreview ?? (await this.desktop.setting.mod.getSearchModPreview());
        try {
            const game = await this.getGameByPath(folderPath);
            if (game && isNteImporter(game.importer)) {
                return await getNteSubGroups(this.desktop, getNteRoots(game), folderPath);
            }

            const relativePath = game
                ? manualSubGroupRelativePath(path.relative(game.modFolderPath, folderPath))
                : "";

            return await this.addManualSubGroupFlags(
                game?.game ?? "",
                relativePath,
                await getCharactersFolder(folderPath, shouldFallback),
            );
        } catch (error) {
            this.desktop.logger.error(error, `Mod:subGroups:${folderPath}`);
            throw error;
        }
    }

    public async manualSubGroups(
        folderPath: string,
        searchModPreview?: boolean,
    ): Promise<FolderGroup[]> {
        const shouldFallback =
            searchModPreview ?? (await this.desktop.setting.mod.getSearchModPreview());
        try {
            const game = await this.getGameByPath(folderPath);
            if (!game) return [];

            const relativePath = manualSubGroupRelativePath(
                path.relative(game.modFolderPath, folderPath),
            );
            const manualChildPaths = await this.getManualChildPaths(game.game, relativePath);
            if (manualChildPaths.size === 0) return [];

            return (await this.subGroups(folderPath, shouldFallback))
                .filter((group) =>
                    manualChildPaths.has(
                        manualSubGroupRelativePath(
                            path.join(relativePath, path.basename(group.path)),
                        ),
                    ),
                )
                .map((group) => ({
                    ...group,
                    isManualSubGroup: true,
                }));
        } catch (error) {
            this.desktop.logger.error(error, `Mod:manualSubGroups:${folderPath}`);
            throw error;
        }
    }

    public async mods(groupPath: string): Promise<FolderGroup> {
        try {
            const game = await this.getGameByPath(groupPath);
            if (game && isNteImporter(game.importer)) {
                return await getNteMods(this.desktop, getNteRoots(game), groupPath);
            }

            const group = await getMods(groupPath);
            if (!game) return group;

            const relativePath = manualSubGroupRelativePath(
                path.relative(game.modFolderPath, groupPath),
            );
            const mods = await this.filterManualSubGroupMods(game.game, relativePath, group.mods);
            return {
                ...group,
                hasManualSubGroups: await this.hasManualChildren(game.game, relativePath),
                mods,
                modCount: mods.length,
            };
        } catch (error) {
            this.desktop.logger.error(error, `Mod:mods:${groupPath}`);
            throw error;
        }
    }

    public async presets(game: string): Promise<Preset[]> {
        const results = await this.desktop.lib.db.modPresets.listByGame(game);

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
        const rows = await this.desktop.lib.db.gamePaths.list();
        return rows.map((row) => this.enrichGameConfig(row));
    }

    private enrichGameConfig(row: GamePathRow) {
        if (!isNteImporter(row.importer) || row.gameInstallPath) {
            return row;
        }

        return {
            ...row,
            gameInstallPath: deriveNteGameInstallPath(row.linkedModFolderPath ?? row.modFolderPath),
        };
    }

    public async resolveDownloadTarget(
        input: string,
        gameFilter?: string,
    ): Promise<{
        game: string;
        group: FolderGroup;
        score: number;
    } | null> {
        const allGames = await this.games();
        const gamesToSearch = gameFilter
            ? allGames.filter((game) => game.game === gameFilter)
            : allGames;

        const candidates = (
            await Promise.all(
                gamesToSearch.map(async (game) =>
                    (
                        await this.characters(game.game).catch((error) => {
                            this.desktop.logger.error(
                                error,
                                `Mod:resolveDownloadTarget:characters:${game.game}`,
                            );
                            return [];
                        })
                    ).map((group) => ({ game: game.game, group })),
                ),
            )
        ).flat();
        const match = findBestFuzzyMatch(
            candidates.map((candidate) => candidate.group.name),
            input,
            { threshold: DOWNLOAD_TARGET_MATCH_THRESHOLD },
        );
        if (!match) return null;

        const matchingCandidates = candidates.filter(
            (candidate) => candidate.group.name === match.value,
        );
        if (matchingCandidates.length !== 1) return null;
        const candidate = matchingCandidates[0];

        return {
            game: candidate.game,
            group: candidate.group,
            score: match.score,
        };
    }

    public async lastGame(): Promise<string | null> {
        return await this.desktop.lib.db.settings.getValue("last_game");
    }

    public async expandedGroups(): Promise<string[]> {
        const value = await this.desktop.lib.db.settings.getValue("expanded_groups");
        if (!value) return [];
        try {
            return JSON.parse(value) as string[];
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
        const existing = await this.desktop.lib.db.gamePaths.getByGame(game);

        if (existing && isNteImporter(existing.importer)) {
            await cleanupNteModFolder(existing.modFolderPath, existing.linkedModFolderPath);
        }

        await this.desktop.lib.db.gamePaths.upsert({
            game,
            modFolderPath,
            importer: null,
            linkedModFolderPath: null,
            gameInstallPath: null,
            gameExecutablePath: null,
            nteLauncherPath: null,
            order: existing?.order ?? 0,
        });
    }

    public async addGame(
        game: string,
        modFolderPath: string,
        importer: string | null,
        linkedModFolderPath: string | null = null,
        gameInstallPath: string | null = null,
        gameExecutablePath: string | null = null,
    ) {
        if (!game || !modFolderPath) {
            throw new Error("INVALID_PARAMS");
        }

        const resolvedGameInstallPath = isNteImporter(importer) ? gameInstallPath : null;
        const resolvedGameExecutablePath = isNteImporter(importer) ? gameExecutablePath : null;

        const exists =
            (await this.desktop.lib.db.gamePaths.findByGameOrModFolderPath(game, modFolderPath)) ??
            (linkedModFolderPath
                ? await this.desktop.lib.db.gamePaths.findByGameOrModFolderPath(
                      game,
                      linkedModFolderPath,
                  )
                : null);

        if (exists) {
            if (exists.game === game) {
                throw new Error("DUPLICATE_GAME_NAME");
            }
            throw new Error("DUPLICATE_MOD_FOLDER_PATH");
        }

        const diskRollbacks: Array<() => Promise<void>> = [];
        let addGameStep = "insert-game-path";
        let nteBootstrapExecutablePath: string | null = null;

        try {
            if (isNteImporter(importer)) {
                addGameStep = "configure-nte-mod-folder";
                await configureNteModFolder(modFolderPath, linkedModFolderPath);
                diskRollbacks.push(() => cleanupNteModFolder(modFolderPath, linkedModFolderPath));
                addGameStep = "resolve-nte-bootstrap-executable-path";
                nteBootstrapExecutablePath = await this.resolveNteBootstrapExecutablePath({
                    modFolderPath,
                    linkedModFolderPath,
                    gameInstallPath: resolvedGameInstallPath,
                });
                addGameStep = "ensure-nte-bootstrap-files";
                const bootstrapRollback = await ensureNteBootstrapFiles(
                    this.desktop,
                    nteBootstrapExecutablePath,
                    this.broadcastNteBootstrapProgress.bind(this),
                );
                if (bootstrapRollback) {
                    diskRollbacks.push(bootstrapRollback);
                }
            }

            addGameStep = "insert-game-path";
            await this.desktop.lib.db.gamePaths.insert({
                game,
                modFolderPath,
                importer,
                linkedModFolderPath: isNteImporter(importer) ? linkedModFolderPath : null,
                gameInstallPath: resolvedGameInstallPath,
                gameExecutablePath: resolvedGameExecutablePath,
                nteLauncherPath: null,
            });
        } catch (err) {
            this.logAddGameFailure(err, {
                game,
                modFolderPath,
                importer,
                linkedModFolderPath,
                gameInstallPath: resolvedGameInstallPath,
                gameExecutablePath: resolvedGameExecutablePath,
                nteBootstrapExecutablePath,
                step: addGameStep,
                rollbackCount: diskRollbacks.length,
            });
            await this.rollbackNteDiskChanges(diskRollbacks);
            throw err;
        }
    }

    public async updateGame(
        game: string,
        updates: {
            modFolderPath: string;
            importer: string | null;
            linkedModFolderPath: string | null;
            gameInstallPath: string | null;
            gameExecutablePath: string | null;
        },
    ) {
        if (!game || !updates.modFolderPath) {
            throw new Error("Game and modFolderPath are required");
        }

        const existingGame = await this.desktop.lib.db.gamePaths.getByGame(game);

        if (!existingGame) {
            throw new Error(`Game ${game} not found`);
        }

        const duplicatePath =
            (await this.desktop.lib.db.gamePaths.findByModFolderPathOtherGame(
                game,
                updates.modFolderPath,
            )) ??
            (updates.linkedModFolderPath
                ? await this.desktop.lib.db.gamePaths.findByModFolderPathOtherGame(
                      game,
                      updates.linkedModFolderPath,
                  )
                : null);

        if (duplicatePath) {
            throw new Error("DUPLICATE_MOD_FOLDER_PATH");
        }

        const shouldCleanupExisting =
            (isNteImporter(existingGame.importer) && !isNteImporter(updates.importer)) ||
            (isNteImporter(existingGame.importer) &&
                isNteImporter(updates.importer) &&
                hasNtePathChanges(existingGame, updates));
        const diskRollbacks: Array<() => Promise<void>> = [];

        try {
            if (shouldCleanupExisting) {
                await cleanupNteModFolder(
                    existingGame.modFolderPath,
                    existingGame.linkedModFolderPath,
                );
                if (isNteImporter(existingGame.importer)) {
                    diskRollbacks.push(() =>
                        configureNteModFolder(
                            existingGame.modFolderPath,
                            existingGame.linkedModFolderPath,
                        ),
                    );
                }
            }

            if (isNteImporter(updates.importer)) {
                await configureNteModFolder(updates.modFolderPath, updates.linkedModFolderPath);
                diskRollbacks.push(() =>
                    cleanupNteModFolder(updates.modFolderPath, updates.linkedModFolderPath),
                );
            }

            await this.desktop.lib.db.gamePaths.update(game, {
                modFolderPath: updates.modFolderPath,
                importer: updates.importer,
                linkedModFolderPath: isNteImporter(updates.importer)
                    ? updates.linkedModFolderPath
                    : null,
                gameInstallPath: isNteImporter(updates.importer) ? updates.gameInstallPath : null,
                gameExecutablePath: isNteImporter(updates.importer)
                    ? updates.gameExecutablePath
                    : null,
            });
        } catch (err) {
            await this.rollbackNteDiskChanges(diskRollbacks);
            throw err;
        }
    }

    public async resolveNteInstallPath(installPath: string) {
        return await resolveNteInstallPath(installPath);
    }

    public async setNteLauncherPath(game: string, launcherPath: string) {
        const trimmedPath = launcherPath.trim();
        const launcherStat = trimmedPath ? await fse.stat(trimmedPath).catch(() => null) : null;
        if (!launcherStat?.isFile()) {
            throw new Error("INVALID_LAUNCHER_PATH");
        }

        const existingGame = await this.desktop.lib.db.gamePaths.getByGame(game);
        if (!existingGame) {
            throw new Error(`Game ${game} not found`);
        }

        await this.desktop.lib.db.gamePaths.setNteLauncherPath(game, trimmedPath);
    }

    public async startNteLauncher(game: string) {
        const existingGame = await this.desktop.lib.db.gamePaths.getByGame(game);
        if (!existingGame?.nteLauncherPath) {
            throw new Error("NTE_LAUNCHER_PATH_NOT_SET");
        }

        const launcherPath = existingGame.nteLauncherPath;
        const launcherStat = await fse.stat(launcherPath).catch(() => null);
        if (!launcherStat?.isFile()) {
            throw new Error("NTE_LAUNCHER_PATH_NOT_FOUND");
        }

        const child = spawn(launcherPath, [], {
            cwd: path.dirname(launcherPath),
            detached: true,
            stdio: "ignore",
        });
        child.once("error", (error) => {
            this.desktop.logger.error(error, `Mod:startNteLauncher:${game}`);
        });
        child.unref();
    }

    public async removeGame(game: string) {
        const existingGame = await this.desktop.lib.db.gamePaths.getByGame(game);

        if (existingGame && isNteImporter(existingGame.importer)) {
            await cleanupNteModFolder(existingGame.modFolderPath, existingGame.linkedModFolderPath);
        }

        await this.desktop.lib.db.gamePaths.delete(game);
    }

    public async reorderGames(games: string[]) {
        const existingGames = await this.games();
        const existingGameNames = new Set(existingGames.map((game) => game.game));

        if (games.length !== existingGames.length) {
            throw new Error("INVALID_GAME_ORDER");
        }

        if (games.some((game) => !existingGameNames.has(game))) {
            throw new Error("INVALID_GAME_ORDER");
        }

        await this.desktop.lib.db.gamePaths.reorder(games);
    }

    public async setLastGame(game: string) {
        await this.desktop.lib.db.settings.upsert("last_game", game);
    }

    public async setExpandedGroups(paths: string[]) {
        await this.desktop.lib.db.settings.upsert("expanded_groups", JSON.stringify(paths));
    }

    public async setManualSubGroup(modPath: string, enabled: boolean) {
        const game = await this.getGameByPath(modPath);
        if (!game) {
            throw new Error("INVALID_MANUAL_SUBGROUP_PATH");
        }

        const relativePath = manualSubGroupRelativePath(path.relative(game.modFolderPath, modPath));
        if (!relativePath) {
            throw new Error("INVALID_MANUAL_SUBGROUP_PATH");
        }

        const task = this.manualSubGroupWriteLock.then(async () => {
            const manualSubGroups = await this.getManualSubGroupsSetting();
            const current = new Set(manualSubGroups[game.game] ?? []);

            if (enabled) current.add(relativePath);
            else current.delete(relativePath);

            const next = {
                ...manualSubGroups,
                [game.game]: [...current].sort((a, b) => a.localeCompare(b)),
            };

            if (next[game.game].length === 0) delete next[game.game];

            await this.desktop.lib.db.settings.upsert(
                MANUAL_SUBGROUPS_SETTING_KEY,
                JSON.stringify(next),
            );
        });

        this.manualSubGroupWriteLock = task.catch(() => undefined);
        await task;
    }

    private async getGameByPath(targetPath: string) {
        const resolvedTargetPath = path.resolve(targetPath);
        const nteGame = findNteGameByPath(await this.games(), resolvedTargetPath);
        if (nteGame) return nteGame;

        const matches = (await this.games()).filter((game) => {
            const relativePath = path.relative(
                path.resolve(game.modFolderPath),
                resolvedTargetPath,
            );
            return (
                relativePath === "" ||
                (!relativePath.startsWith("..") && !path.isAbsolute(relativePath))
            );
        });
        return matches.sort(
            (a, b) => path.resolve(b.modFolderPath).length - path.resolve(a.modFolderPath).length,
        )[0];
    }

    private async getManualSubGroupsSetting(): Promise<ManualSubGroups> {
        const value = await this.desktop.lib.db.settings.getValue(MANUAL_SUBGROUPS_SETTING_KEY);
        if (!value) return {};

        try {
            const parsed = JSON.parse(value) as ManualSubGroups;
            return Object.fromEntries(
                Object.entries(parsed)
                    .filter((entry): entry is [string, string[]] => Array.isArray(entry[1]))
                    .map(([game, paths]) => [
                        game,
                        paths.map(manualSubGroupRelativePath).filter(Boolean),
                    ]),
            );
        } catch {
            return {};
        }
    }

    private manualSubGroupFs() {
        return {
            pathExists: (targetPath: string) => fse.pathExists(targetPath),
            readDirectory: (targetPath: string) => fse.readdir(targetPath),
            statPath: (targetPath: string) => fse.stat(targetPath).catch(() => null),
        };
    }

    private async countManualChildPathsInModCount(game: string, groupRelativePath: string) {
        const manualChildPaths = await this.getManualChildPaths(game, groupRelativePath);
        if (manualChildPaths.size === 0) return 0;

        const modFolderPath = await this.gamePath(game);
        if (!modFolderPath) return 0;

        const fs = this.manualSubGroupFs();
        const counts = await Promise.all(
            [...manualChildPaths].map(async (manualPath) => {
                const diskPaths = await resolveManualSubGroupDiskPaths(
                    modFolderPath,
                    manualPath,
                    fs,
                );
                return (
                    await Promise.all(diskPaths.map((diskPath) => folderHasAnyFile(diskPath, fs)))
                ).some(Boolean)
                    ? 1
                    : 0;
            }),
        );

        return counts.reduce<number>((total, count) => total + count, 0);
    }

    private async getManualChildPaths(game: string, groupRelativePath: string) {
        const normalizedGroupPath = manualSubGroupRelativePath(groupRelativePath);
        const groupPrefix = normalizedGroupPath ? `${normalizedGroupPath}/` : "";
        const manualSubGroups = await this.getManualSubGroupsSetting();
        const modFolderPath = await this.gamePath(game);

        const candidatePaths = (manualSubGroups[game] ?? []).filter((manualPath) => {
            if (!manualPath.startsWith(groupPrefix)) return false;
            return !manualPath.slice(groupPrefix.length).includes("/");
        });

        if (!modFolderPath) return new Set<string>();

        const existingPaths = await Promise.all(
            candidatePaths.map(async (manualPath) => {
                if (
                    await manualSubGroupPathExists(
                        modFolderPath,
                        manualPath,
                        (targetPath) => fse.pathExists(targetPath),
                        (targetPath) => fse.readdir(targetPath),
                        (targetPath) => fse.stat(targetPath).catch(() => null),
                    )
                ) {
                    return manualPath;
                }
                return null;
            }),
        );

        return new Set(
            existingPaths.filter((manualPath): manualPath is string => manualPath !== null),
        );
    }

    private async hasManualChildren(game: string, groupRelativePath: string) {
        return (await this.getManualChildPaths(game, groupRelativePath)).size > 0;
    }

    private async addManualSubGroupFlags(
        game: string,
        parentRelativePath: string,
        groups: FolderGroup[],
    ) {
        if (!game) return groups;

        const manualChildPaths = await this.getManualChildPaths(game, parentRelativePath);

        return await Promise.all(
            groups.map(async (group) => {
                const groupRelativePath = manualSubGroupRelativePath(
                    path.join(parentRelativePath, path.basename(group.path)),
                );

                const ownManualChildPaths = await this.getManualChildPaths(game, groupRelativePath);

                const manualChildPathsInModCount = await this.countManualChildPathsInModCount(
                    game,
                    groupRelativePath,
                );

                return {
                    ...group,
                    isManualSubGroup: manualChildPaths.has(groupRelativePath),
                    hasManualSubGroups: ownManualChildPaths.size > 0,
                    modCount: Math.max(
                        0,
                        (group.modCount ?? group.mods.length) - manualChildPathsInModCount,
                    ),
                };
            }),
        );
    }

    private async filterManualSubGroupMods(
        game: string,
        groupRelativePath: string,
        mods: FolderGroup["mods"],
    ) {
        const manualChildPaths = await this.getManualChildPaths(game, groupRelativePath);
        if (manualChildPaths.size === 0) return mods;

        const normalizedGroupPath = manualSubGroupRelativePath(groupRelativePath);
        return mods.filter((mod) => {
            const fullRelativePath = manualSubGroupRelativePath(
                normalizedGroupPath
                    ? path.join(normalizedGroupPath, path.basename(mod.path))
                    : path.basename(mod.path),
            );
            return !manualChildPaths.has(fullRelativePath);
        });
    }

    private async resolveNteBootstrapExecutablePath(
        game: Pick<GamePathRow, "modFolderPath" | "linkedModFolderPath" | "gameInstallPath">,
    ) {
        const resolution = await resolveNteInstallPath(
            game.gameInstallPath ??
                deriveNteGameInstallPath(game.linkedModFolderPath ?? game.modFolderPath),
        );
        if (!resolution) {
            throw new Error("NTE_EXECUTABLE_PATH_NOT_FOUND");
        }
        return resolution.executablePath;
    }

    private async rollbackNteDiskChanges(rollbacks: Array<() => Promise<void>>) {
        for (const rollback of rollbacks.toReversed()) {
            await rollback().catch((error) => {
                this.desktop.logger.error(error, "Mod:rollbackNteDiskChanges");
            });
        }
    }

    private logAddGameFailure(
        error: unknown,
        context: {
            game: string;
            modFolderPath: string;
            importer: string | null;
            linkedModFolderPath: string | null;
            gameInstallPath: string | null;
            gameExecutablePath: string | null;
            nteBootstrapExecutablePath: string | null;
            step: string;
            rollbackCount: number;
        },
    ) {
        this.desktop.logger.error(error, `Mod:addGame:${context.game}`);
        this.desktop.logger.error(
            {
                ...context,
                isNte: isNteImporter(context.importer),
                error:
                    error instanceof Error
                        ? {
                              name: error.name,
                              message: error.message,
                              stack: error.stack,
                              code: "code" in error ? error.code : undefined,
                          }
                        : { message: String(error) },
            },
            `Mod:addGame:${context.game}:context`,
        );
    }
}
