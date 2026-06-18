import path from "node:path";
import type { NahidaDesktop } from "@main/index";
import { rh } from "@main/ipc/helper";
import { dialog } from "electron";

export function registerModHandlers(desktop: NahidaDesktop) {
    rh("mod:selectFolder", async (game: string) => {
        const result = await dialog.showOpenDialog({
            properties: ["openDirectory"],
            title: `Select ${game} Mod Folder`,
        });

        if (result.canceled || result.filePaths.length === 0) {
            return null;
        }

        const folderPath = result.filePaths[0];
        await desktop.service.mod.fn.setGamePath(game, folderPath);
        return folderPath;
    });

    rh("mod:getGamePath", async (game: string) => {
        return await desktop.service.mod.get.gamePath(game);
    });

    rh("mod:getGames", async () => {
        return await desktop.service.mod.get.games();
    });

    rh(
        "mod:addGame",
        async (
            game: string,
            path: string,
            importer: string | null,
            linkedModFolderPath: string | null = null,
            gameInstallPath: string | null = null,
            gameExecutablePath: string | null = null,
        ) => {
            return await desktop.service.mod.fn.addGame(
                game,
                path,
                importer,
                linkedModFolderPath,
                gameInstallPath,
                gameExecutablePath,
            );
        },
    );

    rh("mod:removeGame", async (game: string) => {
        return await desktop.service.mod.fn.removeGame(game);
    });

    rh(
        "mod:updateGame",
        async (
            game: string,
            updates: {
                modFolderPath: string;
                importer: string | null;
                linkedModFolderPath: string | null;
                gameInstallPath: string | null;
            },
        ) => {
            return await desktop.service.mod.fn.updateGame(game, updates);
        },
    );

    rh("mod:resolveNteInstallPath", async (installPath: string) => {
        return await desktop.service.mod.get.resolveNteInstallPath(installPath);
    });

    rh("mod:pickExecutable", async () => {
        const result = await dialog.showOpenDialog({
            properties: ["openFile"],
            filters: [{ name: "Launcher", extensions: ["exe"] }],
        });

        if (result.canceled || result.filePaths.length === 0) {
            return null;
        }

        return result.filePaths[0];
    });

    rh("mod:setGameExecutablePath", async (game: string, executablePath: string) => {
        return await desktop.service.mod.fn.setGameExecutablePath(game, executablePath);
    });

    rh("mod:startNteGame", async (game: string) => {
        return await desktop.service.mod.fn.startNteGame(game);
    });

    rh("mod:reorderGames", async (games: string[]) => {
        return await desktop.service.mod.fn.reorderGames(games);
    });

    rh("mod:pickFolder", async () => {
        const result = await dialog.showOpenDialog({
            properties: ["openDirectory"],
        });

        if (result.canceled || result.filePaths.length === 0) {
            return null;
        }

        return result.filePaths[0];
    });

    rh("mod:getCharacters", async (game: string, searchModPreview?: boolean) => {
        return await desktop.service.mod.get.characters(game, searchModPreview);
    });

    rh("mod:resolveDownloadTarget", async (input: string, gameFilter?: string) => {
        return await desktop.service.mod.get.resolveDownloadTarget(input, gameFilter);
    });

    rh("mod:getSubGroups", async (folderPath: string, searchModPreview?: boolean) => {
        return await desktop.service.mod.get.subGroups(folderPath, searchModPreview);
    });

    rh("mod:getManualSubGroups", async (folderPath: string, searchModPreview?: boolean) => {
        return await desktop.service.mod.get.manualSubGroups(folderPath, searchModPreview);
    });

    rh("mod:getMods", async (groupPath: string) => {
        return await desktop.service.mod.get.mods(groupPath);
    });

    rh("mod:toggle", async (modPath: string) => {
        return await desktop.service.mod.fn.toggle(modPath);
    });

    rh("mod:exclusiveToggle", async (modPath: string) => {
        return await desktop.service.mod.fn.exclusiveToggle(modPath);
    });

    rh("mod:rename", async (modPath: string, newName: string) => {
        return await desktop.service.mod.fn.rename(modPath, newName);
    });

    rh("mod:enableAll", async (groupPath: string) => {
        return await desktop.service.mod.fn.enableAll(groupPath);
    });

    rh("mod:disableAll", async (groupPath: string) => {
        return await desktop.service.mod.fn.disableAll(groupPath);
    });

    rh("mod:downloadFromUrl", async (url: string, groupPath: string) => {
        return await desktop.lib.customDownloader.downloadToGroup(url, groupPath);
    });

    rh(
        "mod:downloadGameBananaFile",
        async (props: { itemId: number; fileId: number; modelName?: string }) => {
            return await desktop.lib.customDownloader.GBDownloader(props);
        },
    );

    rh("mod:resolveDownloadArchiveExtractPrompt", async (requestId: string, mode) => {
        desktop.lib.customDownloader.resolveArchiveExtractPrompt(requestId, mode);
    });

    rh(
        "mod:updateToggleKey",
        async (
            modPath: string,
            iniFileName: string,
            sectionName: string,
            variable: string,
            value: string,
        ) => {
            let iniPath = iniFileName;
            if (!path.isAbsolute(iniFileName)) {
                iniPath = path.join(modPath, iniFileName);
            }
            return await desktop.service.mod.fn.updateToggleKey(
                iniPath,
                sectionName,
                variable,
                value,
            );
        },
    );

    rh("mod:getPresets", async (game: string) => {
        return await desktop.service.mod.get.presets(game);
    });

    rh("mod:getPresetCreateConflicts", async (game: string) => {
        return await desktop.service.mod.get.presetCreateConflicts(game);
    });

    rh(
        "mod:createPreset",
        async (game: string, name: string, description?: string, resolveConflicts?: boolean) => {
            return await desktop.service.mod.fn.createPreset(
                game,
                name,
                description,
                resolveConflicts,
            );
        },
    );

    rh("mod:applyPreset", async (presetId: string) => {
        return await desktop.service.mod.fn.applyPreset(presetId);
    });

    rh("mod:deletePreset", async (presetId: string) => {
        return await desktop.service.mod.fn.deletePreset(presetId);
    });

    rh("mod:updatePresetName", async (presetId: string, newName: string) => {
        return await desktop.service.mod.fn.updatePresetName(presetId, newName);
    });

    rh("mod:getLastGame", async () => {
        return await desktop.service.mod.get.lastGame();
    });

    rh("mod:getPreviousFocusedGame", async () => {
        return await desktop.service.mod.get.previousFocusedGame();
    });

    rh("mod:setLastGame", async (game: string) => {
        return await desktop.service.mod.fn.setLastGame(game);
    });

    rh("mod:getExpandedGroups", async () => {
        return await desktop.service.mod.get.expandedGroups();
    });

    rh("mod:setExpandedGroups", async (paths: string[]) => {
        return await desktop.service.mod.fn.setExpandedGroups(paths);
    });

    rh("mod:setManualSubGroup", async (modPath: string, enabled: boolean) => {
        return await desktop.service.mod.fn.setManualSubGroup(modPath, enabled);
    });

    rh("mod:extractArchive", async (archivePath: string, groupPath: string, mode) => {
        return await desktop.service.mod.fn.extractArchiveToGroup(archivePath, groupPath, mode);
    });

    rh("mod:hasSingleTopLevelDirectory", async (archivePath: string) => {
        return await desktop.service.archive.hasSingleTopLevelDirectory(archivePath);
    });

    rh("mod:copyFolder", async (folderPath: string, groupPath: string) => {
        const moveInsteadOfCopy = await desktop.setting.mod.getMoveFolderInsteadOfCopy();
        return await desktop.service.mod.fn.copyFolderToGroup(
            folderPath,
            groupPath,
            moveInsteadOfCopy,
        );
    });

    rh(
        "mod:pastePreview",
        async (
            modPath: string,
            data: string,
            type: "url" | "base64" | "path",
            existingPreviewPath?: string,
        ) => {
            return await desktop.service.mod.fn.pastePreview(
                modPath,
                data,
                type,
                existingPreviewPath,
            );
        },
    );

    rh("mod:watchGame", async (game: string) => {
        return await desktop.service.mod.watchGame(game);
    });

    rh("mod:watchCharacter", async (characterPath: string) => {
        return await desktop.service.mod.watchCharacter(characterPath);
    });
}
