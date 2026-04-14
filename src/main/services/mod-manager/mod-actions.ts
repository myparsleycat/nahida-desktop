import path from "node:path";
import { trim } from "es-toolkit";
import fg from "fast-glob";
import type { NahidaDesktop } from "../..";
import {
    DISABLED_PREFIX_REGEX,
    normalizeModPath,
    renameWithUniqueName,
    restoreDisabledPrefix,
    stripDisabledPrefix,
} from "./path-utils";
import type { ShaderFixesProcessedFile } from "./shader-fixes";
import { ModShaderFixesService } from "./shader-fixes";

export class ModActionsService {
    constructor(
        private readonly desktop: NahidaDesktop,
        private readonly shaderFixes: ModShaderFixesService,
    ) {}

    public async enable(modPath: string): Promise<string> {
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
        const folderName = path.basename(modPath);

        if (!DISABLED_PREFIX_REGEX.test(folderName)) {
            const baseFolderName = `DISABLED ${folderName}`;
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
        const folderName = path.basename(modPath);
        const isEnabled = !DISABLED_PREFIX_REGEX.test(folderName);

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
                        await this.disable(currentModPath);
                    }
                } catch (error) {
                    this.desktop.logger.error(
                        error,
                        `Mod:exclusiveToggle:disable:${currentModPath}`,
                    );
                }
            });

            await Promise.all(disablePromises);
            return await this.enable(modPath);
        } else {
            return await this.disable(modPath);
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
}
