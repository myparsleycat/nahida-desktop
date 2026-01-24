import { ipcMain, dialog } from "electron";
import { NahidaDesktop } from "@main/index";

export function registerModHandlers(desktop: NahidaDesktop) {
    ipcMain.handle("mod:selectFolder", async (_event, game: string) => {
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

    ipcMain.handle("mod:getGamePath", async (_event, game: string) => {
        return await desktop.service.mod.get.gamePath(game);
    });

    ipcMain.handle("mod:getGames", async () => {
        return await desktop.service.mod.get.games();
    });

    ipcMain.handle("mod:addGame", async (_event, game: string, path: string) => {
        return await desktop.service.mod.fn.addGame(game, path);
    });

    ipcMain.handle("mod:removeGame", async (_event, game: string) => {
        return await desktop.service.mod.fn.removeGame(game);
    });

    ipcMain.handle("mod:pickFolder", async () => {
        const result = await dialog.showOpenDialog({
            properties: ["openDirectory"],
        });

        if (result.canceled || result.filePaths.length === 0) {
            return null;
        }

        return result.filePaths[0];
    });

    ipcMain.handle("mod:getCharacters", async (_event, game: string) => {
        return await desktop.service.mod.get.characters(game);
    });

    ipcMain.handle("mod:getMods", async (_event, groupPath: string) => {
        return await desktop.service.mod.get.mods(groupPath);
    });

    ipcMain.handle("mod:toggle", async (_event, modPath: string) => {
        return await desktop.service.mod.fn.toggle(modPath);
    });

    ipcMain.handle("mod:enableAll", async (_event, groupPath: string) => {
        return await desktop.service.mod.fn.enableAll(groupPath);
    });

    ipcMain.handle("mod:disableAll", async (_event, groupPath: string) => {
        return await desktop.service.mod.fn.disableAll(groupPath);
    });

    ipcMain.handle(
        "mod:updateToggleKey",
        async (
            _event,
            modPath: string,
            iniFileName: string,
            sectionName: string,
            variable: string,
            value: string,
        ) => {
            const iniPath = require("path").join(modPath, iniFileName);
            return await desktop.service.mod.fn.updateToggleKey(
                iniPath,
                sectionName,
                variable,
                value,
            );
        },
    );

    ipcMain.handle("mod:getPresets", async (_event, game: string) => {
        return await desktop.service.mod.get.presets(game);
    });

    ipcMain.handle("mod:createPreset", async (_event, game: string, name: string) => {
        return await desktop.service.mod.fn.createPreset(game, name);
    });

    ipcMain.handle("mod:applyPreset", async (_event, presetId: string) => {
        return await desktop.service.mod.fn.applyPreset(presetId);
    });

    ipcMain.handle("mod:deletePreset", async (_event, presetId: string) => {
        return await desktop.service.mod.fn.deletePreset(presetId);
    });

    ipcMain.handle("mod:updatePresetName", async (_event, presetId: string, newName: string) => {
        return await desktop.service.mod.fn.updatePresetName(presetId, newName);
    });

    ipcMain.handle("mod:getLastGame", async () => {
        return await desktop.service.mod.get.lastGame();
    });

    ipcMain.handle("mod:setLastGame", async (_event, game: string) => {
        return await desktop.service.mod.fn.setLastGame(game);
    });

    ipcMain.handle("mod:extractArchive", async (_event, archivePath: string, groupPath: string) => {
        return await desktop.service.mod.fn.extractArchiveToGroup(archivePath, groupPath);
    });

    ipcMain.handle("mod:copyFolder", async (_event, folderPath: string, groupPath: string) => {
        const moveInsteadOfCopy = await desktop.setting.mod.getMoveFolderInsteadOfCopy();
        return await desktop.service.mod.fn.copyFolderToGroup(
            folderPath,
            groupPath,
            moveInsteadOfCopy,
        );
    });

    ipcMain.handle(
        "mod:pastePreview",
        async (_event, modPath: string, data: string, type: "url" | "base64" | "path") => {
            return await desktop.service.mod.fn.pastePreview(modPath, data, type);
        },
    );

    ipcMain.handle("mod:watchGame", async (_event, game: string) => {
        return await desktop.service.mod.watchGame(game);
    });

    ipcMain.handle("mod:watchCharacter", async (_event, characterPath: string) => {
        return await desktop.service.mod.watchCharacter(characterPath);
    });
}
