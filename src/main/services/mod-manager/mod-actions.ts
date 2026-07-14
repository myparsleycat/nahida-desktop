import path from "node:path";

import { disabledPrefixString, isNteImporter } from "@shared/mod";
import type { GameConfig } from "@shared/types";
import { retry, trim } from "es-toolkit";
import fg from "fast-glob";

import type { NahidaDesktop } from "../..";
import type { ShaderFixesProcessedFile } from "./shader-fixes";

import {
    findNteGameByPath,
    getNteGroupRelativePath,
    getNteRoots,
    hasNteDirectPak,
    isNteModEnabled,
    setNteModEnabled,
} from "./nte";
import {
    DISABLED_PREFIX_REGEX,
    normalizeModPath,
    renameWithUniqueName,
    restoreDisabledPrefix,
    stripDisabledPrefix,
} from "./path-utils";
import { ModShaderFixesService } from "./shader-fixes";

export class ModActionsService {
    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly shaderFixes: ModShaderFixesService,
    ) {}

    public async enable(modPath: string): Promise<string> {
        const nteGame = findNteGameByPath(await this.desktop.service.mod.get.games(), modPath);
        if (nteGame) {
            if (!(await isNteModEnabled(modPath)))
                return await setNteModEnabled(this.desktop, modPath, true);
            return modPath;
        }

        const folderName = path.basename(modPath);

        if (DISABLED_PREFIX_REGEX.test(folderName)) {
            const baseFolderName = trim(folderName.replace(DISABLED_PREFIX_REGEX, ""));
            let processedShaders: ShaderFixesProcessedFile[] = [];
            const copyShaderFixes = await this.desktop.setting.mod.getCopyShaderFixesOnEnable();
            try {
                if (copyShaderFixes) {
                    processedShaders = await this.shaderFixes.handleShaders(modPath, true);
                }
                return await renameWithUniqueName(this.desktop.lib.fs, modPath, baseFolderName);
            } catch (err) {
                processedShaders =
                    (err as { processedFiles?: ShaderFixesProcessedFile[] }).processedFiles ??
                    processedShaders;
                if (copyShaderFixes) {
                    try {
                        await this.shaderFixes.rollbackEnabledShaders(modPath, processedShaders);
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
    }

    public async disable(modPath: string): Promise<string> {
        const nteGame = findNteGameByPath(await this.desktop.service.mod.get.games(), modPath);
        if (nteGame) {
            if (await isNteModEnabled(modPath))
                return await setNteModEnabled(this.desktop, modPath, false);
            return modPath;
        }

        const folderName = path.basename(modPath);

        if (!DISABLED_PREFIX_REGEX.test(folderName)) {
            const style = await this.desktop.setting.mod.getDisabledPrefixStyle();
            const baseFolderName = `${disabledPrefixString(style)}${folderName}`;
            try {
                await this.shaderFixes.handleShaders(modPath, false);
                return await renameWithUniqueName(this.desktop.lib.fs, modPath, baseFolderName);
            } catch (err) {
                try {
                    await this.shaderFixes.handleShaders(modPath, true);
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
    }

    public async toggle(modPath: string): Promise<string> {
        const nteGame = findNteGameByPath(await this.desktop.service.mod.get.games(), modPath);
        const isEnabled =
            nteGame && isNteImporter(nteGame.importer)
                ? await isNteModEnabled(modPath)
                : !DISABLED_PREFIX_REGEX.test(path.basename(modPath));

        let result: string;

        try {
            if (isEnabled) {
                result = await this.disable(modPath);
            } else {
                result = await this.enable(modPath);
            }
        } catch (err) {
            await this.throwLockedFolderError(err, modPath);
            throw err;
        }

        return result;
    }

    public async exclusiveToggle(modPath: string): Promise<string> {
        const folderName = path.basename(modPath);
        const isEnabled = !DISABLED_PREFIX_REGEX.test(folderName);

        try {
            const nteGame = findNteGameByPath(await this.desktop.service.mod.get.games(), modPath);
            if (nteGame && isNteImporter(nteGame.importer)) {
                return await this.retryExclusiveToggleOperation(
                    () => this.exclusiveToggleNte(modPath, nteGame),
                    modPath,
                );
            }

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
                        const isOtherEnabled = !DISABLED_PREFIX_REGEX.test(modFolderName);
                        if (isOtherEnabled) {
                            await this.retryExclusiveToggleOperation(
                                () => this.disable(currentModPath),
                                currentModPath,
                            );
                        }
                    } catch (error) {
                        this.desktop.logger.error(
                            error,
                            `Mod:exclusiveToggle:disable:${currentModPath}`,
                        );
                    }
                });

                await Promise.all(disablePromises);
                return await this.retryExclusiveToggleOperation(
                    () => this.enable(modPath),
                    modPath,
                );
            }

            return await this.retryExclusiveToggleOperation(() => this.disable(modPath), modPath);
        } catch (err) {
            await this.throwLockedFolderError(err, modPath);
            throw err;
        }
    }

    public async rename(modPath: string, newName: string): Promise<string> {
        const folderName = path.basename(modPath);
        const trimmedName = stripDisabledPrefix(newName);

        if (!trimmedName) {
            throw new Error("INVALID_MOD_NAME");
        }

        this.desktop.lib.fs.assertValidWindowsFilename(trimmedName);

        const nextFolderName = restoreDisabledPrefix(folderName, trimmedName);
        if (folderName === nextFolderName) {
            return modPath;
        }

        const parentPath = path.dirname(modPath);
        const nextPath = path.join(parentPath, nextFolderName);

        if (normalizeModPath(modPath) !== normalizeModPath(nextPath)) {
            const exists = await this.desktop.lib.fs.pathExists(nextPath);
            if (exists) {
                throw new Error(`ALREADY_EXISTS:${nextFolderName}`);
            }
        }

        try {
            await this.desktop.lib.fs.rename(modPath, nextPath);
            return nextPath;
        } catch (err) {
            await this.throwLockedFolderError(err, modPath);
            throw err;
        }
    }

    public async enableAll(groupPath: string): Promise<void> {
        const nteGame = findNteGameByPath(await this.desktop.service.mod.get.games(), groupPath);
        if (nteGame) {
            await this.setAllNte(groupPath, nteGame, true);
            return;
        }

        try {
            const modFolders = await fg("*", {
                cwd: groupPath,
                onlyDirectories: true,
            });

            const enablePromises = modFolders.map(async (modFolderName) => {
                const modPath = path.join(groupPath, modFolderName);
                try {
                    await this.enable(modPath);
                } catch (error) {
                    this.desktop.logger.error(error, `Mod:enableAll:${modPath}`);
                }
            });

            await Promise.all(enablePromises);
        } catch (error) {
            this.desktop.logger.error(error, `Mod:enableAll:${groupPath}`);
            throw error;
        }
    }

    public async disableAll(groupPath: string): Promise<void> {
        const nteGame = findNteGameByPath(await this.desktop.service.mod.get.games(), groupPath);
        if (nteGame) {
            await this.setAllNte(groupPath, nteGame, false);
            return;
        }

        try {
            const modFolders = await fg("*", {
                cwd: groupPath,
                onlyDirectories: true,
            });

            const disablePromises = modFolders.map(async (modFolderName) => {
                const modPath = path.join(groupPath, modFolderName);
                try {
                    await this.disable(modPath);
                } catch (error) {
                    this.desktop.logger.error(error, `Mod:disableAll:${modPath}`);
                }
            });

            await Promise.all(disablePromises);
        } catch (error) {
            this.desktop.logger.error(error, `Mod:disableAll:${groupPath}`);
            throw error;
        }
    }

    private async throwLockedFolderError(err: unknown, modPath: string): Promise<void> {
        const lockInfo = await this.desktop.lib.fs.isLockedPathError(err, modPath);
        if (!lockInfo.isLocked) return;

        if (lockInfo.processes.length > 0) {
            const processNames = lockInfo.processes.map((p) => p.name).join(", ");
            throw new Error(`MOD_FOLDER_LOCKED|${processNames}`);
        }
        throw new Error("MOD_FOLDER_LOCKED");
    }

    private async retryExclusiveToggleOperation<T>(
        operation: () => Promise<T>,
        _modPath: string,
    ): Promise<T> {
        return await retry(operation, {
            retries: 2,
            delay: (attempt) => attempt * 50,
            shouldRetry: (error) => this.isRetryableExclusiveToggleError(error),
        });
    }

    private isRetryableExclusiveToggleError(error: unknown): boolean {
        const code = (error as NodeJS.ErrnoException | undefined)?.code;
        return code === "EBUSY" || code === "EPERM" || code === "EACCES";
    }

    private async exclusiveToggleNte(modPath: string, game: GameConfig) {
        const roots = getNteRoots(game);

        if (!(await isNteModEnabled(modPath))) {
            await this.setAllNte(
                path.dirname(path.join(roots.modRoot, getNteGroupRelativePath(roots, modPath))),
                game,
                false,
            );
            return await setNteModEnabled(this.desktop, modPath, true);
        }

        return await setNteModEnabled(this.desktop, modPath, false);
    }

    private async setAllNte(groupPath: string, game: GameConfig, enabled: boolean) {
        const roots = getNteRoots(game);
        const groupDir = path.join(roots.modRoot, getNteGroupRelativePath(roots, groupPath));

        if (!(await this.desktop.lib.fs.pathExists(groupDir))) return;

        await Promise.all(
            (await this.desktop.lib.fs.listDirectories(groupDir)).map(async (folderName) => {
                const modPath = path.join(groupDir, folderName);
                try {
                    if (!(await hasNteDirectPak(modPath))) return;

                    const currentlyEnabled = await isNteModEnabled(modPath);
                    if (currentlyEnabled === enabled) return;

                    await setNteModEnabled(this.desktop, modPath, enabled);
                } catch (error) {
                    this.desktop.logger.error(error, `Mod:setAllNte:${modPath}`);
                }
            }),
        );
    }
}
